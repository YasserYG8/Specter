package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// attachSpecter starts Specter on the server's port and waits until it reports that it attached.
// The returned function stops it with an interrupt, the way Ctrl+C does.
func attachSpecter(t *testing.T, ctx context.Context, specterBin string, srv *serverProcess, args ...string) (stdout, stderr *safeBuffer, stop func()) {
	t.Helper()
	cmdArgs := append([]string{strconv.Itoa(srv.Port), "--no-color"}, args...)
	cmd := specterCommand(ctx, specterBin, cmdArgs...)
	stdout, stderr = &safeBuffer{}, &safeBuffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start specter: %v", err)
	}
	stopped := false
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
	}
	t.Cleanup(stop)

	if !waitForCondition(t, 30*time.Second, func() bool {
		return strings.Contains(stderr.String(), "Source:")
	}) {
		t.Fatalf("Specter did not attach within timeout. Stderr:\n%s", stderr.String())
	}
	// Give strace a moment to attach to every thread of the target before it writes anything
	time.Sleep(1500 * time.Millisecond)
	return stdout, stderr, stop
}

func httpGet(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	_ = resp.Body.Close()
}

func TestE2E_ConcurrentWorkersLoseNoLines(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	const workers, linesEach = 4, 3000
	stdout, stderr, stop := attachSpecter(t, ctx, specterBin, srv)

	httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/workers?count=%d&lines=%d", srv.Port, workers, linesEach))

	// Wait until the output stops growing
	last, stableSince := -1, time.Now()
	waitForCondition(t, 60*time.Second, func() bool {
		n := len(stdout.String())
		if n != last {
			last, stableSince = n, time.Now()
			return false
		}
		return time.Since(stableSince) > 2*time.Second
	})
	stop()

	captured := map[string]int{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if len(line) >= 4 && line[0] == 'w' && line[2] == '-' {
			captured[line]++
		}
	}

	var missing, duplicated []string
	for w := 0; w < workers; w++ {
		for i := 0; i < linesEach; i++ {
			want := fmt.Sprintf("w%d-%d", w, i)
			switch captured[want] {
			case 0:
				missing = append(missing, want)
			case 1:
			default:
				duplicated = append(duplicated, want)
			}
			delete(captured, want)
		}
	}
	var unexpected []string
	for k := range captured {
		unexpected = append(unexpected, k)
	}
	if len(missing) > 0 || len(duplicated) > 0 || len(unexpected) > 0 {
		sample := func(s []string) []string {
			if len(s) > 5 {
				return s[:5]
			}
			return s
		}
		t.Errorf("wrote %d lines, missing %d (e.g. %v), duplicated %d (e.g. %v), unexpected %d (e.g. %v)\nStderr:\n%s",
			workers*linesEach, len(missing), sample(missing), len(duplicated), sample(duplicated),
			len(unexpected), sample(unexpected), stderr.String())
	}
}

func TestE2E_LongLines(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	stdout, stderr, stop := attachSpecter(t, ctx, specterBin, srv)
	defer stop()

	lineWithPrefix := func(out, prefix string) string {
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, prefix) {
				return l
			}
		}
		return ""
	}

	run := func(size int, marker string) {
		t.Helper()
		before := len(stdout.String())
		httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/longline?bytes=%d", srv.Port, size))
		ok := waitForCondition(t, 20*time.Second, func() bool {
			return strings.Contains(stdout.String()[before:], "after-long-line")
		})
		if !ok {
			t.Fatalf("%d-byte line: the line after it never arrived.\nStderr:\n%s", size, stderr.String())
		}
		out := stdout.String()[before:]
		long := lineWithPrefix(out, "AAAA")
		if marker == "" {
			if long != strings.Repeat("A", size) {
				t.Errorf("%d-byte line should arrive whole, got a line of %d bytes", size, len(long))
			}
			if strings.Contains(out, "[specter:") {
				t.Errorf("%d-byte line should have no marker", size)
			}
		} else if !strings.HasPrefix(long, strings.Repeat("A", 65536)) || !strings.Contains(long, marker) {
			t.Errorf("%d-byte line: expected 65536 bytes then a marker containing %q, got a line of %d bytes ending %q",
				size, marker, len(long), long[max(0, len(long)-60):])
		}
		if lineWithPrefix(out, "after-long-line") != "after-long-line" {
			t.Errorf("%d-byte line: the next line was glued onto it:\n%.200q", size, out)
		}
	}

	run(20000, "")
	run(60000, "")
	// 100000 bytes plus the newline: strace captures 65536 of the 100001 written, so 34465 are reported missing.
	run(100000, "[specter: 34465 more bytes not captured]")
}

func TestE2E_WritevCaptured(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	stdout, stderr, stop := attachSpecter(t, ctx, specterBin, srv)
	defer stop()

	found := waitForCondition(t, 30*time.Second, func() bool {
		httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/writev?msg=e2e-writev-hello", srv.Port))
		time.Sleep(150 * time.Millisecond)
		return strings.Contains(stdout.String(), "writev-line: e2e-writev-hello")
	})
	if !found {
		t.Errorf("a writev to stdout was not captured.\nStdout:\n%s\nStderr:\n%s", stdout.String(), stderr.String())
	}
}

func TestE2E_NoOutputNotice(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")
	const notice = "Nothing captured yet"

	t.Run("silent target", func(t *testing.T) {
		srv, cleanupServer := startTestServer(t, serverBin)
		defer cleanupServer()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		stdout, stderr, stop := attachSpecter(t, ctx, specterBin, srv)
		defer stop()

		if !waitForCondition(t, 25*time.Second, func() bool { return strings.Contains(stderr.String(), notice) }) {
			t.Fatalf("expected the no-output notice on stderr.\nStderr:\n%s", stderr.String())
		}
		time.Sleep(1 * time.Second)
		if n := strings.Count(stderr.String(), notice); n != 1 {
			t.Errorf("expected the notice exactly once, got %d", n)
		}
		if strings.Contains(stdout.String(), notice) || strings.Contains(stdout.String(), "[specter") {
			t.Errorf("stdout must stay clean, got:\n%s", stdout.String())
		}
	})

	t.Run("output hidden by filters", func(t *testing.T) {
		srv, cleanupServer := startTestServer(t, serverBin)
		defer cleanupServer()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		stdout, stderr, stop := attachSpecter(t, ctx, specterBin, srv, "--grep", "no-such-text-anywhere")
		defer stop()

		// Entries are produced all through the notice window, but every one is hidden by --grep
		deadline := time.Now().Add(13 * time.Second)
		for time.Now().Before(deadline) {
			httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/json?msg=hidden-by-grep", srv.Port))
			time.Sleep(400 * time.Millisecond)
		}
		if strings.Contains(stderr.String(), notice) {
			t.Errorf("no notice expected when output is only hidden by filters.\nStderr:\n%s", stderr.String())
		}
		if strings.TrimSpace(stdout.String()) != "" {
			t.Errorf("expected empty stdout, got:\n%s", stdout.String())
		}
	})
}
