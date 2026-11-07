package procwatch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testPeriod = 10 * time.Millisecond

// writeStat writes a /proc/<pid>/stat file in a fake proc tree. The layout is the kernel's: the command name in
// parentheses, then the state, then more fields up to field 22, the start time.
func writeStat(t *testing.T, root string, pid int, comm string, state string, start int) {
	t.Helper()
	dir := filepath.Join(root, fmt.Sprint(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Fields 4 to 21 are 18 numbers, then field 22 is the start time
	line := fmt.Sprintf("%d (%s) %s", pid, comm, state)
	for i := 4; i <= 21; i++ {
		line += " 0"
	}
	line += fmt.Sprintf(" %d 1234 5678\n", start)
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// waitClosed waits for the channel to close, and reports whether it did within the timeout.
func waitClosed(ch <-chan struct{}, timeout time.Duration) bool {
	select {
	case <-ch:
		return true
	case <-time.After(timeout):
		return false
	}
}

func TestWatch_ProcessPresent(t *testing.T) {
	root := t.TempDir()
	writeStat(t, root, 100, "node", "S", 5000)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gone := Watch(ctx, 100, Options{ProcRoot: root, Period: testPeriod})

	if waitClosed(gone, 15*testPeriod) {
		t.Fatal("a running process was reported as gone")
	}
}

func TestWatch_ProcessRemoved(t *testing.T) {
	root := t.TempDir()
	writeStat(t, root, 100, "node", "S", 5000)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gone := Watch(ctx, 100, Options{ProcRoot: root, Period: testPeriod})

	if err := os.RemoveAll(filepath.Join(root, "100")); err != nil {
		t.Fatal(err)
	}
	if !waitClosed(gone, time.Second) {
		t.Fatal("a removed process was not reported as gone")
	}
}

func TestWatch_AlreadyGone(t *testing.T) {
	gone := Watch(context.Background(), 100, Options{ProcRoot: t.TempDir(), Period: testPeriod})
	if !isClosed(gone) {
		t.Fatal("a process that does not exist should be reported as gone at once")
	}
}

func TestWatch_ZombieAndDeadCountAsGone(t *testing.T) {
	for _, state := range []string{"Z", "X"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			writeStat(t, root, 100, "node", "S", 5000)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			gone := Watch(ctx, 100, Options{ProcRoot: root, Period: testPeriod})

			// The process ends, but its parent has not collected it, so /proc/100 still exists
			writeStat(t, root, 100, "node", state, 5000)
			if !waitClosed(gone, time.Second) {
				t.Fatalf("a process in state %s was not reported as gone", state)
			}
		})
	}
}

func TestWatch_PIDReusedByAnotherProcess(t *testing.T) {
	root := t.TempDir()
	writeStat(t, root, 100, "node", "S", 5000)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gone := Watch(ctx, 100, Options{ProcRoot: root, Period: testPeriod})

	// The target ends and a new process gets PID 100 before the next check: alive, but another start time
	writeStat(t, root, 100, "python3", "S", 9999)
	if !waitClosed(gone, time.Second) {
		t.Fatal("a reused PID was not reported as the target being gone")
	}
}

func TestWatch_OtherStatesAreAlive(t *testing.T) {
	for _, state := range []string{"R", "S", "D", "T", "I"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			writeStat(t, root, 100, "node", state, 5000)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if waitClosed(Watch(ctx, 100, Options{ProcRoot: root, Period: testPeriod}), 8*testPeriod) {
				t.Fatalf("a process in state %s is alive", state)
			}
		})
	}
}

func TestWatch_CommandNameWithSpacesAndParentheses(t *testing.T) {
	root := t.TempDir()
	writeStat(t, root, 100, "my (odd) name", "S", 5000)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gone := Watch(ctx, 100, Options{ProcRoot: root, Period: testPeriod})
	if waitClosed(gone, 8*testPeriod) {
		t.Fatal("a command name with parentheses broke the parsing of a live process")
	}

	writeStat(t, root, 100, "my (odd) name", "Z", 5000)
	if !waitClosed(gone, time.Second) {
		t.Fatal("the zombie state was not found after a command name with parentheses")
	}
}

func TestWatch_UnreadableStatIsNotGone(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "100")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if waitClosed(Watch(ctx, 100, Options{ProcRoot: root, Period: testPeriod}), 8*testPeriod) {
		t.Fatal("a stat file that cannot be parsed must not be taken for the process being gone")
	}
}

func TestWatch_StopsWhenContextIsCanceled(t *testing.T) {
	root := t.TempDir()
	writeStat(t, root, 100, "node", "S", 5000)

	ctx, cancel := context.WithCancel(context.Background())
	gone := Watch(ctx, 100, Options{ProcRoot: root, Period: testPeriod})
	cancel()
	time.Sleep(5 * testPeriod)

	// After cancel the watcher is no longer looking, so a later change is not reported
	if err := os.RemoveAll(filepath.Join(root, "100")); err != nil {
		t.Fatal(err)
	}
	if waitClosed(gone, 8*testPeriod) {
		t.Fatal("the watcher kept running after its context was canceled")
	}
}
