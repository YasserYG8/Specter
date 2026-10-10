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
	for _, key := range []string{"timestamp", "time", "ts", "@timestamp"} {
		if val, ok := data[key]; ok {
			switch v := val.(type) {
			case string:
				jl.Timestamp = v
			case float64:
				// Epoch ms or seconds
				if v > 1e11 {
					// ms
					t := time.UnixMilli(int64(v))
					jl.Timestamp = t.Format("15:04:05.000")
				} else {
					// seconds
					sec := int64(v)
					nsec := int64((v - float64(sec)) * 1e9)
					t := time.Unix(sec, nsec)
					jl.Timestamp = t.Format("15:04:05.000")
				}
			}
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
