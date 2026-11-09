package ebpf

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/YasserYG8/Specter/internal/source"
)

// This file has no operating system dependencies, so its tests run everywhere. It turns the records of the ring
// buffer into the values the rest of Specter understands.

// The kinds of records the BPF program sends. They match EV_* in bpf/specter.c.
const (
	kindData   = 0 // bytes the target wrote
	kindFile   = 1 // a descriptor other than 1 and 2 leads to a regular file; no bytes
	kindMarker = 2 // arg bytes of a call were not captured
	kindExit   = 3 // the target ended; arg is the kernel's exit code of its leader
)

// headerSize is the size of struct event in bpf/specter.c. A test on Linux checks it against the generated type.
const headerSize = 48

const (
	// maxCapture is how many bytes of one write are captured, like strace -s 65536. It matches MAX_CAPTURE in the
	// BPF program.
	maxCapture = 65536

	// maxDepth is how far below the target children are followed. Direct children always, deeper ones with --tree.
	// The strace source uses the same limit.
	maxDepthDirect = 1
	maxDepthTree   = 3
)

// event is one record of the ring buffer.
type event struct {
	Tid, Tgid, FD, Len, Kind, Arg, Mode, Dev uint32
	Ino                                      uint64
	TS                                       uint64 // CLOCK_MONOTONIC, in nanoseconds
	Data                                     []byte // the Len bytes after the header; nil when Len is 0
}

// decodeEvent reads a record. It reports false when the record is shorter than its own header says.
func decodeEvent(raw []byte) (event, bool) {
	if len(raw) < headerSize {
		return event{}, false
	}
	le := binary.LittleEndian
	ev := event{
		Tid:  le.Uint32(raw[0:]),
		Tgid: le.Uint32(raw[4:]),
		FD:   le.Uint32(raw[8:]),
		Len:  le.Uint32(raw[12:]),
		Kind: le.Uint32(raw[16:]),
		Arg:  le.Uint32(raw[20:]),
		Mode: le.Uint32(raw[24:]),
		Dev:  le.Uint32(raw[28:]),
		Ino:  le.Uint64(raw[32:]),
		TS:   le.Uint64(raw[40:]),
	}
	if int(ev.Len) > len(raw)-headerSize {
		return event{}, false
	}
	if ev.Len > 0 {
		ev.Data = append([]byte(nil), raw[headerSize:headerSize+int(ev.Len)]...)
	}
	return ev, true
}

// markerText is what follows a write that was cut at the capture limit. It is the same text, and the same newline,
// as the strace source adds, so the line assembler and the renderer treat both engines alike.
func markerText(left uint32) []byte {
	return []byte(fmt.Sprintf("[specter: %d more bytes not captured]\n", left))
}

// kernelDev converts a device number from stat(2) (the encoding glibc uses for st_dev) to the encoding the kernel
// uses inside, MAJOR<<20 | MINOR, which is what the BPF program reads from a superblock.
func kernelDev(dev uint64) uint32 {
	major := uint32((dev>>8)&0xfff) | uint32((dev>>32)&^uint64(0xfff))
	minor := uint32(dev&0xff) | uint32((dev>>12)&^uint64(0xff))
	return major<<20 | minor
}

// signalNames are the Linux signal numbers that strace prints by name. The numbers are the same on amd64 and arm64.
var signalNames = map[uint32]string{
	1: "SIGHUP", 2: "SIGINT", 3: "SIGQUIT", 4: "SIGILL", 5: "SIGTRAP", 6: "SIGABRT", 7: "SIGBUS", 8: "SIGFPE",
	9: "SIGKILL", 10: "SIGUSR1", 11: "SIGSEGV", 12: "SIGUSR2", 13: "SIGPIPE", 14: "SIGALRM", 15: "SIGTERM",
	16: "SIGSTKFLT", 17: "SIGCHLD", 18: "SIGCONT", 19: "SIGSTOP", 20: "SIGTSTP", 21: "SIGTTIN", 22: "SIGTTOU",
	23: "SIGURG", 24: "SIGXCPU", 25: "SIGXFSZ", 26: "SIGVTALRM", 27: "SIGPROF", 28: "SIGWINCH", 29: "SIGIO",
	30: "SIGPWR", 31: "SIGSYS",
}

// exitStatusOf decodes the exit_code the kernel keeps for a process. The low 7 bits are the signal that killed it
// (bit 7 says it dumped core), and otherwise bits 8 to 15 are the exit code.
func exitStatusOf(pid int, exitCode uint32) source.ExitStatus {
	if sig := exitCode & 0x7f; sig != 0 {
		name, ok := signalNames[sig]
		if !ok {
			name = "SIG" + strconv.Itoa(int(sig))
		}
		return source.ExitStatus{PID: pid, Signal: name}
	}
	return source.ExitStatus{PID: pid, Code: int((exitCode >> 8) & 0xff)}
}

// followDepths gives the depth below the target of each process that is followed from the start: the target is 0,
// and a candidate is as deep as its chain of parents is long. parentOf reads the parent of a PID. A candidate
// whose chain does not reach the target within maxDepth levels is dropped, except that a candidate whose own
// parent cannot be read is kept as a direct child, because the caller found it among the target's children and
// losing a worker is worse than following one too many.
func followDepths(target int, candidates []int, parentOf func(pid int) (int, bool), maxDepth int) map[int]uint32 {
	depths := map[int]uint32{target: 0}
	for _, pid := range candidates {
		if pid <= 0 || pid == target {
			continue
		}
		cur := pid
		found := false
		unreadable := false
		for d := 1; d <= maxDepth; d++ {
			parent, ok := parentOf(cur)
			if !ok {
				// Only the candidate's own parent counts. If the chain breaks higher up, it left the target's tree.
				unreadable = d == 1
				break
			}
			if parent == target {
				depths[pid] = uint32(d)
				found = true
				break
			}
			cur = parent
		}
		if !found && unreadable {
			depths[pid] = maxDepthDirect
		}
	}
	return depths
}

// procParent reads the parent PID of a process from its stat file under procRoot. The command name in that file
// may contain spaces and parentheses, so the fields are counted from the last closing parenthesis.
func procParent(procRoot string, pid int) (int, bool) {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, false
	}
	fields := strings.Fields(s[i+1:])
	// After ")": state, ppid, ...
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false
	}
	return ppid, true
}

// lostWritesNotice is the message for writes the BPF program could not send because the ring buffer was full.
func lostWritesNotice(n uint64) string {
	if n == 1 {
		return "1 write was lost because the target wrote faster than Specter could read."
	}
	return fmt.Sprintf("%d writes were lost because the target wrote faster than Specter could read.", n)
}

// lostTracker turns the running total of lost writes that the BPF program keeps into notices. It is polled once per
// second, so a notice appears at most once per second. Each notice counts the writes lost since the one before, and
// nothing is said while nothing is lost.
type lostTracker struct {
	reported uint64
}

// next takes the current total and returns the notice to print, if any.
func (l *lostTracker) next(total uint64) (string, bool) {
	if total <= l.reported {
		return "", false
	}
	msg := lostWritesNotice(total - l.reported)
	l.reported = total
	return msg, true
}
