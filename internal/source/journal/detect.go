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

// IgnoredSystemdUnits lists infrastructure, container, and CI runner services
// that should never be identified as target user applications.
var IgnoredSystemdUnits = []string{
	"hosted-compute-agent.service",
	"actions.runner",
	"actions-runner",
	"github-runner",
	"gitlab-runner",
	"azure-pipelines",
	"cloud-init.service",
	"containerd.service",
	"docker.service",
	"systemd-",
	"dbus.service",
	"polkit.service",
	"cron.service",
	"atd.service",
	"sshd.service",
	"ssh.service",
}

// IsIgnoredSystemdUnit returns true if unit represents an infrastructure or CI runner daemon.
func IsIgnoredSystemdUnit(unit string) bool {
	lower := strings.ToLower(unit)
	for _, ign := range IgnoredSystemdUnits {
		if strings.Contains(lower, ign) {
			return true
		}
	}
	return false
}

// ExtractSystemdUnit parses cgroup file content lines and returns the systemd service unit name, or "".
func ExtractSystemdUnit(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if m := systemdUnitRegex.FindStringSubmatch(line); len(m) > 1 {
			unit := m[1]
			if !IsIgnoredSystemdUnit(unit) {
				return unit
			}
		}
	}
	return ""
}

// DetectSystemdUnit inspects /proc/<pid>/cgroup and standard descriptors to determine if the process runs as a systemd service unit.
func DetectSystemdUnit(pid int, procRoot string) (string, error) {
	if procRoot == "" {
		procRoot = "/proc"
	}

	cgroupPath := filepath.Join(procRoot, strconv.Itoa(pid), "cgroup")
	data, err := os.ReadFile(cgroupPath)
	if err != nil {
		return "", err
	}

	unit := ExtractSystemdUnit(string(data))
	if unit == "" {
		return "", nil
	}

	// Verify the process is actually routing stdout/stderr to systemd journal.
	// For standard systemd services, systemd connects fd 1 / fd 2 to a journal socket (socket:[...]).
	// If fd 1 is an anonymous pipe, pty, or regular file, the process is not logging to journald.
	fdDir := filepath.Join(procRoot, strconv.Itoa(pid), "fd")
	if entries, err := os.ReadDir(fdDir); err == nil && len(entries) > 0 {
		isJournalSocket := false
		fd1Path := filepath.Join(fdDir, "1")
		if link, err := os.Readlink(fd1Path); err == nil {
			if strings.HasPrefix(link, "socket:") {
				isJournalSocket = true
			}
		}
		if !isJournalSocket {
			fd2Path := filepath.Join(fdDir, "2")
			if link, err := os.Readlink(fd2Path); err == nil {
				if strings.HasPrefix(link, "socket:") {
					isJournalSocket = true
				}
			}
		}
		// If neither descriptor is a socket, verify if it's the MainPID of the service via systemctl
		if !isJournalSocket {
			if !isServiceMainPID(pid, unit) {
				return "", nil
			}
		}
	}

	return unit, nil
}

func isServiceMainPID(pid int, unit string) bool {
	if pid <= 0 || unit == "" {
		return false
	}
	out, err := exec.Command("systemctl", "show", "-p", "MainPID", "--value", unit).Output()
	if err != nil {
		return false
	}
	mainPID, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return mainPID > 0 && mainPID == pid
}

// IsJournalctlAvailable checks if the journalctl binary is accessible in PATH.
func IsJournalctlAvailable() bool {
	_, err := exec.LookPath("journalctl")
	return err == nil
}
