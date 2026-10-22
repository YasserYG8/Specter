package resolver

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/specter-cli/specter/internal/source"
	"github.com/specter-cli/specter/internal/util"
)

// ProcessInspector inspects /proc/<PID> to extract process metadata.
type ProcessInspector struct {
	procRoot string
}

// NewProcessInspector creates an inspector targeting the specified proc directory (defaults to /proc).
func NewProcessInspector(procRoot string) *ProcessInspector {
	if procRoot == "" {
		procRoot = "/proc"
	}
	return &ProcessInspector{procRoot: procRoot}
}

// InspectPID reads process cmdline, cwd, and file descriptors 1 & 2 for the given PID.
func (pi *ProcessInspector) InspectPID(ctx context.Context, pid int) (*source.Target, error) {
	procDir := filepath.Join(pi.procRoot, fmt.Sprintf("%d", pid))
	if _, err := os.Stat(procDir); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %d", util.ErrProcessNotFound, pid)
		}
		return nil, err
	}

	target := &source.Target{
		PID: pid,
	}

	// 1. Read cmdline
	cmdlinePath := filepath.Join(procDir, "cmdline")
	if data, err := os.ReadFile(cmdlinePath); err == nil && len(data) > 0 {
		// null-separated tokens
		parts := bytes.Split(data, []byte{0})
		for _, p := range parts {
			if len(p) > 0 {
				target.Cmdline = append(target.Cmdline, string(p))
			}
		}
	}

	// 2. Read cwd
	cwdPath := filepath.Join(procDir, "cwd")
	if link, err := os.Readlink(cwdPath); err == nil {
		target.Cwd = link
	}

	// 3. Readlink fd 1 (stdout) and fd 2 (stderr)
	fd1Path := filepath.Join(procDir, "fd", "1")
	if link, err := os.Readlink(fd1Path); err == nil {
		target.Stdout = link
	}

	fd2Path := filepath.Join(procDir, "fd", "2")
	if link, err := os.Readlink(fd2Path); err == nil {
		target.Stderr = link
	}

	// 4. Check Docker / systemd cgroup
	cgroupPath := filepath.Join(procDir, "cgroup")
	if data, err := os.ReadFile(cgroupPath); err == nil {
		content := string(data)
		if strings.Contains(content, "docker") || strings.Contains(content, "containerd") {
			target.Container = "detected"
		}
		if strings.Contains(content, ".service") {
			// Extract service name if possible
			for _, line := range strings.Split(content, "\n") {
				if idx := strings.Index(line, ".service"); idx != -1 {
					parts := strings.Split(line[:idx+8], "/")
					target.Unit = parts[len(parts)-1]
					break
				}
			}
		}
	}

	return target, nil
}

// DefaultResolver combines PortResolver and ProcessInspector into a complete Resolver.
type DefaultResolver struct {
	portResolver *PortResolver
	inspector    *ProcessInspector
}

// NewDefaultResolver constructs a Resolver with the provided dependencies.
func NewDefaultResolver(runner CommandRunner, procRoot string) *DefaultResolver {
	return &DefaultResolver{
		portResolver: NewPortResolver(runner),
		inspector:    NewProcessInspector(procRoot),
	}
}

// ResolvePort resolves a listening TCP port to an inspected Target.
func (dr *DefaultResolver) ResolvePort(ctx context.Context, port int) (*source.Target, error) {
	pid, err := dr.portResolver.ResolvePortToPID(ctx, port)
	if err == nil && pid > 0 {
		target, inspectErr := dr.inspector.InspectPID(ctx, pid)
		if inspectErr == nil {
			target.Port = port
			// Check if host PID is docker-proxy forwarding a container
			cmdStr := strings.Join(target.Cmdline, " ")
			if strings.Contains(cmdStr, "docker-proxy") {
				if containers, cErr := DiscoverDockerContainers(ctx, dr.portResolver.runner); cErr == nil {
					if c, m := FindContainerByPort(containers, port); c != nil {
						target.Container = c.PrimaryName()
						target.Cmdline = []string{c.Image, fmt.Sprintf("(port %d->%d)", port, m.ContainerPort)}
						return target, nil
					}
				}
			}
			return target, nil
		}
		// Fallback target with just port and PID if /proc inspection is restricted
		return &source.Target{Port: port, PID: pid}, nil
	}

	// Host PID lookup failed (or was permission-denied / not found in WSL2 /proc table).
	// Check if an active Docker container maps this port before giving up!
	if containers, cErr := DiscoverDockerContainers(ctx, dr.portResolver.runner); cErr == nil {
		if c, m := FindContainerByPort(containers, port); c != nil {
			return &source.Target{
				Port:      port,
				PID:       0,
				Container: c.PrimaryName(),
				Cmdline:   []string{c.Image, fmt.Sprintf("(port %d->%d)", port, m.ContainerPort)},
			}, nil
		}
	}

	return nil, err
}

// ResolveContainer inspects an active Docker container target by name or ID.
func (dr *DefaultResolver) ResolveContainer(ctx context.Context, nameOrID string) (*source.Target, error) {
	c, err := InspectDockerContainer(ctx, dr.portResolver.runner, nameOrID)
	if err != nil {
		return nil, err
	}
	return &source.Target{
		PID:       0,
		Container: c.PrimaryName(),
		Cmdline:   []string{c.Image},
	}, nil
}

// ResolvePID inspects an explicitly provided process ID.
func (dr *DefaultResolver) ResolvePID(ctx context.Context, pid int) (*source.Target, error) {
	return dr.inspector.InspectPID(ctx, pid)
}

// DiscoverListeningPorts discovers all currently listening TCP ports using ss or lsof.
func (dr *DefaultResolver) DiscoverListeningPorts(ctx context.Context) ([]ListeningPort, error) {
	return dr.portResolver.DiscoverListeningPorts(ctx)
}

// DiscoverServices discovers active services combining host ports and Docker containers.
func (dr *DefaultResolver) DiscoverServices(ctx context.Context) ([]DiscoveredService, error) {
	return dr.portResolver.DiscoverServices(ctx)
}
