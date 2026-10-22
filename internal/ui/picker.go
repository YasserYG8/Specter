package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"
	"golang.org/x/term"

	"github.com/specter-cli/specter/internal/resolver"
)

// ErrPickerAborted indicates the user canceled the interactive picker without selecting a service.
var ErrPickerAborted = errors.New("picker aborted")

// PickerConfig controls the interactive and non-interactive presentation.
type PickerConfig struct {
	In       io.Reader
	Out      io.Writer
	UseColor bool
}

// DefaultPickerConfig returns a config using standard streams.
func DefaultPickerConfig() PickerConfig {
	return PickerConfig{
		In:       os.Stdin,
		Out:      os.Stderr,
		UseColor: !color.NoColor,
	}
}

// SelectService prompts the user to select an active service.
// In an interactive TTY, it displays an ANSI arrow-key menu.
// In a non-interactive TTY / pipe, it outputs a formatted table and returns nil.
func SelectService(services []resolver.DiscoveredService, cfg PickerConfig) (*resolver.DiscoveredService, error) {
	if len(services) == 0 {
		msg := "[specter] No active listening ports or running Docker containers detected.\n"
		if cfg.UseColor {
			msg = fmt.Sprintf("%s No active listening ports or running Docker containers detected.\n", color.CyanString("[specter]"))
		}
		fmt.Fprint(cfg.Out, msg)
		return nil, nil
	}

	// Check if In is a terminal file descriptor
	stdinFile, isFile := cfg.In.(*os.File)
	isTTY := isFile && term.IsTerminal(int(stdinFile.Fd()))

	if !isTTY {
		RenderNonInteractiveTable(services, cfg.Out, cfg.UseColor)
		return nil, nil
	}

	return RunInteractivePicker(services, stdinFile, cfg.Out, cfg.UseColor)
}

// RenderNonInteractiveTable prints an aligned table of discovered services for non-TTY environments.
func RenderNonInteractiveTable(services []resolver.DiscoveredService, out io.Writer, useColor bool) {
	header := "[specter] Active Dev Servers & Services:"
	if useColor {
		header = color.CyanString(header)
	}
	fmt.Fprintln(out, header)
	fmt.Fprintf(out, "%-8s  %-7s  %-16s  %-18s  %s\n", "TYPE", "PORT", "PROJECT", "PROCESS / IMAGE", "PATH / TARGET")
	fmt.Fprintf(out, "%-8s  %-7s  %-16s  %-18s  %s\n", "----", "----", "-------", "---------------", "-------------")

	for _, s := range services {
		typeStr := string(s.Kind)
		portStr := "-"
		if s.Port > 0 {
			portStr = fmt.Sprintf(":%d", s.Port)
		}

		projStr := s.ProjectName
		if projStr == "" {
			if s.Kind == resolver.ServiceKindDocker {
				projStr = s.ContainerName
			} else {
				projStr = "-"
			}
		}

		procStr := s.ProcessName
		if s.Kind == resolver.ServiceKindDocker {
			if s.Image != "" {
				procStr = s.Image
			} else {
				procStr = s.ContainerName
			}
		} else if s.PID > 0 && procStr != "" {
			procStr = fmt.Sprintf("%s (PID %d)", procStr, s.PID)
		} else if s.PID > 0 {
			procStr = fmt.Sprintf("PID %d", s.PID)
		}

		if s.IsProtected {
			procStr = "protected (sudo)"
		}

		targetStr := "-"
		if s.Kind == resolver.ServiceKindDocker {
			if s.ContainerPort > 0 {
				targetStr = fmt.Sprintf("-> container :%d", s.ContainerPort)
			} else {
				targetStr = "container"
			}
		} else if s.WorkingDir != "" {
			targetStr = resolver.SanitizePath(s.WorkingDir)
		}

		fmt.Fprintf(out, "%-8s  %-7s  %-16s  %-18s  %s\n", typeStr, portStr, truncate(projStr, 16), truncate(procStr, 18), targetStr)
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "To attach, run: specter <port|pid|container|project>")
}

// FormatServiceItem formats a single service entry for picker display with fixed-width columns.
func FormatServiceItem(s resolver.DiscoveredService, isSelected bool, useColor bool, termWidth ...int) string {
	cursor := "  "
	if isSelected {
		cursor = "> "
	}

	var tag string
	var portStr string

	if s.Kind == resolver.ServiceKindDocker {
		tag = "[docker]"
		if s.Port > 0 {
			portStr = fmt.Sprintf(":%-5d", s.Port)
		} else {
			portStr = "      "
		}
	} else {
		tag = "[host]  "
		if s.Port > 0 {
			portStr = fmt.Sprintf(":%-5d", s.Port)
		} else {
			portStr = "      "
		}
	}

	proj := s.ProjectName
	if proj == "" {
		if s.Kind == resolver.ServiceKindDocker {
			proj = s.ContainerName
		} else {
			proj = "-"
		}
	}

	proc := s.ProcessName
	if s.Kind == resolver.ServiceKindDocker {
		if s.Image != "" {
			proc = s.Image
		} else {
			proc = s.ContainerName
		}
	} else if s.PID > 0 && proc != "" {
		proc = fmt.Sprintf("%s (PID %d)", proc, s.PID)
	} else if s.PID > 0 {
		proc = fmt.Sprintf("PID %d", s.PID)
	}

	if s.IsProtected {
		proc = "protected (sudo)"
	}

	var target string
	if s.Kind == resolver.ServiceKindDocker {
		if s.ContainerPort > 0 {
			target = fmt.Sprintf("-> :%d", s.ContainerPort)
		} else {
			target = s.ContainerID
		}
	} else if s.WorkingDir != "" {
		target = resolver.SanitizePath(s.WorkingDir)
	}

	projCol := pad(truncate(proj, 15), 15)
	procCol := pad(truncate(proc, 18), 18)

	rawLine := fmt.Sprintf("%s%s %s %s  %s  %s", cursor, tag, portStr, projCol, procCol, target)

	// Responsive truncation if terminal width is constrained
	maxW := 0
	if len(termWidth) > 0 && termWidth[0] > 0 {
		maxW = termWidth[0]
	}
	if maxW > 0 && len(rawLine) > maxW {
		if maxW > 4 {
			rawLine = rawLine[:maxW-3] + "..."
		} else {
			rawLine = rawLine[:maxW]
		}
	}

	if useColor {
		if isSelected {
			return color.CyanString(rawLine)
		}
		var tagColored string
		if s.Kind == resolver.ServiceKindDocker {
			tagColored = color.BlueString(tag)
		} else {
			tagColored = color.GreenString(tag)
		}
		return fmt.Sprintf("%s%s %s %s  %s  %s",
			cursor,
			tagColored,
			color.YellowString(portStr),
			color.HiWhiteString(projCol),
			procCol,
			color.HiBlackString(target),
		)
	}

	return rawLine
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 2 {
		return s[:maxLen]
	}
	return s[:maxLen-2] + ".."
}

func pad(s string, length int) string {
	if len(s) >= length {
		return s
	}
	return s + strings.Repeat(" ", length-len(s))
}

// RunInteractivePicker handles terminal raw mode and keyboard navigation.
func RunInteractivePicker(services []resolver.DiscoveredService, stdin *os.File, out io.Writer, useColor bool) (*resolver.DiscoveredService, error) {
	fd := int(stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		// Fallback to table if raw mode fails
		RenderNonInteractiveTable(services, out, useColor)
		return nil, nil
	}
	defer term.Restore(fd, oldState)

	// Hide cursor during navigation
	fmt.Fprint(out, "\033[?25l")
	defer fmt.Fprint(out, "\033[?25h")

	selected := 0
	count := len(services)

	// Terminal dimensions
	termW := 80
	maxVisible := 10
	if width, height, err := term.GetSize(fd); err == nil {
		if width > 0 {
			termW = width
		}
		if height > 7 {
			if height-5 < maxVisible {
				maxVisible = height - 5
			}
		}
	}
	if maxVisible > count {
		maxVisible = count
	}

	renderedLines := 0

	render := func() {
		// If previously rendered, move cursor up to beginning
		if renderedLines > 0 {
			fmt.Fprintf(out, "\033[%dA", renderedLines)
		}

		var sb strings.Builder
		title := "Specter · Select Service (Use ↑/↓ or j/k to navigate, Enter to stream, Esc to quit)\r\n"
		if useColor {
			title = color.CyanString("Specter") + " · Select Service " + color.HiBlackString("(↑/↓ to navigate, Enter to stream, Esc to quit)") + "\r\n"
		}
		sb.WriteString("\r\033[K" + title)

		// Column headers
		colHeader := fmt.Sprintf("  %-8s %-7s %-15s  %-18s  %s\r\n", "TYPE", "PORT", "PROJECT", "PROCESS / IMAGE", "PATH / TARGET")
		if useColor {
			colHeader = color.HiBlackString(colHeader)
		}
		sb.WriteString("\r\033[K" + colHeader)

		// Compute visible slice
		startIdx := 0
		if selected >= maxVisible {
			startIdx = selected - maxVisible + 1
		}
		endIdx := startIdx + maxVisible
		if endIdx > count {
			endIdx = count
		}

		linesCount := 2 // title + colHeader
		for i := startIdx; i < endIdx; i++ {
			line := FormatServiceItem(services[i], i == selected, useColor, termW)
			sb.WriteString("\r\033[K" + line + "\r\n")
			linesCount++
		}

		if count > maxVisible {
			scrollInfo := fmt.Sprintf("  ... [%d/%d services]\r\n", selected+1, count)
			if useColor {
				scrollInfo = color.HiBlackString(scrollInfo)
			}
			sb.WriteString("\r\033[K" + scrollInfo)
			linesCount++
		}

		renderedLines = linesCount
		fmt.Fprint(out, sb.String())
	}

	render()

	buf := make([]byte, 16)
	for {
		n, err := stdin.Read(buf)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			continue
		}

		b := buf[0]

		// Ctrl+C (3), Ctrl+D (4), 'q', 'Q'
		if b == 3 || b == 4 || b == 'q' || b == 'Q' {
			if renderedLines > 0 {
				fmt.Fprintf(out, "\033[%dA", renderedLines)
				for i := 0; i < renderedLines; i++ {
					fmt.Fprint(out, "\r\033[K\n")
				}
				fmt.Fprintf(out, "\033[%dA", renderedLines)
			}
			return nil, ErrPickerAborted
		}

		// Enter / Return (CR = 13, LF = 10)
		if b == 13 || b == 10 {
			if renderedLines > 0 {
				fmt.Fprintf(out, "\033[%dA", renderedLines)
				for i := 0; i < renderedLines; i++ {
					fmt.Fprint(out, "\r\033[K\n")
				}
				fmt.Fprintf(out, "\033[%dA", renderedLines)
			}
			return &services[selected], nil
		}

		// Escape sequences
		if b == 27 {
			if n == 1 {
				if renderedLines > 0 {
					fmt.Fprintf(out, "\033[%dA", renderedLines)
					for i := 0; i < renderedLines; i++ {
						fmt.Fprint(out, "\r\033[K\n")
					}
					fmt.Fprintf(out, "\033[%dA", renderedLines)
				}
				return nil, ErrPickerAborted
			}

			// Arrow keys: ESC [ A (up), ESC [ B (down)
			if n >= 3 && buf[1] == '[' {
				switch buf[2] {
				case 'A': // Up
					if selected > 0 {
						selected--
						render()
					}
				case 'B': // Down
					if selected < count-1 {
						selected++
						render()
					}
				}
			}
			continue
		}

		// Vim navigation: 'k' for up, 'j' for down
		if b == 'k' || b == 'K' {
			if selected > 0 {
				selected--
				render()
			}
		} else if b == 'j' || b == 'J' {
			if selected < count-1 {
				selected++
				render()
			}
		}
	}
}
