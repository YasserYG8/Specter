package main

import (
	"context"
	"errors"
	"testing"

	"github.com/YasserYG8/Specter/internal/source"
)

// startFake is a source whose Start behaves as the test says.
type startFake struct {
	startErr error // sent on the error channel at once, like a source that fails while starting
	started  bool
}

func (f *startFake) Name() string { return "fake" }
func (f *startFake) Stop() error  { return nil }
func (f *startFake) Start(ctx context.Context, target source.Target) (<-chan source.Chunk, <-chan error) {
	f.started = true
	chunks := make(chan source.Chunk)
	errs := make(chan error, 1)
	if f.startErr != nil {
		errs <- f.startErr
		close(chunks)
		close(errs)
		return chunks, errs
	}
	return chunks, errs // running: nothing on the error channel yet
}

func TestStartNow_ReturnsTheErrorOfASourceThatFailsToStart(t *testing.T) {
	want := errors.New("the kernel refused the program")
	chunks, errs, err := startNow(context.Background(), &startFake{startErr: want}, source.Target{PID: 1})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if chunks != nil || errs != nil {
		t.Error("a source that failed to start has no channels to read")
	}
}

func TestStartNow_ReturnsTheChannelsOfARunningSource(t *testing.T) {
	src := &startFake{}
	chunks, errs, err := startNow(context.Background(), src, source.Target{PID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !src.started || chunks == nil || errs == nil {
		t.Errorf("started=%v chunks=%v errs=%v", src.started, chunks, errs)
	}
}
