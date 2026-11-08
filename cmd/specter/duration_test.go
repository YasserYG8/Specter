package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/YasserYG8/Specter/internal/util"
)

func TestFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second:                      "30s",
		2 * time.Minute:                       "2m",
		90 * time.Second:                      "1m30s",
		time.Hour:                             "1h",
		time.Hour + 30*time.Minute:            "1h30m",
		1500 * time.Millisecond:               "1.5s",
		-5 * time.Second:                      "-5s",
		2*time.Hour + 5*time.Second:           "2h0m5s",
		10 * time.Minute:                      "10m",
		time.Hour + time.Minute + time.Second: "1h1m1s",
	}
	for d, want := range cases {
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestValidateDuration(t *testing.T) {
	for _, ok := range []time.Duration{0, time.Second, 30 * time.Second, 2 * time.Hour} {
		if err := validateDuration(ok); err != nil {
			t.Errorf("%v should be accepted, got %v", ok, err)
		}
	}
	err := validateDuration(-5 * time.Second)
	if err == nil {
		t.Fatal("a negative duration should be rejected")
	}
	if !errors.Is(err, util.ErrInvalidFlag) {
		t.Error("expected the error to wrap ErrInvalidFlag")
	}
	for _, want := range []string{"Invalid --duration -5s", "must be positive", "--duration 30s", "--duration 2m"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message should contain %q, got:\n%v", want, err)
		}
	}
}

// The command line must reject a bad --duration before anything is resolved or attached, so these behave the same
// on every platform.
func TestCLI_DurationIsValidatedBeforeResolving(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr []string
	}{
		{"missing unit", []string{"3000", "--duration", "30"}, []string{"Invalid --duration value", "30s", "2m"}},
		{"missing unit with equals sign", []string{"3000", "--duration=30"}, []string{"Invalid --duration value", "30s", "2m"}},
		{"not a duration", []string{"3000", "--duration", "soon"}, []string{"Invalid --duration value", "30s", "2m"}},
		{"negative", []string{"3000", "--duration", "-5s"}, []string{"Invalid --duration -5s", "must be positive"}},
		{"negative minutes", []string{"3000", "--duration=-2m"}, []string{"Invalid --duration -2m", "must be positive"}},
		{"missing value", []string{"3000", "--duration"}, []string{"Invalid --duration value"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() { flagDuration = 0 }()
			cmd := newRootCmd()
			var out bytes.Buffer
			cmd.SetErr(&out)
			cmd.SetOut(&out)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected an error")
			}
			if !errors.Is(err, util.ErrInvalidFlag) {
				t.Errorf("expected the error to wrap ErrInvalidFlag, got %v", err)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("expected %q in:\n%v", want, err)
				}
			}
		})
	}
}

func TestCLI_ValidDurationsAreAccepted(t *testing.T) {
	cases := map[string]time.Duration{
		"30s":   30 * time.Second,
		"2m":    2 * time.Minute,
		"1h30m": time.Hour + 30*time.Minute,
		"0":     0, // no limit
		"0s":    0,
	}
	for arg, want := range cases {
		defer func() { flagDuration = 0 }()
		cmd := newRootCmd()
		if err := cmd.ParseFlags([]string{"--duration", arg}); err != nil {
			t.Errorf("--duration %s was rejected: %v", arg, err)
			continue
		}
		if flagDuration != want {
			t.Errorf("--duration %s gave %v, want %v", arg, flagDuration, want)
		}
		if err := validateDuration(flagDuration); err != nil {
			t.Errorf("--duration %s should pass validation, got %v", arg, err)
		}
	}
}

func TestCLI_DurationIsDocumented(t *testing.T) {
	cmd := newRootCmd()
	f := cmd.Flags().Lookup("duration")
	if f == nil {
		t.Fatal("the --duration flag is missing")
	}
	if !strings.Contains(f.Usage, "30s") || !strings.Contains(f.Usage, "no limit") {
		t.Errorf("the usage should show an example and the default, got %q", f.Usage)
	}
}

func TestReportEnd_DurationElapsed(t *testing.T) {
	cases := []struct {
		name       string
		report     endReport
		want       string
		wantStatus int
	}{
		{"process", endReport{Interrupted: true, After: 30 * time.Second, PID: 595}, "Detached from PID 595 after 30s.", 0},
		{"container", endReport{Interrupted: true, After: 10 * time.Second, Container: "web"}, "Detached from container web after 10s.", 0},
		{"minutes", endReport{Interrupted: true, After: 2 * time.Minute, PID: 595}, "Detached from PID 595 after 2m.", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			status := reportEnd(&out, "[specter]", tc.report)
			if status != tc.wantStatus {
				t.Errorf("status = %d, want %d", status, tc.wantStatus)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("expected %q, got %q", tc.want, out.String())
			}
			if strings.Contains(out.String(), "cleanly") {
				t.Errorf("a stop by --duration is not the Ctrl+C message: %q", out.String())
			}
		})
	}

	// Ctrl+C before the time is over keeps its own message
	var out bytes.Buffer
	if status := reportEnd(&out, "[specter]", endReport{Interrupted: true, PID: 595}); status != 0 || !strings.Contains(out.String(), "Detached from PID 595 cleanly.") {
		t.Errorf("Ctrl+C: status %d, output %q", status, out.String())
	}

	// A target that ends before the time is over is reported as it always was, with status 1
	out.Reset()
	if status := reportEnd(&out, "[specter]", endReport{PID: 595, WatcherGone: true}); status != 1 || strings.Contains(out.String(), "Detached") {
		t.Errorf("target ended: status %d, output %q", status, out.String())
	}
}
