package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func createMockProc(t *testing.T, processes map[int]int) string {
	t.Helper()
	tempDir := t.TempDir()

	// Create non-numeric directories to simulate real /proc (sys, net, self)
	_ = os.MkdirAll(filepath.Join(tempDir, "sys"), 0755)
	_ = os.MkdirAll(filepath.Join(tempDir, "net"), 0755)

	for pid, ppid := range processes {
		procDir := filepath.Join(tempDir, fmt.Sprintf("%d", pid))
		if err := os.MkdirAll(procDir, 0755); err != nil {
			t.Fatalf("failed to create proc dir for PID %d: %v", pid, err)
		}

		statusContent := fmt.Sprintf("Name:\tprocess-%d\nState:\tS (sleeping)\nPPid:\t%d\nUid:\t1000\t1000\n", pid, ppid)
		statusPath := filepath.Join(procDir, "status")
		if err := os.WriteFile(statusPath, []byte(statusContent), 0644); err != nil {
			t.Fatalf("failed to write status file for PID %d: %v", pid, err)
		}
	}

	return tempDir
}

func TestFindChildPIDs(t *testing.T) {
	// Setup process tree:
	// 596 (supervisor)
	//   ├── 629 (worker 1)
	//   └── 630 (worker 2)
	// 999 (unrelated process)
	mockProc := createMockProc(t, map[int]int{
		596: 1,   // Parent is init
		629: 596, // Child of 596
		630: 596, // Child of 596
		999: 1,   // Unrelated
	})

	inspector := NewProcessInspector(mockProc)

	t.Run("discovers multiple direct children", func(t *testing.T) {
		children, err := inspector.FindChildPIDs(596)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		expected := []int{629, 630}
		if !reflect.DeepEqual(children, expected) {
			t.Errorf("expected children %v, got %v", expected, children)
		}
	})

	t.Run("returns empty slice when process has no children", func(t *testing.T) {
		children, err := inspector.FindChildPIDs(629)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if len(children) != 0 {
			t.Errorf("expected 0 children for worker, got %v", children)
		}
	})

	t.Run("returns empty slice for non-existent parent PID", func(t *testing.T) {
		children, err := inspector.FindChildPIDs(8888)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if len(children) != 0 {
			t.Errorf("expected 0 children for non-existent PID, got %v", children)
		}
	})
}

func TestFindChildProcessTree_Recursive(t *testing.T) {
	// Setup 3-tier tree:
	// 100 (master)
	//   └── 200 (supervisor)
	//         └── 300 (worker)
	mockProc := createMockProc(t, map[int]int{
		100: 1,
		200: 100,
		300: 200,
	})

	inspector := NewProcessInspector(mockProc)

	t.Run("recursive tree discovery", func(t *testing.T) {
		descendants := inspector.FindChildProcessTree(100, 3)
		expected := []int{200, 300}
		if !reflect.DeepEqual(descendants, expected) {
			t.Errorf("expected descendants %v, got %v", expected, descendants)
		}
	})
}
