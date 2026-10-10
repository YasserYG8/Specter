package docker

import (
	"context"
	"testing"
	"time"

	"github.com/specter-cli/specter/internal/source"
)

func TestDockerSource_Name(t *testing.T) {
	s := NewSource()
	if s.Name() != "docker" {
		t.Errorf("expected Name() to be 'docker', got %q", s.Name())
	}
}

func TestDockerSource_NoContainerFound(t *testing.T) {
	s := NewSource("echo")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Target without container and non-existent PID in mock
	target := source.Target{
		PID: 999999,
	}

	chunks, errs := s.Start(ctx, target)
	err := <-errs
	if err == nil {
		t.Error("expected error when no container found, got nil")
	}

	// Chunks should be closed
	for range chunks {
	}
}

func TestDockerSource_StopCleanly(t *testing.T) {
	s := NewSource()
	if err := s.Stop(); err != nil {
		t.Errorf("unexpected error on Stop(): %v", err)
	}
}
