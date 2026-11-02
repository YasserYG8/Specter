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
