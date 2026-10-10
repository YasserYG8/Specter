package file

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/specter-cli/specter/internal/source"
)

func TestIsRegularFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.log")
	if err := os.WriteFile(filePath, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if !IsRegularFile(filePath) {
		t.Errorf("expected %s to be recognized as regular file", filePath)
	}

	if IsRegularFile(tmpDir) {
		t.Errorf("expected directory %s NOT to be recognized as regular file", tmpDir)
	}

	if IsRegularFile("/dev/pts/1") {
		t.Errorf("expected /dev/pts/1 NOT to be regular file")
	}

	if IsRegularFile("pipe:[12345]") {
		t.Errorf("expected pipe:[12345] NOT to be regular file")
	}
}

func TestFileSource_LiveTailAppendedBytes(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "app.log")

	// 1. Write pre-existing data (must be skipped by seek-to-end)
	if err := os.WriteFile(logPath, []byte("Old data from earlier\n"), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open file for append: %v", err)
	}
	defer f.Close()

	s := NewSource(10 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	target := source.Target{
		PID:    1234,
		Stdout: logPath,
	}

	chunks, errs := s.Start(ctx, target)

	// Small pause to allow reader to seek to end
	time.Sleep(50 * time.Millisecond)

	// 2. Append new data
	if _, err := f.WriteString("New live log line\n"); err != nil {
		t.Fatalf("failed to append to file: %v", err)
	}

	select {
	case chunk := <-chunks:
		if string(chunk.Bytes) != "New live log line\n" {
			t.Errorf("expected 'New live log line\\n', got %q", string(chunk.Bytes))
		}
		if chunk.FD != 1 {
			t.Errorf("expected FD 1, got %d", chunk.FD)
		}
	case err := <-errs:
		t.Fatalf("unexpected error from source: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for appended log chunk")
	}

	if err := s.Stop(); err != nil {
		t.Errorf("unexpected error on Stop(): %v", err)
	}
}

func TestFileSource_NoFilesError(t *testing.T) {
	s := NewSource()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	target := source.Target{
		PID:    999999,
		Stdout: "/dev/pts/1",
		Stderr: "pipe:[123]",
	}

	chunks, errs := s.Start(ctx, target)
	err := <-errs
	if err == nil {
		t.Error("expected error when no regular files, got nil")
	}

	for range chunks {
	}
}
