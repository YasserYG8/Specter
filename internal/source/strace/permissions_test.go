package strace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YasserYG8/Specter/internal/util"
)

// procWithScope builds a fake proc root whose ptrace_scope has the given content. An empty scope leaves the file
// out, like a kernel without Yama.
func procWithScope(t *testing.T, scope string) string {
	t.Helper()
	root := t.TempDir()
	if scope == "" {
		return root
	}
	dir := filepath.Join(root, "sys", "kernel", "yama")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ptrace_scope"), []byte(scope+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCheckPtracePermissionsAs(t *testing.T) {
	const root, user = 0, 1000

	cases := []struct {
		name     string
		scope    string
		euid     int
		wantWarn bool
		wantText string
	}{
		{"normal user at scope 0", "0", user, false, ""},
		{"normal user at scope 1", "1", user, true, "ptrace_scope = 1"},
		{"normal user at scope 2", "2", user, true, "ptrace_scope = 2"},
		{"normal user at scope 3", "3", user, true, "ptrace_scope = 3"},
		{"root at scope 0", "0", root, false, ""},
		{"root at scope 1", "1", root, false, ""},
		{"root at scope 2", "2", root, false, ""},
		{"root at scope 3", "3", root, true, "ptrace_scope = 3"},
		{"no Yama on this kernel", "", user, false, ""},
		{"unknown user, as on Windows", "1", -1, true, "ptrace_scope = 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckPtracePermissionsAs(procWithScope(t, tc.scope), tc.euid)
			if !tc.wantWarn {
				if err != nil {
					t.Fatalf("expected no warning, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("expected a warning containing %q, got %v", tc.wantText, err)
			}
			if !errors.Is(err, util.ErrPtraceDenied) {
				t.Error("expected the warning to wrap ErrPtraceDenied")
			}
		})
	}
}

// CheckPtracePermissions does not know the user, so it keeps warning at scope 1 and 2 for everyone.
func TestCheckPtracePermissions_KeepsItsBehavior(t *testing.T) {
	if err := CheckPtracePermissions(procWithScope(t, "1")); err == nil {
		t.Error("expected the scope 1 warning")
	}
	if err := CheckPtracePermissions(procWithScope(t, "0")); err != nil {
		t.Errorf("expected no warning at scope 0, got %v", err)
	}
}
