package selector

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YasserYG8/Specter/internal/source"
	"github.com/YasserYG8/Specter/internal/source/ebpf"
	"github.com/YasserYG8/Specter/internal/util"
)

func TestSelector_ForceSource(t *testing.T) {
	for _, srcName := range []string{"docker", "file", "journal", "ebpf", "strace"} {
		sel := NewSelector(srcName, false)
		sel.CheckEBPF = ebpfAvailable
		src, reason, err := sel.Select(&source.Target{PID: 100})
		if err != nil {
			t.Fatalf("unexpected error for force source %q: %v", srcName, err)
		}
		if src.Name() != srcName {
			t.Errorf("expected source %q, got %q", srcName, src.Name())
		}
		if reason == "" {
			t.Errorf("expected non-empty reason for %q", srcName)
		}
	}
}

func TestSelector_InvalidForceSource(t *testing.T) {
	sel := NewSelector("invalid_source", false)
	_, _, err := sel.Select(&source.Target{PID: 100})
	if err == nil {
		t.Fatal("expected error for invalid source, got nil")
	}
}

func TestSelector_AutoDetectFile(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "server.log")
	if err := os.WriteFile(logFile, []byte("init"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	sel := NewSelector("", false)
	target := &source.Target{
		PID:    100,
		Stdout: logFile,
	}

	src, reason, err := sel.Select(target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.Name() != "file" {
		t.Errorf("expected source 'file', got %q", src.Name())
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestSelector_AutoDetectDefaultStrace(t *testing.T) {
	sel := NewSelector("", true)
	sel.CheckEBPF = ebpfUnavailable
	target := &source.Target{
		PID:    100,
		Stdout: "/dev/pts/1",
		Stderr: "/dev/pts/1",
	}

	src, reason, err := sel.Select(target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.Name() != "strace" {
		t.Errorf("expected default source 'strace', got %q", src.Name())
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

// A process started by a CI runner sits in the runner service's cgroup but does not log to the journal.
func TestSelector_CIRunnerServiceUsesStrace(t *testing.T) {
	procRoot := t.TempDir()
	pidDir := filepath.Join(procRoot, "4242")
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		t.Fatalf("failed to create mock proc dir: %v", err)
	}
	cgroup := "0::/system.slice/hosted-compute-agent.service\n"
	if err := os.WriteFile(filepath.Join(pidDir, "cgroup"), []byte(cgroup), 0644); err != nil {
		t.Fatalf("failed to write mock cgroup: %v", err)
	}

	sel := NewSelector("", false)
	sel.ProcRoot = procRoot
	sel.CheckEBPF = ebpfUnavailable

	src, _, err := sel.Select(&source.Target{PID: 4242, Stdout: "pipe:[1]", Stderr: "pipe:[2]"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.Name() != "strace" {
		t.Errorf("expected source 'strace', got %q", src.Name())
	}
}

func TestSelector_DirectContainerTarget(t *testing.T) {
	sel := NewSelector("", false)
	target := &source.Target{
		PID:       0,
		Container: "phpmyadmin",
	}

	src, reason, err := sel.Select(target)
	if err != nil {
		// If docker binary is not on host testing machine, error should state docker executable not found
		t.Logf("docker lookpath failed on this environment: %v", err)
		return
	}
	if src.Name() != "docker" {
		t.Errorf("expected source 'docker', got %q", src.Name())
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestSelector_ZeroPIDAndNoContainer(t *testing.T) {
	sel := NewSelector("", false)
	target := &source.Target{
		PID:       0,
		Container: "",
	}

	_, _, err := sel.Select(target)
	if err == nil {
		t.Fatal("expected error for PID 0 without container, got nil")
	}
}

// ebpfAvailable and ebpfUnavailable stand in for the real check of the machine, so these tests give the same result
// on a laptop and on a root CI runner.
func ebpfAvailable() error { return nil }

func ebpfUnavailable() error {
	return &ebpf.Unavailable{
		Reason:      ebpf.ReasonNoPrivileges,
		Notice:      "Run with sudo to use the fast eBPF engine.",
		Explanation: "eBPF needs root, or the capabilities CAP_BPF and CAP_PERFMON, and Specter has neither.",
		Remedy:      "Run Specter with sudo, or give it CAP_BPF and CAP_PERFMON.",
	}
}

var plainTerminalTarget = func() *source.Target {
	return &source.Target{PID: 100, Stdout: "/dev/pts/1", Stderr: "/dev/pts/1"}
}

func TestSelector_EBPFWhenItCanRun(t *testing.T) {
	sel := NewSelector("", false)
	sel.CheckEBPF = ebpfAvailable

	src, reason, err := sel.Select(plainTerminalTarget())
	if err != nil {
		t.Fatal(err)
	}
	if src.Name() != "ebpf" {
		t.Errorf("expected the ebpf source, got %q", src.Name())
	}
	if reason == "" {
		t.Error("expected a reason for the Source: line")
	}
	if sel.EBPFNote != "" {
		t.Errorf("eBPF was chosen, so there is no reason to explain: %q", sel.EBPFNote)
	}
}

func TestSelector_StraceWithTheReasonWhenEBPFCannotRun(t *testing.T) {
	sel := NewSelector("", false)
	sel.CheckEBPF = ebpfUnavailable

	src, _, err := sel.Select(plainTerminalTarget())
	if err != nil {
		t.Fatal(err)
	}
	if src.Name() != "strace" {
		t.Errorf("expected strace, got %q", src.Name())
	}
	if sel.EBPFNote != "Run with sudo to use the fast eBPF engine." {
		t.Errorf("EBPFNote = %q", sel.EBPFNote)
	}
}

func TestSelector_OldKernelNoteNamesTheVersion(t *testing.T) {
	sel := NewSelector("", false)
	sel.CheckEBPF = func() error {
		return &ebpf.Unavailable{Reason: ebpf.ReasonOldKernel, Notice: "eBPF needs Linux 5.8 or later (this kernel is 5.4)."}
	}
	if _, _, err := sel.Select(plainTerminalTarget()); err != nil {
		t.Fatal(err)
	}
	if sel.EBPFNote != "eBPF needs Linux 5.8 or later (this kernel is 5.4)." {
		t.Errorf("EBPFNote = %q", sel.EBPFNote)
	}
}

// eBPF comes after Docker, the journal and files, which do not slow the target and need no privileges.
func TestSelector_FileSourceWinsOverEBPF(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "server.log")
	if err := os.WriteFile(logFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sel := NewSelector("", false)
	sel.CheckEBPF = ebpfAvailable

	src, _, err := sel.Select(&source.Target{PID: 100, Stdout: logFile})
	if err != nil {
		t.Fatal(err)
	}
	if src.Name() != "file" {
		t.Errorf("a regular log file should win over eBPF, got %q", src.Name())
	}
	if sel.EBPFNote != "" {
		t.Errorf("eBPF was never considered, so no reason: %q", sel.EBPFNote)
	}
}

func TestSelector_StraceForcedByTheUserGivesNoReason(t *testing.T) {
	sel := NewSelector("strace", false)
	sel.CheckEBPF = ebpfAvailable // eBPF could run, and the user chose strace anyway

	src, _, err := sel.Select(plainTerminalTarget())
	if err != nil {
		t.Fatal(err)
	}
	if src.Name() != "strace" {
		t.Errorf("expected strace, got %q", src.Name())
	}
	if sel.EBPFNote != "" {
		t.Errorf("the user chose strace: no tip, got %q", sel.EBPFNote)
	}
}

func TestSelector_ForcedEBPFThatCannotRunIsADiagnosticAndNeverFallsBack(t *testing.T) {
	sel := NewSelector("ebpf", false)
	sel.CheckEBPF = ebpfUnavailable

	src, _, err := sel.Select(plainTerminalTarget())
	if err == nil {
		t.Fatalf("expected an error, got the %q source", src.Name())
	}
	if !errors.Is(err, util.ErrEBPFUnavailable) {
		t.Errorf("expected ErrEBPFUnavailable, got %v", err)
	}
	for _, want := range []string{"eBPF needs root", "Run Specter with sudo"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the diagnostic should contain %q, got:\n%v", want, err)
		}
	}
	if sel.EBPFNote != "" {
		t.Errorf("a forced source has no fallback note: %q", sel.EBPFNote)
	}
}

func TestSelector_ForcedEBPFWithAPlainErrorStillExplains(t *testing.T) {
	sel := NewSelector("ebpf", false)
	sel.CheckEBPF = func() error { return errors.New("something odd") }

	_, _, err := sel.Select(plainTerminalTarget())
	if err == nil || !errors.Is(err, util.ErrEBPFUnavailable) || !strings.Contains(err.Error(), "something odd") {
		t.Errorf("got %v", err)
	}
}

func TestSelector_InvalidSourceListsEBPF(t *testing.T) {
	_, _, err := NewSelector("invalid", false).Select(plainTerminalTarget())
	if err == nil || !strings.Contains(err.Error(), "ebpf") {
		t.Errorf("the list of supported sources should include ebpf, got %v", err)
	}
}

func TestSelector_WithoutNewSelectorStillChecksEBPF(t *testing.T) {
	sel := &Selector{ProcRoot: "/proc"} // CheckEBPF is nil
	if _, _, err := sel.Select(plainTerminalTarget()); err != nil {
		t.Fatal(err)
	}
}
