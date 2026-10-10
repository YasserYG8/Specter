package journal

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	// Matches system.slice/<name>.service
	systemdUnitRegex = regexp.MustCompile(`system\.slice/([a-zA-Z0-9_@.-]+\.service)`)
)

// ExtractSystemdUnit parses cgroup file content lines and returns the systemd service unit name, or "".
func ExtractSystemdUnit(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if m := systemdUnitRegex.FindStringSubmatch(line); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// DetectSystemdUnit inspects /proc/<pid>/cgroup to determine if the process runs as a systemd service unit.
func DetectSystemdUnit(pid int, procRoot string) (string, error) {
	if procRoot == "" {
		procRoot = "/proc"
	}

	cgroupPath := filepath.Join(procRoot, strconv.Itoa(pid), "cgroup")
	data, err := os.ReadFile(cgroupPath)
	if err != nil {
		return "", err
	}

	return ExtractSystemdUnit(string(data)), nil
}

// IsJournalctlAvailable checks if the journalctl binary is accessible in PATH.
func IsJournalctlAvailable() bool {
	_, err := exec.LookPath("journalctl")
	return err == nil
}
