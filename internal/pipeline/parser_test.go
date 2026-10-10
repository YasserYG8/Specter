package pipeline

import (
	"testing"
)

func TestParseSeverity(t *testing.T) {
	tests := []struct {
		input    string
		expected Severity
	}{
		{"DEBUG", SeverityDebug},
		{"debug", SeverityDebug},
		{"trace", SeverityDebug},
		{"INFO", SeverityInfo},
		{"info", SeverityInfo},
		{"WARN", SeverityWarn},
		{"warning", SeverityWarn},
		{"ERROR", SeverityError},
		{"fatal", SeverityError},
		{"unknown", SeverityNone},
		{"", SeverityNone},
	}

	for _, tt := range tests {
		got := ParseSeverity(tt.input)
		if got != tt.expected {
			t.Errorf("ParseSeverity(%q) = %v, expected %v", tt.input, got, tt.expected)
		}
	}
}

func TestDetectSeverity_Tokens(t *testing.T) {
	tests := []struct {
		line     string
		expected Severity
	}{
		{"[ERROR] Database connection failed", SeverityError},
		{"2026-10-10 10:00:00 level=error msg=\"timeout\"", SeverityError},
		{"CRITICAL: Out of memory", SeverityError},
		{"java.lang.NullPointerException: null", SeverityError},
		{"[WARN] High memory usage detected", SeverityWarn},
		{"2026-10-10 10:00:00 level=warn msg=\"slow query\"", SeverityWarn},
		{"[INFO] Server listening on port 3000", SeverityInfo},
		{"level=info User logged in", SeverityInfo},
		{"[DEBUG] Cache miss for key 123", SeverityDebug},
		{"Just a normal application message", SeverityNone},
	}

	for _, tt := range tests {
		got := DetectSeverity(tt.line)
		if got != tt.expected {
			t.Errorf("DetectSeverity(%q) = %v, expected %v", tt.line, got, tt.expected)
		}
	}
}

func TestDetectSeverity_HTTPStatus(t *testing.T) {
	tests := []struct {
		line     string
		expected Severity
	}{
		{`127.0.0.1 - - [10/Oct/2026:10:00:00] "GET /api/users HTTP/1.1" 200 452`, SeverityInfo},
		{`127.0.0.1 - - [10/Oct/2026:10:00:00] "GET /api/missing HTTP/1.1" 404 120`, SeverityWarn},
		{`127.0.0.1 - - [10/Oct/2026:10:00:00] "POST /api/pay HTTP/1.1" 500 89`, SeverityError},
		{`127.0.0.1 - - [10/Oct/2026:10:00:00] "POST /api/auth HTTP/1.1" 502 0`, SeverityError},
	}

	for _, tt := range tests {
		got := DetectSeverity(tt.line)
		if got != tt.expected {
			t.Errorf("DetectSeverity(%q) = %v, expected %v", tt.line, got, tt.expected)
		}
	}
}

func TestANSI_DetectionAndStripping(t *testing.T) {
	colored := "\x1b[31mError occurred\x1b[0m"
	if !HasANSI(colored) {
		t.Errorf("expected HasANSI(%q) to be true", colored)
	}

	stripped := StripANSI(colored)
	if stripped != "Error occurred" {
		t.Errorf("expected 'Error occurred', got %q", stripped)
	}

	plain := "Just plain text"
	if HasANSI(plain) {
		t.Errorf("expected HasANSI(%q) to be false", plain)
	}
}
