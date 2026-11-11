package source

import (
	pathpkg "path"
	"slices"
	"strings"
)

// IsLogFile reports whether path is a log file that a target writes to on its own. Both capture engines use it for
// the own log file notice, so they apply one rule.
//
// A path counts when "log" or "logs" is a whole word in it, compared without regard to case: a part of the file name
// (parts are separated by '.', '-' and '_'), or a folder in the path. Letters inside a longer word do not count, so
// "catalog.json" is not a log file.
//
// A file named LOG, LOG.old or LOG.old.<digits> next to a file named CURRENT is the internal log of an embedded
// database (the LevelDB and RocksDB layout), and does not count. exists reports whether a path exists. It is only
// called for those names.
func IsLogFile(path string, exists func(string) bool) bool {
	// dir keeps its final slash, so the folder of "/LOG" is "/" and CURRENT is looked up as "/CURRENT". Without the
	// slash the folder would be empty, and "CURRENT" would be looked up in Specter's own working directory.
	dir, base := pathpkg.Split(path)

	if isDatabaseLogName(base) && exists != nil && exists(pathpkg.Join(dir, "CURRENT")) {
		return false
	}

	return slices.ContainsFunc(strings.FieldsFunc(base, isNamePartSeparator), isLogWord) ||
		slices.ContainsFunc(slices.Collect(strings.SplitSeq(dir, "/")), isLogWord)
}

func isNamePartSeparator(r rune) bool {
	return r == '.' || r == '-' || r == '_'
}

func isLogWord(s string) bool {
	return strings.EqualFold(s, "log") || strings.EqualFold(s, "logs")
}

// isDatabaseLogName reports whether name is what LevelDB and RocksDB call their log: LOG, LOG.old or LOG.old.<digits>.
// The match is case-sensitive, as those databases write the names.
func isDatabaseLogName(name string) bool {
	if name == "LOG" || name == "LOG.old" {
		return true
	}
	digits, ok := strings.CutPrefix(name, "LOG.old.")
	if !ok || digits == "" {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
