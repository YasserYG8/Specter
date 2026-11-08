package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/YasserYG8/Specter/internal/util"
)

// errDurationElapsed is the cause of the stop that --duration triggers. It tells the closing message apart from
// the user's Ctrl+C, while both stop Specter through the same path.
var errDurationElapsed = errors.New("--duration elapsed")

// durationExamples is the remediation text for a --duration value that cannot be used.
const durationExamples = "Use a number with a unit, for example: --duration 30s or --duration 2m (units: s, m, h)"

// validateDuration checks --duration before anything is resolved or attached. Zero means no limit.
func validateDuration(d time.Duration) error {
	if d >= 0 {
		return nil
	}
	return &util.DiagnosticError{
		Err:         util.ErrInvalidFlag,
		Title:       fmt.Sprintf("Invalid --duration %s", formatDuration(d)),
		Explanation: "The duration must be positive.",
		Remediation: durationExamples,
	}
}

// durationFlagError replaces the parser's own message for a --duration that is not a duration, such as "30" with
// no unit, with one that shows what to type.
func durationFlagError() error {
	return &util.DiagnosticError{
		Err:         util.ErrInvalidFlag,
		Title:       "Invalid --duration value",
		Explanation: "The duration needs a unit, such as 30s (seconds), 2m (minutes) or 1h (hours).",
		Remediation: durationExamples,
	}
}

// formatDuration writes a duration the way a person types it: 30s, 2m, 1h30m. time.Duration.String adds zero
// parts ("2m0s", "1h0m0s") that look odd in a message.
func formatDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}
