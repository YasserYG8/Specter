package ui

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// CanReadKeys reports whether Specter may take over the terminal to read keys while it streams. All of these must
// hold: stdin and stderr are terminals (the picker draws on stderr), and Specter's process group is the terminal's
// foreground group. The last one matters because the kernel stops a background job that reads the terminal or
// changes its mode (SIGTTIN, SIGTTOU), so `specter 3000 &` must leave the terminal alone. stdout may be a pipe.
func CanReadKeys(stdin, stderr *os.File) bool {
	if stdin == nil || stderr == nil {
		return false
	}
	if !term.IsTerminal(int(stdin.Fd())) || !term.IsTerminal(int(stderr.Fd())) {
		return false
	}
	fg, err := unix.IoctlGetInt(int(stdin.Fd()), unix.TIOCGPGRP)
	if err != nil {
		return false
	}
	return fg == unix.Getpgrp()
}

// KeyReader reads Esc and Enter from the terminal while Specter streams. It puts the terminal in a mode without
// line buffering and without echo, and gives the mode back in Stop.
type KeyReader struct {
	fd int

	mu      sync.Mutex
	saved   *unix.Termios // the terminal's settings before Start
	cancel  context.CancelFunc
	done    chan struct{}
	stopped bool
}

// NewKeyReader makes a reader for the terminal on f, which is normally os.Stdin.
func NewKeyReader(f *os.File) *KeyReader {
	return &KeyReader{fd: int(f.Fd())}
}

// Start changes the terminal mode and starts reading. The channel closes when the reader stops, which happens when
// ctx ends, when Stop is called, or when the terminal can no longer be read.
//
// The mode turns off ICANON (so keys arrive at once) and ECHO (so typed text does not mix with the logs). It keeps
// ISIG, so Ctrl+C still sends SIGINT and the signal path stays the only way to quit, and OPOST, so "\n" still goes
// back to the first column. term.MakeRaw turns both of those off, which would break the log output.
func (k *KeyReader) Start(ctx context.Context) (<-chan Key, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.saved != nil {
		return nil, errors.New("the key reader is already running")
	}

	saved, err := unix.IoctlGetTermios(k.fd, unix.TCGETS)
	if err != nil {
		return nil, err
	}
	mode := *saved
	mode.Lflag &^= unix.ICANON | unix.ECHO
	mode.Cc[unix.VMIN] = 1
	mode.Cc[unix.VTIME] = 0
	// TCSETSF also throws away what was typed before, so an old Esc or Enter from before the stream is not acted on
	if err := unix.IoctlSetTermios(k.fd, unix.TCSETSF, &mode); err != nil {
		return nil, err
	}
	k.saved = saved

	ctx, k.cancel = context.WithCancel(ctx)
	k.done = make(chan struct{})
	keys := make(chan Key, 8)
	go func() {
		defer close(k.done)
		defer close(keys)
		src := fdSource{fd: k.fd}
		for ctx.Err() == nil {
			key, found, err := nextKey(src, keyIdle, escWait)
			if err != nil {
				return
			}
			if !found {
				continue
			}
			select {
			case keys <- key:
			case <-ctx.Done():
				return
			}
		}
	}()
	return keys, nil
}

// Stop ends the reading and gives the terminal back as it was. When it returns, nothing reads the terminal any
// more, so the picker can use it. It takes at most about 100 ms, and it is safe to call more than once.
func (k *KeyReader) Stop() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.saved == nil || k.stopped {
		return
	}
	k.stopped = true
	k.cancel()
	<-k.done
	_ = unix.IoctlSetTermios(k.fd, unix.TCSETS, k.saved)
}

// fdSource reads bytes from a file descriptor with poll, so a wait can time out. A plain blocking read cannot be
// canceled, and a reader that cannot stop would steal the keys of the picker.
type fdSource struct{ fd int }

func (s fdSource) readByte(timeout time.Duration) (byte, bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		left := time.Until(deadline)
		if left < 0 {
			left = 0
		}
		fds := []unix.PollFd{{Fd: int32(s.fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, int((left+time.Millisecond-1)/time.Millisecond))
		if err == unix.EINTR {
			continue // a signal (such as the one for Ctrl+C) woke the poll: wait for the rest of the time
		}
		if err != nil {
			return 0, false, err
		}
		if n == 0 {
			return 0, false, nil
		}
		if fds[0].Revents&unix.POLLIN == 0 {
			// POLLHUP or POLLERR without data: the terminal is gone
			return 0, false, errors.New("the terminal was closed")
		}
		var buf [1]byte
		got, err := unix.Read(s.fd, buf[:])
		if err == unix.EINTR || err == unix.EAGAIN {
			continue
		}
		if err != nil {
			return 0, false, err
		}
		if got == 0 {
			return 0, false, errors.New("the terminal was closed")
		}
		return buf[0], true, nil
	}
}
