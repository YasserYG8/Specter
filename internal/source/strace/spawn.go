package strace

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/specter-cli/specter/internal/source"
	"github.com/specter-cli/specter/internal/util"
)

// Options configures the strace source behavior.
type Options struct {
	FollowTree bool // Follow child processes and threads via -f
}

// Source implements source.Source using Linux strace.
type Source struct {
	mu            sync.Mutex
	cmd           *exec.Cmd
	parser        *Parser
	capturedError string
	processStatus string
	options       Options
}

// NewSource creates a new StraceSource instance with optional configuration.
func NewSource(opts ...Options) *Source {
	s := &Source{}
	if len(opts) > 0 {
		s.options = opts[0]
	}
	return s
}

// Name returns the identifier for this source mechanism.
func (s *Source) Name() string {
	return "strace"
}

// Start begins tracing the target process PID and streams chunks.
func (s *Source) Start(ctx context.Context, target source.Target) (<-chan source.Chunk, <-chan error) {
	chunks := make(chan source.Chunk, 1024)
	errs := make(chan error, 1)

	// Verify strace is installed upfront
	if installErr := CheckStraceInstalled(); installErr != nil {
		errs <- installErr
		close(chunks)
		close(errs)
		return chunks, errs
	}

	s.parser = NewParser(target.PID)

	// Build strace command: strace -f -qq -xx -s 8192 -e trace=write -p PID
	// Note: -f is required so strace attaches to all threads of process PID
	args := []string{
		"-f",
		"-qq",
		"-xx",
		"-s", "8192",
		"-e", "trace=write",
		"-p", strconv.Itoa(target.PID),
	}

	cmd := exec.CommandContext(ctx, "strace", args...)

	// Guarantee clean detachment on context cancellation (send SIGINT to strace, avoiding SIGKILL)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return cmd.Process.Signal(os.Interrupt)
		}
		return nil
	}
	cmd.WaitDelay = 3 * time.Second

	stderr, err := cmd.StderrPipe()
	if err != nil {
		errs <- fmt.Errorf("failed to create strace stderr pipe: %w", err)
		close(chunks)
		close(errs)
		return chunks, errs
	}

	if err := cmd.Start(); err != nil {
		errs <- fmt.Errorf("failed to start strace: %w", err)
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

		s.readLoop(ctx, stderr, chunks)

		// Await command termination
		waitErr := cmd.Wait()

		// If exited unexpectedly without context cancellation, diagnose the error
		if waitErr != nil && ctx.Err() == nil {
			s.mu.Lock()
			captured := s.capturedError
			s.mu.Unlock()

			if strings.Contains(captured, "Operation not permitted") {
				errs <- &util.DiagnosticError{
					Err:         util.ErrPtraceDenied,
					Title:       fmt.Sprintf("Cannot attach to PID %d: Operation not permitted", target.PID),
					Explanation: "The kernel blocked ptrace attachment. Process may belong to another user or ptrace is restricted.",
					Remediation: fmt.Sprintf("Run Specter with sudo: sudo specter %d (or setcap cap_sys_ptrace+ep on strace)", target.PID),
				}
				return
			}

			if strings.Contains(captured, "No such process") {
				errs <- fmt.Errorf("%w: PID %d exited or does not exist", util.ErrProcessNotFound, target.PID)
				return
			}

			if captured != "" {
				errs <- fmt.Errorf("strace error: %s", captured)
				return
			}

			errs <- fmt.Errorf("strace exited unexpectedly: %w", waitErr)
		}
	}()

	return chunks, errs
}

func (s *Source) readLoop(ctx context.Context, r io.Reader, chunks chan<- source.Chunk) {
	scanner := bufio.NewScanner(r)
	// Buffer up to 1MB per strace line
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// Capture strace diagnostic messages or errors
		if strings.HasPrefix(line, "strace: ") || strings.Contains(line, "attach:") {
			s.mu.Lock()
			s.capturedError = line
			s.mu.Unlock()
			continue
		}

		// Detect target process termination
		if strings.Contains(line, "+++ exited with ") || strings.Contains(line, "+++ killed by ") {
			s.mu.Lock()
			s.processStatus = strings.TrimSpace(line)
			s.mu.Unlock()
			continue
		}

		chunk, err := s.parser.ParseLine(line, time.Now())
		if err != nil {
			// Skip corrupt line without terminating stream
			continue
		}
		if chunk != nil {
			select {
			case chunks <- *chunk:
			case <-ctx.Done():
				return
			}
		}
	}
}

// Stop cleanly terminates the strace process.
func (s *Source) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		// Interrupt signal triggers strace to detach cleanly
		return s.cmd.Process.Signal(os.Interrupt)
	}
	return nil
}

// ProcessStatus returns any exit status detected from strace output.
func (s *Source) ProcessStatus() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.processStatus
}
