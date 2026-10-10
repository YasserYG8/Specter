package pipeline

import (
	"strings"
	"testing"
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
