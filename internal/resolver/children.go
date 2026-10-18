package resolver

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// FindChildPIDs discovers all active child process IDs for a given parent PID
// by scanning /proc/*/status for matching PPid fields.
func (pi *ProcessInspector) FindChildPIDs(parentPID int) ([]int, error) {
	return FindChildrenInProc(pi.procRoot, parentPID)
}

// FindChildrenInProc inspects the specified procRoot directory for all processes whose PPid equals parentPID.
func FindChildrenInProc(procRoot string, parentPID int) ([]int, error) {
	if procRoot == "" {
		procRoot = "/proc"
	}

	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, err
	}

	var children []int

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || pid == parentPID {
			// Not a process PID directory, or the parent itself
			continue
		}

		statusPath := filepath.Join(procRoot, entry.Name(), "status")
		ppid, err := readPPidFromStatus(statusPath)
		if err != nil {
			// Process may have exited or permissions restricted; skip safely
			continue
		}

		if ppid == parentPID {
			children = append(children, pid)
		}
	}

	sort.Ints(children)
	return children, nil
}

// FindChildProcessTree recursively discovers all child and descendant process IDs for parentPID.
func (pi *ProcessInspector) FindChildProcessTree(parentPID int, maxDepth int) []int {
	if maxDepth <= 0 {
		maxDepth = 3
	}

	allChildren := make(map[int]bool)
	queue := []int{parentPID}
	depth := 0

	for len(queue) > 0 && depth < maxDepth {
		depth++
		var nextQueue []int

		for _, currentPID := range queue {
			kids, err := FindChildrenInProc(pi.procRoot, currentPID)
			if err != nil {
				continue
			}

			for _, kid := range kids {
				if !allChildren[kid] {
					allChildren[kid] = true
					nextQueue = append(nextQueue, kid)
				}
			}
		}

		queue = nextQueue
	}

	var result []int
	for pid := range allChildren {
		result = append(result, pid)
	}
	sort.Ints(result)
	return result
}

// readPPidFromStatus parses the PPid line from a Linux /proc/<pid>/status file.
func readPPidFromStatus(statusFile string) (int, error) {
	file, err := os.Open(statusFile)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "PPid:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return strconv.Atoi(fields[1])
			}
		}
	}

	return 0, scanner.Err()
}
