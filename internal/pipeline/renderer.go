package pipeline

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/fatih/color"
)

// RendererOptions configures output styling and colorization.
type RendererOptions struct {
	NoColor   bool
	Raw       bool
	ForceJSON bool
}

// Renderer transforms Entries into styled terminal strings.
type Renderer struct {
	opts RendererOptions
}

// NewRenderer creates a new Renderer instance.
func NewRenderer(opts RendererOptions) *Renderer {
	return &Renderer{opts: opts}
}

// Render formats an Entry into an ANSI-styled or plain string for terminal printing.
func (r *Renderer) Render(entry *Entry) string {
	text := entry.Text()

	// 1. Raw mode: bypass all formatting
	if r.opts.Raw {
		return text
	}

	useColor := !r.opts.NoColor && !color.NoColor

	// 2. Check structured JSON log (single-line only)
	if len(entry.Lines) == 1 {
		if jl := ParseJSONLog(entry.Lines[0]); jl != nil {
			if r.opts.ForceJSON {
				return entry.Lines[0]
			}
			return FormatSingleLineJSON(jl, useColor)
		}
	}

	// 3. Multi-line stack trace
	if entry.IsTrace && len(entry.Lines) > 1 {
		var sb strings.Builder
		for i, line := range entry.Lines {
			if i > 0 {
				sb.WriteString("\n")
			}
			if useColor && !HasANSI(line) {
				if i == 0 {
					// Error header in bold red
					sb.WriteString(color.New(color.FgRed, color.Bold).Sprint(line))
				} else {
					// Stack trace frames in dim gray
					sb.WriteString(color.HiBlackString(line))
				}
			} else {
				sb.WriteString(line)
			}
		}
		return sb.String()
	}

	// 4. Pre-colored lines pass-through without re-coloring
	if HasANSI(text) || !useColor {
		return text
	}

	// 5. Single line with recognized severity level
	switch entry.Level {
	case SeverityError:
		return color.RedString(text)
	case SeverityWarn:
		return color.YellowString(text)
	case SeverityInfo:
		// Style HTTP access log lines (2xx subtle green, 3xx subtle cyan)
		if m := httpStatusRegex.FindStringSubmatch(text); len(m) > 0 {
			codeStr := m[1]
			if codeStr == "" && len(m) > 2 {
				codeStr = m[2]
			}
			if code, err := strconv.Atoi(codeStr); err == nil {
				if code >= 200 && code < 300 {
					return color.GreenString(text)
				}
				if code >= 300 && code < 400 {
					return color.CyanString(text)
				}
			}
			return color.CyanString(text)
		}
		// Subtly highlight [INFO] token if present, else plain
		if strings.Contains(strings.ToLower(text), "info") {
			return color.CyanString(text)
		}
		return text
	case SeverityDebug:
		return color.HiBlackString(text)
	default:
		return text
	}
}

// RenderBadge returns a formatted prefix tag (e.g. `[specter :3000]`).
func RenderBadge(port int, colorize bool) string {
	tag := fmt.Sprintf("[specter :%d]", port)
	if colorize && !color.NoColor {
		return color.CyanString(tag)
	}
	return tag
}
