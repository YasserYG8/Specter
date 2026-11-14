package pipeline

import (
	"strings"
	"testing"
	"time"
)

func TestParseJSONLog_Winston(t *testing.T) {
	line := `{"level":"info","message":"User logged in successfully","timestamp":"2026-10-10T10:00:00Z","userId":1042}`
	jl := ParseJSONLog(line)
	if jl == nil {
		t.Fatal("expected JSONLog, got nil")
	}

	if jl.Level != SeverityInfo {
		t.Errorf("expected SeverityInfo, got %v", jl.Level)
	}
	if jl.Message != "User logged in successfully" {
		t.Errorf("unexpected message: %q", jl.Message)
	}
	if jl.Timestamp != "2026-10-10T10:00:00Z" {
		t.Errorf("unexpected timestamp: %q", jl.Timestamp)
	}
	if val, ok := jl.Fields["userId"]; !ok || int(val.(float64)) != 1042 {
		t.Errorf("unexpected userId field: %v", val)
	}
}

func TestParseJSONLog_PinoNumericLevel(t *testing.T) {
	// Pino uses numbers: 30 = info, 40 = warn, 50 = error
	line := `{"level":50,"time":1700000000000,"msg":"Database connection refused","dbHost":"10.0.0.1"}`
	jl := ParseJSONLog(line)
	if jl == nil {
		t.Fatal("expected JSONLog, got nil")
	}

	if jl.Level != SeverityError {
		t.Errorf("expected SeverityError for level=50, got %v", jl.Level)
	}
	if jl.Message != "Database connection refused" {
		t.Errorf("unexpected message: %q", jl.Message)
	}
	if val, ok := jl.Fields["dbHost"]; !ok || val != "10.0.0.1" {
		t.Errorf("unexpected dbHost field: %v", val)
	}
}

func TestParseJSONLog_ZapAndLogrus(t *testing.T) {
	line := `{"level":"warn","msg":"Rate limit reached","ts":1700000000.5,"ip":"192.168.1.1"}`
	jl := ParseJSONLog(line)
	if jl == nil {
		t.Fatal("expected JSONLog, got nil")
	}

	if jl.Level != SeverityWarn {
		t.Errorf("expected SeverityWarn, got %v", jl.Level)
	}
	if jl.Message != "Rate limit reached" {
		t.Errorf("unexpected message: %q", jl.Message)
	}
}

func TestParseJSONLog_NonJSON(t *testing.T) {
	if jl := ParseJSONLog("Plain text output"); jl != nil {
		t.Errorf("expected nil for plain text, got: %+v", jl)
	}
	if jl := ParseJSONLog("{ invalid json }"); jl != nil {
		t.Errorf("expected nil for invalid json, got: %+v", jl)
	}
}

func TestFormatSingleLineJSON(t *testing.T) {
	jl := &JSONLog{
		Timestamp: "10:00:00",
		Level:     SeverityError,
		Message:   "Payment failed",
		Fields: map[string]any{
			"orderId": "ORD-99",
			"code":    402,
		},
	}

	formatted := FormatSingleLineJSON(jl, false)
	// Expected format: 10:00:00 [ERROR] Payment failed code=402 orderId=ORD-99
	if !strings.Contains(formatted, "[ERROR] Payment failed") {
		t.Errorf("expected '[ERROR] Payment failed' in output, got: %q", formatted)
	}
	if !strings.Contains(formatted, "orderId=ORD-99") || !strings.Contains(formatted, "code=402") {
		t.Errorf("expected fields in output, got: %q", formatted)
	}
}

// want123 is the expected text for the moment 1789000000 s + 123 ms. It is built in local time, the same way the
// parser does, so the tests do not depend on the machine's time zone.
var want123 = time.UnixMilli(1789000000123).Format("15:04:05.000")

func TestParseJSONLog_TimestampKeySelection(t *testing.T) {
	t.Run("duration in time does not hide ts", func(t *testing.T) {
		jl := ParseJSONLog(`{"msg":"GET /users","time":45,"ts":1789000000123}`)
		if jl == nil {
			t.Fatal("expected JSONLog, got nil")
		}
		if jl.Timestamp != want123 {
			t.Errorf("timestamp = %q, want %q", jl.Timestamp, want123)
		}
		if v, ok := jl.Fields["time"]; !ok || v != float64(45) {
			t.Errorf("expected time=45 to stay in Fields, got %v (present=%v)", v, ok)
		}
		if _, ok := jl.Fields["ts"]; ok {
			t.Error("ts was used as the timestamp, so it should not stay in Fields")
		}
	})

	t.Run("first accepted key wins", func(t *testing.T) {
		jl := ParseJSONLog(`{"msg":"x","timestamp":"2026-10-10T10:00:00Z","ts":1789000000123}`)
		if jl == nil {
			t.Fatal("expected JSONLog, got nil")
		}
		if jl.Timestamp != "2026-10-10T10:00:00Z" {
			t.Errorf("timestamp = %q, want the string from timestamp", jl.Timestamp)
		}
		if _, ok := jl.Fields["ts"]; !ok {
			t.Error("expected ts to stay in Fields")
		}
	})

	for name, line := range map[string]string{
		"object":       `{"msg":"x","time":{"ms":12}}`,
		"empty string": `{"msg":"x","time":""}`,
		"boolean":      `{"msg":"x","time":true}`,
		"null":         `{"msg":"x","time":null}`,
		"array":        `{"msg":"x","time":[1,2]}`,
		"small number": `{"msg":"x","time":45}`,
	} {
		t.Run("not a timestamp: "+name, func(t *testing.T) {
			jl := ParseJSONLog(line)
			if jl == nil {
				t.Fatal("expected JSONLog, got nil")
			}
			if jl.Timestamp != "" {
				t.Errorf("expected no timestamp, got %q", jl.Timestamp)
			}
			if _, ok := jl.Fields["time"]; !ok {
				t.Error("expected time to stay in Fields")
			}
		})
	}
}

func TestParseJSONLog_NumericUnits(t *testing.T) {
	for name, line := range map[string]string{
		"milliseconds": `{"msg":"x","time":1789000000123}`,
		"microseconds": `{"msg":"x","ts":1789000000123456}`,
		"nanoseconds":  `{"msg":"x","ts":1789000000123456789}`,
	} {
		t.Run(name, func(t *testing.T) {
			jl := ParseJSONLog(line)
			if jl == nil {
				t.Fatal("expected JSONLog, got nil")
			}
			if jl.Timestamp != want123 {
				t.Errorf("timestamp = %q, want %q", jl.Timestamp, want123)
			}
		})
	}
}

func TestNumericTimestamp(t *testing.T) {
	accepted := []struct {
		name   string
		v      float64
		wantMs int64
	}{
		{"seconds with fraction", 1789000000.5, 1789000000500},
		{"milliseconds", 1789000000123, 1789000000123},
		{"microseconds", 1789000000123456, 1789000000123},
		{"nanoseconds", 1789000000123456789, 1789000000123},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := numericTimestamp(tc.v)
			if !ok {
				t.Fatalf("numericTimestamp(%v) was refused", tc.v)
			}
			if got.UnixMilli() != tc.wantMs {
				t.Errorf("numericTimestamp(%v) = %d ms, want %d ms", tc.v, got.UnixMilli(), tc.wantMs)
			}
			want := time.UnixMilli(tc.wantMs).Format("15:04:05.000")
			if f := got.Format("15:04:05.000"); f != want {
				t.Errorf("formatted %q, want %q", f, want)
			}
		})
	}

	refused := map[string]float64{
		"zero":                0,
		"negative":            -5,
		"small number":        45,
		"before 2000 (s)":     946684799,
		"after 2099 (s)":      4102444800,
		"after 2099 (ms)":     4102444800000,
		"after 2099 (ns)":     4.2e18,
		"far future":          1e19,
		"negative nanosecond": -1789000000123456789,
	}
	for name, v := range refused {
		t.Run("refused: "+name, func(t *testing.T) {
			if got, ok := numericTimestamp(v); ok {
				t.Errorf("numericTimestamp(%v) = %v, want it refused", v, got)
			}
		})
	}

	// The edges of the range: the first and last second that are accepted.
	if _, ok := numericTimestamp(946684800); !ok {
		t.Error("2000-01-01T00:00:00Z should be accepted")
	}
	if _, ok := numericTimestamp(4102444799); !ok {
		t.Error("2099-12-31T23:59:59Z should be accepted")
	}
}
