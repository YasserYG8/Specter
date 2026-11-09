//go:build linux

package ebpf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/YasserYG8/Specter/internal/source"
)

// These tests load the BPF program, so they need root and a kernel with BTF. Without them they skip. They use sh as
// the target, which writes exactly what it is told on stdin. They are quick checks of the source itself; the E2E
// suite runs the whole of Specter against it.

func requireBPF(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("loading BPF programs needs root: run with sudo")
	}
	if _, err := os.Stat("/sys/kernel/btf/vmlinux"); err != nil {
		t.Skip("the kernel has no BTF (/sys/kernel/btf/vmlinux)")
	}
}

// shell is a running sh that runs the lines it is sent.
type shell struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	pid    int
	stdout *os.File // the read end of its stdout, drained into out
	mu     sync.Mutex
}

func startShell(t *testing.T, stdout, stderr *os.File) *shell {
	t.Helper()
	cmd := exec.Command("/bin/sh")
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start sh: %v", err)
	}
	sh := &shell{t: t, cmd: cmd, stdin: stdin, pid: cmd.Process.Pid}
	t.Cleanup(func() {
		_ = stdin.Close()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
	return sh
}

func (sh *shell) run(format string, args ...any) {
	sh.t.Helper()
	if _, err := fmt.Fprintf(sh.stdin, format+"\n", args...); err != nil {
		sh.t.Fatalf("cannot send a command to sh: %v", err)
	}
}

// drainedPipe makes a pipe for a stdout or stderr of the target, drained so the target never blocks.
func drainedPipe(t *testing.T) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, r) }()
	t.Cleanup(func() { _ = w.Close(); _ = r.Close() })
	return w
}

// capture starts a source on a target and returns its chunks, collected in the background.
type capture struct {
	t      *testing.T
	src    *Source
	cancel context.CancelFunc
	mu     sync.Mutex
	chunks []source.Chunk
	done   chan struct{}
	errs   <-chan error
}

func startCapture(t *testing.T, opts Options, target source.Target) *capture {
	t.Helper()
	c := &capture{t: t, src: NewSource(opts), done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	chunks, errs := c.src.Start(ctx, target)
	c.errs = errs
	// A failure to start arrives at once on errs, and the chunk channel is closed
	select {
	case err := <-errs:
		if err != nil {
			cancel()
			t.Fatalf("the source did not start: %v", err)
		}
	case <-time.After(300 * time.Millisecond):
	}
	go func() {
		defer close(c.done)
		for ch := range chunks {
			c.mu.Lock()
			c.chunks = append(c.chunks, ch)
			c.mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		cancel()
		_ = c.src.Stop()
		select {
		case <-c.done:
		case <-time.After(5 * time.Second):
		}
	})
	return c
}

func (c *capture) snapshot() []source.Chunk {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]source.Chunk(nil), c.chunks...)
}

// waitFor polls until pred is true for the chunks so far, or fails after the timeout.
func (c *capture) waitFor(timeout time.Duration, what string, pred func([]source.Chunk) bool) []source.Chunk {
	c.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if got := c.snapshot(); pred(got) {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := c.snapshot()
	c.t.Fatalf("timed out waiting for %s; got %d chunks: %s [kernel %s] [counters: %s]", what, len(got), describe(got), kernelRelease(), c.src.debugCounters())
	return nil
}

// kernelRelease is the running kernel, which a failure on one machine and not another usually depends on.
func kernelRelease() string {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

func describe(chunks []source.Chunk) string {
	var b strings.Builder
	for i, ch := range chunks {
		if i >= 6 {
			fmt.Fprintf(&b, " ... (%d more)", len(chunks)-i)
			break
		}
		s := string(ch.Bytes)
		if len(s) > 30 {
			s = s[:30] + "..."
		}
		fmt.Fprintf(&b, " [pid %d fd %d %q]", ch.PID, ch.FD, s)
	}
	return b.String()
}

func joined(chunks []source.Chunk, fd int) string {
	var b bytes.Buffer
	for _, ch := range chunks {
		if ch.FD == fd {
			b.Write(ch.Bytes)
		}
	}
	return b.String()
}

func contains(text string) func([]source.Chunk) bool {
	return func(chunks []source.Chunk) bool { return strings.Contains(joined(chunks, 1), text) }
}

// quietFor reports whatever else arrives in idle.
func quietFor(c *capture, idle time.Duration) []source.Chunk {
	time.Sleep(idle)
	return c.snapshot()
}

func TestSource_LoadsAndCapturesALine(t *testing.T) {
	requireBPF(t)
	sh := startShell(t, drainedPipe(t), drainedPipe(t))
	c := startCapture(t, Options{}, source.Target{PID: sh.pid})

	sh.run("echo hello-ebpf")
	got := c.waitFor(10*time.Second, "the line", contains("hello-ebpf\n"))

	ch := got[0]
	if ch.FD != 1 || ch.PID != sh.pid {
		t.Errorf("the chunk is from PID %d fd %d, want PID %d fd 1", ch.PID, ch.FD, sh.pid)
	}
	if ch.TS.IsZero() || time.Since(ch.TS) > time.Minute || time.Since(ch.TS) < -time.Minute {
		t.Errorf("the capture time looks wrong: %v", ch.TS)
	}
}

func TestSource_StderrKeepsItsDescriptor(t *testing.T) {
	requireBPF(t)
	sh := startShell(t, drainedPipe(t), drainedPipe(t))
	c := startCapture(t, Options{}, source.Target{PID: sh.pid})

	sh.run("ls /nonexistent-dir-for-specter")
	got := c.waitFor(10*time.Second, "the error line of ls", func(cs []source.Chunk) bool { return strings.Contains(joined(cs, 2), "nonexistent-dir-for-specter") })
	if joined(got, 1) != "" {
		t.Errorf("nothing should be on fd 1: %q", joined(got, 1))
	}
}

func TestSource_WorkersWithSharedOutputAreShownAndPrivateOnesAreNot(t *testing.T) {
	requireBPF(t)
	sh := startShell(t, drainedPipe(t), drainedPipe(t))
	c := startCapture(t, Options{}, source.Target{PID: sh.pid})

	// echo writes into a pipe that leads to /dev/null; cat shares the target's stdout
	sh.run("echo private-secret | cat > /dev/null")
	sh.run("echo shared-line | cat")
	got := c.waitFor(10*time.Second, "the shared line", contains("shared-line\n"))
	got = quietFor(c, 300*time.Millisecond)

	if strings.Contains(joined(got, 1), "private-secret") {
		t.Errorf("a helper's private output was shown: %q", joined(got, 1))
	}
	for _, ch := range got {
		if strings.Contains(string(ch.Bytes), "shared-line") && ch.PID == sh.pid {
			t.Errorf("the line was written by cat, not by sh, but the chunk says PID %d", ch.PID)
		}
	}
}

func TestSource_WritevIsJoined(t *testing.T) {
	requireBPF(t)
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to call writev")
	}
	sh := startShell(t, drainedPipe(t), drainedPipe(t))
	c := startCapture(t, Options{}, source.Target{PID: sh.pid})

	sh.run(`python3 -c 'import os; os.writev(1, [b"ERROR ", b"db down\n"])'`)
	got := c.waitFor(15*time.Second, "the writev line", contains("ERROR db down\n"))
	if !strings.Contains(joined(got, 1), "ERROR db down\n") {
		t.Errorf("got %q", joined(got, 1))
	}
}

func TestSource_LongWritesAreCutAt64KBWithTheMarker(t *testing.T) {
	requireBPF(t)
	sh := startShell(t, drainedPipe(t), drainedPipe(t))
	c := startCapture(t, Options{}, source.Target{PID: sh.pid})

	// dd makes one write of the whole block
	sh.run("dd if=/dev/zero bs=50000 count=1 2>/dev/null | tr '\\0' a")
	got := c.waitFor(15*time.Second, "a 50 KB line", func(cs []source.Chunk) bool { return len(joined(cs, 1)) >= 50000 })
	if n := len(joined(got, 1)); n != 50000 {
		t.Errorf("a 50000 byte write arrived as %d bytes", n)
	}

	sh.run("dd if=/dev/zero bs=100000 count=1 2>/dev/null")
	want := "[specter: 34464 more bytes not captured]\n"
	got = c.waitFor(15*time.Second, "the marker", contains(want))
	text := joined(got, 1)
	if !strings.HasSuffix(text, want) {
		t.Errorf("the marker should end the stream, got the end %q", text[max(0, len(text)-60):])
	}
	// 50000 bytes of 'a', then 65536 bytes of NUL from the second write, then the marker
	if n := len(text) - 50000 - len(want); n != 65536 {
		t.Errorf("the second write kept %d bytes, want 65536", n)
	}
}

func TestSource_OwnLogFileNoticeNamesTheFileOnce(t *testing.T) {
	requireBPF(t)
	dir := t.TempDir()
	logPath := dir + "/app.log"
	sh := startShell(t, drainedPipe(t), drainedPipe(t))

	var mu sync.Mutex
	var notices []string
	src := NewSource(Options{})
	src.SetNotifier(func(m string) { mu.Lock(); notices = append(notices, m); mu.Unlock() })
	ctx, cancel := context.WithCancel(context.Background())
	chunks, _ := src.Start(ctx, source.Target{PID: sh.pid})
	go func() {
		for range chunks {
		}
	}()
	t.Cleanup(func() { cancel(); _ = src.Stop() })

	// tee opens the file by name and writes to it through its own descriptor, like an application with a log file
	// The writer stays alive for a second, like a server would: the path is read from /proc while it runs
	sh.run("(echo one; sleep 1; echo two) | tee %s > /dev/null", logPath)
	sh.run("echo done-marker")

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(notices)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(notices) != 1 {
		t.Fatalf("expected one notice, got %d: %v [kernel %s] [counters: %s]", len(notices), notices, kernelRelease(), src.debugCounters())
	}
	if want := "Target writes to its own log file " + logPath + ", which Specter does not follow."; !strings.HasPrefix(notices[0], want) {
		t.Errorf("notice = %q", notices[0])
	}
}

func openPty(t *testing.T) (slave *os.File, name string) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx: %v", err)
	}
	fd := int(master.Fd())
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	name = fmt.Sprintf("/dev/pts/%d", n)
	slave, err = os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, master) }()
	t.Cleanup(func() { _ = slave.Close(); _ = master.Close() })
	return slave, name
}

// Node.js writes its request lines through a descriptor it opened again on the terminal, not through fd 1.
func TestSource_WriteThroughAReopenedTerminalKeepsItsDescriptor(t *testing.T) {
	requireBPF(t)
	slave, name := openPty(t)
	sh := startShell(t, slave, slave)
	c := startCapture(t, Options{}, source.Target{PID: sh.pid})

	// tee opens the terminal again and writes through that descriptor, as Node.js does
	sh.run("echo reopened-line | tee %s > /dev/null", name)
	got := c.waitFor(10*time.Second, "the reopened line", func(cs []source.Chunk) bool {
		for _, ch := range cs {
			if strings.Contains(string(ch.Bytes), "reopened-line") {
				return true
			}
		}
		return false
	})
	for _, ch := range got {
		if strings.Contains(string(ch.Bytes), "reopened-line") && (ch.FD == 1 || ch.FD == 2) {
			t.Errorf("the line should keep the descriptor it was written through, not fd %d", ch.FD)
		}
	}

	// A descriptor that leads to /dev/null is not output, even though it is another descriptor
	sh.run("echo discarded | tee /dev/null > /dev/null")
	sh.run("echo end-marker | tee %s > /dev/null", name)
	got = c.waitFor(10*time.Second, "the end marker", func(cs []source.Chunk) bool {
		for _, ch := range cs {
			if strings.Contains(string(ch.Bytes), "end-marker") {
				return true
			}
		}
		return false
	})
	for _, ch := range got {
		if strings.Contains(string(ch.Bytes), "discarded") {
			t.Errorf("a write to /dev/null was shown: %q", ch.Bytes)
		}
	}
}

func TestSource_ReportsHowTheTargetEnded(t *testing.T) {
	requireBPF(t)

	t.Run("exit code", func(t *testing.T) {
		sh := startShell(t, drainedPipe(t), drainedPipe(t))
		c := startCapture(t, Options{}, source.Target{PID: sh.pid})
		sh.run("exit 3")
		select {
		case <-c.done:
		case <-time.After(10 * time.Second):
			t.Fatal("the source did not end when the target exited")
		}
		st, known := c.src.ExitStatus()
		if !known || st.Killed() || st.Code != 3 || st.PID != sh.pid {
			t.Errorf("exit status = %+v (known %v), want code 3 for PID %d", st, known, sh.pid)
		}
	})

	t.Run("killed", func(t *testing.T) {
		sh := startShell(t, drainedPipe(t), drainedPipe(t))
		c := startCapture(t, Options{}, source.Target{PID: sh.pid})
		_ = sh.cmd.Process.Kill()
		select {
		case <-c.done:
		case <-time.After(10 * time.Second):
			t.Fatal("the source did not end when the target was killed")
		}
		st, known := c.src.ExitStatus()
		if !known || !st.Killed() || st.Signal != "SIGKILL" {
			t.Errorf("exit status = %+v (known %v), want killed by SIGKILL", st, known)
		}
	})

	t.Run("a worker that exits is not the target", func(t *testing.T) {
		sh := startShell(t, drainedPipe(t), drainedPipe(t))
		c := startCapture(t, Options{}, source.Target{PID: sh.pid})
		sh.run("(exit 5)")
		sh.run("echo still-here")
		c.waitFor(10*time.Second, "the line after the worker ended", contains("still-here\n"))
		if st, known := c.src.ExitStatus(); known {
			t.Errorf("a worker's exit was reported as the target's: %+v", st)
		}
	})
}

func TestSource_TellsHowManyWritesWereLost(t *testing.T) {
	requireBPF(t)
	sh := startShell(t, drainedPipe(t), drainedPipe(t))

	var mu sync.Mutex
	var notices []string
	src := NewSource(Options{RingBytes: 4096}) // a page: a 16 KB chunk can never fit
	src.SetNotifier(func(m string) { mu.Lock(); notices = append(notices, m); mu.Unlock() })
	ctx, cancel := context.WithCancel(context.Background())
	chunks, _ := src.Start(ctx, source.Target{PID: sh.pid})
	go func() {
		for range chunks {
		}
	}()
	t.Cleanup(func() { cancel(); _ = src.Stop() })

	sh.run("dd if=/dev/zero bs=16384 count=40 2>/dev/null")

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(notices)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(notices) == 0 {
		t.Fatalf("no notice about lost writes [kernel %s] [counters: %s]", kernelRelease(), src.debugCounters())
	}
	if !strings.Contains(notices[0], "writes were lost because the target wrote faster than Specter could read.") {
		t.Errorf("notice = %q", notices[0])
	}
}

// The kernel's verifier explores every path through the program before it lets it load. A program with too many
// paths takes seconds to load, which a command that should attach at once cannot afford. This keeps the cost visible.
func TestProgramLoadsQuickly(t *testing.T) {
	requireBPF(t)

	spec, err := loadSpecter()
	if err != nil {
		t.Fatal(err)
	}
	var objs specterObjects
	start := time.Now()

	if err := spec.LoadAndAssign(&objs, nil); err != nil {
		t.Fatalf("the kernel refused the program: %v", err)
	}
	took := time.Since(start)
	defer objs.Close()

	t.Logf("loading all programs took %v", took)
	if took > 10*time.Second {
		t.Errorf("loading took %v: the verifier explores too many paths", took)
	}
}

// Direct children are always followed. Deeper descendants are followed only with --tree, up to 3 levels, as with the
// strace source.
func TestSource_TreeFollowsDeeperDescendants(t *testing.T) {
	requireBPF(t)

	for _, tc := range []struct {
		name string
		opts Options
		deep bool
	}{
		{"default", Options{}, false},
		{"with --tree", Options{FollowTree: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := startShell(t, drainedPipe(t), drainedPipe(t))
			c := startCapture(t, tc.opts, source.Target{PID: sh.pid})

			// The outer sh is a child of the target, and the inner one is its child, so it is two levels down
			sh.run(`sh -c "sh -c 'echo deep-line'; true"`)
			sh.run("echo child-end | cat")
			c.waitFor(10*time.Second, "the line of a direct child", contains("child-end\n"))
			got := quietFor(c, 300*time.Millisecond)

			if has := strings.Contains(joined(got, 1), "deep-line"); has != tc.deep {
				t.Errorf("deep line captured = %v, want %v; got %q", has, tc.deep, joined(got, 1))
			}
		})
	}
}

// The counter of lost writes is read from its own goroutine, while the session can be closed from another. Reading a
// map while its descriptor is closed is a data race, which `go test -race` reports. This keeps the two apart.
func TestSession_ReadingTheCounterWhileClosing(t *testing.T) {
	requireBPF(t)

	src := NewSource(Options{})
	sess, err := src.open(source.Target{PID: os.Getpid()})
	if err != nil {
		t.Fatalf("cannot open a session: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					sess.lostWrites()
				}
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	sess.close()
	close(stop)
	wg.Wait()

	if _, ok := sess.lostWrites(); ok {
		t.Error("a closed session should not report a counter")
	}
	sess.close() // closing twice is harmless
}

// Each stage of a write counts itself in the BPF program. The counters are what tells a failure to capture from a
// failure to read on a kernel where something behaves differently, so they have to count.
func TestSource_CountersFollowEachStageOfAWrite(t *testing.T) {
	requireBPF(t)
	sh := startShell(t, drainedPipe(t), drainedPipe(t))
	c := startCapture(t, Options{}, source.Target{PID: sh.pid})

	sh.run("echo counted-line")
	sh.run("echo shared | cat") // a child: the fork is counted too
	c.waitFor(10*time.Second, "the lines", contains("counted-line\n"))
	c.waitFor(10*time.Second, "the child's line", contains("shared\n"))

	counters := c.src.debugCounters()
	for _, name := range []string{"enter_followed", "call_stored", "exit_matched", "data_sent", "fork_by_followed", "fork_child_added"} {
		if strings.Contains(counters, name+"=0 ") || strings.HasSuffix(counters, name+"=0") || !strings.Contains(counters, name+"=") {
			t.Errorf("the counter %s did not count: %s", name, counters)
		}
	}
	for _, name := range []string{"lost", "unreadable", "ns_lookup_failed", "fork_child_ns_not_found"} {
		if !strings.Contains(counters, name+"=0") {
			t.Errorf("the counter %s should be 0 here: %s", name, counters)
		}
	}
}
