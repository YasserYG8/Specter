package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/YasserYG8/Specter/internal/resolver"
	"github.com/YasserYG8/Specter/internal/source"
	"github.com/YasserYG8/Specter/internal/util"
)

// runCLI runs the root command with args, the way the user would, and returns its error.
func runCLI(args ...string) error {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetErr(&out)
	cmd.SetOut(&out)
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestTargetLabel(t *testing.T) {
	for arg, want := range map[string]string{
		"3000":     ":3000",
		"65535":    ":65535",
		"65536":    "pid:65536", // too large for a port, so it is a PID
		"70123":    "pid:70123",
		"web":      "web",
		"My-App":   "My-App", // the label keeps what the user typed
		"0":        "0",
		"postgres": "postgres",
	} {
		if got := targetLabel(arg); got != want {
			t.Errorf("targetLabel(%q) = %q, want %q", arg, got, want)
		}
	}
}

func TestBuildTargetSpecs_PositionalFirstThenPID(t *testing.T) {
	specs := buildTargetSpecs([]string{"3000", "web"}, []int{70123, 5})
	var labels []string
	for _, s := range specs {
		labels = append(labels, s.Label)
	}
	if got, want := strings.Join(labels, ","), ":3000,web,pid:70123,pid:5"; got != want {
		t.Errorf("labels = %s, want %s", got, want)
	}
	if specs[2].PID != 70123 || specs[2].Arg != "" {
		t.Errorf("--pid spec = %+v", specs[2])
	}
}

func TestValidateTargetCount(t *testing.T) {
	nine := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}
	if err := validateTargetCount(nine[:8], nil); err != nil {
		t.Errorf("8 targets are allowed, got %v", err)
	}
	if err := validateTargetCount(nine[:5], []int{1, 2, 3}); err != nil {
		t.Errorf("5 + 3 targets are allowed, got %v", err)
	}
	for name, err := range map[string]error{
		"nine positional":  validateTargetCount(nine, nil),
		"five and four":    validateTargetCount(nine[:5], []int{1, 2, 3, 4}),
		"nine --pid flags": validateTargetCount(nil, []int{1, 2, 3, 4, 5, 6, 7, 8, 9}),
	} {
		if err == nil || !strings.Contains(err.Error(), "at most 8 targets") {
			t.Errorf("%s: expected an error that names the limit, got %v", name, err)
		}
		if !errors.Is(err, util.ErrInvalidFlag) {
			t.Errorf("%s: expected the error to wrap ErrInvalidFlag", name)
		}
	}
}

// Nine targets must be refused before anything is resolved, on every platform.
func TestCLI_TooManyTargetsFailsBeforeResolving(t *testing.T) {
	err := runCLI("1", "2", "3", "4", "5", "6", "7", "8", "9")
	if err == nil || !strings.Contains(err.Error(), "at most 8 targets") {
		t.Fatalf("expected the limit error, got %v", err)
	}
	err = runCLI("1", "2", "3", "4", "5", "--pid", "10", "--pid", "11", "-p", "12", "-p", "13")
	if err == nil || !strings.Contains(err.Error(), "at most 8 targets") {
		t.Fatalf("positional and --pid together count, got %v", err)
	}
	flagPIDs = nil
}

func TestCLI_SeveralTargetsAreAccepted(t *testing.T) {
	// Not refused by argument checking. Where Specter cannot run (not Linux), the run stops at the platform check,
	// which comes after every argument check, so the error there shows that the arguments were accepted.
	err := runCLI("3000", "4000", "--pid", "70123", "--pid", "70124")
	flagPIDs = nil
	if err != nil && (strings.Contains(err.Error(), "accepts") || strings.Contains(err.Error(), "Too many") || strings.Contains(err.Error(), "Only one target")) {
		t.Errorf("several targets must be accepted, got %v", err)
	}
}

// fakeFullResolver resolves from fixed tables.
type fakeFullResolver struct {
	ports      map[int]*source.Target
	pids       map[int]*source.Target
	containers map[string]*source.Target
	services   []resolver.DiscoveredService
}

func (f *fakeFullResolver) ResolvePort(_ context.Context, port int) (*source.Target, error) {
	if t, ok := f.ports[port]; ok {
		return t, nil
	}
	return nil, &resolver.NoProcessOnPortError{RequestedPort: port}
}

func (f *fakeFullResolver) ResolvePID(_ context.Context, pid int) (*source.Target, error) {
	if t, ok := f.pids[pid]; ok {
		return t, nil
	}
	return nil, util.ErrProcessNotFound
}

func (f *fakeFullResolver) ResolveContainer(_ context.Context, name string) (*source.Target, error) {
	if t, ok := f.containers[name]; ok {
		return t, nil
	}
	return nil, errors.New("no such container")
}

func (f *fakeFullResolver) DiscoverServices(context.Context) ([]resolver.DiscoveredService, error) {
	return f.services, nil
}

func TestResolveSpec(t *testing.T) {
	r := &fakeFullResolver{
		ports:      map[int]*source.Target{3000: {Port: 3000, PID: 11}},
		pids:       map[int]*source.Target{3000: {PID: 3000}, 70123: {PID: 70123}},
		containers: map[string]*source.Target{"web": {Container: "web"}},
		services:   []resolver.DiscoveredService{{ProjectName: "Shop", Kind: resolver.ServiceKindHost, Port: 3000}},
	}
	ctx := context.Background()

	t.Run("a number up to 65535 is a port", func(t *testing.T) {
		got, err := resolveSpec(ctx, r, targetSpec{Label: ":3000", Arg: "3000"})
		if err != nil || got.PID != 11 {
			t.Errorf("got %+v, %v; want the process on port 3000", got, err)
		}
	})
	t.Run("--pid is never a port", func(t *testing.T) {
		got, err := resolveSpec(ctx, r, targetSpec{Label: "pid:3000", PID: 3000})
		if err != nil || got.PID != 3000 {
			t.Errorf("got %+v, %v; want PID 3000", got, err)
		}
	})
	t.Run("a number above 65535 is a PID", func(t *testing.T) {
		got, err := resolveSpec(ctx, r, targetSpec{Label: "pid:70123", Arg: "70123"})
		if err != nil || got.PID != 70123 {
			t.Errorf("got %+v, %v; want PID 70123", got, err)
		}
	})
	t.Run("an empty port gives the port diagnostic", func(t *testing.T) {
		_, err := resolveSpec(ctx, r, targetSpec{Label: ":4000", Arg: "4000"})
		var np *resolver.NoProcessOnPortError
		if !errors.As(err, &np) {
			t.Errorf("expected NoProcessOnPortError, got %v", err)
		}
	})
	t.Run("a container", func(t *testing.T) {
		got, err := resolveSpec(ctx, r, targetSpec{Label: "web", Arg: "web"})
		if err != nil || got.Container != "web" {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("a project name, in any case", func(t *testing.T) {
		got, err := resolveSpec(ctx, r, targetSpec{Label: "shop", Arg: "shop"})
		if err != nil || got.PID != 11 {
			t.Errorf("got %+v, %v; want the project's process", got, err)
		}
	})
	t.Run("an unknown name", func(t *testing.T) {
		_, err := resolveSpec(ctx, r, targetSpec{Label: "nope", Arg: "nope"})
		if err == nil || !strings.Contains(err.Error(), `"nope" is not a valid port, container, or project`) {
			t.Errorf("got %v", err)
		}
	})
	t.Run("a PID that does not exist", func(t *testing.T) {
		_, err := resolveSpec(ctx, r, targetSpec{Label: "pid:9", PID: 9})
		if err == nil || !strings.Contains(err.Error(), "no process with PID 9 exists") {
			t.Errorf("got %v", err)
		}
	})
}

func TestDedupeTargets(t *testing.T) {
	rt := func(label string, tg source.Target) resolvedTarget { return resolvedTarget{Label: label, Target: &tg} }

	cases := []struct {
		name        string
		in          []resolvedTarget
		wantLabels  string
		wantNotices []string // each must be contained in a notice, in order
	}{
		{
			name:       "different processes are all kept",
			in:         []resolvedTarget{rt(":3000", source.Target{PID: 1}), rt(":4000", source.Target{PID: 2}), rt("web", source.Target{Container: "web"})},
			wantLabels: ":3000,:4000,web",
		},
		{
			name:        "one server on two ports",
			in:          []resolvedTarget{rt(":3000", source.Target{PID: 1234}), rt(":9229", source.Target{PID: 1234})},
			wantLabels:  ":3000",
			wantNotices: []string{":9229 is the same process as :3000 (PID 1234)"},
		},
		{
			name:        "the same port twice",
			in:          []resolvedTarget{rt(":3000", source.Target{PID: 7, Port: 3000}), rt(":3000", source.Target{PID: 7, Port: 3000})},
			wantLabels:  ":3000",
			wantNotices: []string{":3000 is the same process as :3000"},
		},
		{
			name:        "a worker of an earlier target",
			in:          []resolvedTarget{rt(":3000", source.Target{PID: 1200, Children: []int{1300, 1301}}), rt("pid:1300", source.Target{PID: 1300})},
			wantLabels:  ":3000",
			wantNotices: []string{"pid:1300 (PID 1300) is a worker that :3000 already follows"},
		},
		{
			name:       "a worker given before its parent is kept, as it is not yet a child of anything kept",
			in:         []resolvedTarget{rt("pid:1300", source.Target{PID: 1300}), rt(":3000", source.Target{PID: 1200, Children: []int{1300}})},
			wantLabels: "pid:1300,:3000",
		},
		{
			name:        "the same container twice",
			in:          []resolvedTarget{rt("web", source.Target{Container: "web"}), rt(":8080", source.Target{Container: "web", Port: 8080})},
			wantLabels:  "web",
			wantNotices: []string{":8080 is the same container as web (web)"},
		},
		{
			name:        "two drops give two notices",
			in:          []resolvedTarget{rt("a", source.Target{PID: 1}), rt("b", source.Target{PID: 1}), rt("c", source.Target{PID: 1})},
			wantLabels:  "a",
			wantNotices: []string{"b is the same process as a", "c is the same process as a"},
		},
		{
			name:       "containers with PID 0 are different unless they are the same container",
			in:         []resolvedTarget{rt("x", source.Target{Container: "x"}), rt("y", source.Target{Container: "y"})},
			wantLabels: "x,y",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kept, notices := dedupeTargets(tc.in)
			var labels []string
			for _, k := range kept {
				labels = append(labels, k.Label)
			}
			if got := strings.Join(labels, ","); got != tc.wantLabels {
				t.Errorf("kept = %s, want %s", got, tc.wantLabels)
			}
			if len(notices) != len(tc.wantNotices) {
				t.Fatalf("notices = %q, want %d of them", notices, len(tc.wantNotices))
			}
			for i, want := range tc.wantNotices {
				if !strings.Contains(notices[i], want) {
					t.Errorf("notice %d = %q, want it to contain %q", i, notices[i], want)
				}
			}
		})
	}
}
