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
	header := "[specter] Active Services & Containers:"
	if useColor {
		header = color.CyanString(header)
	}
	fmt.Fprintln(out, header)
	fmt.Fprintf(out, "%-8s  %-8s  %-16s  %s\n", "TYPE", "PORT", "TARGET", "DETAILS")
	fmt.Fprintf(out, "%-8s  %-8s  %-16s  %s\n", "----", "----", "------", "-------")

	for _, s := range services {
		typeStr := string(s.Kind)
		portStr := "-"
		if s.Port > 0 {
			portStr = fmt.Sprintf(":%d", s.Port)
		}

		targetStr := "-"
		if s.Kind == resolver.ServiceKindDocker {
			targetStr = s.ContainerName
			if targetStr == "" {
				targetStr = s.ContainerID
			}
		} else if s.PID > 0 {
			targetStr = fmt.Sprintf("PID %d", s.PID)
		}

		details := s.Description
		if s.IsProtected {
			details = "protected (requires sudo)"
		}

		fmt.Fprintf(out, "%-8s  %-8s  %-16s  %s\n", typeStr, portStr, targetStr, details)
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "To attach, run: specter <port|pid|container>")
}

// FormatServiceItem formats a single service entry for picker display.
func FormatServiceItem(s resolver.DiscoveredService, isSelected bool, useColor bool) string {
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

	desc := s.Description
	if s.Kind == resolver.ServiceKindDocker && s.ContainerPort > 0 && s.Port > 0 {
		desc = fmt.Sprintf("-> :%d (%s, %s)", s.ContainerPort, s.ContainerName, s.Image)
	}

	line := fmt.Sprintf("%s%s %s %s", cursor, tag, portStr, desc)

	if useColor {
		if isSelected {
			return color.CyanString(line)
		}
		if s.Kind == resolver.ServiceKindDocker {
			return fmt.Sprintf("%s%s %s %s", cursor, color.BlueString(tag), color.YellowString(portStr), desc)
		}
		return fmt.Sprintf("%s%s %s %s", cursor, color.GreenString(tag), color.YellowString(portStr), desc)
	}

	return line
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

	// Determine max visible window
	maxVisible := 10
	if _, height, err := term.GetSize(fd); err == nil && height > 6 {
		if height-4 < maxVisible {
			maxVisible = height - 4
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
		header := "Specter · Select Service (Use ↑/↓ or j/k to navigate, Enter to stream, Esc/Ctrl+C to quit)\r\n"
		if useColor {
			header = color.CyanString("Specter") + " · Select Service " + color.HiBlackString("(Use ↑/↓ to navigate, Enter to stream, Esc to quit)") + "\r\n"
		}
		sb.WriteString("\r\033[K" + header)

		// Compute visible slice
		startIdx := 0
		if selected >= maxVisible {
			startIdx = selected - maxVisible + 1
		}
		endIdx := startIdx + maxVisible
		if endIdx > count {
			endIdx = count
		}

		linesCount := 1 // header
		for i := startIdx; i < endIdx; i++ {
			line := FormatServiceItem(services[i], i == selected, useColor)
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

		// Handle key strokes
		b := buf[0]

		// Ctrl+C (3), Ctrl+D (4), 'q', 'Q'
		if b == 3 || b == 4 || b == 'q' || b == 'Q' {
			// Clear rendered lines
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
			// Clear rendered lines before attaching
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
				// Bare ESC key -> abort
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
