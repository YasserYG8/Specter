package main

import (
	"context"
	"fmt"
	"time"

	"github.com/YasserYG8/Specter/internal/pipeline"
)

// startSessions starts the sessions one after the other, in the order the targets were given. It is all or
// nothing: when one session cannot start, the ones already started are stopped, last first, and the error is
// returned with the label of the target that failed (when several targets run). Nothing is printed from a log
// before this returns, so a failed run shows no log line.
//
// It must be called on the goroutine that holds the locked OS thread; see Session.Start.
func startSessions(ctx context.Context, sessions []*Session) error {
	for i, s := range sessions {
		if err := s.Start(ctx); err != nil {
			for j := i; j >= 0; j-- {
				sessions[j].Stop()
			}
			if s.env.multi {
				return fmt.Errorf("%s: %w", s.Label, err)
			}
			return err
		}
	}
	return nil
}

// sessionEvent is what one session sends to the output loop: an entry, or (when End is set) its end.
type sessionEvent struct {
	idx   int
	entry pipeline.Entry
	end   *sessionEnd
}

// forward copies the session's entries into out, one goroutine per session, and then sends how the session ended.
// Reading the end here (it may wait for the source to finish) keeps a slow session from holding up the others.
func (s *Session) forward(ctx context.Context, out chan<- sessionEvent) {
	for entry := range s.Entries {
		s.sawEntry() // counted before filters: output hidden by --grep or --level is not silence
		out <- sessionEvent{idx: s.Index, entry: entry}
	}
	end := s.finish(ctx)
	out <- sessionEvent{idx: s.Index, end: &end}
}

// traceHold is how long a target's stack trace may keep the output to itself while it waits for its next line.
// Every line of the trace restarts it. It is the same wait the stack-trace grouper uses to decide a trace is over.
const traceHold = pipeline.DefaultTraceIdleTimeout

// merger decides the order in which the events of several sessions are handled. Sessions send one entry per line,
// so without care a line of one target could land inside the stack trace of another. While a session is in the
// middle of a stack trace, the events of the other sessions wait, in the order they arrived, and are handled when
// the trace ends (its closing line, the next line of that session that is not part of the trace, the end of the
// session) or when it has been quiet for traceHold. A single session is never held, so one target is untouched.
type merger struct {
	handle func(sessionEvent) // does the real work: write an entry, print an end report
	single bool

	owner    int // the session whose trace is open, or -1
	deadline time.Time
	held     []sessionEvent // events of other sessions that arrived while the trace was open, oldest first
}

func newMerger(sessions int, handle func(sessionEvent)) *merger {
	return &merger{handle: handle, single: sessions == 1, owner: -1}
}

// accept takes the next event from any session.
func (m *merger) accept(ev sessionEvent) {
	if m.owner >= 0 && m.owner != ev.idx {
		m.held = append(m.held, ev)
		return
	}
	m.handle(ev)
	if m.single {
		return
	}
	if ev.end == nil && ev.entry.IsTrace && !ev.entry.EndsTrace && !ev.entry.IsPartial {
		m.owner, m.deadline = ev.idx, time.Now().Add(traceHold)
		return
	}
	if m.owner == ev.idx {
		m.release()
	}
}

// release ends the open trace and handles the events that waited for it. Handling one of them can open the trace
// of another session, which holds the events after it again.
func (m *merger) release() {
	m.owner = -1
	waiting := m.held
	m.held = nil
	for _, ev := range waiting {
		m.accept(ev)
	}
}

// wait is the time until the open trace is let go, as a channel for select; nil when no trace is open.
func (m *merger) wait() <-chan time.Time {
	if m.owner < 0 {
		return nil
	}
	return time.After(time.Until(m.deadline))
}

// expire lets go of a trace that has been quiet for traceHold. It does nothing if a newer line has moved the
// deadline in the meantime.
func (m *merger) expire() {
	if m.owner >= 0 && !time.Now().Before(m.deadline) {
		m.release()
	}
}

// runSessions is the output loop. It merges the entries of all started sessions, filters and renders them, and
// prints each session's end report as soon as that session ends, while the others keep streaming. It returns
// when every session has ended. blanks may be nil; each value on it asks for one empty line on screen.
//
// anyEnded is true when at least one session ended by itself (its target exited, or its source stopped), which
// makes the exit status of Specter 1. When the only session fails with a source error, runSessions returns that
// error instead, as a single target always did.
func runSessions(ctx context.Context, sessions []*Session, env *runEnv, renderer *pipeline.Renderer, ow *outputWriter, blanks <-chan struct{}) (anyEnded bool, fatal error) {
	merged := make(chan sessionEvent)
	for _, s := range sessions {
		go s.forward(ctx, merged)
	}

	remaining := len(sessions)
	m := newMerger(len(sessions), func(ev sessionEvent) {
		s := sessions[ev.idx]
		if ev.end == nil {
			if s.filter.Matches(&ev.entry) {
				ow.write(ev.idx, renderer.Render(&ev.entry), ev.entry.IsPartial)
			}
			return
		}

		remaining--
		switch {
		case ev.end.Err != nil && !env.multi:
			fatal = ev.end.Err
		case ev.end.Err != nil:
			// One target's source failed while it ran: that is the end of this target only
			fmt.Fprintf(env.errOut, "\n%s Capture of %s ended with an error: %v\n", s.tag, s.Label, ev.end.Err)
			anyEnded = true
		case reportEnd(env.errOut, s.tag, ev.end.Report) != 0:
			anyEnded = true
		}
	})

	for remaining > 0 && fatal == nil {
		select {
		case ev := <-merged:
			m.accept(ev)
		case <-m.wait():
			m.expire()
		case <-blanks: // the user pressed Enter; a nil channel never fires
			ow.blank()
		}
	}
	return anyEnded, fatal
}
