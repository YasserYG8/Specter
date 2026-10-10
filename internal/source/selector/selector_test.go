package selector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/specter-cli/specter/internal/source"
)

func TestSelector_ForceSource(t *testing.T) {
	for _, srcName := range []string{"docker", "file", "journal", "strace"} {
		sel := NewSelector(srcName, false)
		src, reason, err := sel.Select(&source.Target{PID: 100})
		if err != nil {
			t.Fatalf("unexpected error for force source %q: %v", srcName, err)
		}
		if src.Name() != srcName {
			t.Errorf("expected source %q, got %q", srcName, src.Name())
		}
		if reason == "" {
			t.Errorf("expected non-empty reason for %q", srcName)
		}
	}
}

func TestSelector_InvalidForceSource(t *testing.T) {
	sel := NewSelector("invalid_source", false)
	_, _, err := sel.Select(&source.Target{PID: 100})
	if err == nil {
		t.Fatal("expected error for invalid source, got nil")
	}
}

func TestSelector_AutoDetectFile(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "server.log")
	if err := os.WriteFile(logFile, []byte("init"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	sel := NewSelector("", false)
	target := &source.Target{
		PID:    100,
		Stdout: logFile,
	}

	src, reason, err := sel.Select(target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.Name() != "file" {
		t.Errorf("expected source 'file', got %q", src.Name())
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestSelector_AutoDetectDefaultStrace(t *testing.T) {
	sel := NewSelector("", true)
	target := &source.Target{
		PID:    100,
		Stdout: "/dev/pts/1",
		Stderr: "/dev/pts/1",
	}

	src, reason, err := sel.Select(target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.Name() != "strace" {
		t.Errorf("expected default source 'strace', got %q", src.Name())
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}
