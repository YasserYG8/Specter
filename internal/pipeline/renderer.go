package pipeline

import (
	"fmt"
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

	// 2. Structured JSON log (single line)
	if jl := ParseJSONLog(text); jl != nil {
		if r.opts.ForceJSON {
			return text
		}
		return FormatSingleLineJSON(jl, useColor)
	}

	// 3. Multi-line entry (backward compatibility if multi-line is passed)
	if entry.IsTrace && len(entry.Lines) > 1 {
		var sb strings.Builder
		for i, line := range entry.Lines {
			if i > 0 {
				sb.WriteString("\n")
			}
			if useColor && !HasANSI(line) {
				if i == 0 {
					sb.WriteString(color.New(color.FgRed, color.Bold).Sprint(line))
				} else {
					sb.WriteString(color.HiBlackString(line))
				}
			} else {
				sb.WriteString(line)
			}
		}
		return sb.String()
	}

	// 4. Pre-colored lines or disabled color: pass through untouched
	if HasANSI(text) || !useColor {
		return text
	}

	// 5. Continuation stack frames render dimmed
	if entry.IsFrame {
		return color.HiBlackString(text)
	}

	// 6. HTTP Access Log lines styling based on detected status code
	if statusCode, ok := DetectHTTPStatus(text); ok {
		if statusCode >= 500 {
			return color.RedString(text)
		}
		if statusCode >= 400 {
			return color.YellowString(text)
		}
		if statusCode >= 200 && statusCode < 300 {
			return color.GreenString(text)
		}
		if statusCode >= 300 && statusCode < 400 {
			return color.CyanString(text)
		}
	}

	// 7. Standard Severity Level styling
	switch entry.Level {
	case SeverityError:
		if entry.IsTrace {
			return color.New(color.FgRed, color.Bold).Sprint(text)
		}
		return color.RedString(text)
	case SeverityWarn:
		return color.YellowString(text)
	case SeverityInfo:
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
