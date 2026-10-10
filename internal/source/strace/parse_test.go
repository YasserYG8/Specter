package strace

import (
	"testing"
	"time"
)

func TestParser_StandardStdoutAndStderr(t *testing.T) {
	p := NewParser(1000)
	now := time.Now()

	// 1. Stdout write
	chunk, err := p.ParseLine(`write(1, "\x48\x65\x6c\x6c\x6f\x0a", 6) = 6`, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk == nil {
		t.Fatal("expected chunk, got nil")
	}
	if chunk.FD != 1 {
		t.Errorf("expected FD 1, got %d", chunk.FD)
	}
	if string(chunk.Bytes) != "Hello\n" {
		t.Errorf("expected 'Hello\\n', got %q", string(chunk.Bytes))
	}
	if chunk.PID != 1000 {
		t.Errorf("expected PID 1000, got %d", chunk.PID)
	}

	// 2. Stderr write with explicit [pid N] prefix
	chunk, err = p.ParseLine(`[pid 2048] write(2, "\x45\x72\x72\x6f\x72", 5) = 5`, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk == nil {
		t.Fatal("expected chunk, got nil")
	}
	if chunk.FD != 2 {
		t.Errorf("expected FD 2, got %d", chunk.FD)
	}
	if string(chunk.Bytes) != "Error" {
		t.Errorf("expected 'Error', got %q", string(chunk.Bytes))
	}
	if chunk.PID != 2048 {
		t.Errorf("expected PID 2048, got %d", chunk.PID)
	}

	// 3. Ignored fd (fd 3)
	chunk, err = p.ParseLine(`write(3, "\x01\x02", 2) = 2`, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk != nil {
		t.Errorf("expected nil chunk for fd 3, got: %+v", chunk)
	}

	// 4. Failed write (-1 EPIPE)
	chunk, err = p.ParseLine(`write(1, "\x61", 1) = -1 EPIPE`, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk != nil {
		t.Errorf("expected nil chunk for failed write, got: %+v", chunk)
	}
}

func TestParser_MultiThreadUnfinishedAndResumed(t *testing.T) {
	p := NewParser(100)
	now := time.Now()

	// Thread 101 starts write(1, "He" <unfinished ...>
	c1, err := p.ParseLine(`[pid 101] write(1, "\x48\x65" <unfinished ...>`, now)
	if err != nil || c1 != nil {
		t.Fatalf("expected nil chunk for unfinished call, got c1=%v, err=%v", c1, err)
	}

	// Thread 102 does a complete write in between
	c2, err := p.ParseLine(`[pid 102] write(1, "\x57\x6f\x72\x6c\x64\x0a", 6) = 6`, now)
	if err != nil || c2 == nil {
		t.Fatalf("expected complete chunk for thread 102, got c2=%v, err=%v", c2, err)
	}
	if string(c2.Bytes) != "World\n" || c2.PID != 102 {
		t.Errorf("unexpected thread 102 chunk: %s (PID: %d)", string(c2.Bytes), c2.PID)
	}

	// Thread 101 resumes: <... write resumed> "llo\n", 6) = 6
	c3, err := p.ParseLine(`[pid 101] <... write resumed> "\x6c\x6c\x6f\x0a", 6) = 6`, now)
	if err != nil || c3 == nil {
		t.Fatalf("expected completed resumed chunk for thread 101, got c3=%v, err=%v", c3, err)
	}
	if string(c3.Bytes) != "Hello\n" {
		t.Errorf("expected 'Hello\\n' for thread 101, got %q", string(c3.Bytes))
	}
	if c3.PID != 101 {
		t.Errorf("expected PID 101, got %d", c3.PID)
	}
}

func TestParser_MultibyteUTF8(t *testing.T) {
	p := NewParser(1)
	now := time.Now()

	// Ghost emoji 👻 is \xf0\x9f\x91\xbb
	chunk, err := p.ParseLine(`write(1, "\xf0\x9f\x91\xbb\x0a", 5) = 5`, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk == nil {
		t.Fatal("expected chunk, got nil")
	}
	if string(chunk.Bytes) != "👻\n" {
		t.Errorf("expected '👻\\n', got %q", string(chunk.Bytes))
	}
}

func TestDecodeStraceHex_Truncation(t *testing.T) {
	// String truncated by strace ending with ...
	bytes, err := DecodeStraceHex(`\x41\x42...`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(bytes) != "AB" {
		t.Errorf("expected 'AB', got %q", string(bytes))
	}
}
