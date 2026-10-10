package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/specter-cli/specter/internal/source"
)

func TestLineAssembler_SplitChunksReassembled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	la := NewLineAssembler(100 * time.Millisecond)
	chunks := make(chan source.Chunk, 10)

	lines := la.Assemble(ctx, chunks)

	// Send split chunks
	now := time.Now()
	chunks <- source.Chunk{Bytes: []byte("Hello "), FD: 1, PID: 42, TS: now}
	chunks <- source.Chunk{Bytes: []byte("World!\n"), FD: 1, PID: 42, TS: now}
	close(chunks)

	var result []Line
	for l := range lines {
		result = append(result, l)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 line, got %d", len(result))
	}
	if result[0].Text != "Hello World!" {
		t.Errorf("expected 'Hello World!', got %q", result[0].Text)
	}
	if result[0].FD != 1 || result[0].PID != 42 {
		t.Errorf("unexpected FD/PID: %+v", result[0])
	}
	if result[0].IsPartial {
		t.Errorf("expected complete line, got partial")
	}
}

func TestLineAssembler_MultiThreadIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	la := NewLineAssembler(100 * time.Millisecond)
	chunks := make(chan source.Chunk, 10)

	lines := la.Assemble(ctx, chunks)

	now := time.Now()
	// Thread 10 writes partial
	chunks <- source.Chunk{Bytes: []byte("T10: start - "), FD: 1, PID: 10, TS: now}
	// Thread 20 writes complete line in between
	chunks <- source.Chunk{Bytes: []byte("T20: done\n"), FD: 1, PID: 20, TS: now}
	// Thread 10 finishes
	chunks <- source.Chunk{Bytes: []byte("end\n"), FD: 1, PID: 10, TS: now}
	close(chunks)

	var result []Line
	for l := range lines {
		result = append(result, l)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(result))
	}

	// Verify both threads have clean lines without character interleaving
	linesByPID := make(map[int]string)
	for _, l := range result {
		linesByPID[l.PID] = l.Text
	}

	if linesByPID[10] != "T10: start - end" {
		t.Errorf("expected thread 10 'T10: start - end', got %q", linesByPID[10])
	}
	if linesByPID[20] != "T20: done" {
		t.Errorf("expected thread 20 'T20: done', got %q", linesByPID[20])
	}
}

func TestLineAssembler_MultibyteUTF8Split(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	la := NewLineAssembler(100 * time.Millisecond)
	chunks := make(chan source.Chunk, 10)

	lines := la.Assemble(ctx, chunks)

	// Ghost emoji 👻 is 4 bytes: 0xF0 0x9F 0x91 0xBB
	ghost := []byte("👻\n")
	part1 := ghost[:2] // first 2 bytes
	part2 := ghost[2:] // remaining bytes + \n

	now := time.Now()
	chunks <- source.Chunk{Bytes: part1, FD: 1, PID: 1, TS: now}
	chunks <- source.Chunk{Bytes: part2, FD: 1, PID: 1, TS: now}
	close(chunks)

	var result []Line
	for l := range lines {
		result = append(result, l)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 line, got %d", len(result))
	}
	if result[0].Text != "👻" {
		t.Errorf("expected '👻', got %q", result[0].Text)
	}
}

func TestLineAssembler_IdleTimeoutFlush(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Short idle timeout: 50ms
	la := NewLineAssembler(50 * time.Millisecond)
	chunks := make(chan source.Chunk, 10)

	lines := la.Assemble(ctx, chunks)

	// Write without newline
	chunks <- source.Chunk{Bytes: []byte("Password: "), FD: 1, PID: 99, TS: time.Now()}

	// Wait for idle timer to fire
	select {
	case line := <-lines:
		if line.Text != "Password: " {
			t.Errorf("expected 'Password: ', got %q", line.Text)
		}
		if !line.IsPartial {
			t.Errorf("expected partial line flag to be true")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for idle flush")
	}

	close(chunks)
}
