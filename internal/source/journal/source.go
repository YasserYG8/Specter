package journal

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

// Source implements source.Source for systemd services via journalctl.
type Source struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	journalPath string
}

// NewSource creates a new JournalSource instance.
func NewSource(journalPath ...string) *Source {
	jp := "journalctl"
	if len(journalPath) > 0 && journalPath[0] != "" {
		jp = journalPath[0]
	}
	return &Source{journalPath: jp}
}

// Name returns the identifier for this source mechanism.
func (s *Source) Name() string {
	return "journal"
}

// Start begins streaming journalctl output for the target process.
func (s *Source) Start(ctx context.Context, target source.Target) (<-chan source.Chunk, <-chan error) {
	chunks := make(chan source.Chunk, 1024)
	errs := make(chan error, 1)

	unit := target.Unit
	if unit == "" {
		detectedUnit, _ := DetectSystemdUnit(target.PID, "")
		unit = detectedUnit
	}

	args := []string{"-f", "-n", "0"}
	if unit != "" {
		args = append(args, "-u", unit)
	} else if target.PID > 0 {
		args = append(args, fmt.Sprintf("_PID=%d", target.PID))
	} else {
		errs <- fmt.Errorf("neither PID nor systemd unit provided for journal attachment")
		close(chunks)
		close(errs)
		return chunks, errs
	}

	cmd := exec.CommandContext(ctx, s.journalPath, args...)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return cmd.Process.Signal(os.Interrupt)
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		errs <- fmt.Errorf("failed to open journalctl stdout pipe: %w", err)
		close(chunks)
		close(errs)
		return chunks, errs
	}

	if err := cmd.Start(); err != nil {
		errs <- fmt.Errorf("failed to start journalctl: %w", err)
		close(chunks)
		close(errs)
		return chunks, errs
	}

	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()

	go func() {
		defer close(chunks)
		defer close(errs)

		buf := make([]byte, 4096)
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				payload := make([]byte, n)
				copy(payload, buf[:n])
				select {
				case chunks <- source.Chunk{
					Bytes: payload,
					FD:    1,
					PID:   target.PID,
					TS:    time.Now(),
				}:
				case <-ctx.Done():
					return
				}
			}
			if readErr != nil {
				if readErr == io.EOF {
					break
				}
				break
			}
		}

		waitErr := cmd.Wait()
		if waitErr != nil && ctx.Err() == nil {
			errs <- fmt.Errorf("journalctl terminated unexpectedly: %w", waitErr)
		}
	}()

	return chunks, errs
}

// Stop cleanly terminates journalctl streaming.
func (s *Source) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Signal(os.Interrupt)
	}
	return nil
}
