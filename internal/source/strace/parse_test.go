package strace

import (
	"strings"
	"testing"
	"time"

	"github.com/YasserYG8/Specter/internal/source"
)

func TestParser_StandardStdoutAndStderr(t *testing.T) {
	p := NewParser(1000)
	now := time.Now()

	// 1. Stdout write
	chunk, err := p.ParseLine(`write(1, "\x48\x65\x6c\x6c\x6f\x0a", 6) = 6`, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk == nil {
		t.Fatal("expected chunk, got nil")
	}
	if chunk.FD != 1 {
		t.Errorf("expected FD 1, got %d", chunk.FD)
	}
	if string(chunk.Bytes) != "Hello\n" {
		t.Errorf("expected 'Hello\\n', got %q", string(chunk.Bytes))
	}
	if chunk.PID != 1000 {
		t.Errorf("expected PID 1000, got %d", chunk.PID)
	}

	// 2. Stderr write with explicit [pid N] prefix
	chunk, err = p.ParseLine(`[pid 2048] write(2, "\x45\x72\x72\x6f\x72", 5) = 5`, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk == nil {
		t.Fatal("expected chunk, got nil")
	}
	if chunk.FD != 2 {
		t.Errorf("expected FD 2, got %d", chunk.FD)
	}
	if string(chunk.Bytes) != "Error" {
		t.Errorf("expected 'Error', got %q", string(chunk.Bytes))
	}
	if chunk.PID != 2048 {
		t.Errorf("expected PID 2048, got %d", chunk.PID)
	}

	// 3. Ignored fd (fd 3)
	chunk, err = p.ParseLine(`write(3, "\x01\x02", 2) = 2`, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk != nil {
		t.Errorf("expected nil chunk for fd 3, got: %+v", chunk)
	}

	// 4. Failed write (-1 EPIPE)
	chunk, err = p.ParseLine(`write(1, "\x61", 1) = -1 EPIPE`, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk != nil {
		t.Errorf("expected nil chunk for failed write, got: %+v", chunk)
	}
}

// The fixtures in this file were copied from strace 6.8 output (Ubuntu, WSL2). strace prints the whole
// argument list, payload and length included, on the <unfinished ...> line; the resumed line only
// carries the return value, padded with spaces before "=".

func TestParser_MultiThreadUnfinishedAndResumed(t *testing.T) {
	p := NewParser(100)
	now := time.Now()

	mustNil := func(line string) {
		t.Helper()
		c, err := p.ParseLine(line, now)
		if err != nil || c != nil {
			t.Fatalf("expected no chunk for %q, got c=%v err=%v", line, c, err)
		}
	}
	mustChunk := func(line string) *source.Chunk {
		t.Helper()
		c, err := p.ParseLine(line, now)
		if err != nil || c == nil {
			t.Fatalf("expected chunk for %q, got c=%v err=%v", line, c, err)
		}
		return c
	}

	mustNil(`[pid 33143] write(1, "\x77\x30\x2d\x34\x0a", 5 <unfinished ...>`)
	mustNil(`[pid 33144] write(1, "\x77\x31\x2d\x30\x0a", 5 <unfinished ...>`)

	// A complete write from a third thread in between
	c := mustChunk(`[pid 33145] write(1, "\x57\x6f\x72\x6c\x64\x0a", 6) = 6`)
	if string(c.Bytes) != "World\n" || c.PID != 33145 {
		t.Errorf("unexpected chunk for pid 33145: %q (PID %d)", c.Bytes, c.PID)
	}

	c = mustChunk(`[pid 33143] <... write resumed>)        = 5`)
	if string(c.Bytes) != "w0-4\n" || c.PID != 33143 || c.FD != 1 {
		t.Errorf("unexpected chunk for pid 33143: %q (PID %d, FD %d)", c.Bytes, c.PID, c.FD)
	}
	c = mustChunk(`[pid 33144] <... write resumed>)        = 5`)
	if string(c.Bytes) != "w1-0\n" || c.PID != 33144 {
		t.Errorf("unexpected chunk for pid 33144: %q (PID %d)", c.Bytes, c.PID)
	}
}

func TestParser_MultibyteUTF8(t *testing.T) {
	p := NewParser(1)
	now := time.Now()

	// Ghost emoji 👻 is \xf0\x9f\x91\xbb
	chunk, err := p.ParseLine(`write(1, "\xf0\x9f\x91\xbb\x0a", 5) = 5`, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk == nil {
		t.Fatal("expected chunk, got nil")
	}
	if string(chunk.Bytes) != "👻\n" {
		t.Errorf("expected '👻\\n', got %q", string(chunk.Bytes))
	}
}

func TestDecodeStraceHex_Truncation(t *testing.T) {
	// String truncated by strace ending with ...
	bytes, err := DecodeStraceHex(`\x41\x42...`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(bytes) != "AB" {
		t.Errorf("expected 'AB', got %q", string(bytes))
	}
}

func TestParser_TruncatedOutput(t *testing.T) {
	p := NewParser(100)
	now := time.Now()

	// 1. Truncated complete write with ... outside quotes
	chunk, err := p.ParseLine(`write(1, "\x48\x65\x6c\x6c\x6f"... , 50) = 5`, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk == nil {
		t.Fatal("expected chunk for truncated write, got nil")
	}
	if string(chunk.Bytes) != "Hello" {
		t.Errorf("expected 'Hello', got %q", string(chunk.Bytes))
	}

	// 2. Cut payload on an unfinished line: the length argument follows the ellipsis
	c1, err := p.ParseLine(`[pid 101] write(1, "\x48\x65\x79\x21"..., 10 <unfinished ...>`, now)
	if err != nil || c1 != nil {
		t.Fatalf("expected nil chunk for cut unfinished write, got c1=%v, err=%v", c1, err)
	}

	// 3. Its resumed line reports all 10 bytes written, 6 more than were captured
	c2, err := p.ParseLine(`[pid 101] <... write resumed>)          = 10`, now)
	if err != nil || c2 == nil {
		t.Fatalf("expected chunk for cut resumed write, got c2=%v, err=%v", c2, err)
	}
	if string(c2.Bytes) != "Hey![specter: 6 more bytes not captured]\n" {
		t.Errorf("unexpected bytes for cut resumed write: %q", c2.Bytes)
	}
}

func TestParser_ResumedEdgeCases(t *testing.T) {
	now := time.Now()

	t.Run("failed resumed write emits nothing", func(t *testing.T) {
		p := NewParser(1)
		if c, _ := p.ParseLine(`[pid 7] write(1, "\x61\x0a", 2 <unfinished ...>`, now); c != nil {
			t.Fatalf("unexpected chunk: %+v", c)
		}
		c, err := p.ParseLine(`[pid 7] <... write resumed>)            = -1 EPIPE (Broken pipe)`, now)
		if err != nil || c != nil {
			t.Fatalf("expected no chunk for failed write, got c=%v err=%v", c, err)
		}
	})

	t.Run("fd 3 and higher emits nothing and leaves later calls alone", func(t *testing.T) {
		p := NewParser(1)
		var seen [][2]int
		p.OnOtherFD = func(pid, fd int) { seen = append(seen, [2]int{pid, fd}) }

		if c, _ := p.ParseLine(`[pid 7] write(3, "\x61\x0a", 2 <unfinished ...>`, now); c != nil {
			t.Fatalf("unexpected chunk: %+v", c)
		}
		if c, _ := p.ParseLine(`[pid 7] <... write resumed>)            = 2`, now); c != nil {
			t.Fatalf("unexpected chunk for resumed fd 3 write: %+v", c)
		}
		if c, _ := p.ParseLine(`write(9, "\x61\x0a", 2) = 2`, now); c != nil {
			t.Fatalf("unexpected chunk for fd 9: %+v", c)
		}
		if len(seen) != 2 || seen[0] != [2]int{7, 3} || seen[1] != [2]int{1, 9} {
			t.Errorf("expected OnOtherFD for (7,3) and (1,9), got %v", seen)
		}
	})

	t.Run("resumed line with no pending call", func(t *testing.T) {
		p := NewParser(1)
		c, err := p.ParseLine(`[pid 7] <... write resumed>)            = 4`, now)
		if err != nil || c != nil {
			t.Fatalf("expected no chunk, got c=%v err=%v", c, err)
		}
	})

	t.Run("stderr write keeps its fd", func(t *testing.T) {
		p := NewParser(1)
		_, _ = p.ParseLine(`[pid 7] write(2, "\x65\x72\x72\x0a", 4 <unfinished ...>`, now)
		c, _ := p.ParseLine(`[pid 7] <... write resumed>)            = 4`, now)
		if c == nil || c.FD != 2 || string(c.Bytes) != "err\n" {
			t.Fatalf("unexpected chunk: %+v", c)
		}
	})
}

func TestParser_Writev(t *testing.T) {
	now := time.Now()

	t.Run("complete", func(t *testing.T) {
		p := NewParser(50)
		c, err := p.ParseLine(`writev(1, [{iov_base="\x68\x69", iov_len=2}, {iov_base="\x0a", iov_len=1}], 2) = 3`, now)
		if err != nil || c == nil {
			t.Fatalf("expected chunk, got c=%v err=%v", c, err)
		}
		if c.FD != 1 || c.PID != 50 || string(c.Bytes) != "hi\n" {
			t.Errorf("unexpected chunk: fd=%d pid=%d bytes=%q", c.FD, c.PID, c.Bytes)
		}
	})

	t.Run("complete to stderr with three elements", func(t *testing.T) {
		p := NewParser(50)
		c, _ := p.ParseLine(`writev(2, [{iov_base="\x65\x72\x72", iov_len=3}, {iov_base="\x20", iov_len=1}, {iov_base="\x78\x0a", iov_len=2}], 3) = 6`, now)
		if c == nil || c.FD != 2 || string(c.Bytes) != "err x\n" {
			t.Fatalf("unexpected chunk: %+v", c)
		}
	})

	t.Run("overlapping", func(t *testing.T) {
		p := NewParser(50)
		c, _ := p.ParseLine(`[pid 33171] writev(1, [{iov_base="\x76\x30\x2d", iov_len=3}, {iov_base="\x32\x0a", iov_len=2}], 2 <unfinished ...>`, now)
		if c != nil {
			t.Fatalf("unexpected chunk: %+v", c)
		}
		c, _ = p.ParseLine(`[pid 33171] <... writev resumed>)       = 5`, now)
		if c == nil || c.PID != 33171 || c.FD != 1 || string(c.Bytes) != "v0-2\n" {
			t.Fatalf("unexpected chunk: %+v", c)
		}
	})

	t.Run("to an ignored descriptor", func(t *testing.T) {
		p := NewParser(50)
		var got int
		p.OnOtherFD = func(pid, fd int) { got = fd }
		c, _ := p.ParseLine(`writev(4, [{iov_base="\x68\x69", iov_len=2}], 1) = 2`, now)
		if c != nil {
			t.Fatalf("unexpected chunk: %+v", c)
		}
		if got != 4 {
			t.Errorf("expected OnOtherFD for fd 4, got %d", got)
		}
	})

	t.Run("limited to the return value", func(t *testing.T) {
		p := NewParser(50)
		c, _ := p.ParseLine(`writev(1, [{iov_base="\x68\x69", iov_len=2}, {iov_base="\x0a\x0a", iov_len=2}], 2) = 3`, now)
		if c == nil || string(c.Bytes) != "hi\n" {
			t.Fatalf("unexpected chunk: %+v", c)
		}
	})

	t.Run("cut element drops the elements after it", func(t *testing.T) {
		p := NewParser(50)
		c, _ := p.ParseLine(`writev(1, [{iov_base="\x61\x62"..., iov_len=10}, {iov_base="\x63", iov_len=1}], 2) = 11`, now)
		if c == nil || string(c.Bytes) != "ab[specter: 9 more bytes not captured]\n" {
			t.Fatalf("unexpected chunk: %+v", c)
		}
	})
}

func TestParser_CutWrites(t *testing.T) {
	now := time.Now()

	t.Run("complete write cut by -s", func(t *testing.T) {
		p := NewParser(1)
		c, _ := p.ParseLine(`write(1, "\x41\x41\x41"..., 8) = 8`, now)
		if c == nil || string(c.Bytes) != "AAA[specter: 5 more bytes not captured]\n" {
			t.Fatalf("unexpected chunk: %+v", c)
		}
	})

	t.Run("write inside the limit has no marker", func(t *testing.T) {
		p := NewParser(1)
		c, _ := p.ParseLine(`write(1, "\x41\x41\x41\x0a", 4) = 4`, now)
		if c == nil || string(c.Bytes) != "AAA\n" {
			t.Fatalf("unexpected chunk: %+v", c)
		}
	})

	t.Run("partial write whose return value is smaller than its length", func(t *testing.T) {
		p := NewParser(1)
		c, _ := p.ParseLine(`write(1, "\x41\x42\x43\x44", 4) = 2`, now)
		if c == nil || string(c.Bytes) != "AB" {
			t.Fatalf("unexpected chunk: %+v", c)
		}
	})

	t.Run("a line after a cut write stays separate", func(t *testing.T) {
		p := NewParser(1)
		c1, _ := p.ParseLine(`write(1, "\x41"..., 3) = 3`, now)
		c2, _ := p.ParseLine(`write(1, "\x42\x0a", 2) = 2`, now)
		if c1 == nil || c2 == nil {
			t.Fatal("expected two chunks")
		}
		if got := string(c1.Bytes) + string(c2.Bytes); got != "A[specter: 2 more bytes not captured]\nB\n" {
			t.Errorf("unexpected stream: %q", got)
		}
	})

	t.Run("decoded real-sized write", func(t *testing.T) {
		p := NewParser(1)
		payload := strings.Repeat(`\x41`, 65536)
		c, _ := p.ParseLine(`write(1, "`+payload+`"..., 100002) = 100002`, now)
		want := strings.Repeat("A", 65536) + "[specter: 34466 more bytes not captured]\n"
		if c == nil || string(c.Bytes) != want {
			t.Fatalf("unexpected chunk of %d bytes", len(c.Bytes))
		}
	})
}

// The lines below are real output of `strace -f -q -xx -y -s 65536 -e trace=write,writev` (strace 6.8). Under
// -xx the destination that -y prints is hex-encoded as well.
func TestParser_DescriptorDestination(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name     string
		line     string
		wantData string
		wantDest string
	}{
		{
			name:     "write to a pipe",
			line:     `[pid 535] write(1<\x70\x69\x70\x65\x3a\x5b\x31\x33\x38\x30\x36\x5d>, "\x70\x69\x70\x65\x20\x77\x72\x69\x74\x65\x0a", 11) = 11`,
			wantData: "pipe write\n",
			wantDest: "pipe:[13806]",
		},
		{
			name:     "writev to a pipe",
			line:     `[pid 535] writev(1<\x70\x69\x70\x65\x3a\x5b\x31\x33\x38\x30\x36\x5d>, [{iov_base="\x70\x69\x70\x65\x20\x77\x72\x69\x74\x65\x76\x2d\x61\x20", iov_len=14}, {iov_base="\x77\x72\x69\x74\x65\x76\x2d\x62\x0a", iov_len=9}], 2) = 23`,
			wantData: "pipe writev-a writev-b\n",
			wantDest: "pipe:[13806]",
		},
		{
			name:     "write to /dev/null",
			line:     `[pid 568] write(1<\x2f\x64\x65\x76\x2f\x6e\x75\x6c\x6c>, "\x6e\x75\x6c\x6c\x20\x77\x72\x69\x74\x65\x0a", 11) = 11`,
			wantData: "null write\n",
			wantDest: "/dev/null",
		},
		{
			name:     "write to a terminal",
			line:     `[pid 591] write(1<\x2f\x64\x65\x76\x2f\x70\x74\x73\x2f\x32>, "\x70\x74\x79\x20\x77\x72\x69\x74\x65\x0a", 10) = 10`,
			wantData: "pty write\n",
			wantDest: "/dev/pts/2",
		},
		{
			name:     "write to a file",
			line:     `[pid 580] write(1<\x2f\x74\x6d\x70\x2f\x66\x78\x2d\x6f\x75\x74\x2e\x74\x78\x74>, "\x66\x69\x6c\x65\x20\x77\x72\x69\x74\x65\x0a", 11) = 11`,
			wantData: "file write\n",
			wantDest: "/tmp/fx-out.txt",
		},
		{
			name:     "annotation in plain text",
			line:     `[pid 7] write(2</dev/pts/3>, "\x65\x72\x72", 3) = 3`,
			wantData: "err",
			wantDest: "/dev/pts/3",
		},
		{
			name:     "no annotation",
			line:     `[pid 7] write(1, "\x68\x69\x0a", 3) = 3`,
			wantData: "hi\n",
			wantDest: "",
		},
		{
			name:     "single-process line without a pid prefix",
			line:     `write(1<\x2f\x64\x65\x76\x2f\x6e\x75\x6c\x6c>, "\x68\x69\x0a", 3) = 3`,
			wantData: "hi\n",
			wantDest: "/dev/null",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewParser(1)
			c, dest, err := p.ParseLineDest(tc.line, now)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c == nil || string(c.Bytes) != tc.wantData {
				t.Fatalf("expected data %q, got %+v", tc.wantData, c)
			}
			if dest != tc.wantDest {
				t.Errorf("expected destination %q, got %q", tc.wantDest, dest)
			}
		})
	}

	t.Run("split call keeps the destination until it resumes", func(t *testing.T) {
		p := NewParser(1)
		c, dest, err := p.ParseLineDest(`[pid 594] write(1<\x2f\x64\x65\x76\x2f\x70\x74\x73\x2f\x32>, "\x74\x68\x72\x65\x61\x64\x20\x32\x20\x6c\x69\x6e\x65\x20\x30\x0a", 16 <unfinished ...>`, now)
		if err != nil || c != nil || dest != "" {
			t.Fatalf("expected nothing for the unfinished entry, got %+v %q %v", c, dest, err)
		}
		// Another thread finishes a call in between; it must not take over this one's destination
		c2, dest2, _ := p.ParseLineDest(`[pid 593] write(1<\x70\x69\x70\x65\x3a\x5b\x31\x5d>, "\x78\x0a", 2) = 2`, now)
		if c2 == nil || dest2 != "pipe:[1]" {
			t.Fatalf("expected the other thread's own destination, got %+v %q", c2, dest2)
		}
		c, dest, err = p.ParseLineDest(`[pid 594] <... write resumed>)              = 16`, now)
		if err != nil || c == nil || string(c.Bytes) != "thread 2 line 0\n" {
			t.Fatalf("expected the resumed write, got %+v %v", c, err)
		}
		if dest != "/dev/pts/2" {
			t.Errorf("expected the destination of the unfinished entry, got %q", dest)
		}
	})

	t.Run("destination that cannot be decoded is unknown", func(t *testing.T) {
		p := NewParser(1)
		c, dest, err := p.ParseLineDest(`[pid 7] write(1<\xZZ>, "\x68\x69\x0a", 3) = 3`, now)
		if err != nil || c == nil || string(c.Bytes) != "hi\n" {
			t.Fatalf("expected the write to still parse, got %+v %v", c, err)
		}
		if dest != "" {
			t.Errorf("expected an unknown destination, got %q", dest)
		}
	})

	t.Run("write to another descriptor with an annotation is still reported", func(t *testing.T) {
		p := NewParser(1)
		var gotPID, gotFD int
		p.OnOtherFD = func(pid, fd int) { gotPID, gotFD = pid, fd }
		c, _, _ := p.ParseLineDest(`[pid 9] write(3</var/log/app.log>, "\x68\x69", 2) = 2`, now)
		if c != nil || gotPID != 9 || gotFD != 3 {
			t.Errorf("expected no chunk and a report for pid 9 fd 3, got %+v pid=%d fd=%d", c, gotPID, gotFD)
		}
	})
}

// The lines below are real output of `strace -f -q -xx -y` on strace 6.8, read from stderr. With -qq the
// "exited with" lines are not printed at all, but the "killed by" lines are.
func TestParser_ParseExit(t *testing.T) {
	cases := []struct {
		name string
		line string
		want ExitStatus
		ok   bool
	}{
		{"exit code, target without a prefix", `+++ exited with 3 +++`, ExitStatus{PID: 617, Code: 3, Unprefixed: true}, true},
		{"exit code 0", `+++ exited with 0 +++`, ExitStatus{PID: 617, Code: 0, Unprefixed: true}, true},
		{"exit code with a pid prefix", `[pid   575] +++ exited with 0 +++`, ExitStatus{PID: 575, Code: 0}, true},
		{"killed by SIGKILL", `+++ killed by SIGKILL +++`, ExitStatus{PID: 617, Signal: "SIGKILL", Unprefixed: true}, true},
		{"killed by SIGTERM with a prefix", `[pid 595] +++ killed by SIGTERM +++`, ExitStatus{PID: 595, Signal: "SIGTERM"}, true},
		{"killed with a core dump", `+++ killed by SIGSEGV (core dumped) +++`, ExitStatus{PID: 617, Signal: "SIGSEGV", Unprefixed: true}, true},
		{"surrounding whitespace", "  +++ exited with 7 +++  ", ExitStatus{PID: 617, Code: 7, Unprefixed: true}, true},
		{"a write", `[pid 5] write(1, "\x68\x69", 2) = 2`, ExitStatus{}, false},
		{"a signal line", `--- SIGCHLD {si_signo=SIGCHLD, si_code=CLD_EXITED} ---`, ExitStatus{}, false},
		{"text that mentions exiting", `exited with 3`, ExitStatus{}, false},
		{"empty", ``, ExitStatus{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := NewParser(617).ParseExit(tc.line)
			if ok != tc.ok || got != tc.want {
				t.Errorf("ParseExit(%q) = %+v, %v; want %+v, %v", tc.line, got, ok, tc.want, tc.ok)
			}
		})
	}

	if st, _ := NewParser(1).ParseExit(`+++ killed by SIGKILL +++`); !st.Killed() {
		t.Error("expected Killed() for a signal")
	}
	if st, _ := NewParser(1).ParseExit(`+++ exited with 9 +++`); st.Killed() {
		t.Error("an exit code is not a kill")
	}
}
