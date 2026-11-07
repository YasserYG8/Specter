package pipeline

import (
	"regexp"
	"strings"
)

// The level words the positional rules accept.
const logLevels = `ERROR|FATAL|CRIT|CRITICAL|PANIC|WARN|WARNING|INFO|DEBUG|TRACE`

// logTimestamp is the date and time most loggers print: 2026-11-07 10:12:50, with a space or T between them, an
// optional fraction after a comma (Python's default asctime) or a dot, and an optional zone.
const logTimestamp = `\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?`

// Precompiled regexes for terminal safety, severity, HTTP logs, and stack trace geometry.
// All regular expressions are designed with linear execution characteristics (no nested quantifiers).
var (
	// ANSI SGR sequence matcher (colors, bold, dim, reset) - preserved
	ansiSGRRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

	// Non-SGR terminal escape sequences (OSC title changes, CSI cursor movement, BEL) - stripped for safety
	nonSGRControlRegex = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b\[[0-9;]*[a-zA-KSTXYZf-n]|\x07`)

	// Generic ANSI matcher for stripped text comparison
	anyANSIRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x07`)

	// Structured Log Regexes (Rank 1: Highest Confidence)
	jsonLevelRegex   = regexp.MustCompile(`"(?:level|lvl|severity)"\s*:\s*"([a-zA-Z]+)"`)
	logfmtLevelRegex = regexp.MustCompile(`\b(?:level|lvl|severity)=([a-zA-Z]+)`)

	// Positional Token Regexes (Rank 2: High Confidence)
	bracketedLevelRegex = regexp.MustCompile(`\[\s*(ERROR|FATAL|CRIT|CRITICAL|PANIC|WARN|WARNING|INFO|DEBUG|TRACE)\s*\]`)
	monologLevelRegex   = regexp.MustCompile(`\b[a-zA-Z0-9_\-.]+\.(ERROR|CRITICAL|ALERT|EMERGENCY|WARNING|WARN|NOTICE|INFO|DEBUG):`)
	columnarLevelRegex  = regexp.MustCompile(`\|\s*(ERROR|FATAL|CRIT|CRITICAL|PANIC|WARN|WARNING|INFO|DEBUG|TRACE)\s*\|`)
	dotnetLevelRegex    = regexp.MustCompile(`^(fail|crit|warn|info|dbug|trce):\s+[a-zA-Z0-9_.]+(?:\[\d+\])?`)
	positionalWordRegex = regexp.MustCompile(`^(?:\[[^\]]+\]\s+)?(?:(?:` + logTimestamp + `|[0-9T:.\-+Z]{10,35})\s+)?(?:\[[^\]]+\]\s+)?(` + logLevels + `)\b`)

	// Python formats whose level is not right after the timestamp. Both are anchored at the start of the line, so
	// a level word inside a message is never taken.
	//   logging cookbook: 2026-11-07 10:12:50,516 - TajweedPipeline - INFO - Starting
	//   Celery worker:    [2026-11-07 10:12:50,516: ERROR/ForkPoolWorker-1] Task ... raised
	// The logger name is matched lazily, so the first " - LEVEL - " is the level and a later one in the message is not.
	cookbookLevelRegex = regexp.MustCompile(`^` + logTimestamp + ` - [^ ]+(?: [^ ]+)*? - (` + logLevels + `) - `)
	celeryLevelRegex   = regexp.MustCompile(`^\[` + logTimestamp + `: (` + logLevels + `)/[^\]]+\]`)

	// HTTP Access Log Regexes (Rank 3: Medium Confidence)
	// 1. Quoted request line: "GET /path HTTP/1.1" 200
	httpQuotedRegex = regexp.MustCompile(`"(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s+([^"\s]+)(?:\s+HTTP/\d(?:\.\d)?)?"\s+([1-5]\d{2})\b`)
	// 2. Unquoted request line: GET /path 200 (morgan, Next.js)
	httpUnquotedRegex = regexp.MustCompile(`\b(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s+(/[^\s?#]*[^\s]*)\s+([1-5]\d{2})\b`)
	// 3. Gin columnar access log: [GIN] ... | 200 | ... | GET "/path"
	httpGinRegex = regexp.MustCompile(`\[GIN\]\s+[^|]+\|\s*([1-5]\d{2})\s*\|[^|]+\|[^|]+\|\s*(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s+"?([^"\s]+)`)
	// 4. Spring Boot DispatcherServlet completed: Completed 200 OK
	httpSpringRegex = regexp.MustCompile(`\bCompleted\s+([1-5]\d{2})\s+([A-Z_]+)`)

	// Trace Root Anchors
	pythonTraceHeaderRegex = regexp.MustCompile(`^Traceback \(most recent call last\):`)
	goPanicHeaderRegex     = regexp.MustCompile(`^panic:\s+|goroutine\s+\d+\s+\[`)
	uncaughtExceptionRegex = regexp.MustCompile(`\b(?:[a-zA-Z0-9_.]+\.)?[A-Z][a-zA-Z0-9_]*(?:Error|Exception|Panic|Fault)(?::|\b)`)

	// Continuation Frame Regexes
	javaFrameRegex      = regexp.MustCompile(`^\s+at\s+[a-zA-Z0-9_$.]+\([a-zA-Z0-9_$.]+:\d+\)|^\s*Caused by:|\s*\.\.\.\s+\d+\s+more`)
	nodeFrameRegex      = regexp.MustCompile(`^\s+at\s+(?:async\s+)?[^\s(]+(?:\s*\(.*\))?`)
	pythonFrameRegex    = regexp.MustCompile(`^\s+File\s+"[^"]+",\s+line\s+\d+`)
	pythonTrailerRegex  = regexp.MustCompile(`^(?:[a-zA-Z0-9_.]+\.)?[A-Z][a-zA-Z0-9_]*(?:Error|Exception|Warning|Fault)(?::.*)?$`)
	pythonChainHeader   = regexp.MustCompile(`^(?:During handling of the above exception|The above exception was the direct cause)`)
	goGoroutineFunc     = regexp.MustCompile(`^[a-zA-Z0-9_./\-]+(?:\.[a-zA-Z0-9_]+)+\(.*\)$`)
	goGoroutineFileLine = regexp.MustCompile(`^\t[^\s:]+:\d+(?:\s+\+0x[0-9a-f]+)?$`)
	numberedFrameRegex  = regexp.MustCompile(`^#\d+\s+(?:0x[0-9a-f]+\s+in\s+|/[^\s(]+(?:\(\d+\))?|[a-zA-Z0-9_\\]+(?:->|::))`)
)

// StripANSI removes all ANSI escape sequences from a string.
func StripANSI(s string) string {
	return anyANSIRegex.ReplaceAllString(s, "")
}

// SanitizeTerminalOutput preserves SGR color sequences but strips dangerous control codes (OSC, CSI, BEL).
func SanitizeTerminalOutput(s string) string {
	return nonSGRControlRegex.ReplaceAllString(s, "")
}

// CapLineLength bounds the string to maxLen bytes, appending marker if truncated.
func CapLineLength(s string, maxLen int, marker string) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	// Truncate at character boundary if possible
	cut := maxLen
	return s[:cut] + marker
}

// captureMarkerRegex matches the marker a source appends when it captured less than a write contained.
var captureMarkerRegex = regexp.MustCompile(`\[specter: \d+ more bytes not captured\]$`)

// capCapturedLine applies MaxLineLength to a line. A capture marker at the end of the line is kept, because it
// says exactly how much is missing and a cut write fills the whole limit, which would otherwise always remove it.
func capCapturedLine(s string) string {
	if len(s) <= MaxLineLength {
		return s
	}
	if loc := captureMarkerRegex.FindStringIndex(s); loc != nil {
		return CapLineLength(s[:loc[0]], MaxLineLength, TruncationMarker) + s[loc[0]:]
	}
	return CapLineLength(s, MaxLineLength, TruncationMarker)
}

// ParseRankedSeverity parses a string representation of severity.
func ParseRankedSeverity(raw string) Severity {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug", "dbug", "trace", "trce", "verbose":
		return SeverityDebug
	case "info", "notice":
		return SeverityInfo
	case "warn", "warning", "wrn":
		return SeverityWarn
	case "error", "err", "erro", "fail", "fatal", "crit", "critical", "panic", "emerg", "emergency", "alert":
		return SeverityError
	default:
		return SeverityNone
	}
}
