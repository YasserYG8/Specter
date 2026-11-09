package ebpf

import (
	"errors"
	"strings"
	"testing"
)

// capSet builds a capability mask with the given capability numbers set.
func capSet(caps ...uint) uint64 {
	var m uint64
	for _, c := range caps {
		m |= 1 << c
	}
	return m
}

func TestCheckAvailable(t *testing.T) {
	good := environment{goos: "linux", goarch: "amd64", kernelRelease: "6.8.0-60-generic", btfPresent: true, capEff: capSet(capSysAdmin)}

	cases := []struct {
		name       string
		env        func(environment) environment
		wantReason Reason
		wantOK     bool
		notice     string // must appear in the notice
	}{
		{"root on a recent kernel", func(e environment) environment { return e }, 0, true, ""},
		{"CAP_BPF and CAP_PERFMON together", func(e environment) environment { e.capEff = capSet(capBPF, capPerfmon); return e }, 0, true, ""},
		{"CAP_BPF alone is not enough", func(e environment) environment { e.capEff = capSet(capBPF); return e }, ReasonNoPrivileges, false, "Run with sudo to use the fast eBPF engine."},
		{"CAP_PERFMON alone is not enough", func(e environment) environment { e.capEff = capSet(capPerfmon); return e }, ReasonNoPrivileges, false, "Run with sudo"},
		{"a normal user", func(e environment) environment { e.capEff = 0; return e }, ReasonNoPrivileges, false, "Run with sudo to use the fast eBPF engine."},
		{"kernel 5.4", func(e environment) environment { e.kernelRelease = "5.4.0-150-generic"; return e }, ReasonOldKernel, false, "eBPF needs Linux 5.8 or later (this kernel is 5.4)."},
		{"kernel 5.7 is just too old", func(e environment) environment { e.kernelRelease = "5.7.19"; return e }, ReasonOldKernel, false, "this kernel is 5.7"},
		{"kernel 5.8 is enough", func(e environment) environment { e.kernelRelease = "5.8.0"; return e }, 0, true, ""},
		{"kernel 5.15 on WSL2", func(e environment) environment { e.kernelRelease = "5.15.153.1-microsoft-standard-WSL2"; return e }, 0, true, ""},
		{"kernel 4.19", func(e environment) environment { e.kernelRelease = "4.19.0"; return e }, ReasonOldKernel, false, "this kernel is 4.19"},
		{"an unreadable kernel version", func(e environment) environment { e.kernelRelease = ""; return e }, ReasonOldKernel, false, "this kernel is unknown"},
		{"no kernel type information", func(e environment) environment { e.btfPresent = false; return e }, ReasonNoBTF, false, "/sys/kernel/btf/vmlinux"},
		{"arm64 is supported", func(e environment) environment { e.goarch = "arm64"; return e }, 0, true, ""},
		{"riscv64 is not", func(e environment) environment { e.goarch = "riscv64"; return e }, ReasonArch, false, "eBPF supports amd64 and arm64 (this machine is riscv64)."},
		{"not Linux", func(e environment) environment { e.goos = "windows"; return e }, ReasonNotLinux, false, "eBPF needs Linux."},
		{"macOS", func(e environment) environment { e.goos = "darwin"; return e }, ReasonNotLinux, false, "eBPF needs Linux."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkAvailable(tc.env(good))
			if tc.wantOK {
				if err != nil {
					t.Fatalf("expected the source to be available, got %v", err)
				}
				return
			}
			var u *Unavailable
			if !errors.As(err, &u) {
				t.Fatalf("expected an *Unavailable, got %v", err)
			}
			if u.Reason != tc.wantReason {
				t.Errorf("reason = %d, want %d (%v)", u.Reason, tc.wantReason, u)
			}
			if !strings.Contains(u.Notice, tc.notice) {
				t.Errorf("notice %q should contain %q", u.Notice, tc.notice)
			}
			if u.Explanation == "" || u.Remedy == "" {
				t.Errorf("the explanation and the remedy must not be empty: %+v", u)
			}
			if u.Error() != u.Notice {
				t.Error("Error() should be the notice sentence")
			}
		})
	}
}

// The user can fix the system, then the kernel, then the kernel's type information, then privileges. The one
// reported is the first of these that is wrong, so that "run with sudo" is never advice that cannot help.
func TestCheckAvailable_ReportsTheFirstThingToFix(t *testing.T) {
	env := environment{goos: "linux", goarch: "amd64", kernelRelease: "5.4.0", btfPresent: false, capEff: 0}
	var u *Unavailable
	if err := checkAvailable(env); !errors.As(err, &u) || u.Reason != ReasonOldKernel {
		t.Errorf("an old kernel comes first, got %v", err)
	}
	env.kernelRelease = "6.8.0"
	if err := checkAvailable(env); !errors.As(err, &u) || u.Reason != ReasonNoBTF {
		t.Errorf("missing type information comes before privileges, got %v", err)
	}
	env.btfPresent = true
	if err := checkAvailable(env); !errors.As(err, &u) || u.Reason != ReasonNoPrivileges {
		t.Errorf("privileges come last, got %v", err)
	}
}

func TestParseKernel(t *testing.T) {
	cases := []struct {
		in           string
		major, minor int
		ok           bool
	}{
		{"6.8.0-60-generic", 6, 8, true},
		{"5.15.153.1-microsoft-standard-WSL2", 5, 15, true},
		{"5.4.0", 5, 4, true},
		{"6.10-rc1", 6, 10, true},
		{"6.12", 6, 12, true},
		{"4.19.0+", 4, 19, true},
		{"", 0, 0, false},
		{"garbage", 0, 0, false},
		{"6", 0, 0, false},
		{"x.8.0", 0, 0, false},
		{"6.x", 0, 0, false},
	}
	for _, tc := range cases {
		major, minor, ok := parseKernel(tc.in)
		if ok != tc.ok || (ok && (major != tc.major || minor != tc.minor)) {
			t.Errorf("parseKernel(%q) = %d.%d, %v; want %d.%d, %v", tc.in, major, minor, ok, tc.major, tc.minor, tc.ok)
		}
	}
}

func TestParseCapEff(t *testing.T) {
	status := "Name:\tspecter\nUmask:\t0022\nCapInh:\t0000000000000000\nCapPrm:\t000001ffffffffff\nCapEff:\t000001ffffffffff\nCapBnd:\t000001ffffffffff\n"
	v, ok := parseCapEff(status)
	if !ok || v != 0x000001ffffffffff {
		t.Errorf("root: got %#x, ok=%v", v, ok)
	}
	if v&(1<<capBPF) == 0 || v&(1<<capPerfmon) == 0 || v&(1<<capSysAdmin) == 0 {
		t.Error("a full root set has CAP_BPF, CAP_PERFMON and CAP_SYS_ADMIN")
	}
	if v, ok := parseCapEff("CapEff:\t0000000000000000\n"); !ok || v != 0 {
		t.Errorf("a normal user: got %#x, ok=%v", v, ok)
	}
	if _, ok := parseCapEff("Name:\tx\n"); ok {
		t.Error("a status without CapEff should not parse")
	}
	if _, ok := parseCapEff("CapEff:\tnot-hex\n"); ok {
		t.Error("garbage should not parse")
	}
}

func TestLoadFailed_KeepsTheKernelsFirstLine(t *testing.T) {
	err := errors.New("permission denied: operation not permitted\nverifier log line 2\nline 3")
	u := loadFailed(err)
	if u.Reason != ReasonLoadFailed || !errors.Is(u, err) {
		t.Fatalf("got %+v", u)
	}
	if !strings.Contains(u.Notice, "permission denied: operation not permitted") || strings.Contains(u.Notice, "verifier log") {
		t.Errorf("the notice should hold only the first line: %q", u.Notice)
	}
	if strings.Contains(u.Notice, "\n") {
		t.Error("the notice must be one line")
	}
}
