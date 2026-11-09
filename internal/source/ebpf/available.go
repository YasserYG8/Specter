package ebpf

import (
	"fmt"
	"strconv"
	"strings"
)

// The oldest kernel the eBPF source supports. Linux 5.8 is the first with the CAP_BPF and CAP_PERFMON capabilities,
// the ring buffer, and the calls the program uses to find a process in Specter's own PID namespace.
const (
	minKernelMajor = 5
	minKernelMinor = 8
)

// The Linux capabilities that let a process load tracing programs: CAP_BPF and CAP_PERFMON together (kernel 5.8 and
// later), or the old catch-all CAP_SYS_ADMIN.
const (
	capSysAdmin = 21
	capPerfmon  = 38
	capBPF      = 39
)

// Reason says why the eBPF source cannot run.
type Reason int

const (
	ReasonNotLinux Reason = iota
	ReasonOldKernel
	ReasonNoBTF
	ReasonNoPrivileges
	ReasonLoadFailed // the checks passed, but the kernel refused to load or attach the program
)

// Unavailable is the error for an eBPF source that cannot run. It holds the same fact in the words each place needs:
// one sentence for the slowdown notice that Specter prints when it falls back to strace, and an explanation and a
// remedy for the diagnostic of --source ebpf.
type Unavailable struct {
	Reason      Reason
	Notice      string // one sentence that ends the slowdown notice, such as "Run with sudo to use the fast eBPF engine."
	Explanation string // what is missing
	Remedy      string // what to do about it
	Err         error  // the kernel's own message, for ReasonLoadFailed
}

func (u *Unavailable) Error() string { return u.Notice }
func (u *Unavailable) Unwrap() error { return u.Err }

// environment is everything the availability check reads, so tests can give any machine.
type environment struct {
	goos          string
	kernelRelease string // as uname -r prints it, for example "6.8.0-60-generic"
	btfPresent    bool   // /sys/kernel/btf/vmlinux exists
	capEff        uint64 // the effective capability set of this process
}

// checkAvailable decides whether the eBPF source can run in env, in the order of what the user can fix: the system
// first, then the kernel, then the kernel's type information, then privileges. It returns nil, or an *Unavailable.
func checkAvailable(env environment) error {
	if env.goos != "linux" {
		return &Unavailable{
			Reason:      ReasonNotLinux,
			Notice:      "eBPF needs Linux.",
			Explanation: "eBPF needs Linux, and this system is " + env.goos + ".",
			Remedy:      "Run Specter on Linux. On Windows, run it inside WSL2.",
		}
	}

	major, minor, ok := parseKernel(env.kernelRelease)
	if !ok || major < minKernelMajor || (major == minKernelMajor && minor < minKernelMinor) {
		version := "unknown"
		if ok {
			version = fmt.Sprintf("%d.%d", major, minor)
		}
		return &Unavailable{
			Reason:      ReasonOldKernel,
			Notice:      fmt.Sprintf("eBPF needs Linux %d.%d or later (this kernel is %s).", minKernelMajor, minKernelMinor, version),
			Explanation: fmt.Sprintf("eBPF needs Linux %d.%d or later, and this kernel is %s.", minKernelMajor, minKernelMinor, version),
			Remedy:      "Use a newer kernel, or leave out --source ebpf to use strace.",
		}
	}

	if !env.btfPresent {
		return &Unavailable{
			Reason:      ReasonNoBTF,
			Notice:      "eBPF needs kernel type information (/sys/kernel/btf/vmlinux), which this kernel does not provide.",
			Explanation: "This kernel has no type information at /sys/kernel/btf/vmlinux, which eBPF needs to read the kernel's structures.",
			Remedy:      "Use a kernel built with CONFIG_DEBUG_INFO_BTF=y, or leave out --source ebpf to use strace.",
		}
	}

	has := func(c uint) bool { return env.capEff&(1<<c) != 0 }
	if !(has(capSysAdmin) || (has(capBPF) && has(capPerfmon))) {
		return &Unavailable{
			Reason:      ReasonNoPrivileges,
			Notice:      "Run with sudo to use the fast eBPF engine.",
			Explanation: "eBPF needs root, or the capabilities CAP_BPF and CAP_PERFMON, and Specter has neither.",
			Remedy:      "Run Specter with sudo, or give it CAP_BPF and CAP_PERFMON. Without --source ebpf, Specter uses strace.",
		}
	}
	return nil
}

// loadFailed wraps the error of a program that the kernel refused to load or attach, although the checks passed.
// A kernel lockdown policy or a security profile are typical causes.
func loadFailed(err error) *Unavailable {
	return &Unavailable{
		Reason:      ReasonLoadFailed,
		Notice:      "The kernel refused to load the eBPF program (" + oneLine(err) + ").",
		Explanation: "The kernel refused to load or attach the eBPF program: " + oneLine(err),
		Remedy:      "Check for a kernel lockdown policy or a security profile that blocks eBPF, or leave out --source ebpf to use strace.",
		Err:         err,
	}
}

// oneLine shortens an error to its first line, because the kernel's verifier log can be long.
func oneLine(err error) string {
	first, _, _ := strings.Cut(err.Error(), "\n")
	return strings.TrimSpace(first)
}

// parseKernel reads the version out of a release string such as "6.8.0-60-generic" or "5.15.153.1-microsoft-standard-WSL2".
func parseKernel(release string) (major, minor int, ok bool) {
	parts := strings.SplitN(release, ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	// The minor number ends at the first character that is not a digit: "8" in "8.0-60-generic" is "8", and a
	// release like "6.8-rc1" has "8-rc1".
	digits := parts[1]
	for i, r := range digits {
		if r < '0' || r > '9' {
			digits = digits[:i]
			break
		}
	}
	minor, err = strconv.Atoi(digits)
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// parseCapEff reads the effective capability set from the contents of /proc/<pid>/status.
func parseCapEff(status string) (uint64, bool) {
	for line := range strings.SplitSeq(status, "\n") {
		if rest, ok := strings.CutPrefix(line, "CapEff:"); ok {
			v, err := strconv.ParseUint(strings.TrimSpace(rest), 16, 64)
			return v, err == nil
		}
	}
	return 0, false
}
