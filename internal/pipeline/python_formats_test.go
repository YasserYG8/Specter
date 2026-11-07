package pipeline

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// One line per common Python log format, with the severity Specter must find. Python's default timestamp is
// "2026-11-07 10:12:50,516" (a space and a comma), which the positional rule used to reject.
func TestDetectSeverity_PythonLogFormats(t *testing.T) {
	cases := []struct {
		name string
		line string
		want Severity
	}{
		// Formats that already worked and must keep working
		{"basicConfig default, info", "INFO:app:Starting pipeline", SeverityInfo},
		{"basicConfig default, warning", "WARNING:app:disk almost full", SeverityWarn},
		{"basicConfig default, critical", "CRITICAL:app:out of memory", SeverityError},
		{"gunicorn, info", "[2026-11-07 10:12:50 +0000] [1234] [INFO] Booting worker with pid: 1250", SeverityInfo},
		{"gunicorn, warning", "[2026-11-07 10:12:50 +0000] [1234] [WARNING] Worker timeout", SeverityWarn},
		{"gunicorn, critical", "[2026-11-07 10:12:50 +0000] [1234] [CRITICAL] WORKER TIMEOUT (pid:1250)", SeverityError},
		{"loguru, info", "2026-11-07 10:12:50.516 | INFO     | app.main:run:42 - Starting", SeverityInfo},
		{"loguru, warning", "2026-11-07 10:12:50.516 | WARNING  | app.main:run:43 - disk almost full", SeverityWarn},
		{"loguru, critical", "2026-11-07 10:12:50.516 | CRITICAL | app.main:run:44 - out of memory", SeverityError},

		// The logging cookbook format: %(asctime)s - %(name)s - %(levelname)s - %(message)s
		{"cookbook, info", "2026-11-07 10:12:50,516 - TajweedPipeline - INFO - Starting pipeline processing", SeverityInfo},
		{"cookbook, warning", "2026-11-07 10:12:50,516 - TajweedPipeline - WARNING - Model cache is cold", SeverityWarn},
		{"cookbook, critical", "2026-11-07 10:12:50,516 - TajweedPipeline - CRITICAL - Pipeline crashed", SeverityError},
		{"cookbook, error with a dotted logger name", "2026-11-07 10:12:50,516 - app.services.audio_io - ERROR - decode failed", SeverityError},
		{"cookbook, dot before the fraction", "2026-11-07 10:12:50.516 - app - DEBUG - cache hit", SeverityDebug},
		{"cookbook, a later dashed level in the message does not win", "2026-11-07 10:12:50,516 - app - INFO - user typed - ERROR - and left", SeverityInfo},
		{"cookbook, logger name with spaces", "2026-11-07 10:12:50,516 - my app - INFO - Starting", SeverityInfo},

		// Celery workers
		{"celery, error", "[2026-11-07 10:12:50,516: ERROR/ForkPoolWorker-1] Task app.tasks.send[3f2a] raised unexpected: KeyError('x')", SeverityError},
		{"celery, info", "[2026-11-07 10:12:50,516: INFO/MainProcess] Task app.tasks.send[3f2a] received", SeverityInfo},
		{"celery, warning", "[2026-11-07 10:12:50,516: WARNING/ForkPoolWorker-2] retrying", SeverityWarn},
		{"celery, dot before the fraction", "[2026-11-07 10:12:50.516: CRITICAL/MainProcess] worker lost", SeverityError},

		// %(asctime)s %(levelname)s ...: a timestamp with a space, then the level
		{"spaced timestamp, warning", "2026-11-07 10:12:50,516 WARNING [app] disk almost full", SeverityWarn},
		{"spaced timestamp, info with a dot", "2026-11-07 10:12:50.516 INFO app: Starting", SeverityInfo},
		{"spaced timestamp, critical", "2026-11-07 10:12:50,516 CRITICAL app crashed", SeverityError},
		{"ISO timestamp with T and a zone", "2026-11-07T10:12:50,516+00:00 ERROR app: failed", SeverityError},
		{"ISO timestamp with Z", "2026-11-07T10:12:50.516Z INFO app: ready", SeverityInfo},

		// A level word inside the message is not the line's level
		{"dashed word without a timestamp", "Processing item 3 - INFO - not a level", SeverityNone},
		{"error inside a request path", "GET /api/ERROR-codes 200", SeverityInfo}, // INFO comes from the 200 status, not from the word
		{"level word later in a message", "2026-11-07 10:12:50,516 started worker, ERROR count is 0", SeverityNone},
		{"celery-like text in the middle", "see [2026-11-07 10:12:50,516: ERROR/MainProcess] in the log", SeverityNone},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectSeverity(tc.line); got != tc.want {
				t.Errorf("DetectSeverity(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

// A line with a level starts a new record, so a traceback or continuation after it groups correctly.
func TestIsRecordStart_PythonLogFormats(t *testing.T) {
	starts := []string{
		"2026-11-07 10:12:50,516 - TajweedPipeline - INFO - Starting pipeline processing",
		"[2026-11-07 10:12:50,516: ERROR/ForkPoolWorker-1] Task app.tasks.send[3f2a] raised unexpected",
		"2026-11-07 10:12:50,516 WARNING [app] disk almost full",
		"2026-11-07 10:12:50.516 INFO app: Starting",
		"INFO:app:Starting pipeline",
	}
	for _, line := range starts {
		isStart, level, _, _ := isRecordStart(line)
		if !isStart || level == SeverityNone {
			t.Errorf("isRecordStart(%q) = %v, %v; want a record start with a level", line, isStart, level)
		}
	}

	for _, line := range []string{
		"Processing item 3 - INFO - not a level",
		"see [2026-11-07 10:12:50,516: ERROR/MainProcess] in the log",
		"2026-11-07 10:12:50,516 started worker, ERROR count is 0",
	} {
		if isStart, _, _, _ := isRecordStart(line); isStart {
			t.Errorf("isRecordStart(%q) = true, want false for a line whose level word is inside the message", line)
		}
	}
}

// The reason this matters: lines without a level always pass --level, so output in these formats was never hidden.
func TestFilter_LevelWorksOnPythonLogFormats(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	input := []string{
		"2026-11-07 10:12:50,516 - TajweedPipeline - INFO - Starting pipeline processing",
		"2026-11-07 10:12:50,517 - TajweedPipeline - WARNING - Model cache is cold",
		"[2026-11-07 10:12:50,518: INFO/MainProcess] Task app.tasks.send[3f2a] received",
		"[2026-11-07 10:12:50,519: ERROR/ForkPoolWorker-1] Task app.tasks.send[3f2a] raised unexpected: KeyError('x')",
		"Traceback (most recent call last):",
		`  File "/app/tasks.py", line 7, in send`,
		"KeyError: 'x'",
		"2026-11-07 10:12:50,520 INFO app: done",
	}

	lines := make(chan Line, len(input))
	now := time.Now()
	for _, text := range input {
		lines <- Line{Text: text, PID: 7, FD: 2, TS: now}
	}
	close(lines)

	filter := NewFilter(FilterOptions{MinLevel: SeverityWarn})
	var shown []string
	for entry := range NewStackTraceGrouper(DefaultTraceIdleTimeout).Group(ctx, lines) {
		if filter.Matches(&entry) {
			shown = append(shown, entry.Text())
		}
	}

	want := []string{input[1], input[3], input[4], input[5], input[6]}
	if !reflect.DeepEqual(shown, want) {
		t.Errorf("--level warn should hide the INFO lines and keep the WARNING, the ERROR and its traceback\nwant %q\n got %q", want, shown)
	}
}
