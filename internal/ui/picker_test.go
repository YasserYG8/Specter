package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/specter-cli/specter/internal/resolver"
)

func TestFormatServiceItem(t *testing.T) {
	hostSvc := resolver.DiscoveredService{
		Kind:        resolver.ServiceKindHost,
		Port:        3000,
		PID:         1234,
		ProcessName: "node",
		Description: "node (PID 1234)",
	}

	// 1. Unselected host
	unselected := FormatServiceItem(hostSvc, false, false)
	if !strings.HasPrefix(unselected, "  ") || !strings.Contains(unselected, "[host]") || !strings.Contains(unselected, ":3000") {
		t.Errorf("unexpected unselected host format: %q", unselected)
	}

	// 2. Selected host
	selected := FormatServiceItem(hostSvc, true, false)
	if !strings.HasPrefix(selected, "> ") {
		t.Errorf("expected cursor '> ' on selected item: %q", selected)
	}

	// 3. Docker service with port mapping
	dockerSvc := resolver.DiscoveredService{
		Kind:          resolver.ServiceKindDocker,
		Port:          8080,
		ContainerPort: 80,
		ContainerName: "phpmyadmin",
		Image:         "phpmyadmin:latest",
		Description:   "docker:phpmyadmin",
	}

	dockerLine := FormatServiceItem(dockerSvc, false, false)
	if !strings.Contains(dockerLine, "[docker]") || !strings.Contains(dockerLine, ":8080") || !strings.Contains(dockerLine, ":80") {
		t.Errorf("unexpected docker line format: %q", dockerLine)
	}
}

func TestRenderNonInteractiveTable(t *testing.T) {
	services := []resolver.DiscoveredService{
		{
			Kind:        resolver.ServiceKindHost,
			Port:        3000,
			PID:         1234,
			ProcessName: "node",
			Description: "node (PID 1234)",
		},
		{
			Kind:          resolver.ServiceKindDocker,
			Port:          8080,
			ContainerPort: 80,
			ContainerName: "phpmyadmin",
			Image:         "phpmyadmin:latest",
			Description:   "docker:phpmyadmin",
		},
		{
			Kind:          resolver.ServiceKindDocker,
			ContainerName: "queue_worker",
			Image:         "worker:latest",
			Description:   "queue_worker (worker:latest)",
		},
	}

	var buf bytes.Buffer
	RenderNonInteractiveTable(services, &buf, false)
	output := buf.String()

	if !strings.Contains(output, "TYPE") || !strings.Contains(output, "PORT") || !strings.Contains(output, "TARGET") {
		t.Errorf("missing table headers in output: %s", output)
	}
	if !strings.Contains(output, "host") || !strings.Contains(output, ":3000") || !strings.Contains(output, "PID 1234") {
		t.Errorf("missing host service row: %s", output)
	}
	if !strings.Contains(output, "docker") || !strings.Contains(output, ":8080") || !strings.Contains(output, "phpmyadmin") {
		t.Errorf("missing docker service row: %s", output)
	}
	if !strings.Contains(output, "queue_worker") {
		t.Errorf("missing portless container row: %s", output)
	}
	if !strings.Contains(output, "specter <port|pid|container>") {
		t.Errorf("missing remediation advice: %s", output)
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
			Description: "web",
		},
	}

	var inBuf bytes.Buffer
	var outBuf bytes.Buffer

	cfg := PickerConfig{
		In:       &inBuf, // bytes.Buffer is not an os.File, so isTTY is false
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
	if !strings.Contains(outBuf.String(), ":4000") || !strings.Contains(outBuf.String(), "PID 999") {
		t.Errorf("expected table output, got: %s", outBuf.String())
	}
}
