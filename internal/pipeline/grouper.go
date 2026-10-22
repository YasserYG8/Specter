package pipeline

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Constants for stream processing bounds
const (
	// MaxLineLength caps the maximum processed line length (64 KB).
	MaxLineLength = 65536
	// TruncationMarker is appended to lines exceeding MaxLineLength.
	TruncationMarker = " ... [specter: truncated at 64KB]"
	// DefaultTraceIdleTimeout is retained for interface compatibility.
	DefaultTraceIdleTimeout = 200 * time.Millisecond
)

// Entry represents an atomic stream log record emitted immediately to the terminal.
type Entry struct {
	Lines                []string
	FD                   int
	PID                  int
	Level                Severity
	IsTrace              bool // True if part of an exception or stack trace
	IsFrame              bool // True if continuation frame (styled dimmed)
	IsRecordContinuation bool // True if continuation of a record (e.g. .NET message or trace frame)
	IsJSON               bool
	IsPartial            bool
	TS                   time.Time
	Raw                  string
	Safe                 string
	MatchedGrep          bool
}

// Text returns the text representation of the entry.
func (e *Entry) Text() string {
	if len(e.Lines) > 0 {
		return strings.Join(e.Lines, "\n")
	}
	return e.Safe
}

// StreamRecord tracks active state for an ongoing record on a specific (PID, FD) stream.
type streamRecord struct {
	level       Severity
	inTrace     bool
	isDotNet    bool
	isGoDump    bool
	matchedGrep bool
}

// StackTraceGrouper operates as a real-time, zero-buffering record and continuation state machine.
type StackTraceGrouper struct {
	idleTimeout time.Duration
}

// NewStackTraceGrouper creates a grouper instance.
func NewStackTraceGrouper(idleTimeout time.Duration) *StackTraceGrouper {
	if idleTimeout <= 0 {
		idleTimeout = DefaultTraceIdleTimeout
	}
	return &StackTraceGrouper{idleTimeout: idleTimeout}
}

// isTraceHeader reports whether a line initiates an unhandled exception or stack trace.
func isTraceHeader(line string) bool {
	clean := StripANSI(line)
	_, _, inTrace, _ := isRecordStart(clean)
	return inTrace
}

// isRecordStart determines if a clean line initiates a new log record.
func isRecordStart(clean string) (bool, Severity, bool, bool) {
	if clean == "" {
		return false, SeverityNone, false, false
	}

	// 1. Root trace headers
	if pythonTraceHeaderRegex.MatchString(clean) {
		return true, SeverityError, true, false
	}
	if goPanicHeaderRegex.MatchString(clean) {
		return true, SeverityError, true, strings.Contains(clean, "goroutine")
	}

	// 2. Structured JSON log
	if ParseJSONLog(clean) != nil {
		return true, DetectSeverity(clean), false, false
	}

	// 3. .NET Kestrel two-line records (e.g. "info: Category[1]")
	if dotnetLevelRegex.MatchString(clean) {
		return true, DetectSeverity(clean), false, false
	}

	// 4. Prominent level markers or Monolog / Gin prefixes
	if bracketedLevelRegex.MatchString(clean) || monologLevelRegex.MatchString(clean) || columnarLevelRegex.MatchString(clean) {
		return true, DetectSeverity(clean), false, false
	}

	// 5. HTTP access log line
	if _, isHTTP := DetectHTTPStatus(clean); isHTTP {
		return true, DetectSeverity(clean), false, false
	}

	// 6. Positional word after timestamp
	if positionalWordRegex.MatchString(clean) {
		return true, DetectSeverity(clean), false, false
	}

	// 7. General uncaught exception or error pattern at line start (unindented)
	if !strings.HasPrefix(clean, " ") && !strings.HasPrefix(clean, "\t") {
		cleanLower := strings.ToLower(clean)
		if strings.Contains(cleanLower, "exception:") || strings.Contains(cleanLower, "error:") || strings.Contains(cleanLower, "fatal error:") {
			return true, SeverityError, true, false
		}
	}

	return false, SeverityNone, false, false
}

// isContinuation determines if a line continues the active record or stack trace.
func isContinuation(clean string, rec *streamRecord) (bool, bool, bool) {
	if rec == nil {
		return false, false, false
	}

	// Case 1: .NET two-line record continuation (indented message)
	if rec.isDotNet {
		if strings.HasPrefix(clean, "   ") || strings.HasPrefix(clean, "\t") {
			return true, false, true // continues record, but is not a stack frame
		}
		rec.isDotNet = false
	}

	// Case 2: Active stack trace / exception continuation
	if rec.inTrace {
		// Blank lines inside active Python trace or Go dump continue the record
		if clean == "" {
			return true, false, true
		}

		// Java frames
		if javaFrameRegex.MatchString(clean) {
			return true, true, true
		}

		// Node.js frames
		if nodeFrameRegex.MatchString(clean) {
			return true, true, true
		}

		// Python frames & exception chain
		if pythonFrameRegex.MatchString(clean) {
			return true, true, true
		}
		if pythonChainHeader.MatchString(clean) {
			return true, false, true
		}
		if pythonTraceHeaderRegex.MatchString(clean) {
			return true, false, true
		}
		// Python trailing exception line (closes the trace)
		if pythonTrailerRegex.MatchString(clean) {
			return true, false, true
		}

		// Go goroutine dumps
		if rec.isGoDump {
			if goGoroutineFileLine.MatchString(clean) {
				return true, true, true
			}
			if goGoroutineFunc.MatchString(clean) {
				return true, false, true
			}
			if strings.HasPrefix(clean, "\t") {
				return true, true, true
			}
		}

		// PHP / GDB numbered frames (#0 /var/www/...)
		if numberedFrameRegex.MatchString(clean) {
			return true, true, true
		}

		// Any indented line following an active trace header is part of the trace
		if strings.HasPrefix(clean, "  ") || strings.HasPrefix(clean, "\t") {
			return true, true, true
		}
	}

	return false, false, false
}

// Group processes a stream of Lines and immediately emits Entries with inherited context (zero buffering).
func (g *StackTraceGrouper) Group(ctx context.Context, in <-chan Line) <-chan Entry {
	out := make(chan Entry, 1024)

	go func() {
		defer close(out)

		records := make(map[streamKey]*streamRecord)
		var mu sync.Mutex

		sendEntry := func(e Entry) bool {
			select {
			case out <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}

		for {
			select {
			case <-ctx.Done():
				return

			case line, ok := <-in:
				if !ok {
					return
				}

				key := streamKey{pid: line.PID, fd: line.FD}
				mu.Lock()
				rec := records[key]

				now := line.TS
				if now.IsZero() {
					now = time.Now()
				}

				// Enforce bounded length safety and sanitize non-SGR sequences
				rawText := line.Text
				safeText := SanitizeTerminalOutput(CapLineLength(rawText, MaxLineLength, TruncationMarker))
				cleanText := StripANSI(safeText)

				if line.IsPartial {
					// Partial lines emit immediately without breaking ongoing record state
					partialEntry := Entry{
						Lines:     []string{safeText},
						FD:        line.FD,
						PID:       line.PID,
						Level:     DetectSeverity(cleanText),
						IsPartial: true,
						TS:        now,
						Raw:       rawText,
						Safe:      safeText,
					}
					mu.Unlock()
					if !sendEntry(partialEntry) {
						return
					}
					continue
				}

				// Check if this line continues an existing active record
				cont, isFrame, isRecCont := isContinuation(cleanText, rec)
				if cont && rec != nil {
					entry := Entry{
						Lines:                []string{safeText},
						FD:                   line.FD,
						PID:                  line.PID,
						Level:                rec.level,
						IsTrace:              rec.inTrace,
						IsFrame:              isFrame,
						IsRecordContinuation: isRecCont,
						IsPartial:            false,
						TS:                   now,
						Raw:                  rawText,
						Safe:                 safeText,
						MatchedGrep:          rec.matchedGrep,
					}

					// If trailing Python exception closes trace
					if pythonTrailerRegex.MatchString(cleanText) && !pythonTraceHeaderRegex.MatchString(cleanText) {
						rec.inTrace = false
					}
					// If .NET 2-line record completed
					if rec.isDotNet {
						rec.isDotNet = false
						records[key] = nil
					}

					mu.Unlock()
					if !sendEntry(entry) {
						return
					}
					continue
				}

				// Check if line starts a new record
				isStart, lvl, inTrace, isGoDump := isRecordStart(cleanText)
				if isStart {
					isDotNet := dotnetLevelRegex.MatchString(cleanText)
					rec = &streamRecord{
						level:    lvl,
						inTrace:  inTrace,
						isDotNet: isDotNet,
						isGoDump: isGoDump,
					}
					records[key] = rec

					entry := Entry{
						Lines:                []string{safeText},
						FD:                   line.FD,
						PID:                  line.PID,
						Level:                lvl,
						IsTrace:              inTrace,
						IsFrame:              false,
						IsRecordContinuation: false,
						IsJSON:               ParseJSONLog(cleanText) != nil,
						IsPartial:            false,
						TS:                   now,
						Raw:                  rawText,
						Safe:                 safeText,
					}
					mu.Unlock()
					if !sendEntry(entry) {
						return
					}
					continue
				}

				// Unclassified plain text (resets active record state)
				records[key] = nil
				mu.Unlock()

				plainEntry := Entry{
					Lines:                []string{safeText},
					FD:                   line.FD,
					PID:                  line.PID,
					Level:                DetectSeverity(cleanText),
					IsTrace:              false,
					IsFrame:              false,
					IsRecordContinuation: false,
					IsPartial:            false,
					TS:                   now,
					Raw:                  rawText,
					Safe:                 safeText,
				}
				if !sendEntry(plainEntry) {
					return
				}
			}
		}
	}()

	return out
}
