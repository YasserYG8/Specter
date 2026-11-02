package resolver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// InspectPID must leave Unit empty: being inside a service's cgroup does not mean a process
// logs to the journal. journal.DetectSystemdUnit makes that decision for the source selector.
func TestInspectPID_DoesNotAssumeSystemdUnitFromCgroup(t *testing.T) {
	procRoot := t.TempDir()
	pidDir := filepath.Join(procRoot, "4242")
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		t.Fatalf("failed to create mock proc dir: %v", err)
	}
	cgroup := "0::/system.slice/hosted-compute-agent.service\n"
	if err := os.WriteFile(filepath.Join(pidDir, "cgroup"), []byte(cgroup), 0644); err != nil {
		t.Fatalf("failed to write mock cgroup: %v", err)
	}

	target, err := NewProcessInspector(procRoot).InspectPID(context.Background(), 4242)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.Unit != "" {
		t.Errorf("expected empty Unit, got %q", target.Unit)
	}
}
