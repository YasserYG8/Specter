package pipeline

import (
	"context"
	"sort"
	"time"
)

// MaxHeldTraceLines is the most lines TraceHolder keeps for one stack trace. A longer trace, such as a big Go
// goroutine dump, is matched on its first lines and the rest follow the line-by-line rules of the filter.
const MaxHeldTraceLines = 1000

// heldTrace is a stack trace that TraceHolder has not released yet.
type heldTrace struct {
	entries []Entry
	last    time.Time // when the latest line arrived
	seq     int       // order in which traces started, so a flush at the end keeps their order
}

// TraceHolder holds the lines of each stack trace until the trace ends, so that --grep can look at all of them
// before any is shown. A match on a late line, such as the exception message that closes a Python traceback,
// then brings the whole trace with it. The stage is used only when --grep is set; without it nothing is held.
//
// A trace is held per (PID, fd) stream, so lines of other streams keep flowing. It is released when its closing
// line arrives (Entry.EndsTrace), when the same stream writes a line that does not continue it, when the stream
// is quiet for the idle timeout, when it reaches the line cap, and when the input closes.
type TraceHolder struct {
	match    func(string) bool
	idle     time.Duration
	maxLines int
}

// NewTraceHolder creates a holder. match tells whether the text of one line matches the --grep pattern, and is
// normally Filter.MatchText. A non-positive idle or maxLines selects DefaultTraceIdleTimeout or MaxHeldTraceLines.
func NewTraceHolder(match func(string) bool, idle time.Duration, maxLines int) *TraceHolder {
	if idle <= 0 {
		idle = DefaultTraceIdleTimeout
	}
	if maxLines <= 0 {
		maxLines = MaxHeldTraceLines
	}
	return &TraceHolder{match: match, idle: idle, maxLines: maxLines}
}

// Hold passes entries through, holding stack traces as described on TraceHolder, and returns the entries to
// output. A trace that matched has MatchedGrep set on all of its lines.
func (h *TraceHolder) Hold(ctx context.Context, in <-chan Entry) <-chan Entry {
	out := make(chan Entry, 1024)

	go func() {
		defer close(out)

		held := make(map[streamKey]*heldTrace)
		nextSeq := 0

		send := func(e Entry) bool {
			select {
			case out <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}

		// release sends a held trace, marking every line when any of them matches
		release := func(key streamKey) bool {
			ht := held[key]
			if ht == nil {
				return true
			}
			delete(held, key)

			matched := false
			for i := range ht.entries {
				if h.match(ht.entries[i].Text()) {
					matched = true
					break
				}
			}
			for i := range ht.entries {
				ht.entries[i].MatchedGrep = matched
				if !send(ht.entries[i]) {
					return false
				}
			}
			return true
		}

		releaseAll := func() bool {
			keys := make([]streamKey, 0, len(held))
			for key := range held {
				keys = append(keys, key)
			}
			sort.Slice(keys, func(i, j int) bool { return held[keys[i]].seq < held[keys[j]].seq })
			for _, key := range keys {
				if !release(key) {
					return false
				}
			}
			return true
		}

		handle := func(e Entry, now time.Time) bool {
			// Partial lines (prompts without a newline) are not part of any record
			if e.IsPartial {
				return send(e)
			}

			key := streamKey{pid: e.PID, fd: e.FD}
			continues := e.IsFrame || e.IsRecordContinuation

			if ht := held[key]; ht != nil {
				if continues {
					ht.entries = append(ht.entries, e)
					ht.last = now
					if e.EndsTrace || len(ht.entries) >= h.maxLines {
						return release(key)
					}
					return true
				}
				// A line that does not continue the trace ends it, and is then handled as a new line
				if !release(key) {
					return false
				}
			}

			if continues {
				// Nothing is held: this is what follows a trace cut at the line cap, or a record that is not
				// held (such as a .NET two-line record). The filter handles it line by line.
				return send(e)
			}

			if e.IsTrace {
				held[key] = &heldTrace{entries: []Entry{e}, last: now, seq: nextSeq}
				nextSeq++
				return true
			}
			return send(e)
		}

		interval := h.idle / 4
		if interval < time.Millisecond {
			interval = time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case now := <-ticker.C:
				// Release traces whose stream went quiet
				var quiet []streamKey
				for key, ht := range held {
					if now.Sub(ht.last) >= h.idle {
						quiet = append(quiet, key)
					}
				}
				sort.Slice(quiet, func(i, j int) bool { return held[quiet[i]].seq < held[quiet[j]].seq })
				for _, key := range quiet {
					if !release(key) {
						return
					}
				}

			case e, ok := <-in:
				if !ok {
					releaseAll()
					return
				}
				if !handle(e, time.Now()) {
					return
				}
			}
		}
	}()

	return out
}
