package e2e

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/specter-cli/specter/internal/source/strace"
)

// safeBuffer is a thread-safe bytes buffer for concurrent reads/writes.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// requireLinux checks if the current environment supports live strace capture.
func requireLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skipf("Skipping live Linux integration test on %s", runtime.GOOS)
	}
	if err := strace.CheckStraceInstalled(); err != nil {
		t.Skipf("Skipping live integration test: strace not installed (%v)", err)
	}
	if err := strace.CheckPtracePermissions(""); err != nil {
		t.Skipf("Skipping live integration test: ptrace permission restricted (%v)", err)
	}
}

// getBinary returns the path to a binary, either from an environment variable or by compiling it.
func getBinary(t *testing.T, envVar, pkgPath, binaryName string) string {
	t.Helper()
	if envPath := os.Getenv(envVar); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
	}

	outDir := t.TempDir()
	outPath := filepath.Join(outDir, binaryName)

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		t.Fatalf("failed to create directory for %s: %v", outPath, err)
	}

	target := pkgPath
	if strings.HasPrefix(target, "./cmd/") {
		target = "github.com/specter-cli/specter/cmd/" + strings.TrimPrefix(target, "./cmd/")
	} else if strings.HasPrefix(target, "./test/") {
		target = "github.com/specter-cli/specter/test/" + strings.TrimPrefix(target, "./test/")
	}

	cmd := exec.Command("go", "build", "-o", outPath, target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build %s from %s: %v\nOutput: %s", binaryName, target, err, string(out))
	}
	return outPath
}

// serverProcess represents a running testserver instance.
type serverProcess struct {
	Port int
	PID  int
	cmd  *exec.Cmd
}

func startTestServer(t *testing.T, serverBin string) (*serverProcess, func()) {
	t.Helper()
	cmd := exec.Command(serverBin, "-port=0")
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("failed to create server stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}

	// Read startup synchronization line
	portChan := make(chan int, 1)
	pidChan := make(chan int, 1)
	errChan := make(chan error, 1)

	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		readySent := false
		for scanner.Scan() {
			line := scanner.Text()
			if !readySent && strings.HasPrefix(line, "SPECTER_TEST_SERVER_READY") {
				var port, pid int
				parts := strings.Fields(line)
				for _, part := range parts {
					if strings.HasPrefix(part, "port=") {
						port, _ = strconv.Atoi(strings.TrimPrefix(part, "port="))
					} else if strings.HasPrefix(part, "pid=") {
						pid, _ = strconv.Atoi(strings.TrimPrefix(part, "pid="))
					}
				}
				portChan <- port
				pidChan <- pid
				readySent = true
			}
		}
		if err := scanner.Err(); err != nil && !readySent {
			errChan <- err
		}
	}()

	var actualPort, actualPid int
	select {
	case actualPort = <-portChan:
		actualPid = <-pidChan
	case err := <-errChan:
		_ = cmd.Process.Kill()
		t.Fatalf("error waiting for test server readiness: %v", err)
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("timed out waiting for test server readiness")
	}

	// Verify health check endpoint responds
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", actualPort)
	healthy := false
	for i := 0; i < 20; i++ {
		resp, err := http.Get(healthURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			healthy = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !healthy {
		_ = cmd.Process.Kill()
		t.Fatalf("test server failed health check on port %d", actualPort)
	}

	cleanup := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}

	return &serverProcess{Port: actualPort, PID: actualPid, cmd: cmd}, cleanup
}

// waitForCondition polls a check function until true or timeout.
func waitForCondition(t *testing.T, timeout time.Duration, check func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func TestE2E_PortAttachment(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/specter-cli/specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/specter-cli/specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, specterBin, strconv.Itoa(srv.Port), "--no-color")
	var stdout, stderr safeBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start specter: %v", err)
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()

	// Wait for Specter attachment header on stderr
	attached := waitForCondition(t, 15*time.Second, func() bool {
		return strings.Contains(stderr.String(), "Attaching to PID")
	})
	if !attached {
		t.Fatalf("Specter did not attach within timeout. Stderr:\n%s", stderr.String())
	}

	// Repeatedly emit HTTP requests until Specter captures the log (handles attachment lag in CI)
	url := fmt.Sprintf("http://127.0.0.1:%d/json?msg=e2e-order-placed", srv.Port)
	found := waitForCondition(t, 15*time.Second, func() bool {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
		}
		time.Sleep(150 * time.Millisecond)
		return strings.Contains(stdout.String(), "e2e-order-placed")
	})
	if !found {
		t.Errorf("Specter failed to capture stdout log 'e2e-order-placed'.\nStdout:\n%s\nStderr:\n%s",
			stdout.String(), stderr.String())
	}
}

func TestE2E_MultilineErrorTrace(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/specter-cli/specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/specter-cli/specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, specterBin, strconv.Itoa(srv.Port), "--no-color")
	var stdout, stderr safeBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start specter: %v", err)
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()

	attached := waitForCondition(t, 15*time.Second, func() bool {
		return strings.Contains(stderr.String(), "Attaching to PID")
	})
	if !attached {
		t.Fatalf("Specter did not attach within timeout. Stderr:\n%s", stderr.String())
	}

	// Repeatedly emit error trace until Specter captures it
	url := fmt.Sprintf("http://127.0.0.1:%d/error", srv.Port)
	foundHeader := waitForCondition(t, 15*time.Second, func() bool {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
		}
		time.Sleep(150 * time.Millisecond)
		out := stdout.String()
		return strings.Contains(out, "Traceback (most recent call last):") &&
			strings.Contains(out, "ValueError: payment authorization failed")
	})
	if !foundHeader {
		t.Errorf("Specter failed to capture unbroken stack trace.\nStdout:\n%s\nStderr:\n%s", stdout.String(), stderr.String())
	}
}

func TestE2E_FilteringFlags(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/specter-cli/specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/specter-cli/specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	// Test 1: --level warn
	t.Run("LevelFiltering", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, specterBin, strconv.Itoa(srv.Port), "--level=warn", "--no-color")
		var stdout, stderr safeBuffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start specter: %v", err)
		}
		defer func() {
			cancel()
			_ = cmd.Wait()
		}()

		attached := waitForCondition(t, 15*time.Second, func() bool {
			return strings.Contains(stderr.String(), "Attaching to PID")
		})
		if !attached {
			t.Fatalf("Specter did not attach within timeout. Stderr:\n%s", stderr.String())
		}

		infoURL := fmt.Sprintf("http://127.0.0.1:%d/json?level=info&msg=suppressed-info", srv.Port)
		warnURL := fmt.Sprintf("http://127.0.0.1:%d/json?level=warn&msg=allowed-warn", srv.Port)

		foundWarn := waitForCondition(t, 15*time.Second, func() bool {
			resp, err := http.Get(infoURL)
			if err == nil {
				_ = resp.Body.Close()
			}
			resp, err = http.Get(warnURL)
			if err == nil {
				_ = resp.Body.Close()
			}
			time.Sleep(150 * time.Millisecond)
			return strings.Contains(stdout.String(), "allowed-warn")
		})
		if !foundWarn {
			t.Fatalf("level=warn did not render warning log within timeout.\nStdout:\n%s\nStderr:\n%s", stdout.String(), stderr.String())
		}

		out := stdout.String()
		if strings.Contains(out, "suppressed-info") {
			t.Errorf("level=warn failed to suppress info log: %s", out)
		}
	})

	// Test 2: --grep pattern
	t.Run("GrepFiltering", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, specterBin, strconv.Itoa(srv.Port), "--grep=checkout-success", "--no-color")
		var stdout, stderr safeBuffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start specter: %v", err)
		}
		defer func() {
			cancel()
			_ = cmd.Wait()
		}()

		attached := waitForCondition(t, 15*time.Second, func() bool {
			return strings.Contains(stderr.String(), "Attaching to PID")
		})
		if !attached {
			t.Fatalf("Specter did not attach within timeout. Stderr:\n%s", stderr.String())
		}

		mismatchURL := fmt.Sprintf("http://127.0.0.1:%d/json?msg=unrelated-browse", srv.Port)
		matchURL := fmt.Sprintf("http://127.0.0.1:%d/json?msg=checkout-success-order-99", srv.Port)

		foundMatch := waitForCondition(t, 15*time.Second, func() bool {
			resp, err := http.Get(mismatchURL)
			if err == nil {
				_ = resp.Body.Close()
			}
			resp, err = http.Get(matchURL)
			if err == nil {
				_ = resp.Body.Close()
			}
			time.Sleep(150 * time.Millisecond)
			return strings.Contains(stdout.String(), "checkout-success-order-99")
		})
		if !foundMatch {
			t.Fatalf("grep did not render matching message within timeout.\nStdout:\n%s\nStderr:\n%s", stdout.String(), stderr.String())
		}

		out := stdout.String()
		if strings.Contains(out, "unrelated-browse") {
			t.Errorf("grep failed to filter out mismatching message: %s", out)
		}
	})

	// Test 3: --stderr only
	t.Run("StderrOnly", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, specterBin, strconv.Itoa(srv.Port), "--stderr", "--no-color")
		var stdout, stderr safeBuffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start specter: %v", err)
		}
		defer func() {
			cancel()
			_ = cmd.Wait()
		}()

		attached := waitForCondition(t, 15*time.Second, func() bool {
			return strings.Contains(stderr.String(), "Attaching to PID")
		})
		if !attached {
			t.Fatalf("Specter did not attach within timeout. Stderr:\n%s", stderr.String())
		}

		stdoutURL := fmt.Sprintf("http://127.0.0.1:%d/json?msg=stdout-payload", srv.Port)
		stderrURL := fmt.Sprintf("http://127.0.0.1:%d/error", srv.Port)

		foundStderr := waitForCondition(t, 15*time.Second, func() bool {
			resp, err := http.Get(stdoutURL)
			if err == nil {
				_ = resp.Body.Close()
			}
			resp, err = http.Get(stderrURL)
			if err == nil {
				_ = resp.Body.Close()
			}
			time.Sleep(150 * time.Millisecond)
			return strings.Contains(stdout.String(), "ValueError: payment authorization failed")
		})
		if !foundStderr {
			t.Fatalf("--stderr did not render stderr trace within timeout.\nStdout:\n%s\nStderr:\n%s", stdout.String(), stderr.String())
		}

		out := stdout.String()
		if strings.Contains(out, "stdout-payload") {
			t.Errorf("--stderr failed to suppress stdout logs: %s", out)
		}
	})
}

func TestE2E_TreeTracer(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/specter-cli/specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/specter-cli/specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, specterBin, strconv.Itoa(srv.Port), "--tree", "--no-color")
	var stdout, stderr safeBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start specter with --tree: %v", err)
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()

	attached := waitForCondition(t, 15*time.Second, func() bool {
		return strings.Contains(stderr.String(), "Attaching to PID")
	})
	if !attached {
		t.Fatalf("Specter did not attach within timeout. Stderr:\n%s", stderr.String())
	}

	forkURL := fmt.Sprintf("http://127.0.0.1:%d/fork", srv.Port)
	foundChildLog := waitForCondition(t, 15*time.Second, func() bool {
		resp, err := http.Get(forkURL)
		if err == nil {
			_ = resp.Body.Close()
		}
		time.Sleep(250 * time.Millisecond)
		return strings.Contains(stdout.String(), "child worker active")
	})
	if !foundChildLog {
		t.Errorf("Specter --tree failed to capture logs from forked child worker.\nStdout:\n%s\nStderr:\n%s", stdout.String(), stderr.String())
	}
}

func TestE2E_CleanDetachment(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/specter-cli/specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/specter-cli/specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, specterBin, strconv.Itoa(srv.Port), "--no-color")
	var stdout, stderr safeBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start specter: %v", err)
	}

	attached := waitForCondition(t, 15*time.Second, func() bool {
		return strings.Contains(stderr.String(), "Attaching to PID")
	})
	if !attached {
		t.Fatalf("Specter did not attach within timeout. Stderr:\n%s", stderr.String())
	}

	// Verify active capture handshake before detachment
	probeURL := fmt.Sprintf("http://127.0.0.1:%d/json?msg=pre-detach-probe", srv.Port)
	capturedProbe := waitForCondition(t, 15*time.Second, func() bool {
		resp, err := http.Get(probeURL)
		if err == nil {
			_ = resp.Body.Close()
		}
		time.Sleep(150 * time.Millisecond)
		return strings.Contains(stdout.String(), "pre-detach-probe")
	})
	if !capturedProbe {
		t.Fatalf("Specter failed to capture probe log before detachment.\nStdout:\n%s\nStderr:\n%s", stdout.String(), stderr.String())
	}

	// Cancel context to simulate SIGINT / termination
	cancel()
	_ = cmd.Wait()

	// Assert test server is still alive and healthy post-detachment
	healthResp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", srv.Port))
	if err != nil {
		t.Fatalf("target server died or became unreachable after Specter detached: %v", err)
	}
	_ = healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK {
		t.Errorf("expected server to remain 200 OK after detach, got %d", healthResp.StatusCode)
	}
}

func TestE2E_TargetExitDetection(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/specter-cli/specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/specter-cli/specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, specterBin, strconv.Itoa(srv.Port), "--no-color")
	var stdout, stderr safeBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start specter: %v", err)
	}

	attached := waitForCondition(t, 15*time.Second, func() bool {
		return strings.Contains(stderr.String(), "Attaching to PID")
	})
	if !attached {
		t.Fatalf("Specter did not attach within timeout. Stderr:\n%s", stderr.String())
	}

	// Verify active capture handshake before triggering target exit
	probeURL := fmt.Sprintf("http://127.0.0.1:%d/json?msg=pre-exit-probe", srv.Port)
	capturedProbe := waitForCondition(t, 15*time.Second, func() bool {
		resp, err := http.Get(probeURL)
		if err == nil {
			_ = resp.Body.Close()
		}
		time.Sleep(150 * time.Millisecond)
		return strings.Contains(stdout.String(), "pre-exit-probe")
	})
	if !capturedProbe {
		t.Fatalf("Specter failed to capture probe log before exit.\nStdout:\n%s\nStderr:\n%s", stdout.String(), stderr.String())
	}

	// Trigger target exit
	exitResp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/exit", srv.Port))
	if err == nil {
		_ = exitResp.Body.Close()
	}

	// Specter should exit cleanly when target process exits
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil && !strings.Contains(err.Error(), "exit status") {
			t.Logf("Specter exited with: %v (expected upon target termination)", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("Specter did not terminate within timeout after target exited")
	}
}
