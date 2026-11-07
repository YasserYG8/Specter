package strace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/YasserYG8/Specter/internal/source"
	"github.com/YasserYG8/Specter/internal/util"
)

// fakeProc builds a /proc tree in a temp directory.
type fakeProc struct {
	t    *testing.T
	root string
}

func newFakeProc(t *testing.T) *fakeProc {
	t.Helper()
	return &fakeProc{t: t, root: t.TempDir()}
}

func (f *fakeProc) write(pid int, name, content string) {
	f.t.Helper()
	dir := filepath.Join(f.root, fmt.Sprint(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// process adds a process with the given tracer (0 for none), parent, command line and comm.
func (f *fakeProc) process(pid, tracer, ppid int, cmdline []string, comm string) {
	f.write(pid, "status", fmt.Sprintf("Name:\t%s\nPid:\t%d\nPPid:\t%d\nTracerPid:\t%d\nUid:\t1000\n", comm, pid, ppid, tracer))
	if cmdline != nil {
		f.write(pid, "cmdline", strings.Join(cmdline, "\x00")+"\x00")
	}
	if comm != "" {
		f.write(pid, "comm", comm+"\n")
	}
}

func (f *fakeProc) source() *Source {
	s := NewSource()
	s.procRoot = f.root
	return s
}

func TestTracerOf(t *testing.T) {
	t.Run("not traced", func(t *testing.T) {
		f := newFakeProc(t)
		f.process(773, 0, 1, []string{"next-server"}, "next-server")
		if got := tracerOf(f.root, 773); got != (tracerInfo{}) {
			t.Errorf("expected no tracer, got %+v", got)
		}
	})

	t.Run("traced by strace that another specter started", func(t *testing.T) {
		f := newFakeProc(t)
		f.process(773, 844, 1, []string{"next-server"}, "next-server")
		f.process(844, 0, 825, []string{"/usr/bin/strace", "-f", "-q", "-p", "773"}, "strace")
		f.process(825, 0, 1, []string{"/usr/local/bin/specter", "--pid", "773"}, "specter")

		got := tracerOf(f.root, 773)
		want := tracerInfo{PID: 844, Cmd: "strace -f -q -p 773", ParentPID: 825, ParentName: "specter"}
		if got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("traced by a debugger", func(t *testing.T) {
		f := newFakeProc(t)
		f.process(773, 900, 1, []string{"app"}, "app")
		f.process(900, 0, 50, []string{"gdb", "-p", "773"}, "gdb")
		f.process(50, 0, 1, []string{"bash"}, "bash")

		got := tracerOf(f.root, 773)
		if got.PID != 900 || got.Cmd != "gdb -p 773" || got.ParentName != "bash" {
			t.Errorf("unexpected %+v", got)
		}
	})

	t.Run("tracer command line unreadable", func(t *testing.T) {
		f := newFakeProc(t)
		f.process(773, 844, 1, []string{"app"}, "app")
		f.process(844, 0, 825, nil, "strace") // no cmdline file
		got := tracerOf(f.root, 773)
		if got.PID != 844 || got.Cmd != "" {
			t.Errorf("expected the PID without a command, got %+v", got)
		}
		if got.ParentPID != 825 {
			t.Errorf("the parent PID should still be found, got %+v", got)
		}
	})

	t.Run("tracer status unreadable", func(t *testing.T) {
		f := newFakeProc(t)
		f.process(773, 844, 1, []string{"app"}, "app")
		f.write(844, "cmdline", "strace\x00-p\x00773\x00") // no status file
		got := tracerOf(f.root, 773)
		if got.PID != 844 || got.Cmd != "strace -p 773" || got.ParentPID != 0 || got.ParentName != "" {
			t.Errorf("unexpected %+v", got)
		}
	})

	t.Run("target status unreadable", func(t *testing.T) {
		f := newFakeProc(t)
		if got := tracerOf(f.root, 773); got != (tracerInfo{}) {
			t.Errorf("an unreadable status should count as not traced, got %+v", got)
		}
	})

	t.Run("long command line is cut", func(t *testing.T) {
		f := newFakeProc(t)
		f.process(773, 844, 1, []string{"app"}, "app")
		f.process(844, 0, 1, []string{"strace", "-f", "-qq", "-xx", "-s", "65536", "-e", "trace=write,writev", "-p", "773", "-p", "774", "-p", "775"}, "strace")
		got := tracerOf(f.root, 773)
		if len(got.Cmd) > 60 || !strings.HasSuffix(got.Cmd, "...") {
			t.Errorf("expected a command cut to 60 characters, got %q (%d)", got.Cmd, len(got.Cmd))
		}
	})
}

func TestAlreadyTracedError(t *testing.T) {
	t.Run("started by another specter", func(t *testing.T) {
		err := alreadyTracedError(773, tracerInfo{PID: 844, Cmd: "strace -f -p 773", ParentPID: 825, ParentName: "specter"})
		text := err.Error()
		for _, want := range []string{"PID 773", "PID 844", "strace -f -p 773", "only one tracer per process", "specter (PID 825)", "Ctrl+C", "run this command again"} {
			if !strings.Contains(text, want) {
				t.Errorf("message is missing %q:\n%s", want, text)
			}
		}
		if strings.Contains(text, "sudo") {
			t.Errorf("must not recommend sudo:\n%s", text)
		}
		if !errors.Is(err, util.ErrAlreadyTraced) || errors.Is(err, util.ErrPtraceDenied) {
			t.Error("expected ErrAlreadyTraced and not ErrPtraceDenied")
		}
	})

	t.Run("debugger", func(t *testing.T) {
		text := alreadyTracedError(773, tracerInfo{PID: 900, Cmd: "gdb -p 773", ParentName: "bash", ParentPID: 50}).Error()
		for _, want := range []string{"PID 900", "gdb", "only one tracer per process", "Stop or detach the other tracer (PID 900)"} {
			if !strings.Contains(text, want) {
				t.Errorf("message is missing %q:\n%s", want, text)
			}
		}
		if strings.Contains(strings.ToLower(text), "specter") || strings.Contains(text, "sudo") {
			t.Errorf("must not mention Specter or sudo:\n%s", text)
		}
	})

	t.Run("nothing known about the tracer", func(t *testing.T) {
		text := alreadyTracedError(773, tracerInfo{PID: 844}).Error()
		if !strings.Contains(text, "PID 773 is already being traced by PID 844") || strings.Contains(text, "()") {
			t.Errorf("unexpected message:\n%s", text)
		}
	})
}

func TestCheckTracers_Target(t *testing.T) {
	t.Run("traced target stops the start", func(t *testing.T) {
		f := newFakeProc(t)
		f.process(773, 844, 1, []string{"next-server"}, "next-server")
		f.process(844, 0, 825, []string{"strace", "-p", "773"}, "strace")
		f.process(825, 0, 1, []string{"specter"}, "specter")

		_, err := f.source().checkTracers(source.Target{PID: 773})
		if !errors.Is(err, util.ErrAlreadyTraced) {
			t.Fatalf("expected ErrAlreadyTraced, got %v", err)
		}
		if !strings.Contains(err.Error(), "specter (PID 825)") {
			t.Errorf("expected the other Specter to be named:\n%v", err)
		}
	})

	t.Run("target not traced", func(t *testing.T) {
		f := newFakeProc(t)
		f.process(773, 0, 1, []string{"next-server"}, "next-server")
		target := source.Target{PID: 773, Children: []int{}}
		got, err := f.source().checkTracers(target)
		if err != nil || got.PID != 773 {
			t.Fatalf("expected the target to pass, got %+v, %v", got, err)
		}
	})

	t.Run("status unreadable keeps today's behavior", func(t *testing.T) {
		f := newFakeProc(t) // empty tree
		if _, err := f.source().checkTracers(source.Target{PID: 773}); err != nil {
			t.Fatalf("an unreadable status must not block the attach, got %v", err)
		}
	})
}

func TestCheckTracers_Workers(t *testing.T) {
	newTree := func(t *testing.T) *fakeProc {
		f := newFakeProc(t)
		f.process(773, 0, 1, []string{"supervisor"}, "supervisor")
		f.process(1019, 0, 773, []string{"worker"}, "worker")
		f.process(1039, 1500, 773, []string{"worker"}, "worker")
		f.process(1040, 1501, 773, []string{"worker"}, "worker")
		f.process(1500, 0, 1, []string{"strace"}, "strace")
		f.process(1501, 0, 1, []string{"gdb"}, "gdb")
		return f
	}
	collect := func(s *Source) *[]string {
		var notices []string
		s.SetNotifier(func(msg string) { notices = append(notices, msg) })
		return &notices
	}

	t.Run("one worker already traced", func(t *testing.T) {
		f := newTree(t)
		s := f.source()
		notices := collect(s)

		got, err := s.checkTracers(source.Target{PID: 773, Children: []int{1019, 1039}})
		if err != nil {
			t.Fatal(err)
		}
		args := BuildStraceArgs(got)
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "-p 773") || !strings.Contains(joined, "-p 1019") || strings.Contains(joined, "1039") {
			t.Errorf("expected -p for 773 and 1019 only, got %v", args)
		}
		if len(*notices) != 1 || !strings.Contains((*notices)[0], "Worker PID 1039 is already traced by PID 1500") ||
			!strings.Contains((*notices)[0], "not captured") {
			t.Errorf("expected one notice about worker 1039, got %v", *notices)
		}
	})

	t.Run("several workers give one notice", func(t *testing.T) {
		f := newTree(t)
		s := f.source()
		notices := collect(s)

		got, err := s.checkTracers(source.Target{PID: 773, Children: []int{1019, 1039, 1040}})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Children) != 1 || got.Children[0] != 1019 {
			t.Errorf("expected only worker 1019 to be kept, got %v", got.Children)
		}
		if len(*notices) != 1 {
			t.Fatalf("expected one notice, got %v", *notices)
		}
		for _, want := range []string{"PID 1039 (traced by PID 1500)", "PID 1040 (traced by PID 1501)", "not captured"} {
			if !strings.Contains((*notices)[0], want) {
				t.Errorf("notice is missing %q: %s", want, (*notices)[0])
			}
		}
	})

	t.Run("no traced workers means no notice", func(t *testing.T) {
		f := newTree(t)
		s := f.source()
		notices := collect(s)
		got, err := s.checkTracers(source.Target{PID: 773, Children: []int{1019}})
		if err != nil || len(got.Children) != 1 || len(*notices) != 0 {
			t.Errorf("got %+v, %v, notices %v", got, err, *notices)
		}
	})
}

func TestDiagnoseAttach(t *testing.T) {
	const eperm = "strace: attach: ptrace(PTRACE_SEIZE, 773): Operation not permitted"

	t.Run("a tracer appeared after the check", func(t *testing.T) {
		f := newFakeProc(t)
		f.process(773, 844, 1, []string{"app"}, "app")
		f.process(844, 0, 825, []string{"strace"}, "strace")
		f.process(825, 0, 1, []string{"specter"}, "specter")

		err := f.source().diagnoseAttach(eperm, source.Target{PID: 773})
		if !errors.Is(err, util.ErrAlreadyTraced) || strings.Contains(err.Error(), "sudo") {
			t.Fatalf("expected the already traced diagnostic without sudo, got %v", err)
		}
	})

	t.Run("a real permission problem", func(t *testing.T) {
		f := newFakeProc(t)
		f.process(773, 0, 1, []string{"app"}, "app")
		err := f.source().diagnoseAttach(eperm, source.Target{PID: 773})
		if !errors.Is(err, util.ErrPtraceDenied) || !strings.Contains(err.Error(), "sudo specter --pid 773") {
			t.Fatalf("expected the permission diagnostic, got %v", err)
		}
	})

	t.Run("no such process", func(t *testing.T) {
		err := newFakeProc(t).source().diagnoseAttach("strace: attach: ptrace(PTRACE_SEIZE, 773): No such process", source.Target{PID: 773})
		if !errors.Is(err, util.ErrProcessNotFound) {
			t.Fatalf("expected ErrProcessNotFound, got %v", err)
		}
	})

	t.Run("other strace error", func(t *testing.T) {
		err := newFakeProc(t).source().diagnoseAttach("strace: something else", source.Target{PID: 773})
		if err == nil || !strings.Contains(err.Error(), "something else") {
			t.Fatalf("expected the strace message, got %v", err)
		}
	})

	t.Run("nothing captured", func(t *testing.T) {
		if err := newFakeProc(t).source().diagnoseAttach("", source.Target{PID: 773}); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})
}

func TestFailedAttachTo(t *testing.T) {
	cases := []struct {
		line string
		pid  int
		want bool
	}{
		{"strace: attach: ptrace(PTRACE_SEIZE, 773): Operation not permitted", 773, true},
		{"strace: attach: ptrace(PTRACE_ATTACH, 773): Operation not permitted", 773, true},
		{"strace: attach: ptrace(PTRACE_SEIZE, 773): No such process", 773, true},
		{"strace: attach: ptrace(PTRACE_SEIZE, 1039): Operation not permitted", 773, false},
		{"strace: attach: ptrace(PTRACE_SEIZE, 7730): Operation not permitted", 773, false},
		{"strace: Could not attach to process. If your uid matches the uid of the target process", 773, false},
		{"strace: Process 773 attached", 773, false},
		{"", 773, false},
	}
	for _, tc := range cases {
		if got := failedAttachTo(tc.line, tc.pid); got != tc.want {
			t.Errorf("failedAttachTo(%q, %d) = %v, want %v", tc.line, tc.pid, got, tc.want)
		}
	}
}

// writeFakeStrace puts a script named strace first on PATH. Linux only.
func writeFakeStrace(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("needs a shell script named strace, so Linux only")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "strace"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// With several -p arguments strace keeps running when only some attaches fail. When the target itself is the one
// that failed, Specter must stop and say why, so the run ends with an error and not with silence.
func TestStart_TargetAttachFailureEndsWithAnError(t *testing.T) {
	const eperm = `echo "strace: attach: ptrace(PTRACE_SEIZE, 100): Operation not permitted" >&2`

	t.Run("target fails while strace keeps running for a worker", func(t *testing.T) {
		writeFakeStrace(t, eperm+"\nexec sleep 30\n")
		f := newFakeProc(t)
		f.process(100, 0, 1, []string{"app"}, "app")
		f.process(200, 0, 100, []string{"worker"}, "worker")

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		chunks, errs := f.source().Start(ctx, source.Target{PID: 100, Children: []int{200}})
		go func() {
			for range chunks {
			}
		}()

		select {
		case err := <-errs:
			if !errors.Is(err, util.ErrPtraceDenied) {
				t.Fatalf("expected the permission diagnostic, got %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Specter kept waiting although it could not attach to the target")
		}
	})

	t.Run("a worker failing does not end the run", func(t *testing.T) {
		writeFakeStrace(t, `echo "strace: attach: ptrace(PTRACE_SEIZE, 200): Operation not permitted" >&2`+"\nexec sleep 30\n")
		f := newFakeProc(t)
		f.process(100, 0, 1, []string{"app"}, "app")
		f.process(200, 0, 100, []string{"worker"}, "worker")

		ctx, cancel := context.WithCancel(context.Background())
		chunks, errs := f.source().Start(ctx, source.Target{PID: 100, Children: []int{200}})
		go func() {
			for range chunks {
			}
		}()

		select {
		case err := <-errs:
			t.Fatalf("a worker that cannot be attached must not stop the run, got %v", err)
		case <-time.After(1500 * time.Millisecond):
		}
		cancel()
	})

	t.Run("target already traced never starts strace", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "ran")
		writeFakeStrace(t, "echo started > "+marker+"\nexec sleep 30\n")
		f := newFakeProc(t)
		f.process(100, 844, 1, []string{"app"}, "app")
		f.process(844, 0, 825, []string{"strace", "-p", "100"}, "strace")
		f.process(825, 0, 1, []string{"specter"}, "specter")

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		chunks, errs := f.source().Start(ctx, source.Target{PID: 100})
		err := <-errs
		if !errors.Is(err, util.ErrAlreadyTraced) {
			t.Fatalf("expected ErrAlreadyTraced, got %v", err)
		}
		if _, open := <-chunks; open {
			t.Error("expected the chunk channel to be closed")
		}
		time.Sleep(200 * time.Millisecond)
		if _, statErr := os.Stat(marker); statErr == nil {
			t.Error("strace was started although the target is already traced")
		}
	})
}
