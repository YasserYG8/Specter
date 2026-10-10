package pipeline

import (
	"bytes"
	"context"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/specter-cli/specter/internal/source"
)

// DefaultIdleTimeout is the silence threshold before flushing partial lines (for interactive prompts).
const DefaultIdleTimeout = 150 * time.Millisecond

// MaxBufferLength is the maximum bytes buffered before flushing a partial line to bound memory.
const MaxBufferLength = 64 * 1024

// Line represents a reconstructed output line.
type Line struct {
	Text      string
	FD        int
	PID       int
	IsPartial bool
	TS        time.Time
}

type streamKey struct {
	pid int
	fd  int
}

type streamBuffer struct {
	buf        []byte
	lastActive time.Time
}

// LineAssembler reassembles byte chunks into complete lines with idle timer support.
type LineAssembler struct {
	idleTimeout time.Duration
}

// NewLineAssembler creates an assembler with the specified idle timeout (defaults to 150ms).
func NewLineAssembler(idleTimeout time.Duration) *LineAssembler {
	if idleTimeout <= 0 {
		idleTimeout = DefaultIdleTimeout
	}
	return &LineAssembler{idleTimeout: idleTimeout}
}

// Assemble converts a stream of raw Chunks into a stream of Line instances.
func (la *LineAssembler) Assemble(ctx context.Context, chunks <-chan source.Chunk) <-chan Line {
	out := make(chan Line, 1024)

	go func() {
		defer close(out)

		buffers := make(map[streamKey]*streamBuffer)
		var mu sync.Mutex

		// Ticker checks for idle buffers every 25ms
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case chunk, ok := <-chunks:
				if !ok {
					// Channel closed: flush all remaining non-empty buffers
					mu.Lock()
					now := time.Now()
					for key, sb := range buffers {
						if len(sb.buf) > 0 {
							out <- Line{
								Text:      string(sb.buf),
								FD:        key.fd,
								PID:       key.pid,
								IsPartial: true,
								TS:        now,
							}
						}
					}
					mu.Unlock()
					return
				}

				key := streamKey{pid: chunk.PID, fd: chunk.FD}
				mu.Lock()
				sb, exists := buffers[key]
				if !exists {
					sb = &streamBuffer{}
					buffers[key] = sb
				}

				sb.buf = append(sb.buf, chunk.Bytes...)
				sb.lastActive = chunk.TS
				if sb.lastActive.IsZero() {
					sb.lastActive = time.Now()
				}

				// Split on \n
				for {
					idx := bytes.IndexByte(sb.buf, '\n')
					if idx == -1 {
						break
					}
					lineBytes := sb.buf[:idx]
					// Strip carriage return if CRLF
					if len(lineBytes) > 0 && lineBytes[len(lineBytes)-1] == '\r' {
						lineBytes = lineBytes[:len(lineBytes)-1]
					}

					out <- Line{
						Text:      string(lineBytes),
						FD:        key.fd,
						PID:       key.pid,
						IsPartial: false,
						TS:        sb.lastActive,
					}
					sb.buf = sb.buf[idx+1:]
				}

				// Memory protection: if a single line exceeds MaxBufferLength, flush partial chunk
				if len(sb.buf) >= MaxBufferLength {
					out <- Line{
						Text:      string(sb.buf),
						FD:        key.fd,
						PID:       key.pid,
						IsPartial: true,
						TS:        sb.lastActive,
					}
					sb.buf = sb.buf[:0]
				}
				mu.Unlock()

			case now := <-ticker.C:
				mu.Lock()
				for key, sb := range buffers {
					if len(sb.buf) > 0 && now.Sub(sb.lastActive) >= la.idleTimeout {
						// Check if trailing bytes are an incomplete UTF-8 character
						trailingComplete := true
						r, size := utf8.DecodeLastRune(sb.buf)
						if r == utf8.RuneError && size == 1 {
							// Incomplete multibyte rune at the end, wait a bit longer if recent
							if now.Sub(sb.lastActive) < la.idleTimeout*2 {
								trailingComplete = false
							}
						}

						if trailingComplete {
							out <- Line{
								Text:      string(sb.buf),
								FD:        key.fd,
								PID:       key.pid,
								IsPartial: true,
								TS:        now,
							}
							sb.buf = sb.buf[:0]
						}
					}
				}
				mu.Unlock()
			}
		}
	}()

	return out
}
