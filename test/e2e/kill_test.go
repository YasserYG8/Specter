package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// tracerPID returns the PID that traces pid, or 0 when nothing does.
func tracerPID(t *testing.T, pid int) int {
	t.Helper()
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		t.Fatalf("cannot read the status of PID %d: %v", pid, err)
	}
	for line := range strings.SplitSeq(string(status), "\n") {
		if rest, ok := strings.CutPrefix(line, "TracerPid:"); ok {
			n, _ := strconv.Atoi(strings.TrimSpace(rest))
			return n
		}
	}
	return 0
}

// straceFor returns the PIDs of the strace processes that trace pid, found by their command line.
func straceFor(t *testing.T, pid int) []int {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatalf("cannot read /proc: %v", err)
	}
	var found []int
	want := "-p" + "\x00" + strconv.Itoa(pid)
	for _, e := range entries {
		n, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", n))
		if err != nil || strings.TrimSpace(string(comm)) != "strace" {
			continue
		}
		cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", n))
		if err != nil {
			continue
		}
		if strings.Contains(string(cmdline), want) {
			found = append(found, n)
		}
	}
	return found
}

// startKillableSpecter starts Specter on the server's port without any help to stop it: no context, no interrupt.
// The caller decides how it dies. It returns once strace is attached to the server.
func startKillableSpecter(t *testing.T, specterBin string, srv *serverProcess, args ...string) (*exec.Cmd, *safeBuffer, *safeBuffer) {
	t.Helper()
	cmd := exec.Command(specterBin, append(append([]string{strconv.Itoa(srv.Port), "--no-color"}, engineArgs()...), args...)...)
	stdout, stderr := &safeBuffer{}, &safeBuffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start specter: %v", err)
	}
	t.Cleanup(func() {
		// Never leave a Specter or a tracer behind, even when the test fails
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
		for _, p := range straceFor(t, srv.PID) {
			if proc, err := os.FindProcess(p); err == nil {
				_ = proc.Signal(os.Interrupt)
			}
		}
	})

	waitAttached(t, srv.PID, stderr)
	return cmd, stdout, stderr
}

func serverAnswers(srv *serverProcess) error {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", srv.Port))
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// When Specter is killed with SIGKILL it cannot clean up. The strace it started must still end within a second and
// leave the server alone, even when the server is idle and writes nothing that would break strace's output pipe.
func TestE2E_StraceDoesNotOutliveSpecter(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	const repetitions = 20
	for i := 1; i <= repetitions; i++ {
		cmd, _, stderr := startKillableSpecter(t, specterBin, srv)

		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("run %d: cannot kill specter: %v", i, err)
		}
		_ = cmd.Wait()

		// The server stays idle from here on: no requests, so it writes no line
		released := waitForCondition(t, time.Second, func() bool {
			return len(straceFor(t, srv.PID)) == 0 && tracerPID(t, srv.PID) == 0
		})
		if !released {
			t.Fatalf("run %d of %d: strace %v still traces PID %d one second after Specter was killed (tracer %d).\nStderr:\n%s",
				i, repetitions, straceFor(t, srv.PID), srv.PID, tracerPID(t, srv.PID), stderr.String())
		}
		if err := serverAnswers(srv); err != nil {
			t.Fatalf("run %d: the server does not answer after Specter was killed: %v", i, err)
		}
	}
}

// A second Specter must be able to attach after the first one was killed.
func TestE2E_SpecterAttachesAfterAKilledSpecter(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	first, _, _ := startKillableSpecter(t, specterBin, srv)
	if err := first.Process.Kill(); err != nil {
		t.Fatalf("cannot kill the first specter: %v", err)
	}
	_ = first.Wait()

	if !waitForCondition(t, time.Second, func() bool { return tracerPID(t, srv.PID) == 0 }) {
		t.Fatalf("the server still has tracer %d after the first Specter was killed", tracerPID(t, srv.PID))
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	stdout, stderr, stop := attachSpecter(t, ctx, specterBin, srv)
	defer stop()

	if strings.Contains(stderr.String(), "already being traced") {
		t.Fatalf("the second Specter says the server is already traced.\nStderr:\n%s", stderr.String())
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/json?msg=after-a-kill", srv.Port)
	if !waitForCondition(t, 30*time.Second, func() bool {
		if resp, err := http.Get(url); err == nil {
			_ = resp.Body.Close()
		}
		time.Sleep(150 * time.Millisecond)
		return strings.Contains(stdout.String(), "after-a-kill")
	}) {
		t.Errorf("the second Specter did not capture the line.\nStdout:\n%s\nStderr:\n%s", stdout.String(), stderr.String())
	}
}

// Killing strace itself is safe: the kernel releases the server. Specter must say what happened and exit with 1.
func TestE2E_StraceKilledIsReported(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	cmd, _, stderr := startKillableSpecter(t, specterBin, srv)

	pids := straceFor(t, srv.PID)
	if len(pids) != 1 {
		t.Fatalf("expected one strace for PID %d, found %v", srv.PID, pids)
	}
	proc, err := os.FindProcess(pids[0])
	if err != nil {
		t.Fatalf("cannot find strace: %v", err)
	}
	if err := proc.Kill(); err != nil {
		t.Fatalf("cannot kill strace: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatalf("Specter did not exit after strace was killed.\nStderr:\n%s", stderr.String())
	}

	if code := cmd.ProcessState.ExitCode(); code != 1 {
		t.Errorf("expected exit status 1, got %d.\nStderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "strace exited unexpectedly") {
		t.Errorf("expected the message that strace exited unexpectedly.\nStderr:\n%s", stderr.String())
	}
	if n := tracerPID(t, srv.PID); n != 0 {
		t.Errorf("the server still has tracer %d", n)
	}
	if err := serverAnswers(srv); err != nil {
		t.Errorf("the server does not answer after strace was killed: %v", err)
	}
}
