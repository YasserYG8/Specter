package e2e

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// runningSpecter is a Specter started by a test, with everything it printed.
type runningSpecter struct {
	stdout, stderr *safeBuffer
	wait           func() int // waits for the exit and returns the exit status
	interrupt      func()     // Ctrl+C
}

// startSpecter starts Specter with the given arguments (and --no-color, and the current engine) and does not wait
// for it to attach.
func startSpecter(t *testing.T, ctx context.Context, bin string, args ...string) *runningSpecter {
	t.Helper()
	cmd := specterCommand(ctx, bin, append(args, "--no-color")...)
	rs := &runningSpecter{stdout: &safeBuffer{}, stderr: &safeBuffer{}}
	cmd.Stdout, cmd.Stderr = rs.stdout, rs.stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start specter: %v", err)
	}
	waited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(waited)
	}()
	rs.interrupt = func() { _ = cmd.Process.Signal(os.Interrupt) }
	rs.wait = func() int {
		select {
		case <-waited:
		case <-time.After(30 * time.Second):
			_ = cmd.Process.Kill()
			t.Fatalf("Specter did not exit within 30s.\nStderr:\n%s", rs.stderr.String())
		}
		return cmd.ProcessState.ExitCode()
	}
	t.Cleanup(func() {
		rs.interrupt()
		select {
		case <-waited:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
		}
	})
	return rs
}

// waitAttachedAll waits until Specter says it attached to n targets, and the engines had a moment to start.
func (rs *runningSpecter) waitAttachedAll(t *testing.T, n int) {
	t.Helper()
	if !waitForCondition(t, 40*time.Second, func() bool { return strings.Count(rs.stderr.String(), "Source:") >= n }) {
		t.Fatalf("Specter did not attach to %d targets.\nStderr:\n%s", n, rs.stderr.String())
	}
	time.Sleep(1500 * time.Millisecond)
}

// get sends an HTTP request to the test server and ignores the result.
func (s *serverProcess) get(path string) {
	client := &http.Client{Timeout: 10 * time.Second}
	if resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", s.Port, path)); err == nil {
		_ = resp.Body.Close()
	}
}

// lines splits what Specter printed into lines.
func lines(buf *safeBuffer) []string {
	return strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
}

// tagOf returns the tag of an output line (the text before " | "), or "" when the line has none.
func tagOf(line string) string {
	tag, _, ok := strings.Cut(line, " | ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(tag)
}

func TestE2E_MultiTargetTaggedOutput(t *testing.T) { forEachEngine(t, runMultiTargetTaggedOutput) }

// Two servers on two ports: every line shows where it came from, and a stack trace of one stays in one piece while
// the other keeps writing.
func runMultiTargetTaggedOutput(t *testing.T) {
	requireLinux(t)
	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	a, cleanupA := startTestServer(t, serverBin)
	defer cleanupA()
	b, cleanupB := startTestServer(t, serverBin)
	defer cleanupB()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rs := startSpecter(t, ctx, specterBin, fmt.Sprint(a.Port), fmt.Sprint(b.Port))
	rs.waitAttachedAll(t, 2)

	tagA, tagB := fmt.Sprintf(":%d", a.Port), fmt.Sprintf(":%d", b.Port)
	for _, want := range []string{tagA + " Attaching to PID", tagB + " Attaching to PID", tagA + " Source:", tagB + " Source:"} {
		if !strings.Contains(rs.stderr.String(), want) {
			t.Errorf("stderr is missing %q.\nStderr:\n%s", want, rs.stderr.String())
		}
	}

	// :b writes a stack trace while :a writes lines all the time
	stopNoise := make(chan struct{})
	noiseDone := make(chan struct{})
	go func() {
		defer close(noiseDone)
		for i := 0; ; i++ {
			select {
			case <-stopNoise:
				return
			default:
			}
			a.get(fmt.Sprintf("/json?msg=noise-from-a-%d", i))
		}
	}()
	found := waitForCondition(t, 40*time.Second, func() bool {
		b.get("/json?msg=hello-from-b")
		b.get("/error")
		time.Sleep(200 * time.Millisecond)
		out := rs.stdout.String()
		return strings.Contains(out, "noise-from-a-") && strings.Contains(out, "ValueError: payment authorization failed") && strings.Contains(out, "hello-from-b")
	})
	close(stopNoise)
	<-noiseDone
	if !found {
		t.Fatalf("Specter did not show lines of both targets.\nStdout:\n%s\nStderr:\n%s", rs.stdout.String(), rs.stderr.String())
	}
	time.Sleep(500 * time.Millisecond)

	out := lines(rs.stdout)
	for _, l := range out {
		if l == "" {
			continue
		}
		if tag := tagOf(l); tag != tagA && tag != tagB {
			t.Errorf("a line has no tag of a target: %q", l)
		}
		if strings.Contains(l, "noise-from-a-") && tagOf(l) != tagA {
			t.Errorf("a line of :%d carries another tag: %q", a.Port, l)
		}
		if strings.Contains(l, "hello-from-b") && tagOf(l) != tagB {
			t.Errorf("a line of :%d carries another tag: %q", b.Port, l)
		}
	}

	// Every stack trace of b is one block: from its header to its last line, only lines tagged with b
	traces := 0
	for i, l := range out {
		if !strings.Contains(l, "Traceback (most recent call last):") {
			continue
		}
		traces++
		end := -1
		for j := i; j < len(out); j++ {
			if strings.Contains(out[j], "ValueError: payment authorization failed") {
				end = j
				break
			}
		}
		if end < 0 {
			t.Errorf("a stack trace has no last line.\nStdout:\n%s", rs.stdout.String())
			continue
		}
		for j := i; j <= end; j++ {
			if tagOf(out[j]) != tagB {
				t.Errorf("a line of another target is inside the stack trace: %q\nStdout:\n%s", out[j], rs.stdout.String())
			}
		}
		if end-i != 5 {
			t.Errorf("the stack trace should be 6 lines, it is %d.\nStdout:\n%s", end-i+1, rs.stdout.String())
		}
	}
	if traces == 0 {
		t.Error("no stack trace was captured")
	}
}

func TestE2E_MultiTargetOneEndsTheOtherContinues(t *testing.T) {
	forEachEngine(t, runMultiTargetOneEnds)
}

func runMultiTargetOneEnds(t *testing.T) {
	requireLinux(t)
	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	a, cleanupA := startTestServer(t, serverBin)
	defer cleanupA()
	b, cleanupB := startTestServer(t, serverBin)
	defer cleanupB()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rs := startSpecter(t, ctx, specterBin, fmt.Sprint(a.Port), fmt.Sprint(b.Port))
	rs.waitAttachedAll(t, 2)

	tagA, tagB := fmt.Sprintf(":%d", a.Port), fmt.Sprintf(":%d", b.Port)

	// b exits by itself
	b.get("/exit")
	wantEnd := fmt.Sprintf("%s Target PID %d exited with code 0.", tagB, b.PID)
	if !waitForCondition(t, 30*time.Second, func() bool { return strings.Contains(rs.stderr.String(), wantEnd) }) {
		t.Fatalf("Specter did not report that :%d ended (%q).\nStderr:\n%s", b.Port, wantEnd, rs.stderr.String())
	}

	// a is still followed
	if !waitForCondition(t, 30*time.Second, func() bool {
		a.get("/json?msg=still-alive-after-b-ended")
		time.Sleep(150 * time.Millisecond)
		return strings.Contains(rs.stdout.String(), "still-alive-after-b-ended")
	}) {
		t.Fatalf("Specter stopped following :%d after :%d ended.\nStdout:\n%s\nStderr:\n%s", a.Port, b.Port, rs.stdout.String(), rs.stderr.String())
	}

	// Ctrl+C: a is detached cleanly, and the exit status says a target ended by itself
	rs.interrupt()
	if code := rs.wait(); code != 1 {
		t.Errorf("exit status = %d, want 1 because :%d ended by itself.\nStderr:\n%s", code, b.Port, rs.stderr.String())
	}
	if want := fmt.Sprintf("%s Detached from PID %d cleanly.", tagA, a.PID); !strings.Contains(rs.stderr.String(), want) {
		t.Errorf("stderr is missing %q.\nStderr:\n%s", want, rs.stderr.String())
	}
	if strings.Contains(rs.stderr.String(), tagB+" Detached") {
		t.Errorf("the target that ended must not be reported as detached.\nStderr:\n%s", rs.stderr.String())
	}
	if pid := tracerPID(t, a.PID); pid != 0 {
		t.Errorf("PID %d still has a tracer (%d) after Specter exited", a.PID, pid)
	}
}

func TestE2E_MultiTargetCtrlCDetachesAll(t *testing.T) { forEachEngine(t, runMultiTargetCtrlC) }

func runMultiTargetCtrlC(t *testing.T) {
	requireLinux(t)
	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	a, cleanupA := startTestServer(t, serverBin)
	defer cleanupA()
	b, cleanupB := startTestServer(t, serverBin)
	defer cleanupB()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rs := startSpecter(t, ctx, specterBin, fmt.Sprint(a.Port), "--pid", fmt.Sprint(b.PID))
	rs.waitAttachedAll(t, 2)

	rs.interrupt()
	if code := rs.wait(); code != 0 {
		t.Errorf("exit status = %d, want 0 after Ctrl+C with every target running.\nStderr:\n%s", code, rs.stderr.String())
	}
	for _, want := range []string{
		fmt.Sprintf(":%d Detached from PID %d cleanly.", a.Port, a.PID),
		fmt.Sprintf("pid:%d Detached from PID %d cleanly.", b.PID, b.PID),
	} {
		if !strings.Contains(rs.stderr.String(), want) {
			t.Errorf("stderr is missing %q.\nStderr:\n%s", want, rs.stderr.String())
		}
	}
	for _, s := range []*serverProcess{a, b} {
		if pid := tracerPID(t, s.PID); pid != 0 {
			t.Errorf("PID %d still has a tracer (%d) after Specter exited", s.PID, pid)
		}
	}
}

func TestE2E_MultiTargetSameServerTwiceIsFollowedOnce(t *testing.T) {
	forEachEngine(t, runMultiTargetSameServerTwice)
}

func runMultiTargetSameServerTwice(t *testing.T) {
	requireLinux(t)
	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	srv, cleanup := startTestServer(t, serverBin)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// The same server by its port, and by its PID
	rs := startSpecter(t, ctx, specterBin, fmt.Sprint(srv.Port), "--pid", fmt.Sprint(srv.PID))
	rs.waitAttachedAll(t, 1)

	want := fmt.Sprintf("pid:%d is the same process as :%d (PID %d)", srv.PID, srv.Port, srv.PID)
	if !strings.Contains(rs.stderr.String(), want) {
		t.Errorf("stderr is missing the notice %q.\nStderr:\n%s", want, rs.stderr.String())
	}
	if n := strings.Count(rs.stderr.String(), "Source:"); n != 1 {
		t.Errorf("the server should be followed once, but there are %d Source lines.\nStderr:\n%s", n, rs.stderr.String())
	}

	// One request gives one line, without a tag, and no second copy of it
	srv.get("/json?msg=only-once-xyz")
	if !waitForCondition(t, 30*time.Second, func() bool { return strings.Contains(rs.stdout.String(), "only-once-xyz") }) {
		t.Fatalf("no line was captured.\nStderr:\n%s", rs.stderr.String())
	}
	time.Sleep(1500 * time.Millisecond)
	if n := strings.Count(rs.stdout.String(), "only-once-xyz"); n != 1 {
		t.Errorf("the line should be shown once, it was shown %d times.\nStdout:\n%s", n, rs.stdout.String())
	}
	tagged := regexp.MustCompile(`^(:\d+|pid:\d+) +\| `)
	for _, l := range lines(rs.stdout) {
		if tagged.MatchString(l) {
			t.Errorf("a run with one target must not tag its lines: %q", l)
		}
	}
}

func TestE2E_MultiTargetOneEmptyPortAttachesNothing(t *testing.T) {
	forEachEngine(t, runMultiTargetEmptyPort)
}

func runMultiTargetEmptyPort(t *testing.T) {
	requireLinux(t)
	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	srv, cleanup := startTestServer(t, serverBin)
	defer cleanup()

	// A port that is free right now
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("cannot find a free port: %v", err)
	}
	empty := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	rs := startSpecter(t, ctx, specterBin, fmt.Sprint(srv.Port), fmt.Sprint(empty))

	if code := rs.wait(); code == 0 {
		t.Errorf("expected a non-zero exit status.\nStderr:\n%s", rs.stderr.String())
	}
	text := rs.stderr.String()
	if want := fmt.Sprintf("No process is listening on port %d.", empty); !strings.Contains(text, want) {
		t.Errorf("stderr is missing %q.\nStderr:\n%s", want, text)
	}
	if strings.Contains(text, "Attaching to") || strings.Contains(text, "Source:") {
		t.Errorf("nothing may be attached when one target cannot be found.\nStderr:\n%s", text)
	}
	if rs.stdout.String() != "" {
		t.Errorf("no log line may be printed, got %q", rs.stdout.String())
	}
	if pid := tracerPID(t, srv.PID); pid != 0 {
		t.Errorf("PID %d has a tracer (%d), but nothing should have been attached", srv.PID, pid)
	}
}
