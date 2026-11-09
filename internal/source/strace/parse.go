package strace

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/YasserYG8/Specter/internal/source"
)

// Regex patterns for matching strace output. The formats were verified against strace 6.8.
var (
	// [pid 1234] or [pid  1234] prefix
	pidPrefixRegex = regexp.MustCompile(`^\[pid\s+(\d+)\]\s+`)

	// Start of a write or writev call: write(1,  or writev(2,  With -y strace adds the descriptor's destination
	// in angle brackets: write(1<pipe:[4021]>,  Under -xx that text is hex-encoded too.
	callStartRegex = regexp.MustCompile(`^(writev?)\((\d+)(?:<([^>]*)>)?,\s*`)

	// Arguments of write after the fd. Complete: "\x68\x65", 2) = 2. Cut by -s: "\x68\x65"..., 100) = 100.
	// Overlapping with another call: "\x68\x65", 2 <unfinished ...> (the return value is empty).
	writeArgsRegex = regexp.MustCompile(`^"([^"]*)"(?:\s*\.\.\.)?\s*,\s*(\d+)(?:\)\s*=\s*(-?\d+)|\s*<unfinished \.\.\.>)`)

	// Arguments of writev after the fd. [{iov_base="\x68", iov_len=1}, ...], 2) = 3 or ..., 2 <unfinished ...>
	writevArgsRegex = regexp.MustCompile(`^\[(.*?)\](?:\s*\.\.\.)?\s*,\s*(\d+)(?:\)\s*=\s*(-?\d+)|\s*<unfinished \.\.\.>)`)

	// One element of a writev vector. The ellipsis after the quote means strace cut the element.
	iovBaseRegex = regexp.MustCompile(`iov_base="([^"]*)"(\.\.\.)?`)

	// <... write resumed>)        = 5. strace pads the gap before "=" and does not repeat the payload.
	resumedRegex = regexp.MustCompile(`^<\.\.\.\s*(writev?) resumed>\s*\)\s*=\s*(-?\d+)`)
)

// exitRegex matches the line strace prints when a traced process ends, as seen with -q and -f on strace 6.8:
//
//	+++ exited with 3 +++
//	[pid   575] +++ exited with 0 +++
//	+++ killed by SIGKILL +++
//	+++ killed by SIGSEGV (core dumped) +++
var exitRegex = regexp.MustCompile(`^\+\+\+ (?:exited with (\d+)|killed by (\S+)(?: \(core dumped\))?) \+\+\+$`)

type pendingCall struct {
	fd   int
	data []byte
	dest string
}

// Parser parses strace write() and writev() output into source.Chunk instances.
type Parser struct {
	defaultPID int
	pending    map[int]*pendingCall

	// OnOtherFD, when set, is called for every write or writev to a descriptor other than 1 and 2 that is not
	// output (see IsOutputDest).
	OnOtherFD func(pid, fd int)

	// IsOutputDest, when set, is asked about the destination of a write to a descriptor other than 1 and 2. When
	// it returns true the write is parsed like one to fd 1 or 2 and keeps its real descriptor number. Node.js
	// does this for a terminal: libuv opens the terminal again and writes through the new descriptor, such as
	// fd 22. A write without a destination is never asked about and is never output.
	IsOutputDest func(dest string) bool
}

// NewParser creates an strace parser.
func NewParser(defaultPID int) *Parser {
	return &Parser{
		defaultPID: defaultPID,
		pending:    make(map[int]*pendingCall),
	}
}

// ParseLine parses a single strace line and returns a Chunk if a complete stdout/stderr write is found.
func (p *Parser) ParseLine(line string, now time.Time) (*source.Chunk, error) {
	chunk, _, err := p.ParseLineDest(line, now)
	return chunk, err
}

// ParseLineDest is ParseLine that also returns where the write went, as strace printed it with -y, for example
// "pipe:[4021]" or "/dev/pts/3". The destination is empty when the line has no annotation or it cannot be decoded.
func (p *Parser) ParseLineDest(line string, now time.Time) (*source.Chunk, string, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, "", nil
	}

	pid := p.defaultPID
	// Check for [pid N] prefix
	if m := pidPrefixRegex.FindStringSubmatch(line); len(m) > 1 {
		if parsedPID, err := strconv.Atoi(m[1]); err == nil && parsedPID > 0 {
			pid = parsedPID
		}
		line = line[len(m[0]):]
	}

	// 1. A call that was split because another tracee was in a syscall: this line has the return value only
	if m := resumedRegex.FindStringSubmatch(line); m != nil {
		pending, exists := p.pending[pid]
		if !exists {
			return nil, "", nil
		}
		delete(p.pending, pid)

		ret, _ := strconv.Atoi(m[2])
		return buildChunk(pending.data, ret, pending.fd, pid, now), pending.dest, nil
	}

	// 2. The start of a write or writev: complete, or split with <unfinished ...>
	start := callStartRegex.FindStringSubmatch(line)
	if start == nil {
		return nil, "", nil
	}
	isVector := start[1] == "writev"
	fd, _ := strconv.Atoi(start[2])
	dest := decodeDest(start[3])
	rest := line[len(start[0]):]

	reopenedOutput := fd != 1 && fd != 2 && dest != "" && p.IsOutputDest != nil && p.IsOutputDest(dest)
	if fd != 1 && fd != 2 && !reopenedOutput {
		// A new call from this tracee means any earlier unfinished call has already been resumed
		delete(p.pending, pid)
		if p.OnOtherFD != nil {
			p.OnOtherFD(pid, fd)
		}
		return nil, "", nil
	}

	var hexParts []string
	var ret string
	if isVector {
		m := writevArgsRegex.FindStringSubmatch(rest)
		if m == nil {
			return nil, "", nil
		}
		hexParts = vectorHexParts(m[1])
		ret = m[3]
	} else {
		m := writeArgsRegex.FindStringSubmatch(rest)
		if m == nil {
			return nil, "", nil
		}
		hexParts = []string{m[1]}
		ret = m[3]
	}

	var data []byte
	for _, part := range hexParts {
		decoded, err := DecodeStraceHex(part)
		if err != nil {
			delete(p.pending, pid)
			return nil, "", err
		}
		data = append(data, decoded...)
	}

	if ret == "" {
		// <unfinished ...>: keep the payload and destination until the resumed line gives the return value
		p.pending[pid] = &pendingCall{fd: fd, data: data, dest: dest}
		return nil, "", nil
	}

	n, _ := strconv.Atoi(ret)
	return buildChunk(data, n, fd, pid, now), dest, nil
}

// decodeDest turns the text strace printed between < and > into a plain destination. It returns "" when there
// is no annotation or it cannot be decoded, which callers treat as "unknown".
func decodeDest(raw string) string {
	if raw == "" {
		return ""
	}
	decoded, err := DecodeStraceHex(raw)
	if err != nil {
		return ""
	}
	return string(decoded)
}

// ParseExit parses a line that reports the end of a traced process. A line without a "[pid N]" prefix belongs to
// the parser's default PID, which is the target, since strace leaves the prefix off while one process is traced.
// The second result is false for every other line.
func (p *Parser) ParseExit(line string) (source.ExitStatus, bool) {
	line = strings.TrimSpace(line)
	pid := p.defaultPID
	unprefixed := true
	if m := pidPrefixRegex.FindStringSubmatch(line); len(m) > 1 {
		if parsed, err := strconv.Atoi(m[1]); err == nil && parsed > 0 {
			pid = parsed
			unprefixed = false
		}
		line = line[len(m[0]):]
	}

	m := exitRegex.FindStringSubmatch(line)
	if m == nil {
		return source.ExitStatus{}, false
	}
	if m[2] != "" {
		return source.ExitStatus{PID: pid, Signal: m[2], Unprefixed: unprefixed}, true
	}
	code, err := strconv.Atoi(m[1])
	if err != nil {
		return source.ExitStatus{}, false
	}
	return source.ExitStatus{PID: pid, Code: code, Unprefixed: unprefixed}, true
}

// vectorHexParts returns the iov_base strings of a writev vector in order. When strace cut an element, the
// elements after it are dropped, because their bytes would not follow the captured ones.
func vectorHexParts(vector string) []string {
	var parts []string
	for _, m := range iovBaseRegex.FindAllStringSubmatch(vector, -1) {
		parts = append(parts, m[1])
		if m[2] != "" {
			break
		}
	}
	return parts
}

// buildChunk limits the captured bytes to the return value of the call. When the call wrote more than strace
// captured, a marker and a newline follow the bytes, so the missing data is visible and the next line stays apart.
func buildChunk(data []byte, ret, fd, pid int, now time.Time) *source.Chunk {
	if ret <= 0 {
		return nil
	}
	if len(data) > ret {
		data = data[:ret]
	} else if len(data) < ret {
		data = append(data, fmt.Sprintf("[specter: %d more bytes not captured]\n", ret-len(data))...)
	}
	return &source.Chunk{
		Bytes: data,
		FD:    fd,
		PID:   pid,
		TS:    now,
	}
}

// DecodeStraceHex decodes a strace hex-escaped string (e.g. `\x68\x65\x6c\x6c\x6f`).
func DecodeStraceHex(s string) ([]byte, error) {
	// Remove trailing ellipsis if strace truncated with -s
	s = strings.TrimSuffix(s, "...")

	var buf []byte
	i := 0
	for i < len(s) {
		if i+3 < len(s) && s[i] == '\\' && s[i+1] == 'x' {
			hexPair := s[i+2 : i+4]
			b, err := hex.DecodeString(hexPair)
			if err != nil {
				return nil, fmt.Errorf("invalid hex byte %q: %w", hexPair, err)
			}
			buf = append(buf, b[0])
			i += 4
		} else {
			// Plain character pass-through (fallback if not pure -xx)
			buf = append(buf, s[i])
			i++
		}
	}
	return buf, nil
}
