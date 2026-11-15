package ui

import (
	"testing"
	"time"
)

// listSource hands out a fixed list of bytes, then times out, like a terminal where nothing more is typed.
type listSource struct{ data []byte }

func (s *listSource) readByte(time.Duration) (byte, bool, error) {
	if len(s.data) == 0 {
		return 0, false, nil
	}
	b := s.data[0]
	s.data = s.data[1:]
	return b, true, nil
}

// decodeAll reads keys until the source runs dry and returns the keys found.
func decodeAll(t *testing.T, input string) []Key {
	t.Helper()
	src := &listSource{data: []byte(input)}
	var keys []Key
	for range 20 {
		key, found, err := nextKey(src, time.Millisecond, time.Millisecond)
		if err != nil {
			t.Fatalf("nextKey(%q): %v", input, err)
		}
		if found {
			keys = append(keys, key)
		}
		if len(src.data) == 0 {
			break
		}
	}
	return keys
}

func TestNextKey(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []Key
	}{
		{"lone Esc", "\x1b", []Key{KeyEsc}},
		{"Enter as CR", "\r", []Key{KeyEnter}},
		{"Enter as LF", "\n", []Key{KeyEnter}},
		{"up arrow", "\x1b[A", nil},
		{"down arrow", "\x1b[B", nil},
		{"arrow with a modifier", "\x1b[1;5C", nil},
		{"function key", "\x1b[15~", nil},
		{"SS3 arrow", "\x1bOA", nil},
		{"Alt with a letter", "\x1bx", nil},
		{"letters", "abc", nil},
		{"Ctrl+D byte", "\x04", nil},
		{"nothing", "", nil},
		{"arrow, then a real Esc", "\x1b[A\x1b", []Key{KeyEsc}},
		{"Esc, then Enter", "\x1b\r", nil}, // the Enter follows at once, so it is read as Alt+Enter
		{"letters around Enter", "ab\rcd", []Key{KeyEnter}},
		{"two arrows are two dropped keys", "\x1b[A\x1b[B", nil},
		{"an arrow does not hide the Enter after it", "\x1b[A\r", []Key{KeyEnter}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeAll(t, tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("keys = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("key %d = %v, want %v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// A source that fails must give the error back, so the reader can stop.
type failingSource struct{}

func (failingSource) readByte(time.Duration) (byte, bool, error) {
	return 0, false, errTerminalGone
}

var errTerminalGone = &terminalGoneError{}

type terminalGoneError struct{}

func (*terminalGoneError) Error() string { return "terminal gone" }

func TestNextKey_ReturnsReadErrors(t *testing.T) {
	if _, _, err := nextKey(failingSource{}, time.Millisecond, time.Millisecond); err != errTerminalGone {
		t.Errorf("err = %v, want the read error", err)
	}
}
