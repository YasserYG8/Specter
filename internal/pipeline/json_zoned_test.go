package pipeline

import (
	"strconv"
	"testing"
	"time"
)

// withLocalZone sets the local time zone for one test, so the expected text does not depend on the machine.
func withLocalZone(t *testing.T, offsetHours int) {
	t.Helper()
	old := time.Local
	time.Local = time.FixedZone("test", offsetHours*3600)
	t.Cleanup(func() { time.Local = old })
}

// formatted parses one JSON line and renders it without colors.
func formatted(t *testing.T, line string) string {
	t.Helper()
	jl := ParseJSONLog(line)
	if jl == nil {
		t.Fatalf("not parsed as JSON: %s", line)
	}
	return FormatSingleLineJSON(jl, false)
}

func TestFormatSingleLineJSON_ZonedTimestamps(t *testing.T) {
	t.Run("Z is shown in local time", func(t *testing.T) {
		withLocalZone(t, 2)
		got := formatted(t, `{"msg":"x","timestamp":"2026-10-10T10:00:00.123Z"}`)
		if got != "12:00:00.123 x" {
			t.Errorf("got %q, want %q", got, "12:00:00.123 x")
		}
	})

	t.Run("an offset is converted to local time", func(t *testing.T) {
		withLocalZone(t, 0)
		got := formatted(t, `{"msg":"x","time":"2026-10-10T10:00:00+02:00"}`)
		if got != "08:00:00.000 x" {
			t.Errorf("got %q, want %q", got, "08:00:00.000 x")
		}
	})

	t.Run("the same moment as a number and as text looks the same", func(t *testing.T) {
		withLocalZone(t, -5)
		ms := time.Date(2026, 10, 10, 10, 0, 0, 123_000_000, time.UTC).UnixMilli()
		num := formatted(t, `{"msg":"x","ts":`+strconv.FormatInt(ms, 10)+`}`)
		txt := formatted(t, `{"msg":"x","timestamp":"2026-10-10T10:00:00.123Z"}`)
		if num != txt {
			t.Errorf("numeric %q and text %q should be the same", num, txt)
		}
		if txt != "05:00:00.123 x" {
			t.Errorf("got %q, want %q", txt, "05:00:00.123 x")
		}
	})

	t.Run("no time zone keeps the old handling", func(t *testing.T) {
		withLocalZone(t, 2)
		// Not RFC 3339, so it is not converted and not cut after the T either
		if got := formatted(t, `{"msg":"x","time":"2026-10-10 10:00:00"}`); got != "2026-10-10 10:00:00 x" {
			t.Errorf("got %q", got)
		}
		// ISO 8601 with a T but without a zone is cut after the T, as before
		if got := formatted(t, `{"msg":"x","time":"2026-10-10T10:00:00.123456"}`); got != "10:00:00.123456 x" {
			t.Errorf("got %q", got)
		}
	})
}
