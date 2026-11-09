package strace

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YasserYG8/Specter/internal/source"
)

// Options configures the strace source behavior.
type Options struct {
	FollowTree bool // Follow child processes and threads via -f
}

// Source implements source.Source using Linux strace.
type Source struct {
	mu            sync.Mutex
	cmd           *exec.Cmd
	parser        *Parser
	capturedError string
	// The strace line saying that attaching to the target itself failed, for example
	// "strace: attach: ptrace(PTRACE_SEIZE, 773): Operation not permitted"
	targetAttachErr string
	exit            *source.ExitStatus // how the target ended, once strace has printed its exit line
	options         Options

	// Notices and tests
	notifier           func(msg string)
	procRoot           string // proc filesystem root, "/proc" when empty
	readlink           func(path string) (string, error)
	isRegularFile      func(path string) bool
	maxLine            int // longest strace line to process, maxStraceLine when zero
	seenFDs            map[[2]int]bool
	seenLogPaths       map[string]bool
	oversizedPending   int
	lastOversizeNotice time.Time

	// Where the target's own stdout and stderr lead, read when tracing starts. Writes from other processes are
	// output only when they go to the same place. Empty strings mean the destination could not be read.
	targetPID int
	refs      streamRefs
}

// NewSource creates a new StraceSource instance with optional configuration.
func NewSource(opts ...Options) *Source {
	s := &Source{}
	if len(opts) > 0 {
		s.options = opts[0]
	}
	return s
}

// Name returns the identifier for this source mechanism.
func (s *Source) Name() string {
	return "strace"
}

// Start begins tracing the target process PID and streams chunks.
func (s *Source) Start(ctx context.Context, target source.Target) (<-chan source.Chunk, <-chan error) {
	chunks := make(chan source.Chunk, 1024)
	errs := make(chan error, 1)

	// Verify strace is installed upfront
	if installErr := CheckStraceInstalled(); installErr != nil {
		errs <- installErr
		close(chunks)
		close(errs)
		return chunks, errs
	}

	// Only one process can trace another. If someone else already traces the target, say so now, instead of
	// letting strace fail with a permission error. Workers that are traced already are left out and reported.
	target, tracedErr := s.checkTracers(target)
	if tracedErr != nil {
		errs <- tracedErr
		close(chunks)
		close(errs)
		return chunks, errs
	}

	s.parser = NewParser(target.PID)

	// Read the target's stream destinations before strace starts, so no helper can write before they are known
	s.targetPID = target.PID
	s.refs = s.readStreamRefs(target.PID)

	args := BuildStraceArgs(target)

	cmd := exec.CommandContext(ctx, "strace", args...)

	// Guarantee clean detachment on context cancellation (send SIGINT to strace, avoiding SIGKILL)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return cmd.Process.Signal(os.Interrupt)
		}
		return nil
	}
	cmd.WaitDelay = 3 * time.Second
	guardAgainstOrphan(cmd)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		errs <- fmt.Errorf("failed to create strace stderr pipe: %w", err)
		close(chunks)
		close(errs)
		return chunks, errs
	}

	if err := cmd.Start(); err != nil {
		errs <- fmt.Errorf("failed to start strace: %w", err)
		close(chunks)
		close(errs)
		return chunks, errs
	}

	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()

	go func() {
		defer close(chunks)
		defer close(errs)

		s.readLoop(ctx, stderr, chunks)

		// Await command termination
		waitErr := cmd.Wait()

		// If exited unexpectedly without context cancellation, diagnose the error. Failing to attach to the
		// target itself counts too, even when strace stopped cleanly, because Specter then has nothing to follow.
		s.mu.Lock()
		captured := s.capturedError
		attachErr := s.targetAttachErr
		s.mu.Unlock()
		if attachErr != "" {
			captured = attachErr
		}

		if (waitErr != nil || attachErr != "") && ctx.Err() == nil {
			if err := s.diagnoseAttach(captured, target); err != nil {
				errs <- err
				return
			}
			errs <- fmt.Errorf("strace exited unexpectedly: %w", waitErr)
		}
	}()

	return chunks, errs
}

// maxStraceLine is the longest strace line Specter processes. At -s 65536 a hex-encoded write is about
// 262 KB, so this leaves room for a long writev vector.
const maxStraceLine = 1 << 20

// oversizeNoticeInterval limits how often the over-limit line notice is printed.
const oversizeNoticeInterval = 5 * time.Second

func (s *Source) readLoop(ctx context.Context, r io.Reader, chunks chan<- source.Chunk) {
	limit := s.maxLine
	if limit <= 0 {
		limit = maxStraceLine
	}
	s.parser.OnOtherFD = s.checkOtherFD
	s.parser.IsOutputDest = s.isOutputDest

	scanLines(r, limit, func(line string) bool {
		// Capture strace diagnostic messages or errors
		if strings.HasPrefix(line, "strace: ") || strings.Contains(line, "attach:") {
			s.mu.Lock()
			s.capturedError = line
			var stop *exec.Cmd
			// With several -p arguments strace keeps running when only some attaches fail. If the target is the
			// one that failed, there is nothing to follow, so stop strace and report why.
			if s.targetAttachErr == "" && failedAttachTo(line, s.parser.defaultPID) {
				s.targetAttachErr = line
				stop = s.cmd
			}
			s.mu.Unlock()
			if stop != nil && stop.Process != nil {
				_ = stop.Process.Signal(os.Interrupt)
			}
			return true
		}

		// Exit lines: every traced thread, worker and helper prints one under -f, but only the target's counts
		if st, ok := s.parser.ParseExit(line); ok {
			if st.PID == s.parser.defaultPID {
				s.mu.Lock()
				// A line without a prefix is printed while only one process is left. It is the target's own end
				// only if the target has not ended yet; after that it is the last child that outlived the target
				if s.exit == nil || !st.Unprefixed {
					s.exit = &st
				}
				s.mu.Unlock()
			}
			return true
		}

		chunk, dest, err := s.parser.ParseLineDest(line, time.Now())
		if err != nil {
			// Skip corrupt line without terminating stream
			return true
		}
		if chunk != nil && shouldEmit(chunk.PID, dest, s.targetPID, s.refs) {
			select {
			case chunks <- *chunk:
			case <-ctx.Done():
				return false
			}
		}
		return true
	}, s.noteOversizedLine)
}

// scanLines calls onLine for each line of r, until onLine returns false or r ends. A line longer than limit
// bytes is discarded up to its end and reported to onTooLong with its length, and reading continues. Unlike
// bufio.Scanner, a long line never ends the loop, so strace's pipe keeps draining and the target is not held up.
func scanLines(r io.Reader, limit int, onLine func(string) bool, onTooLong func(n int)) {
	br := bufio.NewReaderSize(r, 64*1024)
	var acc []byte
	skipped := 0
	tooLong := false

	finish := func() bool {
		defer func() {
			acc = acc[:0]
			skipped = 0
			tooLong = false
		}()
		if tooLong {
			onTooLong(skipped)
			return true
		}
		return onLine(strings.TrimRight(string(acc), "\r\n"))
	}

	for {
		frag, err := br.ReadSlice('\n')
		if len(frag) > 0 {
			switch {
			case tooLong:
				skipped += len(frag)
			case len(acc)+len(frag) > limit:
				tooLong = true
				skipped = len(acc) + len(frag)
				acc = acc[:0]
			default:
				acc = append(acc, frag...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			// End of stream; a final line without a newline still counts
			if len(acc) > 0 || tooLong {
				finish()
			}
			return
		}
		if !finish() {
			return
		}
	}
}

// streamRefs holds the destinations of the target's fd 1 and fd 2, as readlink on /proc/<pid>/fd prints them
// ("pipe:[4021]", "/dev/pts/3", "/dev/null"). An empty string means that descriptor could not be read.
type streamRefs [2]string

// readStreamRefs reads where the target's stdout and stderr lead.
func (s *Source) readStreamRefs(pid int) streamRefs {
	root, readlink := s.procAccess()
	var refs streamRefs
	for i, fd := range []int{1, 2} {
		if path, err := readlink(filepath.Join(root, strconv.Itoa(pid), "fd", strconv.Itoa(fd))); err == nil {
			refs[i] = path
		}
	}
	return refs
}

// procAccess returns the proc root and the readlink function, using the real ones unless a test replaced them.
func (s *Source) procAccess() (string, func(string) (string, error)) {
	s.mu.Lock()
	root, readlink := s.procRoot, s.readlink
	s.mu.Unlock()
	if root == "" {
		root = "/proc"
	}
	if readlink == nil {
		readlink = os.Readlink
	}
	return root, readlink
}

// isOutputDest reports whether a write to a descriptor other than 1 and 2 reaches the target's own output,
// because the descriptor leads to the same terminal, pipe or socket as the target's stdout or stderr. Node.js
// reopens its terminal and writes through the new descriptor, so its lines only show up this way.
// /dev/null is left out: another descriptor pointing there is usually a library discarding data on purpose.
func (s *Source) isOutputDest(dest string) bool {
	if dest == "" || dest == "/dev/null" {
		return false
	}
	return dest == s.refs[0] || dest == s.refs[1]
}

// shouldEmit decides whether a write to fd 1 or 2 is output. The target's own writes always are, even when its
// stdout is /dev/null, because showing output that goes nowhere is what Specter is for. A write by any other
// traced process is output only when it goes to the same place as the target's stdout or stderr, which is how
// workers that share the server's output are told apart from helpers writing to a private pipe or /dev/null.
// Whenever the answer is unknown the write is output, since losing a real line is worse than showing an extra one.
func shouldEmit(pid int, dest string, targetPID int, refs streamRefs) bool {
	if pid == targetPID || dest == "" {
		return true
	}
	for _, ref := range refs {
		if ref != "" && dest == ref {
			return true
		}
	}
	// The write goes somewhere else. Drop it only if both of the target's streams were read, so an unreadable
	// one cannot hide a line that belongs to it.
	return refs[0] == "" || refs[1] == ""
}

// SetNotifier registers a function that receives status messages for the user, such as a target logging to
// its own file. The messages are plain text; the caller decides where to print them.
func (s *Source) SetNotifier(fn func(msg string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifier = fn
}

func (s *Source) notify(msg string) {
	s.mu.Lock()
	fn := s.notifier
	s.mu.Unlock()
	if fn != nil {
		fn(msg)
	}
}

// noteOversizedLine reports a strace line that was too long to process. Reports are rate limited so a
// flood of long lines does not flood the terminal; the count covers the lines skipped since the last report.
func (s *Source) noteOversizedLine(n int) {
	s.mu.Lock()
	s.oversizedPending++
	if !s.lastOversizeNotice.IsZero() && time.Since(s.lastOversizeNotice) < oversizeNoticeInterval {
		s.mu.Unlock()
		return
	}
	count := s.oversizedPending
	s.oversizedPending = 0
	s.lastOversizeNotice = time.Now()
	s.mu.Unlock()

	if count == 1 {
		s.notify(fmt.Sprintf("Skipped a strace output line of %d bytes because it is too long to process; capture continues.", n))
	} else {
		s.notify(fmt.Sprintf("Skipped %d strace output lines because they are too long to process; capture continues.", count))
	}
}

// checkOtherFD is called for writes to a descriptor other than stdout and stderr. It resolves each (pid, fd)
// pair once and, when it is a regular file with "log" in its name, tells the user once per path.
func (s *Source) checkOtherFD(pid, fd int) {
	key := [2]int{pid, fd}
	s.mu.Lock()
	if s.seenFDs == nil {
		s.seenFDs = make(map[[2]int]bool)
	}
	if s.seenFDs[key] {
		s.mu.Unlock()
		return
	}
	s.seenFDs[key] = true
	isRegular := s.isRegularFile
	s.mu.Unlock()

	root, readlink := s.procAccess()
	if isRegular == nil {
		isRegular = func(path string) bool {
			fi, err := os.Stat(path)
			return err == nil && fi.Mode().IsRegular()
		}
	}

	path, err := readlink(filepath.Join(root, strconv.Itoa(pid), "fd", strconv.Itoa(fd)))
	// Sockets and pipes resolve to names like socket:[123], which are not absolute paths
	if err != nil || !strings.HasPrefix(path, "/") || !strings.Contains(strings.ToLower(pathpkg.Base(path)), "log") {
		return
	}
	if !isRegular(path) {
		return
	}

	s.mu.Lock()
	if s.seenLogPaths == nil {
		s.seenLogPaths = make(map[string]bool)
	}
	already := s.seenLogPaths[path]
	s.seenLogPaths[path] = true
	s.mu.Unlock()
	if already {
		return
	}

	s.notify(fmt.Sprintf("Target writes to its own log file %s, which Specter does not follow. To read it: tail -f %s", path, path))
}

// Stop cleanly terminates the strace process.
func (s *Source) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		// Interrupt signal triggers strace to detach cleanly
		return s.cmd.Process.Signal(os.Interrupt)
	}
	return nil
}

// ExitStatus returns how the target ended, when strace has reported it. The second result is false while the
// target is still running, and when strace never printed its exit line.
func (s *Source) ExitStatus() (source.ExitStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exit == nil {
		return source.ExitStatus{}, false
	}
	return *s.exit, true
}

// BuildStraceArgs constructs the command-line arguments for strace execution,
// including multi-PID attachment for pre-existing child worker processes.
func BuildStraceArgs(target source.Target) []string {
	// Build strace command: strace -f -q -xx -y -s 65536 -e trace=write,writev -p PID [-p CHILD_PID...]
	// Note: -f is required so strace attaches to all threads of target processes
	// Note: a single -q hides attach and detach messages but keeps "+++ exited with N +++", which -qq hides.
	// Those lines are how the end of the target is reported with its exit code.
	// Note: -y makes strace print where each descriptor leads at the moment of the call, which is how writes
	// from helpers with a private stdout are told apart, even when the helper exits right after writing
	args := []string{
		"-f",
		"-q",
		"-xx",
		"-y",
		"-s", "65536",
		"-e", "trace=write,writev",
		"-p", strconv.Itoa(target.PID),
	}

	seen := map[int]bool{target.PID: true}
	// Every discovered worker gets its own -p. Workers started after attaching are followed by -f, but workers
	// that already exist are not, so skipping any of them loses their logs with no message.
	for _, childPID := range target.Children {
		if childPID > 0 && !seen[childPID] {
			seen[childPID] = true
			args = append(args, "-p", strconv.Itoa(childPID))
		}
	}

	return args
}
