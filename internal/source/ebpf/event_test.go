package ebpf

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/YasserYG8/Specter/internal/source"
)

// raw builds a record the way the BPF program lays it out.
func raw(tid, tgid, fd, kind, arg, mode, dev uint32, ino, ts uint64, data string) []byte {
	b := make([]byte, headerSize, headerSize+len(data))
	le := binary.LittleEndian
	le.PutUint32(b[0:], tid)
	le.PutUint32(b[4:], tgid)
	le.PutUint32(b[8:], fd)
	le.PutUint32(b[12:], uint32(len(data)))
	le.PutUint32(b[16:], kind)
	le.PutUint32(b[20:], arg)
	le.PutUint32(b[24:], mode)
	le.PutUint32(b[28:], dev)
	le.PutUint64(b[32:], ino)
	le.PutUint64(b[40:], ts)
	return append(b, data...)
}

func TestDecodeEvent(t *testing.T) {
	ev, ok := decodeEvent(raw(4121, 4120, 1, kindData, 0, 0o010600, 0x0e, 12345, 99, "GET / 200\n"))
	if !ok {
		t.Fatal("a valid record was rejected")
	}
	if ev.Tid != 4121 || ev.Tgid != 4120 || ev.FD != 1 || ev.Kind != kindData || ev.Mode != 0o010600 || ev.Dev != 0x0e ||
		ev.Ino != 12345 || ev.TS != 99 || string(ev.Data) != "GET / 200\n" || ev.Len != 10 {
		t.Errorf("decoded %+v", ev)
	}

	// A record without bytes (a marker, a file notice, an exit) has no data
	ev, ok = decodeEvent(raw(1, 1, 2, kindMarker, 34464, 0, 0, 0, 0, ""))
	if !ok || ev.Data != nil || ev.Arg != 34464 || ev.Kind != kindMarker {
		t.Errorf("marker record: %+v, ok=%v", ev, ok)
	}
}

func TestDecodeEvent_RejectsBrokenRecords(t *testing.T) {
	if _, ok := decodeEvent(nil); ok {
		t.Error("an empty record should be rejected")
	}
	if _, ok := decodeEvent(make([]byte, headerSize-1)); ok {
		t.Error("a record shorter than its header should be rejected")
	}
	// The header says 10 bytes follow, but only 4 do
	b := raw(1, 1, 1, kindData, 0, 0, 0, 0, 0, "0123456789")
	if _, ok := decodeEvent(b[:headerSize+4]); ok {
		t.Error("a record that is cut short should be rejected")
	}
}

func TestDecodeEvent_CopiesTheBytes(t *testing.T) {
	b := raw(1, 1, 1, kindData, 0, 0, 0, 0, 0, "abc")
	ev, _ := decodeEvent(b)
	b[headerSize] = 'X' // the ring buffer reuses its memory
	if string(ev.Data) != "abc" {
		t.Errorf("the decoded bytes changed with the buffer: %q", ev.Data)
	}
}

// The text, and its newline, must equal what the strace source appends, so the pipeline sees no difference.
func TestMarkerText(t *testing.T) {
	if got := string(markerText(34464)); got != "[specter: 34464 more bytes not captured]\n" {
		t.Errorf("marker = %q", got)
	}
}

// glibcMakedev is how stat(2) encodes a device number in st_dev.
func glibcMakedev(major, minor uint32) uint64 {
	return uint64(major&0xfff)<<8 | uint64(minor&0xff) | (uint64(minor&^0xff) << 12) | (uint64(major&^0xfff) << 32)
}

func TestKernelDev(t *testing.T) {
	cases := []struct{ major, minor uint32 }{
		{0, 0},
		{0, 25},      // an anonymous device, like the one of a pipe's filesystem
		{8, 1},       // /dev/sda1
		{136, 3},     // /dev/pts/3, the terminal driver
		{259, 70000}, // large numbers use the upper bits of both encodings
		{4095, 255},
	}
	for _, tc := range cases {
		st := glibcMakedev(tc.major, tc.minor)
		want := tc.major<<20 | tc.minor
		if got := kernelDev(st); got != want {
			t.Errorf("major %d minor %d: stat says %#x, kernelDev = %#x, want %#x", tc.major, tc.minor, st, got, want)
		}
	}
	// The two encodings differ, so passing stat's value as it is would never match
	if glibcMakedev(8, 1) == uint64(kernelDev(glibcMakedev(8, 1))) {
		t.Error("the encodings should differ for 8:1")
	}
	if glibcMakedev(8, 1) != 0x801 {
		t.Errorf("makedev(8,1) = %#x, want 0x801", glibcMakedev(8, 1))
	}
}

func TestExitStatusOf(t *testing.T) {
	cases := []struct {
		name string
		raw  uint32
		want source.ExitStatus
	}{
		{"code 0", 0x0000, source.ExitStatus{PID: 617}},
		{"code 3", 0x0300, source.ExitStatus{PID: 617, Code: 3}},
		{"code 255", 0xff00, source.ExitStatus{PID: 617, Code: 255}},
		{"killed by SIGKILL", 9, source.ExitStatus{PID: 617, Signal: "SIGKILL"}},
		{"killed by SIGTERM", 15, source.ExitStatus{PID: 617, Signal: "SIGTERM"}},
		{"SIGSEGV with a core dump", 0x80 | 11, source.ExitStatus{PID: 617, Signal: "SIGSEGV"}},
		{"SIGABRT", 6, source.ExitStatus{PID: 617, Signal: "SIGABRT"}},
		{"a real-time signal", 40, source.ExitStatus{PID: 617, Signal: "SIG40"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitStatusOf(617, tc.raw); got != tc.want {
				t.Errorf("exitStatusOf(%#x) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
	if !exitStatusOf(1, 9).Killed() || exitStatusOf(1, 0x300).Killed() {
		t.Error("Killed() should be true for a signal and false for an exit code")
	}
}

func TestFollowDepths(t *testing.T) {
	// 100 is the target. 101 and 102 are its children. 103 is a child of 101, and 104 a child of 103.
	// 200 is unrelated, and 105 is a child of 200.
	parents := map[int]int{101: 100, 102: 100, 103: 101, 104: 103, 105: 200, 200: 1}
	parentOf := func(pid int) (int, bool) { p, ok := parents[pid]; return p, ok }
	all := []int{101, 102, 103, 104, 105}

	t.Run("direct children only", func(t *testing.T) {
		got := followDepths(100, all, parentOf, maxDepthDirect)
		want := map[int]uint32{100: 0, 101: 1, 102: 1}
		assertDepths(t, got, want)
	})
	t.Run("with --tree, up to 3 levels", func(t *testing.T) {
		got := followDepths(100, all, parentOf, maxDepthTree)
		want := map[int]uint32{100: 0, 101: 1, 102: 1, 103: 2, 104: 3}
		assertDepths(t, got, want)
	})
	t.Run("the target is never its own candidate", func(t *testing.T) {
		got := followDepths(100, []int{100, 0, -5}, parentOf, maxDepthTree)
		assertDepths(t, got, map[int]uint32{100: 0})
	})
	t.Run("an unreadable parent keeps the candidate as a direct child", func(t *testing.T) {
		gone := func(int) (int, bool) { return 0, false }
		got := followDepths(100, []int{101, 102}, gone, maxDepthDirect)
		assertDepths(t, got, map[int]uint32{100: 0, 101: 1, 102: 1})
	})
}

func assertDepths(t *testing.T, got, want map[int]uint32) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("got %v, want %v", got, want)
		return
	}
	for pid, d := range want {
		if g, ok := got[pid]; !ok || g != d {
			t.Errorf("PID %d: got depth %d (present %v), want %d; all: %v", pid, g, ok, d, got)
		}
	}
}

func TestProcParent(t *testing.T) {
	root := t.TempDir()
	write := func(pid int, content string) {
		dir := filepath.Join(root, strconv.Itoa(pid))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(10, "10 (node) S 7 10 10 0 -1 4194560 100 0 0 0 1 1 0 0 20 0 1 0 100 1000 100 18446744073709551615\n")
	// The name can hold spaces and parentheses, which must not shift the fields
	write(11, "11 (my (weird) app) R 42 11 11 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 100 1000 100 0\n")
	write(12, "garbage")

	if p, ok := procParent(root, 10); !ok || p != 7 {
		t.Errorf("PID 10: parent %d, ok=%v, want 7", p, ok)
	}
	if p, ok := procParent(root, 11); !ok || p != 42 {
		t.Errorf("PID 11: parent %d, ok=%v, want 42", p, ok)
	}
	for _, pid := range []int{12, 13} {
		if _, ok := procParent(root, pid); ok {
			t.Errorf("PID %d should have no readable parent", pid)
		}
	}
}

func TestLostWritesNotice(t *testing.T) {
	if got := lostWritesNotice(120); got != "120 writes were lost because the target wrote faster than Specter could read." {
		t.Errorf("notice = %q", got)
	}
	if got := lostWritesNotice(1); !strings.HasPrefix(got, "1 write was lost") {
		t.Errorf("singular notice = %q", got)
	}
}

func TestSourceBasics(t *testing.T) {
	s := NewSource(Options{})
	if s.Name() != "ebpf" {
		t.Errorf("Name() = %q", s.Name())
	}
	if err := s.Stop(); err != nil {
		t.Errorf("Stop before Start should be harmless: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Errorf("Stop twice should be harmless: %v", err)
	}
	if _, known := s.ExitStatus(); known {
		t.Error("a source that has not started has no exit status")
	}

	var got []string
	s.SetNotifier(func(m string) { got = append(got, m) })
	s.notify("hello")
	if len(got) != 1 || got[0] != "hello" {
		t.Errorf("notifier received %v", got)
	}
}

func TestLostTracker(t *testing.T) {
	var l lostTracker

	if _, ok := l.next(0); ok {
		t.Error("nothing was lost, so nothing should be said")
	}
	msg, ok := l.next(120)
	if !ok || msg != "120 writes were lost because the target wrote faster than Specter could read." {
		t.Errorf("first notice = %q, %v", msg, ok)
	}
	if _, ok := l.next(120); ok {
		t.Error("no new loss, so no new notice")
	}
	// The total only grows. The next notice counts what is new since the last one
	msg, ok = l.next(125)
	if !ok || !strings.HasPrefix(msg, "5 writes were lost") {
		t.Errorf("second notice = %q, %v", msg, ok)
	}
	msg, ok = l.next(126)
	if !ok || !strings.HasPrefix(msg, "1 write was lost") {
		t.Errorf("third notice = %q, %v", msg, ok)
	}
}

func TestRingBytesFrom(t *testing.T) {
	cases := map[string]uint32{
		"":               0,
		"4096":           4096,
		" 65536 ":        65536,
		"8388608":        8388608,
		"4095":           0, // smaller than a page
		"4097":           0, // not a power of two
		"2048":           0,
		"0":              0,
		"-4096":          0,
		"banana":         0,
		"99999999999999": 0, // does not fit in 32 bits
	}
	for in, want := range cases {
		if got := ringBytesFrom(in); got != want {
			t.Errorf("ringBytesFrom(%q) = %d, want %d", in, got, want)
		}
	}
}
