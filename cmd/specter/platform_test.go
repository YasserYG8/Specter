package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/YasserYG8/Specter/internal/util"
)

func TestUnsupportedPlatformError(t *testing.T) {
	cases := []struct {
		name    string
		goos    string
		target  string
		want    []string // every one must appear in the message
		notWant []string
	}{
		{
			name:    "windows suggests WSL with the same target",
			goos:    "windows",
			target:  "3000",
			want:    []string{"this is Windows", "Linux kernel interfaces", "wsl specter 3000"},
			notWant: []string{"macOS", "OrbStack"},
		},
		{
			name:    "windows keeps a --pid target",
			goos:    "windows",
			target:  "--pid 14280",
			want:    []string{"wsl specter --pid 14280"},
			notWant: []string{"macOS"},
		},
		{
			name:    "windows keeps a project name",
			goos:    "windows",
			target:  "my-api",
			want:    []string{"wsl specter my-api"},
			notWant: nil,
		},
		{
			name:    "macOS is named and gets a VM or Docker advice",
			goos:    "darwin",
			target:  "3000",
			want:    []string{"this is macOS", "macOS does not provide", "Linux VM", "OrbStack", "Docker container"},
			notWant: []string{"darwin", "WSL", "wsl specter"},
		},
		{
			name:    "any other system gets the generic advice",
			goos:    "freebsd",
			target:  "3000",
			want:    []string{"this is freebsd", "Run your server and Specter on Linux."},
			notWant: []string{"WSL", "OrbStack"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := unsupportedPlatformError(tc.goos, tc.target)
			if !errors.Is(err, util.ErrUnsupportedPlatform) {
				t.Fatalf("expected ErrUnsupportedPlatform, got %v", err)
			}
			var diag *util.DiagnosticError
			if !errors.As(err, &diag) {
				t.Fatalf("expected a DiagnosticError, got %T", err)
			}
			text := err.Error()
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("message is missing %q:\n%s", want, text)
				}
			}
			for _, not := range tc.notWant {
				if strings.Contains(text, not) {
					t.Errorf("message should not contain %q:\n%s", not, text)
				}
			}
		})
	}
}

// The check is in the root command's RunE, which only runs when Specter would attach. These commands have their
// own handlers, so they work on every system. The test runs them through the command tree, so on macOS or Windows
// it fails if the check moves somewhere that subcommands share.
func TestCLI_CommandsThatDoNotAttachSkipThePlatformCheck(t *testing.T) {
	for _, args := range [][]string{
		{"version"},
		{"completion", "bash"},
		{"completion", "zsh"},
		{"--help"},
		{"--version"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := newRootCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("`specter %s` failed: %v", strings.Join(args, " "), err)
			}
			if strings.Contains(out.String(), "needs Linux") {
				t.Errorf("`specter %s` printed the platform diagnostic:\n%s", strings.Join(args, " "), out.String())
			}
		})
	}

	// Nothing the subcommands share may run the check
	root := newRootCmd()
	if root.PersistentPreRunE != nil || root.PersistentPreRun != nil {
		t.Error("a persistent pre-run hook on the root command would run for version and completion as well")
	}
}
