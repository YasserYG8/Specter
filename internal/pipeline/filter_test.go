package pipeline

import (
	"testing"
)

func TestFilter_MinLevel(t *testing.T) {
	filter := NewFilter(FilterOptions{MinLevel: SeverityWarn})

	debugEntry := &Entry{Lines: []string{"[DEBUG] Cache hit"}, Level: SeverityDebug, FD: 1}
	infoEntry := &Entry{Lines: []string{"[INFO] Connected"}, Level: SeverityInfo, FD: 1}
	warnEntry := &Entry{Lines: []string{"[WARN] High load"}, Level: SeverityWarn, FD: 1}
	errorEntry := &Entry{Lines: []string{"[ERROR] Timeout"}, Level: SeverityError, FD: 2}
	noneEntry := &Entry{Lines: []string{"Just application output"}, Level: SeverityNone, FD: 1}

	if filter.Matches(debugEntry) {
		t.Errorf("expected debugEntry to be filtered out")
	}
	if filter.Matches(infoEntry) {
		t.Errorf("expected infoEntry to be filtered out")
	}
	if !filter.Matches(warnEntry) {
		t.Errorf("expected warnEntry to match")
	}
	if !filter.Matches(errorEntry) {
		t.Errorf("expected errorEntry to match")
	}
	if !filter.Matches(noneEntry) {
		t.Errorf("expected noneEntry (unclassified) to be preserved")
	}
}

func TestFilter_GrepMultiLineAtomic(t *testing.T) {
	filter := NewFilter(FilterOptions{Grep: "net.js"})

	traceEntry := &Entry{
		Lines: []string{
			"Error: Connection reset",
			"    at Socket.onClose (/app/net.js:42:15)",
			"    at Socket.emit (events.js:315:20)",
		},
		Level:   SeverityError,
		IsTrace: true,
		FD:      2,
	}

	otherEntry := &Entry{
		Lines: []string{"Server started"},
		Level: SeverityInfo,
		FD:    1,
	}

	if !filter.Matches(traceEntry) {
		t.Errorf("expected traceEntry to match because 'net.js' is in frame")
	}
	if filter.Matches(otherEntry) {
		t.Errorf("expected otherEntry to not match")
	}
}

func TestFilter_OnlyStderr(t *testing.T) {
	filter := NewFilter(FilterOptions{OnlyStderr: true})

	stdoutEntry := &Entry{Lines: []string{"stdout message"}, FD: 1}
	stderrEntry := &Entry{Lines: []string{"stderr message"}, FD: 2}

	if filter.Matches(stdoutEntry) {
		t.Errorf("expected stdoutEntry to be filtered out")
	}
	if !filter.Matches(stderrEntry) {
		t.Errorf("expected stderrEntry to match")
	}
}
