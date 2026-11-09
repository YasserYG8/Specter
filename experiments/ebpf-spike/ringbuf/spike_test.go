//go:build linux

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// These tests load a BPF program, so they need root and a kernel with BTF. Run them with: sudo go test -v

var writerBin string

func TestMain(m *testing.M) {
	// WRITER_BIN lets CI build the writer as a normal user and run the tests as root, which has no Go toolchain
	// set up.
	writerBin = os.Getenv("WRITER_BIN")
	dir := ""
	if writerBin == "" {
		var err error
		dir, err = os.MkdirTemp("", "writer")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		writerBin = filepath.Join(dir, "writer")
		if out, err := exec.Command("go", "build", "-buildvcs=false", "-o", writerBin, "./cmd/writer").CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "cannot build the writer: %v\n%s", err, out)
			os.Exit(1)
		}
	}
	code := m.Run()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}

func requireBPF(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("loading BPF programs needs root: run with sudo")
	}
	if !exists("/sys/kernel/btf/vmlinux") {
		t.Skip("the kernel has no BTF (/sys/kernel/btf/vmlinux)")
	}
}

func newTestSession(t *testing.T, maxDepth, ringBytes uint32) *session {
	t.Helper()
	s, err := newSession(maxDepth, ringBytes)
	if err != nil {
		t.Fatalf("cannot start the session: %v", err)
	}
	t.Cleanup(s.close)
	return s
}

// writer is a started writer process.
type writer struct {
	t     *testing.T
	cmd   *exec.Cmd
	stdin io.WriteCloser
	reply *os.File
	rd    *bufio.Reader
	pid   int
}

// startWriter starts the writer with the given stdout. fd 3 is its reply pipe, fd 4 a private pipe, fd 5 /dev/null.
func startWriter(t *testing.T, stdout *os.File) *writer {
	t.Helper()
	rr, rw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, pr) }()
	null, err := os.OpenFile("/dev/null", os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(writerBin)
	cmd.Stdout = stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{rw, pw, null}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start the writer: %v", err)
	}
	_ = rw.Close()
	_ = pw.Close()
	_ = null.Close()

	w := &writer{t: t, cmd: cmd, stdin: stdin, reply: rr, rd: bufio.NewReader(rr), pid: cmd.Process.Pid}
	t.Cleanup(func() {
		_ = stdin.Close()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
		_ = rr.Close()
	})
	return w
}

// do sends one command and waits until the writer says it is done.
func (w *writer) do(format string, args ...any) {
	w.t.Helper()
	if _, err := fmt.Fprintf(w.stdin, format+"\n", args...); err != nil {
		w.t.Fatalf("cannot send a command: %v", err)
	}
	_ = w.reply.SetReadDeadline(time.Now().Add(15 * time.Second))
	if _, err := w.rd.ReadString('\n'); err != nil {
		w.t.Fatalf("the writer did not answer %q: %v", fmt.Sprintf(format, args...), err)
	}
}

// pipeStdout makes a pipe for a writer's stdout and drains it, like a log reader would.
func pipeStdout(t *testing.T) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, r) }()
	t.Cleanup(func() { _ = w.Close(); _ = r.Close() })
	return w
}

// collect reads events until done reports true, and fails the test after timeout.
func collect(t *testing.T, ch <-chan event, timeout time.Duration, done func([]event) bool) []event {
	t.Helper()
	var evs []event
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("the event channel closed early after %d events", len(evs))
			}
			evs = append(evs, ev)
			if done(evs) {
				return evs
			}
		case <-deadline:
			t.Fatalf("timed out after %v with %d events: %s", timeout, len(evs), summarize(evs))
		}
	}
}

// quiet reads whatever else arrives within idle and returns it.
func quiet(ch <-chan event, idle time.Duration) []event {
	var evs []event
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return evs
			}
			evs = append(evs, ev)
		case <-time.After(idle):
			return evs
		}
	}
}

func summarize(evs []event) string {
	var b strings.Builder
	for i, e := range evs {
		if i >= 8 {
			fmt.Fprintf(&b, " ... (%d more)", len(evs)-i)
			break
		}
		fmt.Fprintf(&b, " [tgid=%d fd=%d %d/%d len=%d]", e.Tgid, e.FD, e.Seq+1, e.NChunks, e.Len)
	}
	return b.String()
}

func onFD(evs []event, fd uint32) []event {
	var out []event
	for _, e := range evs {
		if e.FD == fd {
			out = append(out, e)
		}
	}
	return out
}

func wantPattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a' + byte(i%26)
	}
	b[n-1] = '\n'
	return b
}

// ---- 1.2: long writes through the ring buffer -------------------------------------------------------------------

func TestLongWritesArriveWholeAndInOrder(t *testing.T) {
	requireBPF(t)
	s := newTestSession(t, 1, 0)
	w := startWriter(t, pipeStdout(t))
	if err := s.follow(w.pid, 0); err != nil {
		t.Fatal(err)
	}
	ch := s.events()

	cases := []struct {
		n          int
		wantChunks int
		wantLeft   uint32
	}{
		{90, 1, 0},
		{16384, 1, 0},
		{16385, 2, 0},
		{50000, 4, 0},
		{65536, 4, 0},
		{100000, 4, 34464},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.n), func(t *testing.T) {
			w.do("write 1 %d", tc.n)
			evs := onFD(collect(t, ch, 10*time.Second, func(evs []event) bool {
				for _, e := range evs {
					if e.FD == 1 && e.Seq+1 == e.NChunks {
						return true
					}
				}
				return false
			}), 1)

			if len(evs) != tc.wantChunks {
				t.Fatalf("expected %d chunks, got %d:%s", tc.wantChunks, len(evs), summarize(evs))
			}
			var got []byte
			for i, e := range evs {
				if int(e.Seq) != i {
					t.Errorf("chunk %d arrived as number %d: order is wrong", i, e.Seq)
				}
				if e.Tgid != uint32(w.pid) {
					t.Errorf("chunk %d has PID %d, want %d", i, e.Tgid, w.pid)
				}
				want := uint32(0)
				if i == len(evs)-1 {
					want = tc.wantLeft
				}
				if e.LeftOut != want {
					t.Errorf("chunk %d reports %d bytes left out, want %d", i, e.LeftOut, want)
				}
				got = append(got, e.Data...)
			}
			total := min(tc.n, 65536)
			if !bytes.Equal(got, wantPattern(tc.n)[:total]) {
				t.Errorf("the bytes differ: got %d bytes, want %d", len(got), total)
			}
		})
	}

	if lost, err := s.stat(0); err != nil || lost != 0 {
		t.Errorf("writes lost: %d (%v)", lost, err)
	}
	if bad, err := s.stat(1); err != nil || bad != 0 {
		t.Errorf("writes whose bytes could not be read: %d (%v)", bad, err)
	}
}

// ---- 1.3: following processes by PID in our own namespace ---------------------------------------------------

func TestFollowsOnlyTheChosenProcess(t *testing.T) {
	requireBPF(t)
	s := newTestSession(t, 1, 0)
	a := startWriter(t, pipeStdout(t))
	b := startWriter(t, pipeStdout(t)) // another process of the same program: same name, not followed
	if err := s.follow(a.pid, 0); err != nil {
		t.Fatal(err)
	}
	ch := s.events()

	a.do("write 1 40")
	b.do("write 1 40")
	evs := onFD(collect(t, ch, 10*time.Second, func(evs []event) bool { return len(onFD(evs, 1)) >= 1 }), 1)
	evs = append(evs, onFD(quiet(ch, 500*time.Millisecond), 1)...)

	if len(evs) != 1 {
		t.Fatalf("expected exactly one chunk, from the followed process:%s", summarize(evs))
	}
	if evs[0].Tgid != uint32(a.pid) {
		t.Errorf("the chunk is from PID %d, want %d (a PID in this namespace)", evs[0].Tgid, a.pid)
	}
	if _, err := os.Stat(fmt.Sprintf("/proc/%d/task/%d", a.pid, evs[0].Tid)); err != nil {
		t.Errorf("thread %d is not a thread of PID %d: %v", evs[0].Tid, a.pid, err)
	}
}

func TestFollowsChildrenUpToTheDepthLimit(t *testing.T) {
	requireBPF(t)
	for _, tc := range []struct {
		maxDepth uint32
		wantGens []int
	}{
		{1, []int{3}},
		{2, []int{3, 2}},
		{3, []int{3, 2, 1}},
	} {
		t.Run(fmt.Sprintf("depth %d", tc.maxDepth), func(t *testing.T) {
			s := newTestSession(t, tc.maxDepth, 0)
			w := startWriter(t, pipeStdout(t))
			if err := s.follow(w.pid, 0); err != nil {
				t.Fatal(err)
			}
			ch := s.events()

			w.do("spawn 3") // a child (gen 3), its child (gen 2) and its child (gen 1)
			evs := onFD(collect(t, ch, 10*time.Second, func(evs []event) bool {
				return len(onFD(evs, 1)) >= len(tc.wantGens)
			}), 1)
			evs = append(evs, onFD(quiet(ch, 500*time.Millisecond), 1)...)

			re := regexp.MustCompile(`child gen=(\d) pid=(\d+)`)
			seen := map[int]bool{}
			for _, e := range evs {
				m := re.FindSubmatch(e.Data)
				if m == nil {
					t.Errorf("an unexpected chunk: %q", e.Data)
					continue
				}
				gen, _ := strconv.Atoi(string(m[1]))
				pid, _ := strconv.Atoi(string(m[2]))
				seen[gen] = true
				if e.Tgid != uint32(pid) {
					t.Errorf("gen %d wrote as PID %d, but the event says %d", gen, pid, e.Tgid)
				}
			}
			for _, g := range tc.wantGens {
				if !seen[g] {
					t.Errorf("generation %d was not captured", g)
				}
			}
			if len(seen) != len(tc.wantGens) {
				t.Errorf("captured generations %v, want exactly %v", seen, tc.wantGens)
			}
		})
	}
}

// The real Specter test server, if its binary is given: its log lines must be captured by PID.
func TestCapturesTheE2ETestServer(t *testing.T) {
	requireBPF(t)
	bin := os.Getenv("TESTSERVER_BIN")
	if bin == "" {
		t.Skip("set TESTSERVER_BIN to the test server binary to run this")
	}

	cmd := exec.Command(bin, "-port=0")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	sc := bufio.NewScanner(out)
	var port, pid int
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "SPECTER_TEST_SERVER_READY") {
			for _, f := range strings.Fields(sc.Text()) {
				if v, ok := strings.CutPrefix(f, "port="); ok {
					port, _ = strconv.Atoi(v)
				}
				if v, ok := strings.CutPrefix(f, "pid="); ok {
					pid, _ = strconv.Atoi(v)
				}
			}
			break
		}
	}
	go func() { _, _ = io.Copy(io.Discard, out) }()
	if port == 0 || pid == 0 {
		t.Fatal("the test server did not say its port and PID")
	}

	s := newTestSession(t, 1, 0)
	if err := s.follow(pid, 0); err != nil {
		t.Fatal(err)
	}
	ch := s.events()

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json?msg=spike-line", port))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	evs := collect(t, ch, 10*time.Second, func(evs []event) bool {
		for _, e := range evs {
			if e.FD == 1 && bytes.Contains(e.Data, []byte(`"msg":"spike-line"`)) {
				return true
			}
		}
		return false
	})
	t.Logf("captured %d chunk(s) from the test server, PID %d (a namespace PID)", len(evs), pid)
}

// ---- 1.4: the device and inode behind a descriptor --------------------------------------------------------------

// identity is what stat says about the file behind a descriptor of a process, with the device in the kernel's
// encoding.
func identity(t *testing.T, pid, fd int) (dev uint32, ino uint64, mode uint32) {
	t.Helper()
	var st unix.Stat_t
	if err := unix.Stat(fmt.Sprintf("/proc/%d/fd/%d", pid, fd), &st); err != nil {
		t.Fatalf("stat /proc/%d/fd/%d: %v", pid, fd, err)
	}
	return kernelDev(uint64(st.Dev)), st.Ino, st.Mode & unix.S_IFMT
}

// eventFor writes n bytes to fd in the writer and returns the chunk it produced.
func eventFor(t *testing.T, ch <-chan event, w *writer, fd int) event {
	t.Helper()
	w.do("write %d 10", fd)
	evs := collect(t, ch, 10*time.Second, func(evs []event) bool { return len(onFD(evs, uint32(fd))) >= 1 })
	return onFD(evs, uint32(fd))[0]
}

func openPty(t *testing.T) *os.File {
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
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, master) }()
	t.Cleanup(func() { _ = slave.Close(); _ = master.Close() })
	return slave
}

func TestDescriptorIdentityMatchesStat(t *testing.T) {
	requireBPF(t)

	for _, tc := range []struct {
		name    string
		stdout  func(*testing.T) *os.File
		stdMode uint32
	}{
		{"stdout is a pipe", pipeStdout, unix.S_IFIFO},
		{"stdout is a terminal", openPty, unix.S_IFCHR},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestSession(t, 1, 0)
			w := startWriter(t, tc.stdout(t))
			if err := s.follow(w.pid, 0); err != nil {
				t.Fatal(err)
			}
			ch := s.events()

			wantDev, wantIno, wantMode := identity(t, w.pid, 1)
			if wantMode != tc.stdMode {
				t.Fatalf("stdout has mode %#o, test expected %#o", wantMode, tc.stdMode)
			}
			e1 := eventFor(t, ch, w, 1)
			if e1.Dev != wantDev || e1.Ino != wantIno || e1.Mode&unix.S_IFMT != wantMode {
				t.Errorf("fd 1: the kernel says dev=%#x ino=%d mode=%#o; stat says dev=%#x ino=%d mode=%#o",
					e1.Dev, e1.Ino, e1.Mode&unix.S_IFMT, wantDev, wantIno, wantMode)
			}

			// A private pipe and /dev/null each match their own stat, and differ from stdout
			for _, other := range []struct {
				name string
				fd   int
				mode uint32
			}{{"a private pipe", 4, unix.S_IFIFO}, {"/dev/null", 5, unix.S_IFCHR}} {
				dev, ino, mode := identity(t, w.pid, other.fd)
				if mode != other.mode {
					t.Fatalf("%s has mode %#o, test expected %#o", other.name, mode, other.mode)
				}
				e := eventFor(t, ch, w, other.fd)
				if e.Dev != dev || e.Ino != ino {
					t.Errorf("%s: the kernel says dev=%#x ino=%d; stat says dev=%#x ino=%d", other.name, e.Dev, e.Ino, dev, ino)
				}
				if e.Dev == e1.Dev && e.Ino == e1.Ino {
					t.Errorf("%s has the same identity as stdout, so the two could not be told apart", other.name)
				}
			}
		})
	}
}
