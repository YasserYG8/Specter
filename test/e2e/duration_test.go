package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// waitExit waits for a started Specter to exit, and fails the test if it does not within timeout.
func waitExit(t *testing.T, cmdWait func() error, timeout time.Duration, stderr *safeBuffer) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmdWait() }()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("Specter did not exit within %v.\nStderr:\n%s", timeout, stderr.String())
	}
}

// The strace source slows the target, so it says so once, on stderr, right after the Source line. stdout carries
// log lines only, so `specter 3000 | grep 500` stays clean.
func TestE2E_SlowdownNoticeGoesToStderrOnce(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	stdout, stderr, stop := attachSpecter(t, ctx, specterBin, srv)

	// Some traffic, so there is something on stdout to look at
	for i := 0; i < 5; i++ {
		httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/json?msg=notice-check-%d", srv.Port, i))
	}
	waitForCondition(t, 10*time.Second, func() bool { return strings.Contains(stdout.String(), "notice-check-4") })
	stop()

	text := stderr.String()
	const marker = "slows the target"
	if n := strings.Count(text, marker); n != 1 {
		t.Fatalf("expected the slowdown notice once on stderr, found it %d times.\nStderr:\n%s", n, text)
	}
	if strings.Index(text, "Source:") > strings.Index(text, marker) {
		t.Errorf("the notice should come after the Source line.\nStderr:\n%s", text)
	}
	if !strings.Contains(text, "[specter:notice]") || !strings.Contains(text, "development and test servers") {
		t.Errorf("expected the usual notice format and the advice.\nStderr:\n%s", text)
	}
	if strings.Contains(stdout.String(), marker) || strings.Contains(stdout.String(), "[specter") {
		t.Errorf("stdout must carry log lines only.\nStdout:\n%s", stdout.String())
	}
}

// --duration detaches after the time, with exit status 0, and leaves the target free and running.
func TestE2E_DurationDetaches(t *testing.T) { forEachEngine(t, runDurationDetaches) }

func runDurationDetaches(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	cmd, _, stderr := startKillableSpecter(t, specterBin, srv, "--duration", "3s")
	started := time.Now()

	waitExit(t, cmd.Wait, 20*time.Second, stderr)
	took := time.Since(started)

	if code := cmd.ProcessState.ExitCode(); code != 0 {
		t.Errorf("a detach after --duration should exit with status 0, got %d.\nStderr:\n%s", code, stderr.String())
	}
	if want := fmt.Sprintf("Detached from PID %d after 3s.", srv.PID); !strings.Contains(stderr.String(), want) {
		t.Errorf("expected %q.\nStderr:\n%s", want, stderr.String())
	}
	if strings.Contains(stderr.String(), "cleanly") {
		t.Errorf("a stop by --duration should not use the Ctrl+C message.\nStderr:\n%s", stderr.String())
	}
	// 3 seconds from the start of the source, plus the detach itself. Far less than the time it takes to be late.
	if took > 12*time.Second {
		t.Errorf("Specter stayed attached for %v after being started with --duration 3s", took)
	}
	if !waitForCondition(t, 2*time.Second, func() bool {
		return len(straceFor(t, srv.PID)) == 0 && tracerPID(t, srv.PID) == 0
	}) {
		t.Errorf("the target still has a tracer after the detach: %d", tracerPID(t, srv.PID))
	}
	if err := serverAnswers(srv); err != nil {
		t.Errorf("the server does not answer after the detach: %v", err)
	}
}

// A target that ends before the time is over is reported as always: how it ended, and exit status 1.
func TestE2E_TargetEndsBeforeDuration(t *testing.T) { forEachEngine(t, runTargetEndsBeforeDuration) }

func runTargetEndsBeforeDuration(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	cmd, _, stderr := startKillableSpecter(t, specterBin, srv, "--duration", "60s")

	if err := srv.cmd.Process.Kill(); err != nil {
		t.Fatalf("cannot kill the test server: %v", err)
	}

	waitExit(t, cmd.Wait, 30*time.Second, stderr)

	if code := cmd.ProcessState.ExitCode(); code != 1 {
		t.Errorf("a target that ended on its own should give exit status 1, got %d.\nStderr:\n%s", code, stderr.String())
	}
	if want := fmt.Sprintf("Target PID %d was killed by SIGKILL.", srv.PID); !strings.Contains(stderr.String(), want) {
		t.Errorf("expected %q.\nStderr:\n%s", want, stderr.String())
	}
	if strings.Contains(stderr.String(), "Detached") {
		t.Errorf("Specter did not detach, the target ended.\nStderr:\n%s", stderr.String())
	}
}

// A --duration that cannot be used is refused before anything is attached.
func TestE2E_BadDurationIsRefusedBeforeAttaching(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	for _, tc := range []struct {
		value string
		want  []string
	}{
		{"30", []string{"30s", "2m"}},
		{"-5s", []string{"must be positive"}},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		cmd := specterCommand(ctx, specterBin, fmt.Sprint(srv.Port), "--duration="+tc.value, "--no-color")
		var stdout, stderr safeBuffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		cancel()

		if err == nil || cmd.ProcessState.ExitCode() == 0 {
			t.Errorf("--duration=%s: expected a non-zero exit.\nStderr:\n%s", tc.value, stderr.String())
		}
		for _, want := range tc.want {
			if !strings.Contains(stderr.String(), want) {
				t.Errorf("--duration=%s: expected %q.\nStderr:\n%s", tc.value, want, stderr.String())
			}
		}
		if strings.Contains(stderr.String(), "Attaching") || tracerPID(t, srv.PID) != 0 {
			t.Errorf("--duration=%s: Specter attached before refusing the value.\nStderr:\n%s", tc.value, stderr.String())
		}
	}
}
