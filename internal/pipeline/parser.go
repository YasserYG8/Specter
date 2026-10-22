package pipeline

import (
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
	return ParseRankedSeverity(s)
}

// HasANSI checks if the string contains terminal ANSI escape sequences.
func HasANSI(s string) bool {
	return ansiSGRRegex.MatchString(s) || anyANSIRegex.MatchString(s)
}

// DetectHTTPStatus parses HTTP status code from supported access log formats.
// Requires method + path or framework completion tag to prevent false hits on arbitrary numbers.
func DetectHTTPStatus(line string) (int, bool) {
	// 1. Quoted request: "GET /api HTTP/1.1" 200
	if m := httpQuotedRegex.FindStringSubmatch(line); len(m) > 3 {
		if code, err := strconv.Atoi(m[3]); err == nil {
			return code, true
		}
	}

	// 2. Unquoted request: GET /api 200 (morgan dev, Next.js)
	if m := httpUnquotedRegex.FindStringSubmatch(line); len(m) > 3 {
		if code, err := strconv.Atoi(m[3]); err == nil {
			return code, true
		}
	}

	// 3. Gin access log: [GIN] ... | 200 | ... | GET "/path"
	if m := httpGinRegex.FindStringSubmatch(line); len(m) > 1 {
		if code, err := strconv.Atoi(m[1]); err == nil {
			return code, true
		}
	}

	// 4. Spring Boot: Completed 200 OK
	if m := httpSpringRegex.FindStringSubmatch(line); len(m) > 1 {
		if code, err := strconv.Atoi(m[1]); err == nil {
			return code, true
		}
	}

	return 0, false
}

// DetectSeverity examines line tokens using confidence-ranked rules:
// Rank 1: Structured JSON/logfmt key
// Rank 2: Positional prefix / token
// Rank 3: HTTP status code hint
// Rank 4: Weak body keyword anchor (last resort)
func DetectSeverity(line string) Severity {
	clean := StripANSI(line)
	if clean == "" {
		return SeverityNone
	}

	// Rank 1: Structured Key (Highest Confidence)
	if m := jsonLevelRegex.FindStringSubmatch(clean); len(m) > 1 {
		if lvl := ParseRankedSeverity(m[1]); lvl != SeverityNone {
			return lvl
		}
	}
	if m := logfmtLevelRegex.FindStringSubmatch(clean); len(m) > 1 {
		if lvl := ParseRankedSeverity(m[1]); lvl != SeverityNone {
			return lvl
		}
	}

	// Rank 2: Positional Token (High Confidence)
	posLevel := SeverityNone
	if m := bracketedLevelRegex.FindStringSubmatch(clean); len(m) > 1 {
		posLevel = ParseRankedSeverity(m[1])
	} else if m := monologLevelRegex.FindStringSubmatch(clean); len(m) > 1 {
		posLevel = ParseRankedSeverity(m[1])
	} else if m := columnarLevelRegex.FindStringSubmatch(clean); len(m) > 1 {
		posLevel = ParseRankedSeverity(m[1])
	} else if m := dotnetLevelRegex.FindStringSubmatch(clean); len(m) > 1 {
		posLevel = ParseRankedSeverity(m[1])
	} else if m := positionalWordRegex.FindStringSubmatch(clean); len(m) > 1 {
		posLevel = ParseRankedSeverity(m[1])
	}

	// Rank 3: HTTP Status Code Hint (Medium Confidence)
	if statusCode, ok := DetectHTTPStatus(clean); ok {
		if statusCode >= 500 {
			return SeverityError
		}
		if statusCode >= 400 {
			if posLevel < SeverityWarn {
				return SeverityWarn
			}
			return posLevel
		}
		if posLevel > SeverityInfo {
			return posLevel
		}
		return SeverityInfo
	}

	if posLevel != SeverityNone {
		return posLevel
	}

	// Rank 4: Weak Body Keyword / Root Crash Anchor (Lowest Confidence, Last Resort)
	if pythonTraceHeaderRegex.MatchString(clean) || goPanicHeaderRegex.MatchString(clean) {
		return SeverityError
	}
	cleanLower := strings.ToLower(clean)
	if strings.Contains(cleanLower, "exception:") || strings.Contains(cleanLower, "fatal error:") || strings.HasPrefix(cleanLower, "panic:") {
		return SeverityError
	}

	return SeverityNone
}
