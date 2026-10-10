package strace

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/specter-cli/specter/internal/source"
)

// Regex patterns for matching strace output
var (
	// [pid 1234] or [pid  1234] prefix
	pidPrefixRegex = regexp.MustCompile(`^\[pid\s+(\d+)\]\s+`)

	// Complete write: write(1, "\x68\x65...", 5) = 5
	writeCompleteRegex = regexp.MustCompile(`^write\((\d+),\s*"([^"]*)",\s*(\d+)\)\s*=\s*(-?\d+)`)

	// Unfinished write: write(1, "\x68\x65..." <unfinished ...>
	writeUnfinishedRegex = regexp.MustCompile(`^write\((\d+),\s*"([^"]*)"\s*<unfinished \.\.\.>`)

	// Resumed write with content: <... write resumed> "\x6c\x6f", 5) = 5
	writeResumedWithContentRegex = regexp.MustCompile(`^<\.\.\.\s*write resumed>\s*"([^"]*)",\s*(\d+)\)\s*=\s*(-?\d+)`)

	// Resumed write without additional content: <... write resumed> ) = 5
	writeResumedRegex = regexp.MustCompile(`^<\.\.\.\s*write resumed>\s*\)\s*=\s*(-?\d+)`)
)

type pendingCall struct {
	fd       int
	hexParts []string
}

// Parser parses strace write() output into source.Chunk instances.
type Parser struct {
	defaultPID int
	pending    map[int]*pendingCall
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
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil
	}

	pid := p.defaultPID
	// Check for [pid N] prefix
	if m := pidPrefixRegex.FindStringSubmatch(line); len(m) > 1 {
		if parsedPID, err := strconv.Atoi(m[1]); err == nil && parsedPID > 0 {
			pid = parsedPID
		}
		line = line[len(m[0]):]
	}

	// 1. Check for resumed calls
	if m := writeResumedWithContentRegex.FindStringSubmatch(line); len(m) > 0 {
		pending, exists := p.pending[pid]
		if !exists {
			return nil, nil
		}
		delete(p.pending, pid)

		hexStr := m[1]
		ret, _ := strconv.Atoi(m[3])
		if ret <= 0 {
			return nil, nil
		}

		pending.hexParts = append(pending.hexParts, hexStr)
		allHex := strings.Join(pending.hexParts, "")
		decoded, err := DecodeStraceHex(allHex)
		if err != nil {
			return nil, err
		}
		if len(decoded) > ret {
			decoded = decoded[:ret]
		}

		return &source.Chunk{
			Bytes: decoded,
			FD:    pending.fd,
			PID:   pid,
			TS:    now,
		}, nil
	}

	if m := writeResumedRegex.FindStringSubmatch(line); len(m) > 0 {
		pending, exists := p.pending[pid]
		if !exists {
			return nil, nil
		}
		delete(p.pending, pid)

		ret, _ := strconv.Atoi(m[1])
		if ret <= 0 {
			return nil, nil
		}

		allHex := strings.Join(pending.hexParts, "")
		decoded, err := DecodeStraceHex(allHex)
		if err != nil {
			return nil, err
		}
		if len(decoded) > ret {
			decoded = decoded[:ret]
		}

		return &source.Chunk{
			Bytes: decoded,
			FD:    pending.fd,
			PID:   pid,
			TS:    now,
		}, nil
	}

	// 2. Check for unfinished calls
	if m := writeUnfinishedRegex.FindStringSubmatch(line); len(m) > 0 {
		fd, _ := strconv.Atoi(m[1])
		if fd != 1 && fd != 2 {
			return nil, nil
		}
		hexStr := m[2]
		p.pending[pid] = &pendingCall{
			fd:       fd,
			hexParts: []string{hexStr},
		}
		return nil, nil
	}

	// 3. Check for standard complete write calls
	if m := writeCompleteRegex.FindStringSubmatch(line); len(m) > 0 {
		fd, _ := strconv.Atoi(m[1])
		if fd != 1 && fd != 2 {
			return nil, nil
		}

		ret, _ := strconv.Atoi(m[4])
		if ret <= 0 {
			return nil, nil
		}

		hexStr := m[2]
		decoded, err := DecodeStraceHex(hexStr)
		if err != nil {
			return nil, err
		}

		if len(decoded) > ret {
			decoded = decoded[:ret]
		}

		return &source.Chunk{
			Bytes: decoded,
			FD:    fd,
			PID:   pid,
			TS:    now,
		}, nil
	}

	// Not a write to fd 1 or 2
	return nil, nil
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
