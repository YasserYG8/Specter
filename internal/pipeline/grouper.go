package pipeline

import (
	"context"
	"strings"
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
	// MatchedGrep is set by TraceHolder on every line of a stack trace in which some line matched --grep, so
	// the filter admits the whole trace.
	MatchedGrep bool
	// EndsTrace is true on the line that closes a Python traceback (its exception line). Traces of other
	// languages have no closing line.
	EndsTrace bool

	// json and jsonKnown carry the grouper's one JSON decode of this line on to the renderer, so the line is not
	// decoded again. When jsonKnown is true, json is the result (nil when the line is not a JSON object). When it
	// is false, which is the case for every Entry built outside the grouper, the renderer decodes the text itself.
	// They are unexported so that no caller can set a result that does not belong to the text.
	json      *JSONLog
	jsonKnown bool
}

// jsonMemo holds the JSON decision for one line, so that the line is decoded at most once however many parts of
// the pipeline ask: the record decision, IsJSON and, through the Entry, the renderer. The decode happens on the
// first call to get and never earlier, so a path that did not decode a line before still does not.
type jsonMemo struct {
	text  string
	known bool
	jl    *JSONLog
}

// get returns the line decoded as a JSON log, or nil when it is not a JSON object.
func (m *jsonMemo) get() *JSONLog {
	if !m.known {
		m.jl = ParseJSONLog(m.text)
		m.known = true
	}
	return m.jl
}

// carryTo puts the decision on the entry for the renderer, when there is one to carry.
//
// The memo decodes the grouper's clean text (the line with its escape codes stripped once), and the renderer would
// decode the entry's own text, which ParseJSONLog strips once. Both reach the same bytes only when stripping the
// clean text again changes nothing. That holds when it has no ESC or BEL left. A line that still has one, such as
// `\x1b[3\x1b[0m1m`, whose strip builds a new sequence, is left to the renderer's own decode.
func (m *jsonMemo) carryTo(e *Entry) {
	if m.known && !hasEscapeByte(m.text) {
		e.json, e.jsonKnown = m.jl, true
	}
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
	level    Severity
	inTrace  bool
	isDotNet bool
	isGoDump bool
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
	return isRecordStartMemo(clean, &jsonMemo{text: clean})
}

// isRecordStartMemo is isRecordStart with the JSON decision of the line kept in memo, which the caller can reuse.
func isRecordStartMemo(clean string, memo *jsonMemo) (bool, Severity, bool, bool) {
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
	if memo.get() != nil {
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
	if positionalWordRegex.MatchString(clean) || cookbookLevelRegex.MatchString(clean) || celeryLevelRegex.MatchString(clean) {
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

		// Only this goroutine touches records, so it needs no lock
		records := make(map[streamKey]*streamRecord)

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
				rec := records[key]

				now := line.TS
				if now.IsZero() {
					now = time.Now()
				}

				// Enforce bounded length safety and sanitize non-SGR sequences
				rawText := line.Text
				safeText := SanitizeTerminalOutput(capCapturedLine(rawText))
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
					}

					// If trailing Python exception closes trace
					if pythonTrailerRegex.MatchString(cleanText) && !pythonTraceHeaderRegex.MatchString(cleanText) {
						rec.inTrace = false
						entry.EndsTrace = true
					}
					// If .NET 2-line record completed
					if rec.isDotNet {
						rec.isDotNet = false
						records[key] = nil
					}

					if !sendEntry(entry) {
						return
					}
					continue
				}

				// Check if line starts a new record
				memo := jsonMemo{text: cleanText}
				isStart, lvl, inTrace, isGoDump := isRecordStartMemo(cleanText, &memo)
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
						IsJSON:               memo.get() != nil,
						IsPartial:            false,
						TS:                   now,
						Raw:                  rawText,
						Safe:                 safeText,
					}
					memo.carryTo(&entry)
					if !sendEntry(entry) {
						return
					}
					continue
				}

				// Unclassified plain text (resets active record state)
				records[key] = nil

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
				memo.carryTo(&plainEntry)
				if !sendEntry(plainEntry) {
					return
				}
			}
		}
	}()

	return out
}
