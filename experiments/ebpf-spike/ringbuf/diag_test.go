//go:build linux

package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
	"golang.org/x/sys/unix"
)

// DIAGNOSTIC. On kernel 7.0 the syscall tracepoint hook missed writes of processes that were already running when
// the program was attached. This attaches five different hooks at once and counts, for the same writes, which of
// them fired, for a process that existed before the attach and for one started after it.
func TestDiagWhichHooksSeeWrites(t *testing.T) {
	requireBPF(t)
	if err := rlimit.RemoveMemlock(); err != nil {
		t.Fatal(err)
	}

	var objs diagObjects
	if err := loadDiagObjects(&objs, nil); err != nil {
		t.Fatalf("DIAG cannot load: %v", err)
	}
	defer objs.Close()

	// The PID namespace of this process, in the encoding the kernel helper wants
	var st unix.Stat_t
	if err := unix.Stat("/proc/self/ns/pid", &st); err != nil {
		t.Fatal(err)
	}
	if err := objs.NsMap.Put(uint32(0), diagNsinfo{Dev: uint64(kernelDev(uint64(st.Dev))), Ino: st.Ino}); err != nil {
		t.Fatal(err)
	}

	nr := uint64(1) // write on amd64
	if runtime.GOARCH == "arm64" {
		nr = 64
	}
	if err := objs.NrWrite.Put(uint32(0), nr); err != nil {
		t.Fatal(err)
	}

	// A process that exists BEFORE anything is attached, like the target of Specter
	existing := startWriter(t, pipeStdout(t))
	if err := objs.Watch.Put(uint32(existing.pid), uint8(1)); err != nil {
		t.Fatal(err)
	}

	var attach []string
	var links []link.Link
	add := func(name string, l link.Link, err error) {
		if err != nil {
			attach = append(attach, name+"=FAILED("+firstLine(err)+")")
			return
		}
		attach = append(attach, name+"=ok")
		links = append(links, l)
	}
	l, err := link.Tracepoint("syscalls", "sys_enter_write", objs.M1, nil)
	add("1.tp_sys_enter_write", l, err)
	l, err = link.AttachTracing(link.TracingOptions{Program: objs.M2})
	add("2.tp_btf_sys_enter", l, err)
	l, err = link.Kprobe("vfs_write", objs.M3, nil)
	add("3.kprobe_vfs_write", l, err)
	if runtime.GOARCH == "arm64" {
		l, err = link.Kprobe("__arm64_sys_write", objs.M4a, nil)
	} else {
		l, err = link.Kprobe("__x64_sys_write", objs.M4x, nil)
	}
	add("4.kprobe_sys_write", l, err)
	l, err = link.Tracepoint("raw_syscalls", "sys_enter", objs.M5, nil)
	add("5.tp_raw_syscalls", l, err)
	defer func() {
		for _, l := range links {
			_ = l.Close()
		}
	}()

	counts := func(pid int) string {
		var parts []string
		for mech := uint32(1); mech <= 5; mech++ {
			var v uint64
			_ = objs.Hits.Lookup(diagKey{Mech: mech, Tgid: uint32(pid)}, &v)
			parts = append(parts, fmt.Sprintf("m%d=%d", mech, v))
		}
		return strings.Join(parts, " ")
	}

	// Each command makes the writer write twice: once to fd 1 and once to its reply pipe
	for i := 0; i < 5; i++ {
		existing.do("write 1 10")
	}
	a := counts(existing.pid)
	time.Sleep(3 * time.Second)
	for i := 0; i < 5; i++ {
		existing.do("write 1 10")
	}
	b := counts(existing.pid)

	// A process started AFTER the attach
	fresh := startWriter(t, pipeStdout(t))
	if err := objs.Watch.Put(uint32(fresh.pid), uint8(1)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		fresh.do("write 1 10")
	}
	c := counts(fresh.pid)

	host, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	t.Fatalf("DIAG kernel=%s arch=%s expected: 10 per 5 commands. attach: %s | EXISTING after 5 cmds: %s | EXISTING after 3s and 5 more cmds (20 expected): %s | NEW after 5 cmds: %s",
		strings.TrimSpace(string(host)), runtime.GOARCH, strings.Join(attach, " "), a, b, c)
}

func firstLine(err error) string {
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
