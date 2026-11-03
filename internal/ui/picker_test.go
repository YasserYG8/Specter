package ui

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/YasserYG8/Specter/internal/resolver"
	"github.com/fatih/color"
)

func TestFormatServiceItem(t *testing.T) {
	hostSvc := resolver.DiscoveredService{
		Kind:        resolver.ServiceKindHost,
		Port:        3000,
		PID:         1234,
		ProcessName: "node",
		ProjectName: "example-app",
		WorkingDir:  "/home/yasser/projects/example-app",
		Description: "node (PID 1234)",
	}

	// 1. Unselected host with project
	unselected := FormatServiceItem(hostSvc, false, false)
	if !strings.HasPrefix(unselected, "  ") || !strings.Contains(unselected, "[host]") || !strings.Contains(unselected, ":3000") || !strings.Contains(unselected, "example-app") {
		t.Errorf("unexpected unselected host format: %q", unselected)
	}

	// 2. Selected host
	selected := FormatServiceItem(hostSvc, true, false)
	if !strings.HasPrefix(selected, "> ") {
		t.Errorf("expected cursor '> ' on selected item: %q", selected)
	}

	// 3. Docker service with project name (e.g. Compose)
	dockerSvc := resolver.DiscoveredService{
		Kind:          resolver.ServiceKindDocker,
		Port:          8080,
		ContainerPort: 80,
		ContainerName: "phpmyadmin",
		ProjectName:   "soa/phpmyadmin",
		Image:         "phpmyadmin:latest",
		Description:   "docker:phpmyadmin",
	}

	dockerLine := FormatServiceItem(dockerSvc, false, false)
	if !strings.Contains(dockerLine, "[docker]") || !strings.Contains(dockerLine, ":8080") || !strings.Contains(dockerLine, "soa/phpmyadmin") {
		t.Errorf("unexpected docker line format: %q", dockerLine)
	}

	// 4. Responsive width truncation
	truncated := FormatServiceItem(hostSvc, false, false, 35)
	if len(truncated) > 35 {
		t.Errorf("expected line to be truncated to max 35 chars, got length %d: %q", len(truncated), truncated)
	}
}

func TestRenderNonInteractiveTable(t *testing.T) {
	services := []resolver.DiscoveredService{
		{
			Kind:        resolver.ServiceKindHost,
			Port:        3000,
			PID:         1234,
			ProcessName: "node",
			ProjectName: "example-app",
			WorkingDir:  "/home/user/example-app",
			Description: "node (PID 1234)",
		},
		{
			Kind:          resolver.ServiceKindDocker,
			Port:          8080,
			ContainerPort: 80,
			ContainerName: "phpmyadmin",
			ProjectName:   "soa/phpmyadmin",
			Image:         "phpmyadmin:latest",
			Description:   "docker:phpmyadmin",
		},
		{
			Kind:          resolver.ServiceKindDocker,
			ContainerName: "queue_worker",
			ProjectName:   "queue_worker",
			Image:         "worker:latest",
			Description:   "queue_worker (worker:latest)",
		},
	}

	var buf bytes.Buffer
	RenderNonInteractiveTable(services, &buf, false)
	output := buf.String()

	if !strings.Contains(output, "TYPE") || !strings.Contains(output, "PORT") || !strings.Contains(output, "PROJECT") {
		t.Errorf("missing table headers in output: %s", output)
	}
	if !strings.Contains(output, "host") || !strings.Contains(output, ":3000") || !strings.Contains(output, "example-app") {
		t.Errorf("missing host service row: %s", output)
	}
	if !strings.Contains(output, "docker") || !strings.Contains(output, ":8080") || !strings.Contains(output, "soa/phpmyadmin") {
		t.Errorf("missing docker service row: %s", output)
	}
	if !strings.Contains(output, "queue_worker") {
		t.Errorf("missing portless container row: %s", output)
	}
}

func TestSelectService_Empty(t *testing.T) {
	var inBuf bytes.Buffer
	var outBuf bytes.Buffer

	cfg := PickerConfig{
		In:       &inBuf,
		Out:      &outBuf,
		UseColor: false,
	}

	selected, err := SelectService(nil, cfg)
	if err != nil {
		t.Fatalf("unexpected error for empty services: %v", err)
	}
	if selected != nil {
		t.Errorf("expected nil selection, got %+v", selected)
	}
	if !strings.Contains(outBuf.String(), "No active listening ports") {
		t.Errorf("expected empty state notice, got %q", outBuf.String())
	}
}

func TestSelectService_NonTTYFallback(t *testing.T) {
	services := []resolver.DiscoveredService{
		{
			Kind:        resolver.ServiceKindHost,
			Port:        4000,
			PID:         999,
			ProcessName: "web",
			ProjectName: "dashboard",
			Description: "web",
		},
	}

	var inBuf bytes.Buffer
	var outBuf bytes.Buffer

	cfg := PickerConfig{
		In:       &inBuf,
		Out:      &outBuf,
		UseColor: false,
	}

	selected, err := SelectService(services, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected != nil {
		t.Errorf("expected nil selection in non-TTY mode, got %+v", selected)
	}
	if !strings.Contains(outBuf.String(), ":4000") || !strings.Contains(outBuf.String(), "dashboard") {
		t.Errorf("expected table output, got: %s", outBuf.String())
	}
}

// The picker redraws by moving the cursor up one line per row it printed. A row wider than the terminal wraps
// onto a second line, the cursor ends up one line short, and the title is left behind on every keypress.
// So every row must fit the terminal width, selected or not, colored or not.
func TestFormatServiceItem_NeverWiderThanTerminal(t *testing.T) {
	oldNoColor := color.NoColor
	color.NoColor = false
	defer func() { color.NoColor = oldNoColor }()

	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	svc := resolver.DiscoveredService{
		Kind:        resolver.ServiceKindHost,
		Port:        4000,
		PID:         63787,
		ProcessName: "bun",
		ProjectName: "agentgraph",
		WorkingDir:  "/mnt/c/Users/Lenovo/Desktop/projects/agentgraph",
	}

	for _, width := range []int{60, 80, 100} {
		for _, useColor := range []bool{false, true} {
			for _, selected := range []bool{false, true} {
				row := FormatServiceItem(svc, selected, useColor, width)
				if visible := len(ansi.ReplaceAllString(row, "")); visible > width {
					t.Errorf("width=%d color=%v selected=%v: row is %d columns wide and will wrap: %q",
						width, useColor, selected, visible, ansi.ReplaceAllString(row, ""))
				}
			}
		}
	}
}
