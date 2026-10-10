package journal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/specter-cli/specter/internal/source"
)

func TestExtractSystemdUnit(t *testing.T) {
	sample := `0::/system.slice/nginx.service
`
	unit := ExtractSystemdUnit(sample)
	if unit != "nginx.service" {
		t.Errorf("expected 'nginx.service', got %q", unit)
	}

	sample2 := `0::/system.slice/docker-1234.scope
`
	unit2 := ExtractSystemdUnit(sample2)
	if unit2 != "" {
		t.Errorf("expected empty string for non-service slice, got %q", unit2)
	}
}

func TestDetectSystemdUnit_MockProc(t *testing.T) {
	tmpDir := t.TempDir()
	pidDir := filepath.Join(tmpDir, "5555")
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		t.Fatalf("failed to create mock proc dir: %v", err)
	}

	cgroupContent := "0::/system.slice/redis-server.service\n"
	if err := os.WriteFile(filepath.Join(pidDir, "cgroup"), []byte(cgroupContent), 0644); err != nil {
		t.Fatalf("failed to write mock cgroup: %v", err)
	}

	unit, err := DetectSystemdUnit(5555, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit != "redis-server.service" {
		t.Errorf("expected 'redis-server.service', got %q", unit)
	}
}

func TestJournalSource_NameAndStop(t *testing.T) {
	s := NewSource()
	if s.Name() != "journal" {
		t.Errorf("expected Name() to be 'journal', got %q", s.Name())
	}

	if err := s.Stop(); err != nil {
		t.Errorf("unexpected error on Stop(): %v", err)
	}
}

func TestJournalSource_InvalidTarget(t *testing.T) {
	s := NewSource("echo")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	target := source.Target{
		PID: 0,
	}

	chunks, errs := s.Start(ctx, target)
	err := <-errs
	if err == nil {
		t.Error("expected error for empty target, got nil")
	}

	for range chunks {
	}
}
