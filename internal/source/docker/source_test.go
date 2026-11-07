package docker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/YasserYG8/Specter/internal/source"
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

func TestParseContainerState(t *testing.T) {
	cases := []struct {
		out  string
		code int
		ok   bool
	}{
		{"exited 137\n", 137, true},
		{"exited 0", 0, true},
		{"exited 3", 3, true},
		{"running 0\n", 0, false},
		{"restarting 1", 0, false},
		{"dead 0", 0, false},
		{"exited", 0, false},
		{"exited abc", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		if code, ok := parseContainerState(tc.out); code != tc.code || ok != tc.ok {
			t.Errorf("parseContainerState(%q) = %d, %v; want %d, %v", tc.out, code, ok, tc.code, tc.ok)
		}
	}
}

func TestContainerExit(t *testing.T) {
	newSource := func(out string, err error, asked *string) *Source {
		s := NewSource()
		s.containerID = "billing-api"
		s.inspect = func(_ context.Context, container string) ([]byte, error) {
			if asked != nil {
				*asked = container
			}
			return []byte(out), err
		}
		return s
	}

	t.Run("exited with a code", func(t *testing.T) {
		var asked string
		code, ok := newSource("exited 137\n", nil, &asked).ContainerExit(context.Background())
		if !ok || code != 137 {
			t.Errorf("got %d, %v; want 137, true", code, ok)
		}
		if asked != "billing-api" {
			t.Errorf("asked about %q, want the container being followed", asked)
		}
	})

	t.Run("docker inspect fails", func(t *testing.T) {
		if code, ok := newSource("", errors.New("no such container"), nil).ContainerExit(context.Background()); ok {
			t.Errorf("expected an unknown state, got %d", code)
		}
	})

	t.Run("container is still running", func(t *testing.T) {
		if _, ok := newSource("running 0", nil, nil).ContainerExit(context.Background()); ok {
			t.Error("a running container has not exited")
		}
	})

	t.Run("no container followed yet", func(t *testing.T) {
		s := NewSource()
		s.inspect = func(context.Context, string) ([]byte, error) {
			t.Fatal("must not run docker without a container")
			return nil, nil
		}
		if _, ok := s.ContainerExit(context.Background()); ok {
			t.Error("expected an unknown state")
		}
	})

	t.Run("the lookup is bounded by a timeout", func(t *testing.T) {
		s := NewSource()
		s.containerID = "slow"
		s.inspect = func(ctx context.Context, _ string) ([]byte, error) {
			if _, has := ctx.Deadline(); !has {
				t.Error("expected the inspect call to have a deadline")
			}
			return nil, ctx.Err()
		}
		s.ContainerExit(context.Background())
	})
}
