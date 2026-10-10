package pipeline

import (
	"regexp"
	"strings"
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
	opts FilterOptions
}

// NewFilter creates a Filter with the given options.
func NewFilter(opts FilterOptions) *Filter {
	f := &Filter{opts: opts}
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

	// 3. Grep pattern check
	if f.opts.Grep != "" {
		text := entry.Text()
		if f.opts.grepRegex != nil {
			if !f.opts.grepRegex.MatchString(text) {
				return false
			}
		} else {
			if !strings.Contains(strings.ToLower(text), strings.ToLower(f.opts.Grep)) {
				return false
			}
		}
	}

	return true
}
