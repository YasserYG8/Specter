package resolver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ContainerPortMapping represents a host port mapped to a container port.
type ContainerPortMapping struct {
	HostIP        string
	HostPort      int
	ContainerPort int
	Protocol      string // tcp or udp
}

// DockerContainer represents an active container discovered via Docker.
type DockerContainer struct {
	ID        string                 `json:"ID"`
	Names     string                 `json:"Names"`
	Image     string                 `json:"Image"`
	PortsRaw  string                 `json:"Ports"`
	State     string                 `json:"State"`
	Status    string                 `json:"Status"`
	LabelsRaw string                 `json:"Labels"`
	LabelsMap map[string]string      `json:"-"`
	Ports     []ContainerPortMapping `json:"-"`
}

// ParseLabels parses a comma-separated key=value string of Docker labels.
func ParseLabels(labelsRaw string) map[string]string {
	labels := make(map[string]string)
	for _, item := range strings.Split(labelsRaw, ",") {
		item = strings.TrimSpace(item)
		if idx := strings.Index(item, "="); idx != -1 {
			k := strings.TrimSpace(item[:idx])
			v := strings.TrimSpace(item[idx+1:])
			labels[k] = v
		}
	}
	return labels
}

// ComposeProject returns the Docker Compose project label if set.
func (c *DockerContainer) ComposeProject() string {
	if c.LabelsMap != nil && c.LabelsMap["com.docker.compose.project"] != "" {
		return c.LabelsMap["com.docker.compose.project"]
	}
	labels := ParseLabels(c.LabelsRaw)
	return labels["com.docker.compose.project"]
}

// ComposeService returns the Docker Compose service label if set.
func (c *DockerContainer) ComposeService() string {
	if c.LabelsMap != nil && c.LabelsMap["com.docker.compose.service"] != "" {
		return c.LabelsMap["com.docker.compose.service"]
	}
	labels := ParseLabels(c.LabelsRaw)
	return labels["com.docker.compose.service"]
}

// ProjectDisplayName returns "project/service" if compose labels exist, or primary container name.
func (c *DockerContainer) ProjectDisplayName() string {
	proj := c.ComposeProject()
	svc := c.ComposeService()
	if proj != "" && svc != "" {
		return fmt.Sprintf("%s/%s", proj, svc)
	}
	if proj != "" {
		return proj
	}
	return c.PrimaryName()
}

// PrimaryName returns the first container name stripped of any leading slash.
func (c *DockerContainer) PrimaryName() string {
	name := strings.TrimSpace(c.Names)
	if idx := strings.Index(name, ","); idx != -1 {
		name = name[:idx]
	}
	name = strings.TrimPrefix(name, "/")
	return strings.TrimSpace(name)
}

// ShortID returns the 12-character short container ID.
func (c *DockerContainer) ShortID() string {
	id := strings.TrimSpace(c.ID)
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// ParseDockerPorts parses the raw Ports string from `docker ps` into structured mappings.
// Example: "0.0.0.0:8080->80/tcp, [::]:8080->80/tcp, 0.0.0.0:3306->3306/tcp, 33060/tcp"
func ParseDockerPorts(portsRaw string) []ContainerPortMapping {
	var mappings []ContainerPortMapping
	seen := make(map[int]bool)

	parts := strings.Split(portsRaw, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || !strings.Contains(p, "->") {
			continue
		}

		arrowIdx := strings.Index(p, "->")
		hostPart := strings.TrimSpace(p[:arrowIdx])
		containerPart := strings.TrimSpace(p[arrowIdx+2:])

		// Parse host IP and port
		var hostIP string
		var hostPortStr string
		if strings.HasPrefix(hostPart, "[") {
			// IPv6 e.g. [::]:8080
			closeBracket := strings.Index(hostPart, "]")
			if closeBracket != -1 && len(hostPart) > closeBracket+1 && hostPart[closeBracket+1] == ':' {
				hostIP = hostPart[1:closeBracket]
				hostPortStr = hostPart[closeBracket+2:]
			}
		} else {
			lastColon := strings.LastIndex(hostPart, ":")
			if lastColon != -1 {
				hostIP = hostPart[:lastColon]
				hostPortStr = hostPart[lastColon+1:]
			} else {
				hostPortStr = hostPart
			}
		}

		hostPort, err := strconv.Atoi(hostPortStr)
		if err != nil || hostPort <= 0 {
			continue
		}

		// Avoid duplicate mappings if both IPv4 and IPv6 map the same host port
		if seen[hostPort] {
			continue
		}

		// Parse container port and protocol e.g. 80/tcp
		proto := "tcp"
		containerPortStr := containerPart
		if slashIdx := strings.Index(containerPart, "/"); slashIdx != -1 {
			containerPortStr = containerPart[:slashIdx]
			proto = containerPart[slashIdx+1:]
		}

		containerPort, err := strconv.Atoi(containerPortStr)
		if err != nil || containerPort <= 0 {
			containerPort = hostPort
		}

		seen[hostPort] = true
		mappings = append(mappings, ContainerPortMapping{
			HostIP:        hostIP,
			HostPort:      hostPort,
			ContainerPort: containerPort,
			Protocol:      proto,
		})
	}

	return mappings
}

// ParseDockerContainers parses newline-delimited JSON output from `docker ps --format "{{json .}}"`.
func ParseDockerContainers(data []byte) ([]DockerContainer, error) {
	var containers []DockerContainer
	scanner := bufio.NewScanner(bytes.NewReader(data))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var c DockerContainer
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue
		}

		// Only include running or active containers
		isUp := strings.ToLower(c.State) == "running" || strings.HasPrefix(strings.ToLower(c.Status), "up")
		if !isUp && c.State != "" {
			continue
		}

		c.Ports = ParseDockerPorts(c.PortsRaw)
		containers = append(containers, c)
	}

	return containers, scanner.Err()
}

// DiscoverDockerContainers queries Docker to discover active containers with ports.
func DiscoverDockerContainers(ctx context.Context, runner CommandRunner) ([]DockerContainer, error) {
	if runner == nil {
		runner = DefaultCommandRunner
	}

	// Use a 500ms timeout context to prevent blocking if Docker daemon is slow
	tCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	out, err := runner(tCtx, "docker", "ps", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}

	return ParseDockerContainers(out)
}

// InspectDockerContainer inspects a specific container by name or ID.
func InspectDockerContainer(ctx context.Context, runner CommandRunner, target string) (*DockerContainer, error) {
	if runner == nil {
		runner = DefaultCommandRunner
	}

	target = strings.TrimSpace(target)
	if target == "" {
		return nil, errors.New("container target cannot be empty")
	}

	tCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()

	out, err := runner(tCtx, "docker", "inspect", target, "--format", "{{.Id}}\t{{.Name}}\t{{.State.Running}}\t{{.Config.Image}}\t{{json .Config.Labels}}")
	if err != nil {
		return nil, fmt.Errorf("container %q not found or inspect failed: %w", target, err)
	}

	parts := strings.Split(strings.TrimSpace(string(out)), "\t")
	if len(parts) < 4 {
		// Fallback: try raw json or minimal fields
		parts = strings.Fields(strings.TrimSpace(string(out)))
		if len(parts) < 3 {
			return nil, fmt.Errorf("invalid inspect output for container %q: %s", target, string(out))
		}
	}

	id := parts[0]
	name := strings.TrimPrefix(parts[1], "/")
	isRunning := strings.ToLower(parts[2]) == "true"
	var image string
	if len(parts) >= 4 {
		image = parts[3]
	}

	var labelsMap map[string]string
	if len(parts) >= 5 && parts[4] != "" && parts[4] != "null" {
		_ = json.Unmarshal([]byte(parts[4]), &labelsMap)
	}

	if !isRunning {
		return nil, fmt.Errorf("container %q exists but is not running", target)
	}

	return &DockerContainer{
		ID:        id,
		Names:     name,
		Image:     image,
		State:     "running",
		Status:    "Up",
		LabelsMap: labelsMap,
	}, nil
}

// FindContainerByPort finds a container that maps the specified host port.
func FindContainerByPort(containers []DockerContainer, port int) (*DockerContainer, *ContainerPortMapping) {
	for i := range containers {
		for j := range containers[i].Ports {
			if containers[i].Ports[j].HostPort == port {
				return &containers[i], &containers[i].Ports[j]
			}
		}
	}
	return nil, nil
}
