package ui

import "time"

// Key is a key press that Specter reacts to while it streams. Every other key is ignored.
type Key int

const (
	// KeyEsc is the Esc key pressed alone. A key sequence that only starts with the Esc byte (an arrow key, Alt with
	// a letter) is not KeyEsc.
	KeyEsc Key = iota + 1
	// KeyEnter is Enter, whether the terminal sends it as CR or as LF.
	KeyEnter
)

const (
	// keyIdle is how long one wait for a byte lasts. The reader checks whether it should stop between waits, so this
	// is also how long Stop can take.
	keyIdle = 100 * time.Millisecond
	// escWait is how long the reader waits after an Esc byte for more bytes. An arrow key or Alt+letter sends its
	// bytes together, within a fraction of a millisecond on a local terminal. A lone Esc sends nothing after it. This
	// is the rule terminals and editors use, and 30 ms is far below human typing speed.
	escWait = 30 * time.Millisecond
)

// byteSource gives the decoder the bytes of the terminal. The terminal code implements it with poll and read, and
// tests implement it with a fixed list of bytes, so the decoding needs no terminal.
type byteSource interface {
	// readByte waits up to timeout for one byte. ok is false when the time ran out.
	readByte(timeout time.Duration) (b byte, ok bool, err error)
}

// nextKey reads from src until it has seen one key or the idle wait has run out. found is false when it ran out, or
// when the bytes were a key Specter ignores (they are read and dropped, so they cannot be mistaken for Esc later).
func nextKey(src byteSource, idle, escWait time.Duration) (key Key, found bool, err error) {
	b, ok, err := src.readByte(idle)
	if err != nil || !ok {
		return 0, false, err
	}

	switch b {
	case '\r', '\n':
		return KeyEnter, true, nil
	case 0x1b:
		return decodeEscape(src, escWait)
	}
	return 0, false, nil
}

// decodeEscape tells a lone Esc from an escape sequence, after the Esc byte has been read.
func decodeEscape(src byteSource, wait time.Duration) (Key, bool, error) {
	next, ok, err := src.readByte(wait)
	if err != nil {
		return 0, false, err
	}
	if !ok {
		return KeyEsc, true, nil // nothing followed: the user pressed Esc alone
	}

	switch next {
	case '[':
		// CSI sequence, such as an arrow key (ESC [ A): parameter bytes, then one final byte from 0x40 to 0x7e
		for {
			b, ok, err := src.readByte(wait)
			if err != nil || !ok {
				return 0, false, err
			}
			if b >= 0x40 && b <= 0x7e {
				return 0, false, nil
			}
		}
	case 'O':
		// SS3 sequence (ESC O A): one more byte
		if _, _, err := src.readByte(wait); err != nil {
			return 0, false, err
		}
		return 0, false, nil
	}
	// Alt with a key (ESC x): the second byte is read and dropped
	return 0, false, nil
}
