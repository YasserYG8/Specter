package docker

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	// Matches cgroups v2 docker scope: docker-<64hex>.scope
	dockerScopeRegex = regexp.MustCompile(`docker-([0-9a-fA-F]{12,64})\.scope`)

	// Matches cgroups v1, containerd, moby, or podman: /docker/<64hex>, containerd/<64hex>, /moby/<64hex>
	dockerPathRegex = regexp.MustCompile(`(?:/docker/|/docker-|/containerd/|/moby/|/libpod-)([0-9a-fA-F]{12,64})`)
)

// ExtractContainerID parses cgroup file content lines and returns the first container ID found, or "".
func ExtractContainerID(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if m := dockerScopeRegex.FindStringSubmatch(line); len(m) > 1 {
			return m[1]
		}

		if m := dockerPathRegex.FindStringSubmatch(line); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// DetectContainer inspects /proc/<pid>/cgroup to determine if the process is running inside a container.
// procRoot defaults to "/proc" if empty (useful for testing with mock proc filesystems).
func DetectContainer(pid int, procRoot string) (string, error) {
	if procRoot == "" {
		procRoot = "/proc"
	}

	cgroupPath := filepath.Join(procRoot, strconv.Itoa(pid), "cgroup")
	data, err := os.ReadFile(cgroupPath)
	if err != nil {
		return "", err
	}

	containerID := ExtractContainerID(string(data))
	return containerID, nil
}
