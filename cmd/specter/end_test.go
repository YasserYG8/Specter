package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/YasserYG8/Specter/internal/source"
)

func intPtr(n int) *int { return &n }

func TestReportEnd(t *testing.T) {
	exited := func(code int) *source.ExitStatus { return &source.ExitStatus{PID: 617, Code: code} }
	killed := func(sig string) *source.ExitStatus { return &source.ExitStatus{PID: 595, Signal: sig} }

	cases := []struct {
		name       string
		report     endReport
		want       []string // every line that must appear
		notWant    []string
		wantStatus int
	}{
		{
			name:       "user interrupts a process",
			report:     endReport{Interrupted: true, PID: 595},
			want:       []string{"Detached from PID 595 cleanly."},
			notWant:    []string{"exited", "killed"},
			wantStatus: 0,
		},
		{
			name:       "user interrupts a container",
			report:     endReport{Interrupted: true, Container: "billing-api"},
			want:       []string{"Detached from container billing-api cleanly."},
			wantStatus: 0,
		},
		{
			name:       "the user's interrupt wins over an exit that arrived at the same time",
			report:     endReport{Interrupted: true, PID: 617, Exit: exited(3), WatcherGone: true},
			want:       []string{"Detached from PID 617 cleanly."},
			notWant:    []string{"exited with code"},
			wantStatus: 0,
		},
		{
			name:       "exit with a code",
			report:     endReport{PID: 617, Exit: exited(3)},
			want:       []string{"Target PID 617 exited with code 3."},
			notWant:    []string{"Detached", "out-of-memory"},
			wantStatus: 1,
		},
		{
			name:       "exit with code 0 is still the target ending",
			report:     endReport{PID: 617, Exit: exited(0)},
			want:       []string{"Target PID 617 exited with code 0."},
			wantStatus: 1,
		},
		{
			name:       "killed by SIGTERM",
			report:     endReport{PID: 595, Exit: killed("SIGTERM")},
			want:       []string{"Target PID 595 was killed by SIGTERM."},
			notWant:    []string{"out-of-memory"},
			wantStatus: 1,
		},
		{
			name:       "killed by SIGKILL adds the out-of-memory hint",
			report:     endReport{PID: 595, Exit: killed("SIGKILL")},
			want:       []string{"Target PID 595 was killed by SIGKILL.", "SIGKILL often comes from the kernel's out-of-memory killer. To check: dmesg | grep -i oom"},
			wantStatus: 1,
		},
		{
			name:       "the watcher saw the target go and no code is known",
			report:     endReport{PID: 595, WatcherGone: true},
			want:       []string{"Target PID 595 exited."},
			notWant:    []string{"with code", "killed"},
			wantStatus: 1,
		},
		{
			name:       "strace's code is preferred when the watcher fired too",
			report:     endReport{PID: 617, Exit: exited(3), WatcherGone: true},
			want:       []string{"Target PID 617 exited with code 3."},
			wantStatus: 1,
		},
		{
			name:       "container exits with a code",
			report:     endReport{Container: "billing-api", DockerEnded: true, ContainerCode: intPtr(137)},
			want:       []string{"Container billing-api exited with code 137."},
			notWant:    []string{"Detached"},
			wantStatus: 1,
		},
		{
			name:       "container exits with code 0",
			report:     endReport{Container: "billing-api", DockerEnded: true, ContainerCode: intPtr(0)},
			want:       []string{"Container billing-api exited with code 0."},
			wantStatus: 1,
		},
		{
			name:       "container state unreadable",
			report:     endReport{Container: "billing-api", DockerEnded: true},
			want:       []string{"Container billing-api stopped."},
			notWant:    []string{"with code"},
			wantStatus: 1,
		},
		{
			name:       "the source stopped and nothing says why",
			report:     endReport{PID: 42},
			want:       []string{"Capture of PID 42 ended because its log source stopped."},
			notWant:    []string{"Detached"},
			wantStatus: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			status := reportEnd(&out, "[specter]", tc.report)
			if status != tc.wantStatus {
				t.Errorf("exit status = %d, want %d", status, tc.wantStatus)
			}
			text := out.String()
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("output is missing %q:\n%s", want, text)
				}
			}
			for _, not := range tc.notWant {
				if strings.Contains(text, not) {
					t.Errorf("output should not contain %q:\n%s", not, text)
				}
			}
		})
	}
}

// "Detached cleanly" must mean one thing: the user interrupted.
func TestReportEnd_DetachedOnlyWhenInterrupted(t *testing.T) {
	reports := []endReport{
		{PID: 1, Exit: &source.ExitStatus{PID: 1, Code: 0}},
		{PID: 1, Exit: &source.ExitStatus{PID: 1, Signal: "SIGKILL"}},
		{PID: 1, WatcherGone: true},
		{Container: "c", DockerEnded: true},
		{Container: "c", DockerEnded: true, ContainerCode: intPtr(0)},
		{PID: 1},
	}
	for _, r := range reports {
		var out bytes.Buffer
		if status := reportEnd(&out, "[specter]", r); status != 1 || strings.Contains(out.String(), "Detached") {
			t.Errorf("%+v: status %d, output %q; want status 1 and no 'Detached'", r, status, out.String())
		}
	}
}

// fakeSource only has a name, which is all watchesPID looks at, plus an optional FollowsPID.
type fakeSource struct{ name string }

func (f fakeSource) Name() string { return f.name }
func (f fakeSource) Start(context.Context, source.Target) (<-chan source.Chunk, <-chan error) {
	return nil, nil
}
func (f fakeSource) Stop() error { return nil }

type fakeJournal struct {
	fakeSource
	pid bool
}

func (f fakeJournal) FollowsPID() bool { return f.pid }

func TestWatchesPID(t *testing.T) {
	host := &source.Target{PID: 100}
	cases := []struct {
		name   string
		src    source.Source
		target *source.Target
		want   bool
	}{
		{"strace", fakeSource{"strace"}, host, true},
		{"file", fakeSource{"file"}, host, true},
		{"journal by PID", fakeJournal{fakeSource{"journal"}, true}, host, true},
		{"journal by unit", fakeJournal{fakeSource{"journal"}, false}, host, false},
		{"docker", fakeSource{"docker"}, host, false},
		{"docker, container with no host PID", fakeSource{"docker"}, &source.Target{Container: "web"}, false},
		{"no PID to watch", fakeSource{"strace"}, &source.Target{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := watchesPID(tc.src, tc.target); got != tc.want {
				t.Errorf("watchesPID = %v, want %v", got, tc.want)
			}
		})
	}
}
