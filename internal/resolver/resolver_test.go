package resolver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/specter-cli/specter/internal/util"
)

func TestPortResolver_LsofSuccess(t *testing.T) {
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "lsof" {
			return []byte("4920\n"), nil
		}
		return nil, errors.New("command not found")
	}

	pr := NewPortResolver(mockRunner)
	pid, err := pr.ResolvePortToPID(context.Background(), 3000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 4920 {
		t.Errorf("expected PID 4920, got %d", pid)
	}
}

func TestPortResolver_SSFallback(t *testing.T) {
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "lsof" {
			return nil, errors.New("lsof: command not found")
		}
		if name == "ss" {
			sampleOutput := "LISTEN 0 511 0.0.0.0:8080 0.0.0.0:* users:((\"node\",pid=12345,fd=19))\n"
			return []byte(sampleOutput), nil
		}
		return nil, errors.New("unknown command")
	}

	pr := NewPortResolver(mockRunner)
	pid, err := pr.ResolvePortToPID(context.Background(), 8080)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 12345 {
		t.Errorf("expected PID 12345, got %d", pid)
	}
}

func TestPortResolver_SSEveryPortDistinct(t *testing.T) {
	// Verify that port 3000 does NOT match port 13000 or 30000
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "lsof" {
			return nil, errors.New("lsof not found")
		}
		if name == "ss" {
			// Contains 13000 and 30000, but NOT 3000
			sampleOutput := "LISTEN 0 128 0.0.0.0:13000 0.0.0.0:* users:((\"python\",pid=9999,fd=3))\n" +
				"LISTEN 0 128 0.0.0.0:30000 0.0.0.0:* users:((\"java\",pid=8888,fd=4))\n"
			return []byte(sampleOutput), nil
		}
		return nil, errors.New("unknown command")
	}

	pr := NewPortResolver(mockRunner)
	_, err := pr.ResolvePortToPID(context.Background(), 3000)
	if err == nil {
		t.Fatal("expected error, port 3000 should not match 13000 or 30000")
	}
	if !errors.Is(err, util.ErrNoProcessFound) {
		t.Errorf("expected ErrNoProcessFound, got %v", err)
	}
}

func TestPortResolver_SSPermissionDenied(t *testing.T) {
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "lsof" {
			return nil, errors.New("lsof not found")
		}
		if name == "ss" {
			// Port 80 is listening, but users:(...) was stripped because running without sudo!
			sampleOutput := "LISTEN 0 511 0.0.0.0:80 0.0.0.0:*\n"
			return []byte(sampleOutput), nil
		}
		return nil, errors.New("unknown command")
	}

	pr := NewPortResolver(mockRunner)
	_, err := pr.ResolvePortToPID(context.Background(), 80)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, util.ErrPermissionDenied) {
		t.Errorf("expected ErrPermissionDenied, got %v", err)
	}
}

func TestPortResolver_NotFound(t *testing.T) {
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(""), nil
	}

	pr := NewPortResolver(mockRunner)
	_, err := pr.ResolvePortToPID(context.Background(), 9999)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, util.ErrNoProcessFound) {
		t.Errorf("expected ErrNoProcessFound, got: %v", err)
	}
}

func TestProcessInspector_Inspect(t *testing.T) {
	tmpDir := t.TempDir()
	procDir := filepath.Join(tmpDir, "5555")
	if err := os.MkdirAll(filepath.Join(procDir, "fd"), 0755); err != nil {
		t.Fatal(err)
	}

	// Write cmdline with null delimiters
	cmdline := []byte("node\x00server.js\x00--port\x003000\x00")
	if err := os.WriteFile(filepath.Join(procDir, "cmdline"), cmdline, 0644); err != nil {
		t.Fatal(err)
	}

	inspector := NewProcessInspector(tmpDir)
	target, err := inspector.InspectPID(context.Background(), 5555)
	if err != nil {
		t.Fatalf("unexpected inspect error: %v", err)
	}

	if target.PID != 5555 {
		t.Errorf("expected PID 5555, got %d", target.PID)
	}
	if len(target.Cmdline) != 4 || target.Cmdline[0] != "node" || target.Cmdline[1] != "server.js" {
		t.Errorf("unexpected cmdline tokens: %v", target.Cmdline)
	}
}

func TestDefaultResolver_ResolvePort(t *testing.T) {
	tmpDir := t.TempDir()
	procDir := filepath.Join(tmpDir, "8888")
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}

	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "lsof" {
			return []byte("8888\n"), nil
		}
		return nil, nil
	}

	resolver := NewDefaultResolver(mockRunner, tmpDir)
	target, err := resolver.ResolvePort(context.Background(), 3000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.Port != 3000 {
		t.Errorf("expected Port 3000, got %d", target.Port)
	}
	if target.PID != 8888 {
		t.Errorf("expected PID 8888, got %d", target.PID)
	}
}
