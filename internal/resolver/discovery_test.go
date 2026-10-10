package resolver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/specter-cli/specter/internal/util"
)

func TestParseSSListeningPorts(t *testing.T) {
	sample := `
State  Recv-Q Send-Q Local Address:Port  Peer Address:PortProcess
LISTEN 0      511          0.0.0.0:8080       0.0.0.0:*    users:(("python3",pid=8912,fd=3))
LISTEN 0      511             [::]:3000          [::]:*    users:(("node",pid=4920,fd=19))
LISTEN 0      128          0.0.0.0:80         0.0.0.0:*
LISTEN 0      4096         0.0.0.0:5432       0.0.0.0:*    users:(("postgres",pid=1420,fd=7))
`

	ports := parseSSListeningPorts(sample)
	if len(ports) != 4 {
		t.Fatalf("expected 4 listening ports, got %d", len(ports))
	}

	// 1. Port 8080 (python3)
	if ports[0].Port != 8080 || ports[0].PID != 8912 || ports[0].ProcessName != "python3" || ports[0].IsProtected {
		t.Errorf("unexpected port 8080 entry: %+v", ports[0])
	}

	// 2. Port 3000 (node, IPv6)
	if ports[1].Port != 3000 || ports[1].PID != 4920 || ports[1].ProcessName != "node" || ports[1].IsProtected {
		t.Errorf("unexpected port 3000 entry: %+v", ports[1])
	}

	// 3. Port 80 (protected without PID)
	if ports[2].Port != 80 || ports[2].PID != 0 || !ports[2].IsProtected {
		t.Errorf("expected protected port 80 entry, got: %+v", ports[2])
	}

	// 4. Port 5432 (postgres)
	if ports[3].Port != 5432 || ports[3].PID != 1420 || ports[3].ProcessName != "postgres" {
		t.Errorf("unexpected port 5432 entry: %+v", ports[3])
	}
}

func TestParseLsofListeningPorts(t *testing.T) {
	sample := `
COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
node     4920 dev    19u  IPv4  32819      0t0  TCP *:3001 (LISTEN)
python3  8912 dev     3u  IPv4  41920      0t0  TCP 127.0.0.1:8080 (LISTEN)
systemd     1 root   12u  IPv6  12345      0t0  TCP [::]:53 (LISTEN)
`

	ports := parseLsofListeningPorts(sample)
	if len(ports) != 3 {
		t.Fatalf("expected 3 ports, got %d", len(ports))
	}

	if ports[0].Port != 3001 || ports[0].PID != 4920 || ports[0].ProcessName != "node" {
		t.Errorf("unexpected port 3001 entry: %+v", ports[0])
	}
	if ports[1].Port != 8080 || ports[1].PID != 8912 || ports[1].ProcessName != "python3" {
		t.Errorf("unexpected port 8080 entry: %+v", ports[1])
	}
	if ports[2].Port != 53 || ports[2].PID != 1 || ports[2].ProcessName != "systemd" {
		t.Errorf("unexpected port 53 entry: %+v", ports[2])
	}
}

func TestRankSuggestions(t *testing.T) {
	active := []ListeningPort{
		{Port: 53, ProcessName: "systemd-resolved"},
		{Port: 5432, ProcessName: "postgres"},
		{Port: 3001, ProcessName: "node", PID: 4920},
		{Port: 8080, ProcessName: "python3", PID: 8912},
	}

	// Case 1: Target 3000 should suggest 3001 due to numerical proximity (+1) and node runtime
	ranked, top := RankSuggestions(3000, active)
	if len(ranked) != 4 {
		t.Fatalf("expected 4 ranked ports, got %d", len(ranked))
	}
	if top == nil || top.Port != 3001 {
		t.Fatalf("expected top suggestion 3001, got %+v", top)
	}

	// Case 2: Target 8000 should suggest 8080 due to block, edit distance, and python runtime
	_, top = RankSuggestions(8000, active)
	if top == nil || top.Port != 8080 {
		t.Fatalf("expected top suggestion 8080, got %+v", top)
	}

	// Case 3: Requested port itself (3001) in active list must NEVER be suggested as an alternative
	_, top = RankSuggestions(3001, active)
	if top != nil && top.Port == 3001 {
		t.Fatalf("suggestion must not be the requested port itself (got 3001)")
	}

	// Case 4: Only requested port active -> suggestion must be nil
	_, top = RankSuggestions(3000, []ListeningPort{{Port: 3000, ProcessName: "node"}})
	if top != nil {
		t.Fatalf("expected nil suggestion when only requested port is active, got %+v", top)
	}

	// Case 5: Invalid requested port (0 or negative) -> suggestion must be nil
	_, top = RankSuggestions(0, active)
	if top != nil {
		t.Fatalf("expected nil suggestion for invalid port 0, got %+v", top)
	}

	// Case 6: Binary containing 'go' as substring (e.g. dragonfly) should not get false dev runtime bonus
	daemons := []ListeningPort{{Port: 9000, ProcessName: "dragonfly"}}
	_, top = RankSuggestions(5000, daemons)
	if top != nil {
		t.Fatalf("expected nil suggestion for daemon dragonfly without proximity, got %+v", top)
	}
}

func TestDiscoverListeningPorts_Fallback(t *testing.T) {
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "ss" {
			return nil, errors.New("ss: command not found")
		}
		if name == "lsof" {
			sample := "node 1234 dev 19u IPv4 1234 0t0 TCP *:3000 (LISTEN)\n"
			return []byte(sample), nil
		}
		return nil, errors.New("command not found")
	}

	pr := NewPortResolver(mockRunner)
	ports, err := pr.DiscoverListeningPorts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ports) != 1 || ports[0].Port != 3000 {
		t.Fatalf("expected 1 port (3000), got %v", ports)
	}
}

func TestNoProcessOnPortError_Formatting(t *testing.T) {
	err := &NoProcessOnPortError{
		RequestedPort: 3000,
		ActivePorts: []ListeningPort{
			{Port: 3001, PID: 4920, ProcessName: "node"},
			{Port: 8080, PID: 8912, ProcessName: "python3"},
			{Port: 80, IsProtected: true},
		},
		Suggestion: &ListeningPort{Port: 3001},
	}

	// 1. Check errors.Is compatibility
	if !errors.Is(err, util.ErrNoProcessFound) {
		t.Errorf("expected NoProcessOnPortError to unwrap to ErrNoProcessFound")
	}

	// 2. Plain diagnostic output
	plain := err.FormatDiagnostic(false)
	if !strings.Contains(plain, "No process is listening on port 3000.") {
		t.Errorf("missing header in plain diagnostic: %s", plain)
	}
	if !strings.Contains(plain, ":3001") || !strings.Contains(plain, "PID 4920") || !strings.Contains(plain, "(node)") {
		t.Errorf("missing port 3001 details in plain diagnostic: %s", plain)
	}
	if !strings.Contains(plain, ":80") || !strings.Contains(plain, "run with sudo") {
		t.Errorf("missing protected port 80 details: %s", plain)
	}
	if !strings.Contains(plain, "Did you mean: specter 3001 ?") {
		t.Errorf("missing suggestion in plain diagnostic: %s", plain)
	}

	// 3. Colored diagnostic output
	colored := err.FormatDiagnostic(true)
	if !strings.Contains(colored, "No process is listening on port 3000.") {
		t.Errorf("missing header in colored diagnostic: %s", colored)
	}
	if !strings.Contains(colored, "Did you mean:") {
		t.Errorf("missing suggestion in colored diagnostic: %s", colored)
	}
}

func TestDefaultResolver_UnallocatedPortDiagnostic(t *testing.T) {
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "lsof" {
			// lsof finds nothing for port 3000
			return nil, errors.New("exit status 1")
		}
		if name == "ss" {
			// ss reports port 3001 and 8080 active
			sample := "LISTEN 0 511 0.0.0.0:3001 0.0.0.0:* users:((\"node\",pid=4920,fd=19))\n" +
				"LISTEN 0 511 0.0.0.0:8080 0.0.0.0:* users:((\"python3\",pid=8912,fd=3))\n"
			return []byte(sample), nil
		}
		return nil, errors.New("unknown command")
	}

	resolver := NewDefaultResolver(mockRunner, t.TempDir())
	_, err := resolver.ResolvePort(context.Background(), 3000)
	if err == nil {
		t.Fatal("expected error for unallocated port 3000, got nil")
	}

	var noProcErr *NoProcessOnPortError
	if !errors.As(err, &noProcErr) {
		t.Fatalf("expected error to be *NoProcessOnPortError, got: %T (%v)", err, err)
	}

	if noProcErr.RequestedPort != 3000 {
		t.Errorf("expected requested port 3000, got %d", noProcErr.RequestedPort)
	}
	if len(noProcErr.ActivePorts) != 2 {
		t.Fatalf("expected 2 active ports, got %d", len(noProcErr.ActivePorts))
	}
	if noProcErr.Suggestion == nil || noProcErr.Suggestion.Port != 3001 {
		t.Errorf("expected suggestion for port 3001, got: %+v", noProcErr.Suggestion)
	}

	diag := noProcErr.FormatDiagnostic(false)
	if !strings.Contains(diag, "No process is listening on port 3000.") {
		t.Errorf("expected diagnostic header, got: %s", diag)
	}
	if !strings.Contains(diag, "Did you mean: specter 3001 ?") {
		t.Errorf("expected suggestion 'specter 3001', got: %s", diag)
	}
}

func TestFormatDiagnostic_TruncationFooter(t *testing.T) {
	ports := make([]ListeningPort, 15)
	for i := 0; i < 15; i++ {
		ports[i] = ListeningPort{
			Port:        3000 + i,
			PID:         1000 + i,
			ProcessName: "app",
		}
	}

	err := &NoProcessOnPortError{
		RequestedPort: 9999,
		ActivePorts:   ports,
	}

	diag := err.FormatDiagnostic(false)

	// Top 8 ports (3000..3007) should be displayed
	for i := 0; i < 8; i++ {
		portStr := fmt.Sprintf(":%d", 3000+i)
		if !strings.Contains(diag, portStr) {
			t.Errorf("expected port %s to be displayed, output:\n%s", portStr, diag)
		}
	}

	// Ports 8..14 (3008..3014) should not be displayed
	for i := 8; i < 15; i++ {
		portStr := fmt.Sprintf(":%d", 3000+i)
		if strings.Contains(diag, portStr) {
			t.Errorf("expected port %s to be omitted, output:\n%s", portStr, diag)
		}
	}

	// Truncation footer: "  • ... and 7 more active ports"
	expectedFooter := "  • ... and 7 more active ports"
	if !strings.Contains(diag, expectedFooter) {
		t.Errorf("expected truncation footer %q, got output:\n%s", expectedFooter, diag)
	}
}

func TestRankSuggestions_TypoCorrection(t *testing.T) {
	t.Run("typo 3000 when 3001 is open", func(t *testing.T) {
		active := []ListeningPort{
			{Port: 22, ProcessName: "sshd"},
			{Port: 53, ProcessName: "systemd-resolved"},
			{Port: 3001, ProcessName: "node", PID: 4920},
		}

		_, top := RankSuggestions(3000, active)
		if top == nil {
			t.Fatal("expected suggestion for typo 3000 -> 3001, got nil")
		}
		if top.Port != 3001 {
			t.Errorf("expected top suggestion port 3001, got %d (%s)", top.Port, top.ProcessName)
		}
		if top.ProcessName != "node" {
			t.Errorf("expected dev runtime 'node', got %s", top.ProcessName)
		}
	})

	t.Run("typo 8000 when 8080 is open", func(t *testing.T) {
		active := []ListeningPort{
			{Port: 22, ProcessName: "sshd"},
			{Port: 53, ProcessName: "systemd-resolved"},
			{Port: 8080, ProcessName: "python3", PID: 8912},
		}

		_, top := RankSuggestions(8000, active)
		if top == nil {
			t.Fatal("expected suggestion for typo 8000 -> 8080, got nil")
		}
		if top.Port != 8080 {
			t.Errorf("expected top suggestion port 8080, got %d (%s)", top.Port, top.ProcessName)
		}
		if top.ProcessName != "python3" {
			t.Errorf("expected dev runtime 'python3', got %s", top.ProcessName)
		}
	})
}

func TestParseLsof_SpacesInCommand(t *testing.T) {
	sample := `
COMMAND        PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
Google Chrome 1234 dev    22u  IPv4  32821      0t0  TCP 127.0.0.1:9222 (LISTEN)
python 3.11   5020 dev    20u  IPv6  32820      0t0  TCP [::]:8080 (LISTEN)
`

	ports := parseLsofListeningPorts(sample)
	if len(ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(ports))
	}

	if ports[0].Port != 9222 || ports[0].PID != 1234 || ports[0].ProcessName != "Google Chrome" {
		t.Errorf("unexpected first port entry: %+v", ports[0])
	}

	if ports[1].Port != 8080 || ports[1].PID != 5020 || ports[1].ProcessName != "python 3.11" {
		t.Errorf("unexpected second port entry: %+v", ports[1])
	}
}

func TestParseSS_IPv6AndWildcard(t *testing.T) {
	sample := `
State  Recv-Q Send-Q Local Address:Port  Peer Address:PortProcess
LISTEN 0      511             [::]:8080          [::]:*    users:(("python3",pid=8912,fd=3))
LISTEN 0      511           [::1]:3000          [::]:*    users:(("node",pid=4920,fd=19))
LISTEN 0      128                *:8000             *:*    users:(("caddy",pid=1420,fd=7))
`

	ports := parseSSListeningPorts(sample)
	if len(ports) != 3 {
		t.Fatalf("expected 3 ports, got %d", len(ports))
	}

	if ports[0].Port != 8080 || ports[0].PID != 8912 || ports[0].ProcessName != "python3" {
		t.Errorf("unexpected port 8080 entry: %+v", ports[0])
	}

	if ports[1].Port != 3000 || ports[1].PID != 4920 || ports[1].ProcessName != "node" {
		t.Errorf("unexpected port 3000 entry: %+v", ports[1])
	}

	if ports[2].Port != 8000 || ports[2].PID != 1420 || ports[2].ProcessName != "caddy" {
		t.Errorf("unexpected port 8000 entry: %+v", ports[2])
	}
}
