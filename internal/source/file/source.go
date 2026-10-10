package file

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/specter-cli/specter/internal/source"
)

// Source implements source.Source by following regular log files attached to fd 1 and/or fd 2.
type Source struct {
	mu         sync.Mutex
	cancel     context.CancelFunc
	files      []*os.File
	pollPeriod time.Duration
}

// NewSource creates a new FileTailSource instance.
func NewSource(pollPeriod ...time.Duration) *Source {
	pp := 50 * time.Millisecond
	if len(pollPeriod) > 0 && pollPeriod[0] > 0 {
		pp = pollPeriod[0]
	}
	return &Source{pollPeriod: pp}
}

// Name returns the identifier for this source mechanism.
func (s *Source) Name() string {
	return "file"
}

// Start begins tailing target file descriptors and streams chunks.
func (s *Source) Start(ctx context.Context, target source.Target) (<-chan source.Chunk, <-chan error) {
	chunks := make(chan source.Chunk, 1024)
	errs := make(chan error, 1)

	// Determine file targets
	targets, err := DetectFileTargets(target.PID, "")
	if err != nil && target.Stdout == "" && target.Stderr == "" {
		errs <- fmt.Errorf("failed to detect file targets for PID %d: %w", target.PID, err)
		close(chunks)
		close(errs)
		return chunks, errs
	}

	stdoutPath := ""
	if IsRegularFile(target.Stdout) {
		stdoutPath = target.Stdout
	} else if targets != nil {
		stdoutPath = targets.StdoutPath
	}

	stderrPath := ""
	if IsRegularFile(target.Stderr) {
		stderrPath = target.Stderr
	} else if targets != nil {
		stderrPath = targets.StderrPath
	}

	if stdoutPath == "" && stderrPath == "" {
		errs <- fmt.Errorf("no regular disk files found on stdout or stderr for PID %d", target.PID)
		close(chunks)
		close(errs)
		return chunks, errs
	}

	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

	var wg sync.WaitGroup

	tailFile := func(path string, fd int) {
		defer wg.Done()

		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()

		s.mu.Lock()
		s.files = append(s.files, f)
		s.mu.Unlock()

		// Seek to the end for live tailing
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return
		}

		// Reusable timer for zero-allocation polling on EOF
		timer := time.NewTimer(s.pollPeriod)
		defer timer.Stop()

		buf := make([]byte, 4096)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, readErr := f.Read(buf)
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

			if readErr != nil {
				if readErr == io.EOF {
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(s.pollPeriod)

					select {
					case <-timer.C:
					case <-ctx.Done():
						return
					}
					continue
				}
				// Encountered unexpected read error
				return
			}
		}
	}

	if stdoutPath != "" {
		wg.Add(1)
		go tailFile(stdoutPath, 1)
	}

	if stderrPath != "" {
		// Avoid opening the exact same file twice if stdout and stderr are redirected to the same path
		if stderrPath != stdoutPath {
			wg.Add(1)
			go tailFile(stderrPath, 2)
		}
	}

	go func() {
		defer close(chunks)
		defer close(errs)
		wg.Wait()
	}()

	return chunks, errs
}

// Stop cleanly terminates file tailing.
func (s *Source) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}
	for _, f := range s.files {
		_ = f.Close()
	}
	s.files = nil
	return nil
}
