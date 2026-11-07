package e2e

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestE2E_PIDFlagAttaches(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	for _, flag := range []string{"--pid", "-p"} {
		t.Run(flag, func(t *testing.T) {
			srv, cleanupServer := startTestServer(t, serverBin)
			defer cleanupServer()

			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()

			cmd := specterCommand(ctx, specterBin, flag, strconv.Itoa(srv.PID), "--no-color")
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

			if !waitForCondition(t, 30*time.Second, func() bool { return strings.Contains(stderr.String(), "Attaching to PID") }) {
				t.Fatalf("Specter did not attach within timeout. Stderr:\n%s", stderr.String())
			}

			url := fmt.Sprintf("http://127.0.0.1:%d/json?msg=e2e-by-pid", srv.Port)
			found := waitForCondition(t, 30*time.Second, func() bool {
				if resp, err := http.Get(url); err == nil {
					_ = resp.Body.Close()
				}
				time.Sleep(150 * time.Millisecond)
				return strings.Contains(stdout.String(), "e2e-by-pid")
			})
			if !found {
				t.Errorf("Specter did not capture the log line.\nStdout:\n%s\nStderr:\n%s", stdout.String(), stderr.String())
			}
		})
	}
}

// A bare number is a port. When nothing listens on it, Specter must not attach to the process whose PID is that
// number. The test server's own PID is the number, so a process with that PID exists for certain.
func TestE2E_EmptyPortNeverAttachesToThePID(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	if srv.PID > 65535 {
		t.Skipf("PID %d is above 65535, so it cannot also be a port", srv.PID)
	}
	if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", srv.PID)); err != nil {
		t.Skipf("port %d is in use, so it is not an empty port: %v", srv.PID, err)
	} else {
		_ = ln.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := specterCommand(ctx, specterBin, strconv.Itoa(srv.PID), "--no-color")
	var stdout, stderr safeBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	if err == nil {
		t.Errorf("expected a non-zero exit for an empty port.\nStderr:\n%s", stderr.String())
	}
	out := stderr.String()
	if strings.Contains(out, "Attaching to PID") || strings.Contains(out, "Source:") {
		t.Errorf("Specter attached to the process with PID %d.\nStderr:\n%s", srv.PID, out)
	}
	for _, want := range []string{
		fmt.Sprintf("No process is listening on port %d.", srv.PID),
		fmt.Sprintf("Hint: PID %d exists (testserver). To attach to it: specter --pid %d", srv.PID, srv.PID),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr is missing %q.\nStderr:\n%s", want, out)
		}
	}

	// The real proof that nothing was attached: no tracer on the process
	status, readErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", srv.PID))
	if readErr != nil {
		t.Fatalf("cannot read the server's status: %v", readErr)
	}
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "TracerPid:") && strings.TrimSpace(strings.TrimPrefix(line, "TracerPid:")) != "0" {
			t.Errorf("the process has a tracer attached: %s", line)
		}
	}
}

// Linux allows one tracer per process. A second Specter on a target that another Specter already traces must say
// so, name the first one, exit with status 1, and leave the first capture working.
func TestE2E_SecondSpecterExplainsAlreadyTraced(t *testing.T) {
	requireLinux(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")

	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// The first Specter attaches and captures
	stdout, _, stop := attachSpecter(t, ctx, specterBin, srv)
	defer stop()

	// The second one asks for the same process
	second := specterCommand(ctx, specterBin, "--pid", strconv.Itoa(srv.PID), "--no-color")
	var secondOut, secondErr safeBuffer
	second.Stdout = &secondOut
	second.Stderr = &secondErr
	err := second.Run()

	text := secondErr.String()
	if code := second.ProcessState.ExitCode(); err == nil || code != 1 {
		t.Errorf("expected exit status 1, got %d (%v).\nStderr:\n%s", code, err, text)
	}
	if want := fmt.Sprintf("PID %d is already being traced by PID", srv.PID); !strings.Contains(text, want) {
		t.Errorf("expected %q.\nStderr:\n%s", want, text)
	}
	if !strings.Contains(text, "(strace") {
		t.Errorf("expected the tracer's command to be named.\nStderr:\n%s", text)
	}
	if !regexp.MustCompile(`started by specter\S* \(PID \d+\)`).MatchString(text) {
		t.Errorf("expected the first Specter to be named.\nStderr:\n%s", text)
	}
	if !strings.Contains(text, "only one tracer per process") || !strings.Contains(text, "Ctrl+C") {
		t.Errorf("expected the explanation and the way out.\nStderr:\n%s", text)
	}
	if strings.Contains(text, "sudo") || strings.Contains(text, "Operation not permitted") {
		t.Errorf("must not give permission advice for an already traced process.\nStderr:\n%s", text)
	}
	if strings.Contains(text, "Detached") {
		t.Errorf("the second Specter never attached, so it must not say it detached.\nStderr:\n%s", text)
	}

	// The first capture still works
	url := fmt.Sprintf("http://127.0.0.1:%d/json?msg=still-capturing", srv.Port)
	found := waitForCondition(t, 30*time.Second, func() bool {
		if resp, err := http.Get(url); err == nil {
			_ = resp.Body.Close()
		}
		time.Sleep(150 * time.Millisecond)
		return strings.Contains(stdout.String(), "still-capturing")
	})
	if !found {
		t.Errorf("the first Specter stopped capturing after the second one failed.\nStdout:\n%s", stdout.String())
	}
}
