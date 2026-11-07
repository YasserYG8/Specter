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

func TestFilter_MatchedGrepAdmitsWholeTrace(t *testing.T) {
	t.Run("lines marked by the holder pass without matching themselves", func(t *testing.T) {
		filter := NewFilter(FilterOptions{Grep: "ZeroDivision"})
		header := &Entry{Lines: []string{"Traceback (most recent call last):"}, FD: 2, PID: 5, IsTrace: true, MatchedGrep: true}
		frame := &Entry{Lines: []string{`  File "/app/x.py", line 1`}, FD: 2, PID: 5, IsTrace: true, IsFrame: true, MatchedGrep: true}
		if !filter.Matches(header) || !filter.Matches(frame) {
			t.Error("expected both lines of a matching trace to be admitted")
		}
	})

	t.Run("later lines of the record inherit the match", func(t *testing.T) {
		filter := NewFilter(FilterOptions{Grep: "ZeroDivision"})
		filter.Matches(&Entry{Lines: []string{"Error: boom"}, FD: 2, PID: 5, IsTrace: true, MatchedGrep: true})
		later := &Entry{Lines: []string{"    at later (/app/f.js:9:1)"}, FD: 2, PID: 5, IsTrace: true, IsFrame: true}
		if !filter.Matches(later) {
			t.Error("expected a frame after the held lines to inherit the match")
		}
	})

	t.Run("the mark does not reach other streams", func(t *testing.T) {
		filter := NewFilter(FilterOptions{Grep: "ZeroDivision"})
		filter.Matches(&Entry{Lines: []string{"Error: boom"}, FD: 2, PID: 5, IsTrace: true, MatchedGrep: true})
		other := &Entry{Lines: []string{"    at x (/app/f.js:9:1)"}, FD: 2, PID: 6, IsTrace: true, IsFrame: true}
		if filter.Matches(other) {
			t.Error("expected a frame of another process to stay filtered out")
		}
	})

	t.Run("stderr and level checks still come first", func(t *testing.T) {
		stderrOnly := NewFilter(FilterOptions{Grep: "x", OnlyStderr: true})
		if stderrOnly.Matches(&Entry{Lines: []string{"a"}, FD: 1, PID: 5, MatchedGrep: true}) {
			t.Error("expected --stderr to drop a stdout line even when marked")
		}
		leveled := NewFilter(FilterOptions{Grep: "x", MinLevel: SeverityError})
		if leveled.Matches(&Entry{Lines: []string{"a"}, FD: 1, PID: 5, Level: SeverityInfo, MatchedGrep: true}) {
			t.Error("expected --level to drop an info line even when marked")
		}
	})
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

func TestFilter_GrepError(t *testing.T) {
	t.Run("invalid regex falls back to literal and reports", func(t *testing.T) {
		f := NewFilter(FilterOptions{Grep: "[error"})
		if f.GrepError() == nil {
			t.Fatal("expected a compile error for an invalid regex")
		}
		hit := &Entry{FD: 1, Lines: []string{"a [error happened"}}
		miss := &Entry{FD: 1, Lines: []string{"an error happened"}}
		if !f.Matches(hit) {
			t.Error("expected literal match of '[error'")
		}
		if f.Matches(miss) {
			t.Error("did not expect a match without the literal text")
		}
	})

	t.Run("valid regex and empty pattern report nothing", func(t *testing.T) {
		if err := NewFilter(FilterOptions{Grep: "auth|token"}).GrepError(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if err := NewFilter(FilterOptions{}).GrepError(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
