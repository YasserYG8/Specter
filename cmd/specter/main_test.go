package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"bytes"
	"github.com/YasserYG8/Specter/internal/util"
	"testing"
)

func TestCLI_InvalidArgument(t *testing.T) {
	cmd := newRootCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"abc"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-numeric argument, got nil")
	}
}

func TestCLI_NegativePort(t *testing.T) {
	cmd := newRootCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"-5"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for negative port/pid argument, got nil")
	}
}

func TestBuildVersionString(t *testing.T) {
	str := buildVersionString()
	if str == "" {
		t.Fatal("expected non-empty version string")
	}
	if !bytes.Contains([]byte(str), []byte(version)) {
		t.Errorf("expected version %q in string, got %s", version, str)
	}
	if !bytes.Contains([]byte(str), []byte("commit:")) {
		t.Errorf("expected commit marker in version string, got %s", str)
	}
	if !bytes.Contains([]byte(str), []byte("built at:")) {
		t.Errorf("expected built at marker in version string, got %s", str)
	}
}

func TestCLI_VersionSubcommand(t *testing.T) {
	cmd := newRootCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error executing version command: %v", err)
	}
}

func TestCLI_ZeroArgsAccepted(t *testing.T) {
	cmd := newRootCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil && bytes.Contains([]byte(err.Error()), []byte("accepts 1 arg(s)")) {
		t.Fatalf("unexpected arg count error: %v", err)
	}
}

func TestValidateLevel(t *testing.T) {
	for _, ok := range []string{"", "  ", "debug", "info", "warn", "warning", "ERROR", "err", "fatal", "trace", "critical"} {
		if err := validateLevel(ok); err != nil {
			t.Errorf("validateLevel(%q) = %v, want nil", ok, err)
		}
	}

	err := validateLevel("banana")
	if err == nil {
		t.Fatal("expected an error for an unknown level")
	}
	var diag *util.DiagnosticError
	if !errors.As(err, &diag) {
		t.Fatalf("expected a DiagnosticError, got %T", err)
	}
	for _, level := range []string{"debug", "info", "warn", "error"} {
		if !strings.Contains(diag.Remediation, level) {
			t.Errorf("remediation %q does not list %q", diag.Remediation, level)
		}
	}
	if !errors.Is(err, util.ErrInvalidFlag) {
		t.Error("expected the error to wrap ErrInvalidFlag")
	}
}

func TestCLI_UnknownLevelFailsBeforeResolving(t *testing.T) {
	cmd := newRootCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetOut(&stderr)
	cmd.SetArgs([]string{"3000", "--level", "banana"})
	defer func() { flagLevel = "" }()

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "banana") {
		t.Fatalf("expected an error naming the bad level, got %v", err)
	}
}

func TestNoOutputNotice(t *testing.T) {
	t.Run("silent target gets one notice", func(t *testing.T) {
		var buf syncBuffer
		_, stop := startNoOutputNotice(context.Background(), 20*time.Millisecond, &buf)
		defer stop()
		time.Sleep(150 * time.Millisecond)
		out := buf.String()
		if strings.Count(out, "Nothing captured yet") != 1 {
			t.Errorf("expected exactly one notice, got %q", out)
		}
	})

	t.Run("output within the delay cancels it", func(t *testing.T) {
		var buf syncBuffer
		saw, stop := startNoOutputNotice(context.Background(), 50*time.Millisecond, &buf)
		defer stop()
		saw()
		time.Sleep(150 * time.Millisecond)
		if out := buf.String(); out != "" {
			t.Errorf("expected no notice, got %q", out)
		}
	})

	t.Run("canceled context prints nothing", func(t *testing.T) {
		var buf syncBuffer
		ctx, cancel := context.WithCancel(context.Background())
		_, stop := startNoOutputNotice(ctx, 20*time.Millisecond, &buf)
		defer stop()
		cancel()
		time.Sleep(100 * time.Millisecond)
		if out := buf.String(); out != "" {
			t.Errorf("expected no notice, got %q", out)
		}
	})
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The README's flag table promises these short forms, so a test keeps the two in step.
func TestCLI_ShortFlagsMapToLongFlags(t *testing.T) {
	for short, long := range map[string]string{"l": "level", "g": "grep", "t": "tree", "s": "source"} {
		f := newRootCmd().Flags().ShorthandLookup(short)
		if f == nil {
			t.Errorf("-%s is not defined, want a short form of --%s", short, long)
			continue
		}
		if f.Name != long {
			t.Errorf("-%s maps to --%s, want --%s", short, f.Name, long)
		}
	}
}

func TestCLI_ShortFlagsBehaveLikeLongFlags(t *testing.T) {
	defer func() { flagLevel, flagGrep, flagSource, flagTree = "", "", "", false }()

	cmd := newRootCmd()
	if err := cmd.ParseFlags([]string{"-l", "warn", "-g", "GET /api", "-s", "file", "-t"}); err != nil {
		t.Fatalf("short flags were rejected: %v", err)
	}
	if flagLevel != "warn" || flagGrep != "GET /api" || flagSource != "file" || !flagTree {
		t.Errorf("short flags did not set their values: level=%q grep=%q source=%q tree=%v", flagLevel, flagGrep, flagSource, flagTree)
	}
}

func TestCLI_ShortLevelFlagIsValidatedLikeLongLevelFlag(t *testing.T) {
	defer func() { flagLevel = "" }()

	run := func(args ...string) error {
		cmd := newRootCmd()
		var out bytes.Buffer
		cmd.SetErr(&out)
		cmd.SetOut(&out)
		cmd.SetArgs(args)
		return cmd.Execute()
	}

	long := run("3000", "--level", "banana")
	short := run("3000", "-l", "banana")
	if long == nil || short == nil {
		t.Fatalf("expected both forms to fail, got long=%v short=%v", long, short)
	}
	if long.Error() != short.Error() {
		t.Errorf("-l and --level should give the same error:\n  --level: %v\n  -l:      %v", long, short)
	}
	if !strings.Contains(short.Error(), "banana") {
		t.Errorf("expected the error to name the bad level, got %v", short)
	}
}

func TestCLI_HelpShowsShortAndLongForms(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("--help failed: %v", err)
	}
	for _, want := range []string{"-l, --level", "-g, --grep", "-t, --tree", "-s, --source"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help does not show %q", want)
		}
	}
}

func TestCLI_TreeHelpDoesNotImplyWorkersNeedIt(t *testing.T) {
	f := newRootCmd().Flags().Lookup("tree")
	if f == nil {
		t.Fatal("--tree is not defined")
	}
	if !strings.Contains(f.Usage, "deeper") || !strings.Contains(f.Usage, "always") {
		t.Errorf("--tree help should say it adds deeper descendants and that direct workers are always traced, got %q", f.Usage)
	}
}
