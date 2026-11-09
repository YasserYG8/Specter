//go:build linux

package ebpf

import (
	"context"
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	cebpf "github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"golang.org/x/sys/unix"

	"github.com/YasserYG8/Specter/internal/source"
)

// lostPollInterval is how often the counter of lost writes is read, so a notice appears at most once per second.
const lostPollInterval = time.Second

// session is the loaded programs and the reader of their ring buffer.
type session struct {
	objs  specterObjects
	links []link.Link
	rd    *ringbuf.Reader
	once  sync.Once

	// mu keeps the maps from being closed while one is being read: the counter of lost writes is read from its own
	// goroutine, and a map's descriptor must not be closed at the same moment.
	mu     sync.RWMutex
	closed bool
}

// statNames are the counters of the BPF program, in the order of the ST_ indexes in bpf/specter.c.
var statNames = []string{
	"lost", "unreadable", "enter_followed", "dropped_by_rules", "call_stored", "exit_matched", "data_sent",
	"ns_lookup_failed", "fork_by_followed", "fork_child_added", "fork_child_ns_not_found", "exit_removed", "fd_unknown",
	"DIAG_enter_all", "DIAG_not_followed",
}

// counters returns the counters of the BPF program, summed over all CPUs, as one line of name=value. It is how a
// failure to capture is told from a failure to read: each stage of a write counts itself.
func (c *session) counters() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return "(session closed)"
	}
	var parts []string
	for i, name := range statNames {
		var perCPU []uint64
		if err := c.objs.Stats.Lookup(uint32(i), &perCPU); err != nil {
			return fmt.Sprintf("(cannot read counters: %v)", err)
		}
		var total uint64
		for _, v := range perCPU {
			total += v
		}
		parts = append(parts, fmt.Sprintf("%s=%d", name, total))
	}
	// The followed set and the IDs of the last write by a process that was not in it
	var followed []string
	var key uint32
	var val specterFollowedProc
	it := c.objs.Followed.Iterate()
	for it.Next(&key, &val) {
		followed = append(followed, fmt.Sprintf("%d:d%d", key, val.Depth))
	}
	var last [3]uint64
	for i := range last {
		_ = c.objs.LastOther.Lookup(uint32(i), &last[i])
	}
	head := fmt.Sprintf("followed=[%s] last_other(ns_pid=%d ns_tgid=%d host=%d/%d)", strings.Join(followed, ","), last[0], last[1], last[2]>>32, last[2]&0xffffffff)
	// The diagnostic counters come first, so that a long line cut short by an annotation still shows them
	var diag, rest []string
	for _, p := range parts {
		if strings.HasPrefix(p, "DIAG_") {
			diag = append(diag, p)
		} else {
			rest = append(rest, p)
		}
	}
	return strings.Join(append(append([]string{head}, diag...), rest...), " ")
}

// close detaches the programs and releases everything. The kernel does the same on its own when Specter exits,
// even when it is killed, because the programs, links and maps belong to descriptors that Specter holds.
func (c *session) close() {
	c.once.Do(func() {
		if c.rd != nil {
			_ = c.rd.Close() // unblocks a Read in progress
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.closed = true
		for _, l := range c.links {
			_ = l.Close()
		}
		_ = c.objs.Close()
	})
}

// lostWrites returns how many events the BPF program could not send because the ring buffer was full, summed over
// all CPUs. It reports false once the session is closed.
func (c *session) lostWrites() (uint64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return 0, false
	}
	var perCPU []uint64
	if err := c.objs.Stats.Lookup(uint32(0), &perCPU); err != nil {
		return 0, false
	}
	var total uint64
	for _, v := range perCPU {
		total += v
	}
	return total, true
}

// Start loads the programs, attaches them, and begins sending the target's output.
func (s *Source) Start(ctx context.Context, target source.Target) (<-chan source.Chunk, <-chan error) {
	chunks := make(chan source.Chunk, 1024)
	errs := make(chan error, 1)

	sess, err := s.open(target)
	if err != nil {
		errs <- err
		close(chunks)
		close(errs)
		return chunks, errs
	}
	s.mu.Lock()
	s.stop = sess.close
	s.sess = sess
	s.mu.Unlock()

	go func() {
		defer close(chunks)
		defer close(errs)

		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-ctx.Done():
				sess.close() // unblocks the reader
			case <-done:
			}
		}()
		go s.watchLost(sess, done)

		s.read(ctx, sess, target, chunks)
		sess.close()
	}()
	return chunks, errs
}

// open loads and attaches everything. On any failure it releases what it had.
func (s *Source) open(target source.Target) (*session, error) {
	if target.PID <= 0 {
		return nil, errors.New("the eBPF source needs a process: this target has no PID on this machine")
	}
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, loadFailed(fmt.Errorf("cannot raise the memory limit for eBPF: %w", err))
	}
	spec, err := loadSpecter()
	if err != nil {
		return nil, fmt.Errorf("cannot read the eBPF program: %w", err)
	}
	if s.opts.RingBytes != 0 {
		spec.Maps["events"].MaxEntries = s.opts.RingBytes
	}

	sess := &session{}
	if err := spec.LoadAndAssign(&sess.objs, nil); err != nil {
		var ve *cebpf.VerifierError
		if errors.As(err, &ve) {
			return nil, loadFailed(fmt.Errorf("the kernel refused the eBPF program: %w\n%+v", err, ve))
		}
		return nil, loadFailed(fmt.Errorf("cannot load the eBPF program: %w", err))
	}

	fail := func(err error) (*session, error) {
		sess.close()
		return nil, err
	}

	cfg, err := s.buildConfig(target)
	if err != nil {
		return fail(err)
	}
	if err := sess.objs.ConfigMap.Put(uint32(0), cfg); err != nil {
		return fail(fmt.Errorf("cannot configure the eBPF program: %w", err))
	}

	maxDepth := maxDepthDirect
	if s.opts.FollowTree {
		maxDepth = maxDepthTree
	}
	parentOf := func(pid int) (int, bool) { return procParent("/proc", pid) }
	for pid, depth := range followDepths(target.PID, target.Children, parentOf, maxDepth) {
		if err := sess.objs.Followed.Put(uint32(pid), specterFollowedProc{Depth: depth}); err != nil {
			return fail(fmt.Errorf("cannot follow PID %d: %w", pid, err))
		}
	}

	// The exit handlers go first: a write that starts before its exit handler is attached would leave a stale entry
	attach := func(l link.Link, err error) error {
		if err != nil {
			return err
		}
		sess.links = append(sess.links, l)
		return nil
	}
	steps := []struct {
		name string
		do   func() error
	}{
		{"sys_exit_write", func() error { return attach(link.Tracepoint("syscalls", "sys_exit_write", sess.objs.OnWriteExit, nil)) }},
		{"sys_exit_writev", func() error {
			return attach(link.Tracepoint("syscalls", "sys_exit_writev", sess.objs.OnWritevExit, nil))
		}},
		{"sys_enter_write", func() error {
			return attach(link.Tracepoint("syscalls", "sys_enter_write", sess.objs.OnWriteEnter, nil))
		}},
		{"sys_enter_writev", func() error {
			return attach(link.Tracepoint("syscalls", "sys_enter_writev", sess.objs.OnWritevEnter, nil))
		}},
		{"sched_process_fork", func() error { return attach(link.AttachTracing(link.TracingOptions{Program: sess.objs.OnFork})) }},
		{"sched_process_exit", func() error { return attach(link.AttachTracing(link.TracingOptions{Program: sess.objs.OnExit})) }},
	}
	for _, st := range steps {
		if err := st.do(); err != nil {
			return fail(loadFailed(fmt.Errorf("cannot attach to %s: %w", st.name, err)))
		}
	}

	sess.rd, err = ringbuf.NewReader(sess.objs.Events)
	if err != nil {
		return fail(loadFailed(fmt.Errorf("cannot read the eBPF ring buffer: %w", err)))
	}
	return sess, nil
}

// buildConfig tells the program where the target's output goes and which PID namespace is Specter's.
func (s *Source) buildConfig(target source.Target) (specterConfig, error) {
	var cfg specterConfig

	var ns unix.Stat_t
	if err := unix.Stat("/proc/self/ns/pid", &ns); err != nil {
		return cfg, fmt.Errorf("cannot read Specter's PID namespace: %w", err)
	}
	cfg.NsDev, cfg.NsIno = uint64(kernelDev(uint64(ns.Dev))), ns.Ino

	cfg.TargetTgid = uint32(target.PID)
	cfg.MaxDepth = maxDepthDirect
	if s.opts.FollowTree {
		cfg.MaxDepth = maxDepthTree
	}

	// Where the target's stdout and stderr lead. When one cannot be read the program shows every write of the
	// followed processes to fd 1 and 2, as the strace source does.
	for i, fd := range []int{1, 2} {
		if dev, ino, ok := identityOf(filepath.Join("/proc", strconv.Itoa(target.PID), "fd", strconv.Itoa(fd))); ok {
			cfg.OutDev[i], cfg.OutIno[i] = dev, ino
			cfg.OutKnown |= 1 << uint(i)
		}
	}
	if dev, ino, ok := identityOf("/dev/null"); ok {
		cfg.NullDev, cfg.NullIno, cfg.NullKnown = dev, ino, 1
	}
	return cfg, nil
}

// identityOf returns the device (in the kernel's encoding) and inode of the file a path leads to.
func identityOf(path string) (dev, ino uint64, ok bool) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return 0, 0, false
	}
	return uint64(kernelDev(uint64(st.Dev))), st.Ino, true
}

// monotonicNow is CLOCK_MONOTONIC in nanoseconds, the clock of the timestamps in the events.
func monotonicNow() int64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return 0
	}
	return ts.Nano()
}

// read turns the records of the ring buffer into chunks until the session is closed or the target ends.
func (s *Source) read(ctx context.Context, sess *session, target source.Target, chunks chan<- source.Chunk) {
	wallBase := time.Now()
	monoBase := monotonicNow()
	stamp := func(ts uint64) time.Time {
		return wallBase.Add(time.Duration(int64(ts) - monoBase))
	}
	send := func(c source.Chunk) bool {
		select {
		case chunks <- c:
			return true
		case <-ctx.Done():
			return false
		}
	}

	for {
		rec, err := sess.rd.Read()
		if err != nil {
			return // the reader was closed
		}
		ev, ok := decodeEvent(rec.RawSample)
		if !ok {
			continue
		}
		switch ev.Kind {
		case kindData:
			if !send(source.Chunk{Bytes: ev.Data, FD: int(ev.FD), PID: int(ev.Tid), TS: stamp(ev.TS)}) {
				return
			}
		case kindMarker:
			if !send(source.Chunk{Bytes: markerText(ev.Arg), FD: int(ev.FD), PID: int(ev.Tid), TS: stamp(ev.TS)}) {
				return
			}
		case kindFile:
			s.checkLogFile(int(ev.Tgid), int(ev.FD))
		case kindExit:
			status := exitStatusOf(target.PID, ev.Arg)
			s.mu.Lock()
			s.exit = &status
			s.mu.Unlock()
			return
		}
	}
}

// checkLogFile is called once per regular file that a followed process writes to through a descriptor other than 1
// and 2. When its name says it is a log, the user is told once, with the same words as for the strace source.
func (s *Source) checkLogFile(pid, fd int) {
	path, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "fd", strconv.Itoa(fd)))
	if err != nil || !strings.HasPrefix(path, "/") || !strings.Contains(strings.ToLower(pathpkg.Base(path)), "log") {
		return
	}
	s.mu.Lock()
	if s.seenLogPaths == nil {
		s.seenLogPaths = make(map[string]bool)
	}
	already := s.seenLogPaths[path]
	s.seenLogPaths[path] = true
	s.mu.Unlock()
	if already {
		return
	}
	s.notify(fmt.Sprintf("Target writes to its own log file %s, which Specter does not follow. To read it: tail -f %s", path, path))
}

// watchLost reads the program's counter of events it could not send, once per second, and tells the user how
// many were lost since the last time. It never says anything when nothing was lost.
func (s *Source) watchLost(sess *session, done <-chan struct{}) {
	ticker := time.NewTicker(lostPollInterval)
	defer ticker.Stop()
	var lost lostTracker
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
		}
		total, ok := sess.lostWrites()
		if !ok {
			return // the session is closed
		}
		if msg, ok := lost.next(total); ok {
			s.notify(msg)
		}
	}
}

// debugCounters is the counters of the running session, for tests and diagnostics. It is empty before Start.
func (s *Source) debugCounters() string {
	s.mu.Lock()
	sess := s.sess
	s.mu.Unlock()
	if sess == nil {
		return ""
	}
	return sess.counters()
}
