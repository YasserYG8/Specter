// Package procwatch tells when a process has ended, by polling /proc. It exists for sources that cannot say so
// themselves: a log file keeps no record of who wrote it, so only the process can be asked.
package procwatch

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultPeriod is how often the process is checked.
const DefaultPeriod = time.Second

// Options configures Watch. The zero value watches /proc once per DefaultPeriod.
type Options struct {
	ProcRoot string        // proc filesystem root, "/proc" when empty
	Period   time.Duration // time between checks, DefaultPeriod when not positive
}

// stat holds the two fields of /proc/<pid>/stat that matter here.
type stat struct {
	state     byte   // third field: R, S, D, Z, X ...
	startTime string // field 22: when the process started, in clock ticks since boot
}

// readStat reads /proc/<pid>/stat. The command name is the second field and may contain spaces and
// parentheses, so the rest of the line is parsed from the last closing parenthesis.
func readStat(procRoot string, pid int) (stat, error) {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "stat"))
	if err != nil {
		return stat{}, err
	}
	text := string(data)
	end := strings.LastIndexByte(text, ')')
	if end < 0 {
		return stat{}, errors.New("malformed stat")
	}
	// Fields after the command name start at field 3 (state), so field 22 is at index 19
	fields := strings.Fields(text[end+1:])
	if len(fields) < 20 || len(fields[0]) == 0 {
		return stat{}, errors.New("short stat")
	}
	return stat{state: fields[0][0], startTime: fields[19]}, nil
}

// Watch starts watching the process pid and returns a channel that is closed when the process is gone. Gone
// means that /proc/<pid>/stat no longer exists, that the process is a zombie or dead (it has ended, but its
// parent has not collected it yet), or that its start time changed, which means another process now has the
// same PID. The start time is recorded before Watch returns, so it reflects the process Specter attached to.
//
// If the process is already gone, the returned channel is already closed. If ctx ends first, the channel is
// never closed and the watcher stops.
func Watch(ctx context.Context, pid int, opts Options) <-chan struct{} {
	root := opts.ProcRoot
	if root == "" {
		root = "/proc"
	}
	period := opts.Period
	if period <= 0 {
		period = DefaultPeriod
	}

	gone := make(chan struct{})
	first, err := readStat(root, pid)
	if err != nil && errors.Is(err, fs.ErrNotExist) {
		close(gone)
		return gone
	}
	// Any other error (a malformed file, a permission problem) means the process may be alive, so it is not gone

	go func() {
		ticker := time.NewTicker(period)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			cur, err := readStat(root, pid)
			switch {
			case err != nil && errors.Is(err, fs.ErrNotExist):
				close(gone)
				return
			case err != nil:
				continue
			case cur.state == 'Z' || cur.state == 'X':
				close(gone)
				return
			case first.startTime != "" && cur.startTime != first.startTime:
				close(gone)
				return
			}
		}
	}()
	return gone
}
