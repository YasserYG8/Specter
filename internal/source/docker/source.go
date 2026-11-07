package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YasserYG8/Specter/internal/source"
)

// Source implements source.Source for Docker containers.
type Source struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	dockerPath  string // Custom path or binary name (defaults to "docker")
	containerID string

	// inspect runs `docker inspect` for the container state. Tests replace it; nil runs the real command.
	inspect func(ctx context.Context, container string) ([]byte, error)
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

// containerStateTimeout bounds the one `docker inspect` that runs after the log stream ends.
const containerStateTimeout = 3 * time.Second

// ContainerExit asks Docker how the container ended, for use after `docker logs -f` stopped on its own. The
// second result is false when the container has not exited or its state cannot be read, and callers then say
// only that it stopped.
func (s *Source) ContainerExit(ctx context.Context) (int, bool) {
	s.mu.Lock()
	id, path, inspect := s.containerID, s.dockerPath, s.inspect
	s.mu.Unlock()
	if id == "" {
		return 0, false
	}

	ctx, cancel := context.WithTimeout(ctx, containerStateTimeout)
	defer cancel()

	var out []byte
	var err error
	if inspect != nil {
		out, err = inspect(ctx, id)
	} else {
		out, err = exec.CommandContext(ctx, path, "inspect", "-f", "{{.State.Status}} {{.State.ExitCode}}", id).Output()
	}
	if err != nil {
		return 0, false
	}
	return parseContainerState(string(out))
}

// parseContainerState reads the "<status> <exit code>" text that `docker inspect -f` prints. Only an exited
// container has an exit code worth reporting.
func parseContainerState(out string) (int, bool) {
	fields := strings.Fields(out)
	if len(fields) != 2 || fields[0] != "exited" {
		return 0, false
	}
	code, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false
	}
	return code, true
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
