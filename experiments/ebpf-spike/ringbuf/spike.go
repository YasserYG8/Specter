//go:build linux

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"golang.org/x/sys/unix"
)

// headerSize is the size of struct event in bpf/spike.c.
const headerSize = 56

// event is one chunk of one write, as the kernel program sent it.
type event struct {
	Tid, Tgid, FD, Len, Seq, NChunks, LeftOut, Mode, Dev uint32
	Ino                                                  uint64
	TS                                                   uint64
	Data                                                 []byte
}

// config mirrors struct config in bpf/spike.c.
type config struct {
	NsDev    uint64
	NsIno    uint64
	MaxDepth uint32
	_        uint32
}

// kernelDev converts a device number from stat(2) to the encoding the kernel uses internally, MAJOR<<20 | MINOR.
func kernelDev(d uint64) uint32 {
	return unix.Major(d)<<20 | unix.Minor(d)
}

// session is a loaded program, attached to the write and fork tracepoints.
type session struct {
	objs   spikeObjects
	links  []link.Link
	rd     *ringbuf.Reader
	closed sync.Once
}

// newSession loads and attaches the program. Followed processes are added with follow.
func newSession(maxDepth uint32, ringBytes uint32) (*session, error) {
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("memlock: %w", err)
	}
	spec, err := loadSpike()
	if err != nil {
		return nil, err
	}
	if ringBytes != 0 {
		spec.Maps["events"].MaxEntries = ringBytes
	}
	s := &session{}
	if err := spec.LoadAndAssign(&s.objs, nil); err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			return nil, fmt.Errorf("the kernel refused the program:\n%+v", ve)
		}
		return nil, err
	}

	var st unix.Stat_t
	if err := unix.Stat("/proc/self/ns/pid", &st); err != nil {
		s.close()
		return nil, fmt.Errorf("stat the PID namespace: %w", err)
	}
	cfg := config{NsDev: uint64(kernelDev(uint64(st.Dev))), NsIno: st.Ino, MaxDepth: maxDepth}
	if err := s.objs.ConfigMap.Put(uint32(0), cfg); err != nil {
		s.close()
		return nil, fmt.Errorf("config: %w", err)
	}

	attach := func(l link.Link, err error) error {
		if err != nil {
			return err
		}
		s.links = append(s.links, l)
		return nil
	}
	for _, step := range []func() error{
		func() error {
			return attach(link.Tracepoint("syscalls", "sys_enter_write", s.objs.OnWriteEnter, nil))
		},
		func() error {
			return attach(link.Tracepoint("syscalls", "sys_exit_write", s.objs.OnWriteExit, nil))
		},
		func() error {
			return attach(link.AttachTracing(link.TracingOptions{Program: s.objs.OnFork}))
		},
	} {
		if err := step(); err != nil {
			s.close()
			return nil, fmt.Errorf("attach: %w", err)
		}
	}

	s.rd, err = ringbuf.NewReader(s.objs.Events)
	if err != nil {
		s.close()
		return nil, fmt.Errorf("ring buffer: %w", err)
	}
	return s, nil
}

// follow adds a process, by its PID in this process's own PID namespace.
func (s *session) follow(pid int, depth uint32) error {
	return s.objs.Followed.Put(uint32(pid), depth)
}

// events reads the ring buffer until the session is closed.
func (s *session) events() <-chan event {
	out := make(chan event, 65536)
	go func() {
		defer close(out)
		for {
			rec, err := s.rd.Read()
			if err != nil {
				return // closed
			}
			if ev, ok := decode(rec.RawSample); ok {
				out <- ev
			}
		}
	}()
	return out
}

func decode(raw []byte) (event, bool) {
	if len(raw) < headerSize {
		return event{}, false
	}
	var ev event
	le := binary.LittleEndian
	ev.Tid = le.Uint32(raw[0:])
	ev.Tgid = le.Uint32(raw[4:])
	ev.FD = le.Uint32(raw[8:])
	ev.Len = le.Uint32(raw[12:])
	ev.Seq = le.Uint32(raw[16:])
	ev.NChunks = le.Uint32(raw[20:])
	ev.LeftOut = le.Uint32(raw[24:])
	ev.Mode = le.Uint32(raw[28:])
	ev.Dev = le.Uint32(raw[32:])
	ev.Ino = le.Uint64(raw[40:])
	ev.TS = le.Uint64(raw[48:])
	if int(ev.Len) > len(raw)-headerSize {
		return event{}, false
	}
	ev.Data = bytes.Clone(raw[headerSize : headerSize+int(ev.Len)])
	return ev, true
}

// stat returns the per-CPU counters summed: writes lost because the ring buffer was full, and writes whose
// bytes could not be read.
func (s *session) stat(which uint32) (uint64, error) {
	var vals []uint64
	if err := s.objs.Stats.Lookup(which, &vals); err != nil {
		return 0, err
	}
	var sum uint64
	for _, v := range vals {
		sum += v
	}
	return sum, nil
}

func (s *session) close() {
	s.closed.Do(func() {
		if s.rd != nil {
			_ = s.rd.Close()
		}
		for _, l := range s.links {
			_ = l.Close()
		}
		_ = s.objs.Close()
	})
}

// exists reports whether a path exists. The tests use it to look for BTF.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
