package pipeline

import (
	"regexp"
	"strconv"
	"strings"
)

// Severity represents the recognized log priority level.
type Severity int

const (
	SeverityNone Severity = iota
	SeverityDebug
	SeverityInfo
	SeverityWarn
	SeverityError
)

// String returns the uppercase name of the severity.
func (s Severity) String() string {
	switch s {
	case SeverityDebug:
		return "DEBUG"
	case SeverityInfo:
		return "INFO"
	case SeverityWarn:
		return "WARN"
	case SeverityError:
		return "ERROR"
	default:
		return "NONE"
	}
}

// ParseSeverity parses a severity name case-insensitively.
func ParseSeverity(s string) Severity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug", "trace", "verbose":
		return SeverityDebug
	case "info":
		return SeverityInfo
	case "warn", "warning":
		return SeverityWarn
	case "error", "err", "fatal", "critical", "panic":
		return SeverityError
	default:
		return SeverityNone
	}
}

var (
	// Regex matching ANSI escape sequences
	ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

	// Regex matching standard HTTP access log status codes (e.g. "GET /path" 200, HTTP/1.1" 500)
	httpStatusRegex = regexp.MustCompile(`"(?:GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s+[^"]+"\s+(\d{3})\b|HTTP/\d\.\d"\s+(\d{3})\b`)

	// Common regex token patterns for explicit level markers
	errorTokenRegex = regexp.MustCompile(`(?i)\b(?:error|fatal|panic|critical)\b|\[(?:ERROR|FATAL|PANIC|CRIT)\]|level=(?:error|fatal)`)
	warnTokenRegex  = regexp.MustCompile(`(?i)\b(?:warn|warning)\b|\[(?:WARN|WARNING)\]|level=(?:warn|warning)`)
	infoTokenRegex  = regexp.MustCompile(`(?i)\b(?:info)\b|\[INFO\]|level=info`)
	debugTokenRegex = regexp.MustCompile(`(?i)\b(?:debug|trace)\b|\[(?:DEBUG|TRACE)\]|level=(?:debug|trace)`)
)

// HasANSI checks if the string contains terminal ANSI escape sequences.
func HasANSI(s string) bool {
	return ansiRegex.MatchString(s)
}

// StripANSI removes all ANSI escape sequences from a string.
func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// DetectSeverity examines line tokens, HTTP status codes, and tags to classify its log level.
func DetectSeverity(line string) Severity {
	clean := StripANSI(line)
	cleanLower := strings.ToLower(clean)

	// 1. Check HTTP access log status codes
	if m := httpStatusRegex.FindStringSubmatch(clean); len(m) > 0 {
		codeStr := m[1]
		if codeStr == "" && len(m) > 2 {
			codeStr = m[2]
		}
		if code, err := strconv.Atoi(codeStr); err == nil {
			if code >= 500 && code < 600 {
				return SeverityError
			}
			if code >= 400 && code < 500 {
				return SeverityWarn
			}
			if code >= 200 && code < 400 {
				return SeverityInfo
			}
		}
	}

	// 2. Explicit tokens (checked in order of importance: Error > Warn > Info > Debug)
	if errorTokenRegex.MatchString(clean) {
		return SeverityError
	}
	if warnTokenRegex.MatchString(clean) {
		return SeverityWarn
	}
	if infoTokenRegex.MatchString(clean) {
		return SeverityInfo
	}
	if debugTokenRegex.MatchString(clean) {
		return SeverityDebug
	}

	// 3. Fallback checks for keywords like Exception or Failed
	if strings.Contains(cleanLower, "exception:") || strings.Contains(cleanLower, "failed") || strings.Contains(cleanLower, "failure") {
		return SeverityError
	}

	return SeverityNone
}
