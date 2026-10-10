package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/specter-cli/specter/internal/source"
)

// Source implements source.Source for Docker containers.
type Source struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	dockerPath  string // Custom path or binary name (defaults to "docker")
	containerID string
}

// NewSource creates a new Docker source instance.
func NewSource(dockerPath ...string) *Source {
	dp := "docker"
	if len(dockerPath) > 0 && dockerPath[0] != "" {
		dp = dockerPath[0]
	}
	return &Source{dockerPath: dp}
}

// Name returns the identifier for this source mechanism.
func (s *Source) Name() string {
	return "docker"
}

// Start spawns `docker logs -f --tail 0 <container>` and streams chunks.
func (s *Source) Start(ctx context.Context, target source.Target) (<-chan source.Chunk, <-chan error) {
	chunks := make(chan source.Chunk, 1024)
	errs := make(chan error, 1)

	containerID := target.Container
	if containerID == "" {
		var err error
		containerID, err = DetectContainer(target.PID, "")
		if err != nil || containerID == "" {
			errs <- fmt.Errorf("no container found for target PID %d", target.PID)
			close(chunks)
			close(errs)
			return chunks, errs
		}
	}

	s.mu.Lock()
	s.containerID = containerID
	s.mu.Unlock()

	cmd := exec.CommandContext(ctx, s.dockerPath, "logs", "-f", "--tail", "0", containerID)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return cmd.Process.Signal(os.Interrupt)
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		errs <- fmt.Errorf("failed to open docker stdout pipe: %w", err)
		close(chunks)
		close(errs)
		return chunks, errs
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		errs <- fmt.Errorf("failed to open docker stderr pipe: %w", err)
		close(chunks)
		close(errs)
		return chunks, errs
	}

	if err := cmd.Start(); err != nil {
		errs <- fmt.Errorf("failed to start docker logs: %w", err)
		close(chunks)
		close(errs)
		return chunks, errs
	}

	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)

	readStream := func(r io.Reader, fd int) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				payload := make([]byte, n)
				copy(payload, buf[:n])
				select {
				case chunks <- source.Chunk{
					Bytes: payload,
					FD:    fd,
					PID:   target.PID,
					TS:    time.Now(),
				}:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}

	go readStream(stdout, 1)
	go readStream(stderr, 2)

	go func() {
		defer close(chunks)
		defer close(errs)

		wg.Wait()
		waitErr := cmd.Wait()
		if waitErr != nil && ctx.Err() == nil {
			errs <- fmt.Errorf("docker logs terminated unexpectedly: %w", waitErr)
		}
	}()

	return chunks, errs
}

// Stop cleanly terminates the docker logs subprocess.
func (s *Source) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Signal(os.Interrupt)
	}
	return nil
}
