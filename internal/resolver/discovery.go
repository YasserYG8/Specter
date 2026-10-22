package resolver

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/specter-cli/specter/internal/util"
)

// ListeningPort represents an active TCP listening socket discovered on the system.
type ListeningPort struct {
	Port          int
	PID           int
	ProcessName   string
	Cmdline       string
	IsProtected   bool // True if listening but process info was hidden due to permissions
	ContainerID   string
	ContainerName string
	ContainerPort int
	Image         string
}

// NoProcessOnPortError is returned when a target TCP port has no listening process.
type NoProcessOnPortError struct {
	RequestedPort int
	ActivePorts   []ListeningPort
	Suggestion    *ListeningPort
}

// Error implements the standard error interface.
func (e *NoProcessOnPortError) Error() string {
	return e.FormatDiagnostic(false)
}

// Unwrap returns ErrNoProcessFound for compatibility with errors.Is.
func (e *NoProcessOnPortError) Unwrap() error {
	return util.ErrNoProcessFound
}

// FormatDiagnostic builds a human-readable diagnostic message listing active ports and suggestions.
func (e *NoProcessOnPortError) FormatDiagnostic(useColor bool) string {
	var sb strings.Builder

	header := "[specter]"
	if useColor {
		header = color.CyanString("[specter]")
	}

	sb.WriteString(fmt.Sprintf("%s No process is listening on port %d.\n", header, e.RequestedPort))

	if len(e.ActivePorts) == 0 {
		sb.WriteString("No active listening TCP ports were found on this machine.\n")
		return sb.String()
	}

	sb.WriteString("\nActive listening ports on this machine:\n")
	displayCount := len(e.ActivePorts)
	if displayCount > 8 {
		displayCount = 8
	}

	for i := 0; i < displayCount; i++ {
		p := e.ActivePorts[i]
		portStr := fmt.Sprintf(":%d", p.Port)
		if useColor {
			portStr = color.YellowString(portStr)
		}

		if p.ContainerName != "" {
			desc := fmt.Sprintf("docker:%s", p.ContainerName)
			if p.Image != "" {
				desc = fmt.Sprintf("docker:%s (%s)", p.ContainerName, p.Image)
			}
			sb.WriteString(fmt.Sprintf("  • %-7s %s\n", portStr, desc))
		} else if p.IsProtected {
			sb.WriteString(fmt.Sprintf("  • %-7s (process owned by root/another user — run with sudo)\n", portStr))
		} else if p.ProcessName != "" {
			sb.WriteString(fmt.Sprintf("  • %-7s PID %-6d (%s)\n", portStr, p.PID, p.ProcessName))
		} else if p.PID > 0 {
			sb.WriteString(fmt.Sprintf("  • %-7s PID %-6d\n", portStr, p.PID))
		} else {
			sb.WriteString(fmt.Sprintf("  • %-7s\n", portStr))
		}
	}

	if len(e.ActivePorts) > 8 {
		remaining := len(e.ActivePorts) - 8
		sb.WriteString(fmt.Sprintf("  • ... and %d more active ports\n", remaining))
	}

	if e.Suggestion != nil {
		suggestCmd := fmt.Sprintf("specter %d", e.Suggestion.Port)
		if useColor {
			suggestCmd = color.GreenString(suggestCmd)
		}
		sb.WriteString(fmt.Sprintf("\nDid you mean: %s ?\n", suggestCmd))
	}

	return sb.String()
}

// DiscoverListeningPorts discovers all currently listening TCP ports using ss or lsof,
// enriched with active Docker container port mappings.
func (r *PortResolver) DiscoverListeningPorts(ctx context.Context) ([]ListeningPort, error) {
	var ports []ListeningPort

	// 1. Try ss: ss -H -ltnp
	out, err := r.runner(ctx, "ss", "-H", "-ltnp")
	if err == nil && len(out) > 0 {
		ports = parseSSListeningPorts(string(out))
	} else {
		// 2. Fallback to lsof: lsof -iTCP -sTCP:LISTEN -P -n
		out, err = r.runner(ctx, "lsof", "-iTCP", "-sTCP:LISTEN", "-P", "-n")
		if err == nil && len(out) > 0 {
			ports = parseLsofListeningPorts(string(out))
		}
	}

	// 3. Enrich with running Docker containers (non-blocking fallback)
	if containers, cErr := DiscoverDockerContainers(ctx, r.runner); cErr == nil && len(containers) > 0 {
		ports = mergeDockerContainersIntoListeningPorts(ports, containers)
	}

	return ports, nil
}

// mergeDockerContainersIntoListeningPorts enriches listening ports with container metadata
// and appends published container ports that were not detected in host ss/lsof tables.
func mergeDockerContainersIntoListeningPorts(ports []ListeningPort, containers []DockerContainer) []ListeningPort {
	seen := make(map[int]int) // port -> index in ports
	for i, p := range ports {
		seen[p.Port] = i
	}

	for _, c := range containers {
		for _, m := range c.Ports {
			if idx, ok := seen[m.HostPort]; ok {
				// Enrich existing port
				if ports[idx].IsProtected || ports[idx].ProcessName == "docker-proxy" || ports[idx].ProcessName == "" {
					ports[idx].IsProtected = false
					ports[idx].ContainerName = c.PrimaryName()
					ports[idx].ContainerID = c.ShortID()
					ports[idx].ContainerPort = m.ContainerPort
					ports[idx].ProcessName = c.PrimaryName()
					ports[idx].Image = c.Image
				}
			} else {
				// Append container port mapping
				newPort := ListeningPort{
					Port:          m.HostPort,
					ContainerName: c.PrimaryName(),
					ContainerID:   c.ShortID(),
					ContainerPort: m.ContainerPort,
					ProcessName:   c.PrimaryName(),
					Image:         c.Image,
					IsProtected:   false,
				}
				ports = append(ports, newPort)
				seen[m.HostPort] = len(ports) - 1
			}
		}
	}

	sort.Slice(ports, func(i, j int) bool {
		return ports[i].Port < ports[j].Port
	})

	return ports
}

// ServiceKind distinguishes host processes from containerized services.
type ServiceKind string

const (
	ServiceKindHost   ServiceKind = "host"
	ServiceKindDocker ServiceKind = "docker"
)

// DiscoveredService unifies host listening sockets and active Docker containers for the picker.
type DiscoveredService struct {
	Kind          ServiceKind
	Port          int
	ContainerPort int
	PID           int
	ProcessName   string
	ContainerID   string
	ContainerName string
	Image         string
	Description   string
	IsProtected   bool
}

// DiscoverServices returns a unified list of active host ports and Docker containers.
func (r *PortResolver) DiscoverServices(ctx context.Context) ([]DiscoveredService, error) {
	ports, _ := r.DiscoverListeningPorts(ctx)
	containers, _ := DiscoverDockerContainers(ctx, r.runner)

	var services []DiscoveredService
	seenContainerNames := make(map[string]bool)

	// 1. Add listening ports (both host and container mapped)
	for _, p := range ports {
		if p.ContainerName != "" {
			seenContainerNames[p.ContainerName] = true
			services = append(services, DiscoveredService{
				Kind:          ServiceKindDocker,
				Port:          p.Port,
				ContainerPort: p.ContainerPort,
				ContainerName: p.ContainerName,
				ContainerID:   p.ContainerID,
				Image:         p.Image,
				ProcessName:   p.ContainerName,
				Description:   fmt.Sprintf("docker:%s (%s)", p.ContainerName, p.Image),
			})
		} else {
			desc := p.ProcessName
			if p.IsProtected {
				desc = "protected process (run with sudo)"
			} else if p.PID > 0 && p.ProcessName != "" {
				desc = fmt.Sprintf("%s (PID %d)", p.ProcessName, p.PID)
			} else if p.PID > 0 {
				desc = fmt.Sprintf("PID %d", p.PID)
			}
			services = append(services, DiscoveredService{
				Kind:        ServiceKindHost,
				Port:        p.Port,
				PID:         p.PID,
				ProcessName: p.ProcessName,
				Description: desc,
				IsProtected: p.IsProtected,
			})
		}
	}

	// 2. Add containers without published ports
	for _, c := range containers {
		cName := c.PrimaryName()
		if seenContainerNames[cName] {
			continue
		}
		seenContainerNames[cName] = true
		services = append(services, DiscoveredService{
			Kind:          ServiceKindDocker,
			ContainerName: cName,
			ContainerID:   c.ShortID(),
			Image:         c.Image,
			ProcessName:   cName,
			Description:   fmt.Sprintf("%s (%s)", cName, c.Image),
		})
	}

	return services, nil
}

// Regex to extract port and process info from ss output
// e.g. LISTEN 0 511 0.0.0.0:8080 0.0.0.0:* users:(("python3",pid=8912,fd=3))
var (
	ssLocalAddrRegex = regexp.MustCompile(`(?:\s|^)(?:[0-9a-fA-F.:*\[\]]+):(\d+)\s+`)
	ssUserRegex      = regexp.MustCompile(`users:\(\("([^"]+)",pid=(\d+)`)
)

func parseSSListeningPorts(output string) []ListeningPort {
	lines := strings.Split(output, "\n")
	seen := make(map[int]*ListeningPort)
	var order []int

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Find local port
		addrMatches := ssLocalAddrRegex.FindStringSubmatch(line)
		if len(addrMatches) < 2 {
			continue
		}

		portNum, err := strconv.Atoi(addrMatches[1])
		if err != nil || portNum <= 0 || portNum > 65535 {
			continue
		}

		var pid int
		var procName string
		isProtected := false

		userMatches := ssUserRegex.FindStringSubmatch(line)
		if len(userMatches) >= 3 {
			procName = userMatches[1]
			pid, _ = strconv.Atoi(userMatches[2])
		} else {
			// LISTEN entry without users:(...) typically indicates lack of privileges
			isProtected = true
		}

		if existing, exists := seen[portNum]; exists {
			// Enrich if we previously had an unprivileged/empty record
			if existing.PID == 0 && pid > 0 {
				existing.PID = pid
				existing.ProcessName = procName
				existing.IsProtected = false
			}
		} else {
			item := &ListeningPort{
				Port:        portNum,
				PID:         pid,
				ProcessName: procName,
				IsProtected: isProtected,
			}
			seen[portNum] = item
			order = append(order, portNum)
		}
	}

	result := make([]ListeningPort, 0, len(order))
	for _, p := range order {
		result = append(result, *seen[p])
	}
	return result
}

// Regex to parse lsof output lines:
// COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
// node     4920 dev    19u  IPv4  32819      0t0  TCP *:3001 (LISTEN)
func parseLsofListeningPorts(output string) []ListeningPort {
	lines := strings.Split(output, "\n")
	seen := make(map[int]*ListeningPort)
	var order []int

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "COMMAND") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}

		var pid int
		var cmd string

		typeIdx := -1
		for i := 3; i < len(fields); i++ {
			if strings.EqualFold(fields[i], "IPv4") || strings.EqualFold(fields[i], "IPv6") {
				if p, err := strconv.Atoi(fields[i-3]); err == nil && p > 0 {
					typeIdx = i
					break
				}
			}
		}

		if typeIdx != -1 {
			pid, _ = strconv.Atoi(fields[typeIdx-3])
			cmd = strings.Join(fields[:typeIdx-3], " ")
		} else {
			// Fallback: search for first integer PID field
			for i := 1; i < len(fields)-3; i++ {
				if p, err := strconv.Atoi(fields[i]); err == nil && p > 0 {
					pid = p
					cmd = strings.Join(fields[:i], " ")
					break
				}
			}
		}

		if pid <= 0 || cmd == "" {
			continue
		}

		var portNum int
		for i := len(fields) - 1; i >= 0; i-- {
			f := fields[i]
			idx := strings.LastIndex(f, ":")
			if idx == -1 || idx+1 >= len(f) {
				continue
			}

			var digits []rune
			for _, r := range f[idx+1:] {
				if r >= '0' && r <= '9' {
					digits = append(digits, r)
				} else {
					break
				}
			}
			if len(digits) > 0 {
				if p, err := strconv.Atoi(string(digits)); err == nil && p > 0 && p <= 65535 {
					portNum = p
					break
				}
			}
		}

		if portNum <= 0 {
			continue
		}

		if _, exists := seen[portNum]; !exists {
			item := &ListeningPort{
				Port:        portNum,
				PID:         pid,
				ProcessName: cmd,
				IsProtected: false,
			}
			seen[portNum] = item
			order = append(order, portNum)
		}
	}

	result := make([]ListeningPort, 0, len(order))
	for _, p := range order {
		result = append(result, *seen[p])
	}
	return result
}

// Common dev application runtimes
var devRuntimeNames = []string{
	"node", "python", "python3", "go", "ruby", "java",
	"vite", "next", "uvicorn", "gunicorn", "flask", "django",
	"docker", "docker-proxy", "caddy", "nginx", "deno", "bun",
}

// Common system background daemons
var systemDaemonNames = []string{
	"rpcbind", "systemd", "cupsd", "sshd", "chrony", "ntpd", "avahi",
}

// RankSuggestions ranks discovered listening ports against a requested target port.
// It returns the candidate list ordered by relevance, plus the highest-confidence suggestion (if any).
func RankSuggestions(requestedPort int, activePorts []ListeningPort) ([]ListeningPort, *ListeningPort) {
	if len(activePorts) == 0 {
		return nil, nil
	}
	if requestedPort <= 0 || requestedPort > 65535 {
		result := make([]ListeningPort, len(activePorts))
		copy(result, activePorts)
		return result, nil
	}

	type scoredPort struct {
		port  ListeningPort
		score int
	}

	scored := make([]scoredPort, 0, len(activePorts))

	for _, p := range activePorts {
		score := 0
		dist := p.Port - requestedPort
		if dist < 0 {
			dist = -dist
		}

		// 1. Numerical proximity (dist == 0 is the requested port itself, not an alternative)
		switch {
		case dist == 0:
			// Exact match: not an alternative suggestion delta
		case dist == 1:
			score += 100 // Common dev port increment: 3000 -> 3001
		case dist <= 5:
			score += 80 - dist*5
		case dist <= 20:
			score += 40
		case dist <= 100:
			score += 20
		case dist <= 500:
			score += 10
		}

		// 2. Same 1000-port block (e.g. 3000 vs 3500, 8000 vs 8080)
		if requestedPort >= 1000 && p.Port >= 1000 && p.Port/1000 == requestedPort/1000 {
			score += 15
		}

		// 3. String edit distance (meaningful typos within plausible proximity or same block)
		if dist > 0 && (dist <= 500 || (requestedPort >= 1000 && p.Port/1000 == requestedPort/1000)) &&
			levenshtein(strconv.Itoa(p.Port), strconv.Itoa(requestedPort)) <= 1 {
			score += 25
		}

		// 4. Runtime classification bonus / penalty
		procLower := strings.ToLower(p.ProcessName)
		isDev := false
		if p.ContainerName != "" {
			score += 35
			isDev = true
		} else {
			for _, name := range devRuntimeNames {
				if name == "go" {
					if procLower == "go" || strings.HasPrefix(procLower, "go-") || strings.HasSuffix(procLower, "-go") || strings.Contains(procLower, "golang") {
						score += 35
						isDev = true
						break
					}
				} else if strings.Contains(procLower, name) {
					score += 35
					isDev = true
					break
				}
			}
		}

		if !isDev {
			for _, name := range systemDaemonNames {
				if strings.Contains(procLower, name) {
					score -= 50
					break
				}
			}
		}

		if p.IsProtected {
			score -= 10
		}

		scored = append(scored, scoredPort{port: p, score: score})
	}

	// Sort by score descending; if score tied, sort by numerical distance ascending
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		distI := scored[i].port.Port - requestedPort
		if distI < 0 {
			distI = -distI
		}
		distJ := scored[j].port.Port - requestedPort
		if distJ < 0 {
			distJ = -distJ
		}
		return distI < distJ
	})

	result := make([]ListeningPort, len(scored))
	for i, sp := range scored {
		result[i] = sp.port
	}

	// Suggestion must be a distinct port from requestedPort with sufficient confidence (score >= 25)
	var suggestion *ListeningPort
	for _, sp := range scored {
		if sp.port.Port != requestedPort && sp.score >= 25 {
			s := sp.port
			suggestion = &s
			break
		}
	}

	return result, suggestion
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	d := make([][]int, la+1)
	for i := range d {
		d[i] = make([]int, lb+1)
		d[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		d[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			minVal := d[i-1][j] + 1
			if d[i][j-1]+1 < minVal {
				minVal = d[i][j-1] + 1
			}
			if d[i-1][j-1]+cost < minVal {
				minVal = d[i-1][j-1] + cost
			}
			d[i][j] = minVal
		}
	}
	return d[la][lb]
}
