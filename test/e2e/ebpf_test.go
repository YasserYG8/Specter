package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	cebpf "github.com/cilium/ebpf"
)

// Tests for what only the eBPF engine does. They skip, with the reason, on a machine that cannot run it, unless
// SPECTER_REQUIRE_EBPF=1.

// specterPrograms are the names of the BPF programs that Specter loads.
var specterPrograms = map[string]bool{
	"on_write_enter": true, "on_write_exit": true, "on_writev_enter": true, "on_writev_exit": true,
	"on_fork": true, "on_exit": true,
}

// loadedSpecterPrograms counts the loaded BPF programs that have one of Specter's names.
func loadedSpecterPrograms(t *testing.T) int {
	t.Helper()
	count := 0
	id := cebpf.ProgramID(0)
	for {
		next, err := cebpf.ProgramGetNextID(id)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return count
			}
			t.Fatalf("cannot list the loaded BPF programs: %v", err)
		}
		id = next
		prog, err := cebpf.NewProgramFromID(id)
		if err != nil {
			continue // it was unloaded between the two calls
		}
		if info, err := prog.Info(); err == nil && specterPrograms[info.Name] {
			count++
		}
		_ = prog.Close()
	}
}

// Run as root on a machine that can run eBPF, Specter picks it without being asked, says so, prints no slowdown
// notice, and leaves no tracer on the target.
func TestE2E_EBPFIsChosenAutomatically(t *testing.T) {
	requireLinux(t)
	requireEBPF(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")
	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	previous := currentEngine
	currentEngine = "auto" // Specter chooses
	defer func() { currentEngine = previous }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	stdout, stderr, stop := attachSpecter(t, ctx, specterBin, srv)
	defer stop()

	httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/json?msg=chosen-by-itself", srv.Port))
	if !waitForCondition(t, 20*time.Second, func() bool { return strings.Contains(stdout.String(), "chosen-by-itself") }) {
		t.Fatalf("the line was not captured.\nStderr:\n%s", stderr.String())
	}

	text := stderr.String()
	if !strings.Contains(text, "Source: ebpf") {
		t.Errorf("the Source: line should say ebpf.\nStderr:\n%s", text)
	}
	if strings.Contains(text, "slows the target") {
		t.Errorf("the eBPF engine does not slow the target, so there is no slowdown notice.\nStderr:\n%s", text)
	}
	if n := tracerPID(t, srv.PID); n != 0 {
		t.Errorf("the target has tracer %d, but eBPF never traces", n)
	}
}

// A target that another tool already traces (gdb, or another strace) can still be watched with eBPF, which is not a
// tracer, and there is no "already traced" diagnostic.
func TestE2E_EBPFCapturesATargetThatIsAlreadyTraced(t *testing.T) {
	requireLinux(t)
	requireEBPF(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")
	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	// Another strace traces the server and prints nothing
	tracer := exec.Command("strace", "-f", "-p", strconv.Itoa(srv.PID), "-e", "trace=none", "-o", "/dev/null")
	if err := tracer.Start(); err != nil {
		t.Fatalf("cannot start the other tracer: %v", err)
	}
	defer func() {
		_ = tracer.Process.Signal(os.Interrupt)
		_ = tracer.Wait()
	}()
	if !waitForCondition(t, 20*time.Second, func() bool { return tracerPID(t, srv.PID) == tracer.Process.Pid }) {
		t.Fatalf("the other strace did not become the tracer of PID %d", srv.PID)
	}

	previous := currentEngine
	currentEngine = "ebpf"
	defer func() { currentEngine = previous }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	stdout, stderr, stop := attachSpecter(t, ctx, specterBin, srv)
	defer stop()

	httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/json?msg=while-traced", srv.Port))
	if !waitForCondition(t, 20*time.Second, func() bool { return strings.Contains(stdout.String(), "while-traced") }) {
		t.Fatalf("a traced target should still be captured.\nStderr:\n%s", stderr.String())
	}
	if strings.Contains(stderr.String(), "already being traced") {
		t.Errorf("eBPF must not report 'already traced'.\nStderr:\n%s", stderr.String())
	}
	if n := tracerPID(t, srv.PID); n != tracer.Process.Pid {
		t.Errorf("the tracer of the server changed to %d: eBPF must not touch it", n)
	}
}

// When the target writes faster than Specter reads, Specter says how many writes were lost, on stderr, and never
// loses them in silence. A ring buffer of one page makes any write of a few KB overflow it.
func TestE2E_EBPFTellsHowManyWritesWereLost(t *testing.T) {
	requireLinux(t)
	requireEBPF(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")
	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	previous := currentEngine
	currentEngine = "ebpf"
	defer func() { currentEngine = previous }()

	cmd := specterCommand(ctx, specterBin, strconv.Itoa(srv.Port), "--no-color")
	cmd.Env = append(os.Environ(), "SPECTER_EBPF_RING_BYTES=4096")
	var stdout, stderr safeBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start specter: %v", err)
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()
	if !waitForCondition(t, 30*time.Second, func() bool { return strings.Contains(stderr.String(), "Source:") }) {
		t.Fatalf("Specter did not attach.\nStderr:\n%s", stderr.String())
	}
	time.Sleep(time.Second)

	for i := 0; i < 5; i++ {
		httpGet(t, fmt.Sprintf("http://127.0.0.1:%d/longline?bytes=30000", srv.Port))
	}

	const want = "writes were lost because the target wrote faster than Specter could read."
	if !waitForCondition(t, 10*time.Second, func() bool { return strings.Contains(stderr.String(), want) }) {
		t.Fatalf("no notice about lost writes.\nStderr:\n%s", stderr.String())
	}
	if strings.Contains(stdout.String(), "writes were lost") {
		t.Errorf("the notice must go to stderr, not stdout")
	}
}

// The kernel removes Specter's BPF programs when Specter dies, even when it cannot clean up after itself, and the
// target keeps running.
func TestE2E_EBPFProgramsAreRemovedAfterSIGKILL(t *testing.T) {
	requireLinux(t)
	requireEBPF(t)

	specterBin := getBinary(t, "SPECTER_BIN", "github.com/YasserYG8/Specter/cmd/specter", "specter")
	serverBin := getBinary(t, "TESTSERVER_BIN", "github.com/YasserYG8/Specter/test/e2e/testserver", "testserver")
	srv, cleanupServer := startTestServer(t, serverBin)
	defer cleanupServer()

	if n := loadedSpecterPrograms(t); n != 0 {
		t.Skipf("%d Specter BPF programs are loaded already (another Specter on this machine?)", n)
	}

	previous := currentEngine
	currentEngine = "ebpf"
	defer func() { currentEngine = previous }()

	cmd, _, stderr := startKillableSpecter(t, specterBin, srv)
	if n := loadedSpecterPrograms(t); n < len(specterPrograms) {
		t.Fatalf("expected Specter's %d programs to be loaded while it runs, found %d.\nStderr:\n%s", len(specterPrograms), n, stderr.String())
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("cannot kill specter: %v", err)
	}
	_ = cmd.Wait()

	if !waitForCondition(t, time.Second, func() bool { return loadedSpecterPrograms(t) == 0 }) {
		t.Fatalf("%d of Specter's BPF programs are still loaded one second after it was killed", loadedSpecterPrograms(t))
	}
	if err := serverAnswers(srv); err != nil {
		t.Errorf("the server does not answer after Specter was killed: %v", err)
	}
}
