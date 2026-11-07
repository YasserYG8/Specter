package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/YasserYG8/Specter/internal/resolver"
	"github.com/YasserYG8/Specter/internal/source"
	"github.com/YasserYG8/Specter/internal/util"
)

// fakeResolver answers port and PID lookups from maps and records every call.
type fakeResolver struct {
	ports     map[int]*source.Target // ports with a listener
	pids      map[int]*source.Target // processes that exist
	portErrs  map[int]error          // errors for a port other than "nothing listens"
	activeFor []resolver.ListeningPort
	pidCalls  []int
	portCalls []int
}

func (f *fakeResolver) ResolvePort(_ context.Context, port int) (*source.Target, error) {
	f.portCalls = append(f.portCalls, port)
	if err, ok := f.portErrs[port]; ok {
		return nil, err
	}
	if t, ok := f.ports[port]; ok {
		return t, nil
	}
	return nil, &resolver.NoProcessOnPortError{RequestedPort: port, ActivePorts: f.activeFor}
}

func (f *fakeResolver) ResolvePID(_ context.Context, pid int) (*source.Target, error) {
	f.pidCalls = append(f.pidCalls, pid)
	if t, ok := f.pids[pid]; ok {
		return t, nil
	}
	return nil, fmt.Errorf("%w: %d", util.ErrProcessNotFound, pid)
}

func journald(pid int) *source.Target {
	return &source.Target{PID: pid, Cmdline: []string{"/usr/lib/systemd/systemd-journald"}}
}

func TestResolveNumericTarget_PortWithListener(t *testing.T) {
	f := &fakeResolver{ports: map[int]*source.Target{3000: {PID: 812, Port: 3000}}, pids: map[int]*source.Target{3000: journald(3000)}}

	got, err := resolveNumericTarget(context.Background(), f, 3000)
	if err != nil || got == nil || got.PID != 812 {
		t.Fatalf("expected the listener on port 3000, got %+v, %v", got, err)
	}
	if len(f.pidCalls) != 0 {
		t.Errorf("a port with a listener must not look at PIDs, looked at %v", f.pidCalls)
	}
}

// An empty port used to fall back to the process whose PID is the same number, and Specter attached to it.
func TestResolveNumericTarget_EmptyPortNeverAttachesToThePID(t *testing.T) {
	f := &fakeResolver{pids: map[int]*source.Target{3000: journald(3000)}}

	got, err := resolveNumericTarget(context.Background(), f, 3000)
	if got != nil {
		t.Fatalf("expected no target, got %+v", got)
	}
	var noProc *resolver.NoProcessOnPortError
	if !errors.As(err, &noProc) || noProc.RequestedPort != 3000 {
		t.Fatalf("expected the empty-port diagnostic for port 3000, got %v", err)
	}
	if !errors.Is(err, util.ErrNoProcessFound) {
		t.Error("expected the error to wrap ErrNoProcessFound")
	}
}

func TestResolveNumericTarget_HintNamesTheProcess(t *testing.T) {
	f := &fakeResolver{pids: map[int]*source.Target{3000: journald(3000)}}

	_, err := resolveNumericTarget(context.Background(), f, 3000)
	var noProc *resolver.NoProcessOnPortError
	if !errors.As(err, &noProc) {
		t.Fatalf("expected the empty-port diagnostic, got %v", err)
	}
	if noProc.HintPID != 3000 || noProc.HintName != "systemd-journald" {
		t.Errorf("unexpected hint fields: pid=%d name=%q", noProc.HintPID, noProc.HintName)
	}

	const want = "Hint: PID 3000 exists (systemd-journald). To attach to it: specter --pid 3000"
	lines := strings.Split(strings.TrimRight(noProc.FormatDiagnostic(false), "\n"), "\n")
	if last := lines[len(lines)-1]; last != want {
		t.Errorf("the last line should be the hint\nwant %q\n got %q", want, last)
	}
}

func TestResolveNumericTarget_HintComesAfterActivePortsAndSuggestion(t *testing.T) {
	suggested := resolver.ListeningPort{Port: 3001, PID: 55, ProcessName: "node"}
	f := &fakeResolver{
		pids:      map[int]*source.Target{3000: journald(3000)},
		activeFor: []resolver.ListeningPort{{Port: 8080, PID: 70, ProcessName: "python3"}, suggested},
	}
	_, err := resolveNumericTarget(context.Background(), f, 3000)
	var noProc *resolver.NoProcessOnPortError
	if !errors.As(err, &noProc) {
		t.Fatalf("expected the empty-port diagnostic, got %v", err)
	}
	noProc.Suggestion = &suggested

	out := noProc.FormatDiagnostic(false)
	for _, want := range []string{"No process is listening on port 3000.", ":8080", "Did you mean: specter 3001 ?"} {
		if !strings.Contains(out, want) {
			t.Errorf("diagnostic lost %q:\n%s", want, out)
		}
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "Hint: PID 3000 exists") {
		t.Errorf("the hint should be the last line, got %q", last)
	}
}

func TestResolveNumericTarget_HintWithoutAProcessName(t *testing.T) {
	f := &fakeResolver{pids: map[int]*source.Target{3000: {PID: 3000}}}
	_, err := resolveNumericTarget(context.Background(), f, 3000)
	var noProc *resolver.NoProcessOnPortError
	if !errors.As(err, &noProc) {
		t.Fatalf("expected the empty-port diagnostic, got %v", err)
	}
	if out := noProc.FormatDiagnostic(false); !strings.Contains(out, "Hint: PID 3000 exists. To attach to it: specter --pid 3000") {
		t.Errorf("expected a hint without parentheses, got:\n%s", out)
	}
}

func TestResolveNumericTarget_EmptyPortWithoutAMatchingPIDHasNoHint(t *testing.T) {
	f := &fakeResolver{}

	got, err := resolveNumericTarget(context.Background(), f, 3000)
	var noProc *resolver.NoProcessOnPortError
	if got != nil || !errors.As(err, &noProc) {
		t.Fatalf("expected the empty-port diagnostic and no target, got %+v, %v", got, err)
	}
	if noProc.HintPID != 0 || strings.Contains(noProc.FormatDiagnostic(false), "Hint:") {
		t.Errorf("expected no hint when no process has PID 3000:\n%s", noProc.FormatDiagnostic(false))
	}
}

func TestResolveNumericTarget_OtherPortErrorsAreReturnedWithoutLookingAtPIDs(t *testing.T) {
	denied := errors.New("permission denied reading /proc/net/tcp")
	f := &fakeResolver{
		portErrs: map[int]error{3000: denied},
		pids:     map[int]*source.Target{3000: journald(3000)},
	}

	got, err := resolveNumericTarget(context.Background(), f, 3000)
	if got != nil || !errors.Is(err, denied) {
		t.Fatalf("expected the permission error, got %+v, %v", got, err)
	}
	if len(f.pidCalls) != 0 {
		t.Errorf("expected no PID lookup after a port error, got %v", f.pidCalls)
	}
}

func TestResolveNumericTarget_PortRangeBoundary(t *testing.T) {
	f := &fakeResolver{pids: map[int]*source.Target{65535: journald(65535), 65536: journald(65536)}}

	if _, err := resolveNumericTarget(context.Background(), f, 65535); err == nil {
		t.Error("65535 is a port, so an empty one should fail rather than attach to PID 65535")
	}
	if len(f.portCalls) != 1 || f.portCalls[0] != 65535 {
		t.Errorf("expected 65535 to be looked up as a port, got %v", f.portCalls)
	}

	f.portCalls = nil
	got, err := resolveNumericTarget(context.Background(), f, 65536)
	if err != nil || got == nil || got.PID != 65536 {
		t.Fatalf("65536 cannot be a port and should be the PID, got %+v, %v", got, err)
	}
	if len(f.portCalls) != 0 {
		t.Errorf("a number above 65535 must not be looked up as a port, got %v", f.portCalls)
	}
}

func TestResolveNumericTarget_PIDAbovePortRange(t *testing.T) {
	f := &fakeResolver{pids: map[int]*source.Target{70000: journald(70000)}}
	got, err := resolveNumericTarget(context.Background(), f, 70000)
	if err != nil || got == nil || got.PID != 70000 {
		t.Fatalf("expected PID 70000, got %+v, %v", got, err)
	}
}

func TestResolveNumericTarget_MissingPIDAbovePortRange(t *testing.T) {
	f := &fakeResolver{}
	got, err := resolveNumericTarget(context.Background(), f, 70000)
	if got != nil || err == nil {
		t.Fatalf("expected an error, got %+v, %v", got, err)
	}
	if !errors.Is(err, util.ErrProcessNotFound) {
		t.Error("expected the error to wrap ErrProcessNotFound")
	}
	if !strings.Contains(err.Error(), "no process with PID 70000 exists") {
		t.Errorf("unexpected message %q", err.Error())
	}
}

func TestResolvePIDTarget_NeverChecksThePort(t *testing.T) {
	// A server listens on port 3000 and a different process has PID 3000
	f := &fakeResolver{
		ports: map[int]*source.Target{3000: {PID: 812, Port: 3000}},
		pids:  map[int]*source.Target{3000: journald(3000)},
	}
	got, err := resolvePIDTarget(context.Background(), f, 3000)
	if err != nil || got == nil || got.PID != 3000 {
		t.Fatalf("--pid 3000 should attach to PID 3000, got %+v, %v", got, err)
	}
	if len(f.portCalls) != 0 {
		t.Errorf("--pid must not look at ports, looked at %v", f.portCalls)
	}
}

func TestResolvePIDTarget_MissingPID(t *testing.T) {
	_, err := resolvePIDTarget(context.Background(), &fakeResolver{}, 3000)
	if err == nil || !strings.Contains(err.Error(), "no process with PID 3000 exists") || !errors.Is(err, util.ErrProcessNotFound) {
		t.Errorf("expected a 'no process with PID 3000 exists' error wrapping ErrProcessNotFound, got %v", err)
	}
}

func TestResolvePIDTarget_OtherErrorsPassThrough(t *testing.T) {
	denied := errors.New("permission denied")
	r := &errPIDResolver{err: denied}
	if _, err := resolvePIDTarget(context.Background(), r, 3000); !errors.Is(err, denied) {
		t.Errorf("expected the original error, got %v", err)
	}
}

type errPIDResolver struct{ err error }

func (r *errPIDResolver) ResolvePort(context.Context, int) (*source.Target, error) { return nil, r.err }
func (r *errPIDResolver) ResolvePID(context.Context, int) (*source.Target, error)  { return nil, r.err }

func TestValidatePIDFlag(t *testing.T) {
	cases := []struct {
		name    string
		pids    []int
		args    []string
		wantErr string // empty means no error
	}{
		{"not used", nil, []string{"3000"}, ""},
		{"one PID", []int{3000}, nil, ""},
		{"two values", []int{3000, 4000}, nil, "Only one target is supported for now"},
		{"positional target as well", []int{3000}, []string{"8080"}, "Give either a target or --pid, not both"},
		{"zero", []int{0}, nil, "The PID must be a positive whole number"},
		{"negative", []int{-5}, nil, "The PID must be a positive whole number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePIDFlag(tc.pids, tc.args)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected an error containing %q, got %v", tc.wantErr, err)
			}
			if !errors.Is(err, util.ErrInvalidFlag) {
				t.Error("expected the error to wrap ErrInvalidFlag")
			}
		})
	}
}

// The command line must reject these before anything is resolved or attached, so they behave the same on every
// platform. (A valid --pid goes on to resolve, which only works on Linux, so it is not run here.)
func TestCLI_PIDFlagIsValidatedBeforeResolving(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"two flags", []string{"--pid", "3000", "--pid", "4000"}, "Only one target is supported for now"},
		{"comma list", []string{"--pid", "3000,4000"}, "Only one target is supported for now"},
		{"short form", []string{"-p", "3000", "-p", "4000"}, "Only one target is supported for now"},
		{"with a positional target", []string{"8080", "--pid", "3000"}, "Give either a target or --pid, not both"},
		{"zero", []string{"--pid", "0"}, "The PID must be a positive whole number"},
		{"negative", []string{"--pid=-7"}, "The PID must be a positive whole number"},
		{"not a number", []string{"--pid", "abc"}, "The PID must be a positive whole number"},
		{"short form, not a number", []string{"-p", "abc"}, "The PID must be a positive whole number"},
		{"missing value", []string{"--pid"}, "The PID must be a positive whole number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newRootCmd()
			var out bytes.Buffer
			cmd.SetErr(&out)
			cmd.SetOut(&out)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected an error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestCLI_ShortPIDFlagBehavesLikeLongPIDFlag(t *testing.T) {
	for _, args := range [][]string{{"--pid", "3000"}, {"-p", "3000"}, {"--pid=3000"}, {"-p3000"}} {
		cmd := newRootCmd()
		if err := cmd.ParseFlags(args); err != nil {
			t.Fatalf("%v was rejected: %v", args, err)
		}
		if len(flagPIDs) != 1 || flagPIDs[0] != 3000 {
			t.Errorf("%v did not set the PID, got %v", args, flagPIDs)
		}
	}
	flagPIDs = nil
}

func TestCLI_PIDFlagIsDocumented(t *testing.T) {
	cmd := newRootCmd()
	if !strings.Contains(cmd.Example, "specter --pid 14280") {
		t.Error("the examples should show attaching by PID with --pid")
	}
	if strings.Contains(cmd.Example, "specter 14280") {
		t.Error("the examples still show a bare PID, which is now read as a port")
	}
	if !strings.Contains(cmd.Long, "--pid") || !strings.Contains(cmd.Long, "bare number is always a port") {
		t.Errorf("the long description should say a bare number is a port and point to --pid, got %q", cmd.Long)
	}
	if strings.Contains(cmd.Use, "pid") {
		t.Errorf("the usage line still offers a bare PID: %q", cmd.Use)
	}
}
