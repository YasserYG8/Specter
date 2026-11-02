package strace

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/YasserYG8/Specter/internal/source"
)

func TestNewSource_Options(t *testing.T) {
	sDefault := NewSource()
	if sDefault.options.FollowTree {
		t.Errorf("expected default FollowTree to be false, got true")
	}

	sTree := NewSource(Options{FollowTree: true})
	if !sTree.options.FollowTree {
		t.Errorf("expected FollowTree to be true, got false")
	}

	if sTree.Name() != "strace" {
		t.Errorf("expected Name() to be 'strace', got %q", sTree.Name())
	}
}

func TestBuildStraceArgs_MultiPID(t *testing.T) {
	t.Run("single target without children", func(t *testing.T) {
		target := source.Target{
			PID: 596,
		}
		args := BuildStraceArgs(target)
		expected := []string{"-f", "-qq", "-xx", "-s", "65536", "-e", "trace=write,writev", "-p", "596"}
		if !reflect.DeepEqual(args, expected) {
			t.Errorf("expected args %v, got %v", expected, args)
		}
	})

	t.Run("target with child workers", func(t *testing.T) {
		target := source.Target{
			PID:      596,
			Children: []int{629, 630},
		}
		args := BuildStraceArgs(target)
		expected := []string{"-f", "-qq", "-xx", "-s", "65536", "-e", "trace=write,writev", "-p", "596", "-p", "629", "-p", "630"}
		if !reflect.DeepEqual(args, expected) {
			t.Errorf("expected args %v, got %v", expected, args)
		}
	})

	t.Run("deduplicates self PID and redundant children", func(t *testing.T) {
		target := source.Target{
			PID:      596,
			Children: []int{596, 629, 629, 0, -1},
		}
		args := BuildStraceArgs(target)
		expected := []string{"-f", "-qq", "-xx", "-s", "65536", "-e", "trace=write,writev", "-p", "596", "-p", "629"}
		if !reflect.DeepEqual(args, expected) {
			t.Errorf("expected args %v, got %v", expected, args)
		}
	})

	t.Run("traces every child worker, however many there are", func(t *testing.T) {
		children := make([]int, 30)
		for i := range children {
			children[i] = 1000 + i
		}
		target := source.Target{
			PID:      500,
			Children: children,
		}
		args := BuildStraceArgs(target)

		expected := []string{"-f", "-qq", "-xx", "-s", "65536", "-e", "trace=write,writev", "-p", "500"}
		for _, c := range children {
			expected = append(expected, "-p", strconv.Itoa(c))
		}
		if !reflect.DeepEqual(args, expected) {
			t.Errorf("expected a -p flag for the target and all 30 children (%d args), got %d: %v", len(expected), len(args), args)
		}
	})
}

func runReadLoop(t *testing.T, s *Source, input string) []source.Chunk {
	t.Helper()
	s.parser = NewParser(1)
	chunks := make(chan source.Chunk, 64)
	s.readLoop(context.Background(), strings.NewReader(input), chunks)
	close(chunks)
	var out []source.Chunk
	for c := range chunks {
		out = append(out, c)
	}
	return out
}

func TestReadLoop_OverLimitLineDoesNotEndCapture(t *testing.T) {
	s := NewSource()
	s.maxLine = 200
	var notices []string
	s.SetNotifier(func(msg string) { notices = append(notices, msg) })

	long := `write(1, "` + strings.Repeat(`\x41`, 500) + `", 500) = 500`
	input := `write(1, "\x61\x0a", 2) = 2` + "\n" +
		long + "\n" +
		`write(1, "\x62\x0a", 2) = 2` + "\n" +
		`write(1, "\x63\x0a", 2) = 2` // no trailing newline

	chunks := runReadLoop(t, s, input)
	var got string
	for _, c := range chunks {
		got += string(c.Bytes)
	}
	if got != "a\nb\nc\n" {
		t.Errorf("expected writes around the long line to be captured, got %q", got)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "too long") {
		t.Errorf("expected one over-limit notice, got %v", notices)
	}
}

func TestReadLoop_OverLimitNoticesAreRateLimited(t *testing.T) {
	s := NewSource()
	s.maxLine = 50
	var notices []string
	s.SetNotifier(func(msg string) { notices = append(notices, msg) })

	long := strings.Repeat("x", 100) + "\n"
	runReadLoop(t, s, long+long+long)
	if len(notices) != 1 {
		t.Errorf("expected one notice for a burst of long lines, got %d: %v", len(notices), notices)
	}
}

func TestScanLines_LongLineSpanningReaderBuffer(t *testing.T) {
	// Longer than the 64 KB reader buffer but under the limit: must arrive whole
	line := strings.Repeat("y", 200*1024)
	var got []string
	scanLines(strings.NewReader(line+"\nnext\n"), 1<<20, func(l string) bool {
		got = append(got, l)
		return true
	}, func(int) { t.Error("line is within the limit") })
	if len(got) != 2 || got[0] != line || got[1] != "next" {
		t.Errorf("unexpected lines: %d lines, first length %d", len(got), len(got[0]))
	}
}

func logNoticeSource(links map[string]string, regular map[string]bool) (*Source, *[]string) {
	s := NewSource()
	s.procRoot = "/fakeproc"
	s.readlink = func(path string) (string, error) {
		if target, ok := links[filepath.ToSlash(path)]; ok {
			return target, nil
		}
		return "", os.ErrNotExist
	}
	s.isRegularFile = func(path string) bool { return regular[path] }
	notices := &[]string{}
	s.SetNotifier(func(msg string) { *notices = append(*notices, msg) })
	return s, notices
}

func TestOwnLogFileNotice(t *testing.T) {
	links := map[string]string{
		"/fakeproc/10/fd/3": "/var/log/app.log",
		"/fakeproc/10/fd/4": "/var/log/app.log",
		"/fakeproc/11/fd/3": "/var/log/app.log",
		"/fakeproc/10/fd/5": "socket:[12345]",
		"/fakeproc/10/fd/6": "/data/app.db",
	}
	regular := map[string]bool{"/var/log/app.log": true, "/data/app.db": true}

	t.Run("log file", func(t *testing.T) {
		s, notices := logNoticeSource(links, regular)
		s.checkOtherFD(10, 3)
		if len(*notices) != 1 || !strings.Contains((*notices)[0], "/var/log/app.log") {
			t.Fatalf("expected one notice naming the log file, got %v", *notices)
		}
	})

	t.Run("repeated path through other fds and pids", func(t *testing.T) {
		s, notices := logNoticeSource(links, regular)
		for i := 0; i < 5; i++ {
			s.checkOtherFD(10, 3)
		}
		s.checkOtherFD(10, 4)
		s.checkOtherFD(11, 3)
		if len(*notices) != 1 {
			t.Errorf("expected one notice for one path, got %d: %v", len(*notices), *notices)
		}
	})

	t.Run("socket", func(t *testing.T) {
		s, notices := logNoticeSource(links, regular)
		s.checkOtherFD(10, 5)
		if len(*notices) != 0 {
			t.Errorf("expected no notice for a socket, got %v", *notices)
		}
	})

	t.Run("regular file without log in its name", func(t *testing.T) {
		s, notices := logNoticeSource(links, regular)
		s.checkOtherFD(10, 6)
		if len(*notices) != 0 {
			t.Errorf("expected no notice for a .db file, got %v", *notices)
		}
	})

	t.Run("log-named path that is not a regular file", func(t *testing.T) {
		s, notices := logNoticeSource(links, map[string]bool{})
		s.checkOtherFD(10, 3)
		if len(*notices) != 0 {
			t.Errorf("expected no notice, got %v", *notices)
		}
	})

	t.Run("fd that cannot be resolved", func(t *testing.T) {
		s, notices := logNoticeSource(links, regular)
		s.checkOtherFD(99, 3)
		if len(*notices) != 0 {
			t.Errorf("expected no notice, got %v", *notices)
		}
	})
}

func TestReadLoop_ReportsWritesToOtherFDs(t *testing.T) {
	s, notices := logNoticeSource(
		map[string]string{"/fakeproc/1/fd/3": "/srv/app/server.log"},
		map[string]bool{"/srv/app/server.log": true},
	)
	chunks := runReadLoop(t, s, `write(3, "\x61\x0a", 2) = 2`+"\n")
	if len(chunks) != 0 {
		t.Errorf("expected no chunks, got %v", chunks)
	}
	if len(*notices) != 1 || !strings.Contains((*notices)[0], "/srv/app/server.log") {
		t.Errorf("expected a notice naming the log file, got %v", *notices)
	}
}

// A worker that exits between discovery and attach makes strace print this line for its PID. strace keeps
// tracing the others, so Specter has to keep parsing what they write.
func TestReadLoop_FailedAttachDoesNotStopCapture(t *testing.T) {
	s := NewSource()
	input := strings.Join([]string{
		`strace: attach: ptrace(PTRACE_SEIZE, 4242): No such process`,
		`[pid 4243] write(1, "\x61\x0a", 2) = 2`,
		`strace: attach: ptrace(PTRACE_SEIZE, 4244): No such process`,
		`[pid 4245] write(1, "\x62\x0a", 2) = 2`,
		``,
	}, "\n")

	chunks := runReadLoop(t, s, input)

	if len(chunks) != 2 || string(chunks[0].Bytes) != "a\n" || string(chunks[1].Bytes) != "b\n" {
		t.Fatalf("expected the writes after the failed attach lines, got %+v", chunks)
	}
	// The line is kept so a real failure can be explained, but it must not be treated as one while strace runs
	s.mu.Lock()
	captured := s.capturedError
	s.mu.Unlock()
	if !strings.Contains(captured, "No such process") {
		t.Errorf("expected the attach error to be remembered, got %q", captured)
	}
}
