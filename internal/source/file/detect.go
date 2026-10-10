package file

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// TargetFiles stores the paths of regular files attached to standard output descriptors.
type TargetFiles struct {
	StdoutPath string
	StderrPath string
}

// HasRegularFiles returns true if at least one standard descriptor targets a regular file.
func (tf *TargetFiles) HasRegularFiles() bool {
	return tf.StdoutPath != "" || tf.StderrPath != ""
}

// IsRegularFile returns true if the path points to a regular file on disk (not pseudo-terminal, pipe, socket, or device).
func IsRegularFile(path string) bool {
	if path == "" {
		return false
	}
	// Filter out non-file pseudopaths
	if strings.HasPrefix(path, "/dev/") ||
		strings.HasPrefix(path, "pipe:") ||
		strings.HasPrefix(path, "socket:") ||
		strings.HasPrefix(path, "anon_inode:") {
		return false
	}

	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return st.Mode().IsRegular()
}

// DetectFileTargets inspects /proc/<pid>/fd/1 and /proc/<pid>/fd/2 to see if they resolve to regular files.
func DetectFileTargets(pid int, procRoot string) (*TargetFiles, error) {
	if procRoot == "" {
		procRoot = "/proc"
	}

	targets := &TargetFiles{}

	fd1Path := filepath.Join(procRoot, strconv.Itoa(pid), "fd", "1")
	if link, err := os.Readlink(fd1Path); err == nil {
		if IsRegularFile(link) {
			targets.StdoutPath = link
		}
	}

	fd2Path := filepath.Join(procRoot, strconv.Itoa(pid), "fd", "2")
	if link, err := os.Readlink(fd2Path); err == nil {
		if IsRegularFile(link) {
			targets.StderrPath = link
		}
	}

	return targets, nil
}
