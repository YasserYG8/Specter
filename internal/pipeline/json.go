package pipeline

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
)

// JSONLog represents an extracted structured JSON log record.
type JSONLog struct {
	Raw       string
	Timestamp string
	Level     Severity
	Message   string
	Fields    map[string]any
}

// ParseJSONLog attempts to parse a string as a structured JSON object.
// Returns nil if the line is not a JSON object.
func ParseJSONLog(line string) *JSONLog {
	// A JSON object must start with '{' once the escape codes are stripped, and stripping only removes bytes, so
	// a line with no '{' at all cannot be one. This skips the strip for most lines that are not JSON.
	if strings.IndexByte(line, '{') < 0 {
		return nil
	}
	trimmed := strings.TrimSpace(StripANSI(line))
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return nil
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(trimmed), &data); err != nil {
		return nil
	}

	jl := &JSONLog{
		Raw:    line,
		Fields: make(map[string]any),
		Level:  SeverityNone,
	}

	// 1. Extract message
	for _, key := range []string{"message", "msg", "event", "error", "log"} {
		if val, ok := data[key]; ok {
			if strVal, isStr := val.(string); isStr && strVal != "" {
				jl.Message = strVal
				delete(data, key)
				break
			}
		}
	}

	// 2. Extract level
	for _, key := range []string{"level", "severity", "lvl"} {
		if val, ok := data[key]; ok {
			switch v := val.(type) {
			case string:
				jl.Level = ParseSeverity(v)
			case float64:
				// Pino / Bunyan integer levels: 10=trace, 20=debug, 30=info, 40=warn, 50=error, 60=fatal
				num := int(v)
				switch {
				case num <= 20:
					jl.Level = SeverityDebug
				case num == 30:
					jl.Level = SeverityInfo
				case num == 40:
					jl.Level = SeverityWarn
				case num >= 50:
					jl.Level = SeverityError
				}
			}
			delete(data, key)
			break
		}
	}

	// 3. Extract timestamp
	// Only a value that really looks like a timestamp is taken, and only that key is removed from the fields. A key
	// with another kind of value (a duration like "time":45, an object, an empty string) stays a field, and the next
	// key is tried.
	for _, key := range []string{"timestamp", "time", "ts", "@timestamp"} {
		val, ok := data[key]
		if !ok {
			continue
		}
		accepted := false
		switch v := val.(type) {
		case string:
			if v != "" {
				jl.Timestamp = v
				accepted = true
			}
		case float64:
			if t, ok := numericTimestamp(v); ok {
				jl.Timestamp = t.Format("15:04:05.000")
				accepted = true
			}
		}
		if accepted {
			delete(data, key)
			break
		}
	}

	// Delete internal noise keys
	delete(data, "pid")
	delete(data, "hostname")
	delete(data, "v")

	// Remaining metadata
	jl.Fields = data

	return jl
}

// Unix seconds for the start of 2000 and the start of 2100 (UTC). A number is only a timestamp inside this range.
const (
	minTimestampUnix = 946684800
	maxTimestampUnix = 4102444800
)

// numericTimestamp turns a JSON number into a time. It picks the unit (seconds, milliseconds, microseconds or
// nanoseconds) from the size of the number. The thresholds sit between the values of the years 2000 to 2099 in two
// neighboring units, so a real timestamp never lands in the wrong unit. It returns false when the result is not in
// the years 2000 to 2099, which also turns away durations, counters, zero and negative numbers.
func numericTimestamp(v float64) (time.Time, bool) {
	if v <= 0 {
		return time.Time{}, false
	}
	var t time.Time
	switch {
	case v < 1e11:
		sec := int64(v)
		t = time.Unix(sec, int64((v-float64(sec))*1e9))
	case v < 1e14:
		t = time.UnixMilli(int64(v))
	case v < 1e17:
		t = time.UnixMicro(int64(v))
	case v < 9e18: // stay below the int64 limit
		t = time.Unix(0, int64(v))
	default:
		return time.Time{}, false
	}
	if u := t.Unix(); u < minTimestampUnix || u >= maxTimestampUnix {
		return time.Time{}, false
	}
	return t, true
}

// FormatSingleLineJSON renders a parsed JSONLog onto a clean single line with optional ANSI colors.
func FormatSingleLineJSON(jl *JSONLog, colorize bool) string {
	var sb strings.Builder

	// 1. Timestamp
	if jl.Timestamp != "" {
		tsStr := jl.Timestamp
		// Shorten ISO 8601 timestamps like "2026-10-10T10:00:00.123Z" to "10:00:00.123"
		if len(tsStr) > 19 && strings.Contains(tsStr, "T") {
			parts := strings.Split(tsStr, "T")
			if len(parts) == 2 {
				tsStr = strings.TrimSuffix(parts[1], "Z")
			}
		}
		if colorize && !color.NoColor {
			sb.WriteString(color.HiBlackString(tsStr))
		} else {
			sb.WriteString(tsStr)
		}
		sb.WriteString(" ")
	}

	// 2. Level Badge
	if jl.Level != SeverityNone {
		levelStr := fmt.Sprintf("[%s]", jl.Level.String())
		if colorize && !color.NoColor {
			switch jl.Level {
			case SeverityError:
				sb.WriteString(color.New(color.FgRed, color.Bold).Sprint(levelStr))
			case SeverityWarn:
				sb.WriteString(color.New(color.FgYellow, color.Bold).Sprint(levelStr))
			case SeverityInfo:
				sb.WriteString(color.New(color.FgCyan, color.Bold).Sprint(levelStr))
			case SeverityDebug:
				sb.WriteString(color.New(color.FgHiBlack).Sprint(levelStr))
			default:
				sb.WriteString(levelStr)
			}
		} else {
			sb.WriteString(levelStr)
		}
		sb.WriteString(" ")
	}

	// 3. Message
	if jl.Message != "" {
		if colorize && !color.NoColor {
			sb.WriteString(color.WhiteString(jl.Message))
		} else {
			sb.WriteString(jl.Message)
		}
	}

	// 4. Extra Fields (sorted for deterministic output)
	if len(jl.Fields) > 0 {
		var keys []string
		for k := range jl.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			v := jl.Fields[k]
			valStr := fmt.Sprintf("%v", v)
			sb.WriteString(" ")
			if colorize && !color.NoColor {
				sb.WriteString(color.CyanString("%s=", k))
				sb.WriteString(color.HiBlackString("%v", valStr))
			} else {
				sb.WriteString(fmt.Sprintf("%s=%s", k, valStr))
			}
		}
	}

	return sb.String()
}
