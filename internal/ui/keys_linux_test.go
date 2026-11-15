package ui

import (
	"context"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// openPTY opens a pseudo-terminal. The master side is where the test "types"; the slave side is the terminal the
// key reader takes over.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx here: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Skipf("cannot unlock the pseudo-terminal: %v", err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Skipf("cannot name the pseudo-terminal: %v", err)
	}
	slave, err = os.OpenFile("/dev/pts/"+itoa(n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("cannot open the pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	return master, slave
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func termios(t *testing.T, f *os.File) *unix.Termios {
	t.Helper()
	tio, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatalf("cannot read the terminal settings: %v", err)
	}
	return tio
}

func waitKey(t *testing.T, keys <-chan Key) (Key, bool) {
	t.Helper()
	select {
	case k, ok := <-keys:
		return k, ok
	case <-time.After(2 * time.Second):
		t.Fatal("no key arrived")
		return 0, false
	}
}

func TestKeyReader_SetsAndRestoresTheTerminalMode(t *testing.T) {
	master, slave := openPTY(t)
	_ = master
	before := *termios(t, slave)
	if before.Lflag&unix.ECHO == 0 || before.Lflag&unix.ICANON == 0 {
		t.Fatalf("a new terminal should echo and buffer lines, Lflag = %#x", before.Lflag)
	}

	kr := NewKeyReader(slave)
	if _, err := kr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	during := termios(t, slave)
	if during.Lflag&unix.ECHO != 0 {
		t.Error("echo must be off while keys are read")
	}
	if during.Lflag&unix.ICANON != 0 {
		t.Error("line buffering must be off while keys are read")
	}
	if during.Lflag&unix.ISIG == 0 {
		t.Error("ISIG must stay on, so Ctrl+C still sends SIGINT")
	}
	if during.Oflag&unix.OPOST == 0 {
		t.Error("OPOST must stay on, so a line break still returns to the first column")
	}

	kr.Stop()
	after := *termios(t, slave)
	if after != before {
		t.Errorf("the terminal settings were not restored:\n before %+v\n after  %+v", before, after)
	}
	kr.Stop() // a second Stop does no harm
}

func TestKeyReader_StopReturnsPromptlyWhenNoKeyIsPressed(t *testing.T) {
	_, slave := openPTY(t)
	kr := NewKeyReader(slave)
	keys, err := kr.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	start := time.Now()
	kr.Stop()
	if took := time.Since(start); took > 200*time.Millisecond {
		t.Errorf("Stop took %v, want under 200 ms", took)
	}
	if _, ok := <-keys; ok {
		t.Error("the key channel should be closed after Stop")
	}
}

func TestKeyReader_ReadsEscAndEnterButNotArrows(t *testing.T) {
	master, slave := openPTY(t)
	kr := NewKeyReader(slave)
	keys, err := kr.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer kr.Stop()

	// An arrow key and typed text give no key. Then Enter, then a lone Esc.
	if _, err := master.WriteString("\x1b[Aabc\r"); err != nil {
		t.Fatal(err)
	}
	if k, _ := waitKey(t, keys); k != KeyEnter {
		t.Errorf("first key = %v, want Enter (the arrow and the letters are ignored)", k)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := master.WriteString("\x1b"); err != nil {
		t.Fatal(err)
	}
	if k, _ := waitKey(t, keys); k != KeyEsc {
		t.Errorf("second key = %v, want Esc", k)
	}
}

func TestKeyReader_TypedTextIsNotEchoed(t *testing.T) {
	master, slave := openPTY(t)
	kr := NewKeyReader(slave)
	if _, err := kr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer kr.Stop()

	if _, err := master.WriteString("abc"); err != nil {
		t.Fatal(err)
	}

	// What the terminal echoed would be readable on the master side
	fds := []unix.PollFd{{Fd: int32(master.Fd()), Events: unix.POLLIN}}
	if n, err := unix.Poll(fds, 300); err != nil {
		t.Fatalf("poll: %v", err)
	} else if n > 0 {
		t.Errorf("typed text was echoed back (poll reports data on the master side)")
	}
}

func TestKeyReader_StopsWhenItsContextEnds(t *testing.T) {
	_, slave := openPTY(t)
	kr := NewKeyReader(slave)
	ctx, cancel := context.WithCancel(context.Background())
	keys, err := kr.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancel()
	select {
	case _, ok := <-keys:
		if ok {
			t.Error("unexpected key")
		}
	case <-time.After(time.Second):
		t.Error("the key channel did not close after the context ended")
	}
	kr.Stop()
}

func TestCanReadKeys_FalseWhenStdinIsNotATerminal(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if CanReadKeys(f, f) {
		t.Error("/dev/null is not a terminal")
	}
	if CanReadKeys(nil, nil) {
		t.Error("nil files cannot be read")
	}
}
