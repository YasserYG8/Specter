package pipeline

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DefaultTraceIdleTimeout is the silence threshold to flush an accumulated stack trace.
const DefaultTraceIdleTimeout = 200 * time.Millisecond

// Entry represents an atomic log record (a single line or an assembled multi-line stack trace).
type Entry struct {
	Lines     []string
	FD        int
	PID       int
	Level     Severity
	IsTrace   bool
	IsJSON    bool
	IsPartial bool
	TS        time.Time
}

// Text returns the joined lines of the entry.
func (e *Entry) Text() string {
	return strings.Join(e.Lines, "\n")
}

var (
	// Stack trace frame matchers
	nodeFrameRegex       = regexp.MustCompile(`^\s+at\s+(?:async\s+)?[^\s(]+(?:\s*\(.*\))?`)
	javaFrameRegex       = regexp.MustCompile(`^\s+at\s+[a-zA-Z0-9_$.]+\([a-zA-Z0-9_$.]+:\d+\)|^\s*Caused by:|\s*\.\.\.\s+\d+\s+more`)
	pythonTraceHeader    = regexp.MustCompile(`^Traceback \(most recent call last\):`)
	pythonFrameRegex     = regexp.MustCompile(`^\s+File\s+"[^"]+",\s+line\s+\d+`)
	pythonExceptionRegex = regexp.MustCompile(`^(?:[a-zA-Z0-9_.]+\.)?[A-Z][a-zA-Z0-9_]*(?:Error|Exception|Fault|Warning|Interrupt|Exit|Iteration|Failure|Timeout)(?::.*)?$`)
	goPanicRegex         = regexp.MustCompile(`^panic:\s+|goroutine\s+\d+\s+\[`)
)

// isTraceHeader detects lines that initiate an error stack trace.
func isTraceHeader(line string) bool {
	clean := StripANSI(line)
	if pythonTraceHeader.MatchString(clean) || goPanicRegex.MatchString(clean) {
		return true
	}
	cleanLower := strings.ToLower(clean)
	sanitized := benignErrorRegex.ReplaceAllString(cleanLower, "")
	if (strings.Contains(sanitized, "error:") || strings.Contains(sanitized, "exception:") || strings.HasPrefix(strings.TrimSpace(sanitized), "error")) &&
		!strings.HasPrefix(clean, " ") && !strings.HasPrefix(clean, "\t") {
		return true
	}
	return false
}

// isTraceContinuation checks if a line is a stack frame continuation.
func isTraceContinuation(line string) bool {
	clean := StripANSI(line)
	if nodeFrameRegex.MatchString(clean) || javaFrameRegex.MatchString(clean) || pythonFrameRegex.MatchString(clean) || pythonExceptionRegex.MatchString(clean) {
		return true
	}
	// Indented lines following a trace header often belong to the trace
	if strings.HasPrefix(clean, "    ") || strings.HasPrefix(clean, "\t") {
		return true
	}
	return false
}

// StackTraceGrouper aggregates consecutive stack trace lines into a single Entry.
type StackTraceGrouper struct {
	idleTimeout time.Duration
}

// NewStackTraceGrouper creates a grouper with the given timeout.
func NewStackTraceGrouper(idleTimeout time.Duration) *StackTraceGrouper {
	if idleTimeout <= 0 {
		idleTimeout = DefaultTraceIdleTimeout
	}
	return &StackTraceGrouper{idleTimeout: idleTimeout}
}

type grouperBuffer struct {
	entry      *Entry
	lastActive time.Time
	inTrace    bool
}

// Group processes a stream of Lines and emits grouped Entry instances.
func (g *StackTraceGrouper) Group(ctx context.Context, in <-chan Line) <-chan Entry {
	out := make(chan Entry, 1024)

	go func() {
		defer close(out)

		buffers := make(map[streamKey]*grouperBuffer)
		var mu sync.Mutex

		ticker := time.NewTicker(30 * time.Millisecond)
		defer ticker.Stop()

		sendEntry := func(e Entry) bool {
			select {
			case out <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}

		flushEntry := func(gb *grouperBuffer) bool {
			if gb.entry != nil {
				// Re-classify level if not already set
				if gb.entry.Level == SeverityNone {
					for _, l := range gb.entry.Lines {
						lvl := DetectSeverity(l)
						if lvl != SeverityNone {
							gb.entry.Level = lvl
							break
						}
					}
				}
				if gb.entry.IsTrace && gb.entry.Level < SeverityError {
					gb.entry.Level = SeverityError
				}
				e := *gb.entry
				gb.entry = nil
				gb.inTrace = false
				return sendEntry(e)
			}
			return true
		}

		for {
			select {
			case <-ctx.Done():
				return

			case line, ok := <-in:
				if !ok {
					// Input channel closed: flush all pending buffers
					mu.Lock()
					for _, gb := range buffers {
						if !flushEntry(gb) {
							break
						}
					}
					mu.Unlock()
					return
				}

				key := streamKey{pid: line.PID, fd: line.FD}
				mu.Lock()
				gb, exists := buffers[key]
				if !exists {
					gb = &grouperBuffer{}
					buffers[key] = gb
				}

				now := line.TS
				if now.IsZero() {
					now = time.Now()
				}
				gb.lastActive = now

				if line.IsPartial {
					// Partial line: flush any pending trace and emit partial immediately
					if !flushEntry(gb) {
						mu.Unlock()
						return
					}
					partialEntry := Entry{
						Lines:     []string{line.Text},
						FD:        line.FD,
						PID:       line.PID,
						Level:     DetectSeverity(line.Text),
						IsPartial: true,
						TS:        now,
					}
					mu.Unlock()
					if !sendEntry(partialEntry) {
						return
					}
					continue
				}

				// Check if this line continues an active stack trace
				if gb.inTrace && isTraceContinuation(line.Text) {
					gb.entry.Lines = append(gb.entry.Lines, line.Text)
					gb.entry.IsTrace = true
					if pythonExceptionRegex.MatchString(StripANSI(line.Text)) {
						if !flushEntry(gb) {
							mu.Unlock()
							return
						}
					}
					mu.Unlock()
					continue
				}

				// If we were in a trace and this line does not continue it, flush the trace
				if gb.inTrace {
					if !flushEntry(gb) {
						mu.Unlock()
						return
					}
				}

				// Check if this line starts a new stack trace
				if isTraceHeader(line.Text) {
					gb.inTrace = true
					gb.entry = &Entry{
						Lines:     []string{line.Text},
						FD:        line.FD,
						PID:       line.PID,
						Level:     DetectSeverity(line.Text),
						IsTrace:   true,
						IsPartial: false,
						TS:        now,
					}
					mu.Unlock()
					continue
				}

				// Normal single-line entry
				normalEntry := Entry{
					Lines:     []string{line.Text},
					FD:        line.FD,
					PID:       line.PID,
					Level:     DetectSeverity(line.Text),
					IsTrace:   false,
					IsPartial: false,
					TS:        now,
				}
				mu.Unlock()
				if !sendEntry(normalEntry) {
					return
				}

			case now := <-ticker.C:
				mu.Lock()
				for _, gb := range buffers {
					if gb.entry != nil && now.Sub(gb.lastActive) >= g.idleTimeout {
						if !flushEntry(gb) {
							mu.Unlock()
							return
						}
					}
				}
				mu.Unlock()
			}
		}
	}()

	return out
}
