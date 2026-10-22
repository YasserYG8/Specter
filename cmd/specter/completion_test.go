package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/specter-cli/specter/internal/resolver"
)

func TestCompletionCmd_Shells(t *testing.T) {
	shells := []struct {
		shell       string
		expectedSub string
	}{
		{"bash", "complete"},
		{"zsh", "compdef"},
		{"fish", "complete -c specter"},
		{"powershell", "Register-ArgumentCompleter"},
	}

	for _, tt := range shells {
		t.Run(tt.shell, func(t *testing.T) {
			cmd := newRootCmd()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"completion", tt.shell})

			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error executing completion for %s: %v", tt.shell, err)
			}

			out := stdout.String()
			if len(out) == 0 {
				t.Fatalf("expected non-empty completion output for %s", tt.shell)
			}
			if !strings.Contains(out, tt.expectedSub) {
				t.Errorf("expected completion script for %s to contain %q, but got:\n%s", tt.shell, tt.expectedSub, out[:min(len(out), 300)])
			}
		})
	}
}

func TestCompletionCmd_UnsupportedShell(t *testing.T) {
	cmd := newRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"completion", "tcsh"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unsupported shell, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Errorf("expected error message to mention 'unsupported shell', got: %v", err)
	}
}

func TestCompletionCmd_MissingArg(t *testing.T) {
	cmd := newRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"completion"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when shell argument is missing, got nil")
	}
}

func TestValidArgsFunction_PortDiscovery(t *testing.T) {
	origPortFunc := portDiscoveryFunc
	t.Cleanup(func() {
		portDiscoveryFunc = origPortFunc
	})

	mockPorts := []resolver.ListeningPort{
		{Port: 3000, PID: 1234, ProcessName: "node"},
		{Port: 8080, PID: 5678, ProcessName: "python3"},
		{Port: 443, PID: 0, ProcessName: "", IsProtected: true},
		{Port: 9000, PID: 9999, ProcessName: ""},
		{Port: 9001, PID: 0, ProcessName: ""},
	}

	portDiscoveryFunc = func(ctx context.Context) ([]resolver.ListeningPort, error) {
		return mockPorts, nil
	}

	cmd := newRootCmd()

	// 1. Unfiltered completion
	completions, directive := cmd.ValidArgsFunction(cmd, []string{}, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}
	if len(completions) != 5 {
		t.Fatalf("expected 5 completions, got %d: %v", len(completions), completions)
	}

	expectedMatches := []string{
		"3000\tnode (PID 1234)",
		"8080\tpython3 (PID 5678)",
		"443\tprotected process (run with sudo)",
		"9000\tPID 9999",
		"9001\tlistening port",
	}
	for i, exp := range expectedMatches {
		if completions[i] != exp {
			t.Errorf("item %d: expected %q, got %q", i, exp, completions[i])
		}
	}

	// 2. Prefix filtered completion
	filtered, _ := cmd.ValidArgsFunction(cmd, []string{}, "8")
	if len(filtered) != 1 || !strings.HasPrefix(filtered[0], "8080") {
		t.Errorf("expected only 8080 for prefix '8', got %v", filtered)
	}

	// 3. Positional argument already provided
	alreadySupplied, _ := cmd.ValidArgsFunction(cmd, []string{"3000"}, "")
	if len(alreadySupplied) != 0 {
		t.Errorf("expected no completions when argument already given, got %v", alreadySupplied)
	}
}

func TestValidArgsFunction_EmptyOrError(t *testing.T) {
	origPortFunc := portDiscoveryFunc
	t.Cleanup(func() {
		portDiscoveryFunc = origPortFunc
	})

	// Case 1: Error during discovery
	portDiscoveryFunc = func(ctx context.Context) ([]resolver.ListeningPort, error) {
		return nil, errors.New("ss failed")
	}

	cmd := newRootCmd()
	completions, directive := cmd.ValidArgsFunction(cmd, []string{}, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp on error, got %v", directive)
	}
	if len(completions) != 0 {
		t.Errorf("expected empty completions on error, got %v", completions)
	}

	// Case 2: No ports discovered
	portDiscoveryFunc = func(ctx context.Context) ([]resolver.ListeningPort, error) {
		return []resolver.ListeningPort{}, nil
	}

	completions, directive = cmd.ValidArgsFunction(cmd, []string{}, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp on empty, got %v", directive)
	}
	if len(completions) != 0 {
		t.Errorf("expected empty completions on empty list, got %v", completions)
	}
}

func TestValidArgsFunction_SanitizesControlCharacters(t *testing.T) {
	origPortFunc := portDiscoveryFunc
	t.Cleanup(func() {
		portDiscoveryFunc = origPortFunc
	})

	mockPorts := []resolver.ListeningPort{
		{Port: 5000, PID: 111, ProcessName: "evil\tproc\nname\r"},
	}

	portDiscoveryFunc = func(ctx context.Context) ([]resolver.ListeningPort, error) {
		return mockPorts, nil
	}

	cmd := newRootCmd()
	completions, directive := cmd.ValidArgsFunction(cmd, []string{}, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}
	if len(completions) != 1 {
		t.Fatalf("expected 1 completion, got %d", len(completions))
	}

	// Completion must have exactly one tab delimiter between candidate and description
	parts := strings.Split(completions[0], "\t")
	if len(parts) != 2 {
		t.Fatalf("expected exactly 2 tab-separated fields, got %d in %q", len(parts), completions[0])
	}
	if parts[0] != "5000" {
		t.Errorf("expected candidate 5000, got %q", parts[0])
	}
	if strings.ContainsAny(parts[1], "\t\n\r") {
		t.Errorf("description contains unsanitized control characters: %q", parts[1])
	}
	if !strings.Contains(parts[1], "evil proc name (PID 111)") {
		t.Errorf("unexpected description format: %q", parts[1])
	}
}

func TestFlagCompletions(t *testing.T) {
	cmd := newRootCmd()

	// 1. Test --source flag completions
	sourceFn, exists := cmd.GetFlagCompletionFunc("source")
	if !exists || sourceFn == nil {
		t.Fatal("expected flag completion function for --source")
	}
	candidates, directive := sourceFn(cmd, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp for --source, got %v", directive)
	}
	expectedSources := []string{"strace", "file", "docker", "journal"}
	for _, exp := range expectedSources {
		found := false
		for _, c := range candidates {
			if strings.HasPrefix(c, exp+"\t") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected source %q in completions: %v", exp, candidates)
		}
	}

	// 2. Test --level flag completions
	levelFn, exists := cmd.GetFlagCompletionFunc("level")
	if !exists || levelFn == nil {
		t.Fatal("expected flag completion function for --level")
	}
	candidates, directive = levelFn(cmd, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp for --level, got %v", directive)
	}
	expectedLevels := []string{"debug", "info", "warn", "error"}
	for _, exp := range expectedLevels {
		found := false
		for _, c := range candidates {
			if strings.HasPrefix(c, exp+"\t") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected level %q in completions: %v", exp, candidates)
		}
	}
}

func TestCLI_HelpExamples(t *testing.T) {
	cmd := newRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error executing --help: %v", err)
	}

	out := stdout.String()
	requiredSnippets := []string{
		"Examples:",
		"specter 3000",
		"specter 14280",
		"--level warn",
		"--grep",
		"--stderr",
		"--tree",
		"--source strace",
		"--json",
		"--no-color",
	}

	for _, snippet := range requiredSnippets {
		if !strings.Contains(out, snippet) {
			t.Errorf("expected --help output to contain %q, but got:\n%s", snippet, out)
		}
	}
}

func TestValidArgsFunction_ContainerCompletions(t *testing.T) {
	origPortFunc := portDiscoveryFunc
	t.Cleanup(func() {
		portDiscoveryFunc = origPortFunc
	})

	mockPorts := []resolver.ListeningPort{
		{
			Port:          8080,
			PID:           0,
			ContainerName: "phpmyadmin",
			Image:         "phpmyadmin:latest",
		},
		{
			Port:          3306,
			PID:           0,
			ContainerName: "mysql_db",
			Image:         "mysql:8.4",
		},
	}

	portDiscoveryFunc = func(ctx context.Context) ([]resolver.ListeningPort, error) {
		return mockPorts, nil
	}

	cmd := newRootCmd()

	// 1. All completions should include both ports and container names
	completions, directive := cmd.ValidArgsFunction(cmd, []string{}, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}

	if len(completions) != 4 {
		t.Fatalf("expected 4 completions, got %d: %v", len(completions), completions)
	}

	// 2. Filter by container name prefix
	filtered, _ := cmd.ValidArgsFunction(cmd, []string{}, "php")
	if len(filtered) != 1 || !strings.HasPrefix(filtered[0], "phpmyadmin") {
		t.Errorf("expected only phpmyadmin for prefix 'php', got %v", filtered)
	}
}

func TestValidArgsFunction_ProjectCompletions(t *testing.T) {
	origPortFunc := portDiscoveryFunc
	t.Cleanup(func() {
		portDiscoveryFunc = origPortFunc
	})

	mockPorts := []resolver.ListeningPort{
		{
			Port:        3000,
			PID:         1234,
			ProcessName: "node",
			ProjectName: "example-app",
		},
	}

	portDiscoveryFunc = func(ctx context.Context) ([]resolver.ListeningPort, error) {
		return mockPorts, nil
	}

	cmd := newRootCmd()

	completions, _ := cmd.ValidArgsFunction(cmd, []string{}, "")
	if len(completions) != 2 {
		t.Fatalf("expected 2 completions (port + project), got %d: %v", len(completions), completions)
	}

	filtered, _ := cmd.ValidArgsFunction(cmd, []string{}, "ex")
	if len(filtered) != 1 || !strings.HasPrefix(filtered[0], "example-app") {
		t.Errorf("expected only example-app for prefix 'ex', got %v", filtered)
	}
}
