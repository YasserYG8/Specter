package pipeline

import (
	"regexp"
	"strings"
	"sync"
)

// FilterOptions defines criteria for filtering log entries.
type FilterOptions struct {
	MinLevel   Severity
	Grep       string
	OnlyStderr bool
	grepRegex  *regexp.Regexp
}

// Filter evaluates log entries against configured criteria.
type Filter struct {
	opts        FilterOptions
	mu          sync.Mutex
	activeMatch map[streamKey]bool
}

// NewFilter creates a Filter with the given options.
func NewFilter(opts FilterOptions) *Filter {
	f := &Filter{
		opts:        opts,
		activeMatch: make(map[streamKey]bool),
	}
	if opts.Grep != "" {
		pattern := opts.Grep
		// Handle regex notation /pattern/ or raw regex
		if strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") && len(pattern) > 2 {
			pattern = pattern[1 : len(pattern)-1]
		}
		if re, err := regexp.Compile("(?i)" + pattern); err == nil {
			f.opts.grepRegex = re
		}
	}
	return f
}

// matchText tests a single string against the configured grep pattern.
func (f *Filter) matchText(text string) bool {
	if f.opts.Grep == "" {
		return true
	}
	if f.opts.grepRegex != nil {
		return f.opts.grepRegex.MatchString(text)
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(f.opts.Grep))
}

// Matches returns true if the entry satisfies all filter conditions.
func (f *Filter) Matches(entry *Entry) bool {
	// 1. OnlyStderr check
	if f.opts.OnlyStderr && entry.FD != 2 {
		return false
	}

	// 2. Minimum log level check
	if f.opts.MinLevel > SeverityNone {
		// If entry has a recognized severity, it must be >= MinLevel
		if entry.Level != SeverityNone && entry.Level < f.opts.MinLevel {
			return false
		}
	}

	// 3. Grep pattern check with real-time stream inheritance
	if f.opts.Grep != "" {
		key := streamKey{pid: entry.PID, fd: entry.FD}
		f.mu.Lock()
		defer f.mu.Unlock()

		lineMatched := f.matchText(entry.Text())

		if entry.IsFrame || entry.IsRecordContinuation {
			// Continuation line:
			// If parent header matched, inherit match
			if f.activeMatch[key] {
				return true
			}
			// If this specific frame matches, admit it and activate subsequent frames
			if lineMatched {
				f.activeMatch[key] = true
				return true
			}
			return false
		}

		// New record or plain line: evaluate anew
		f.activeMatch[key] = lineMatched
		return lineMatched
	}

	return true
}
