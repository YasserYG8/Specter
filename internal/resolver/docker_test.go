package resolver

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseDockerPorts(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		expected []ContainerPortMapping
	}{
		{
			name:     "empty",
			raw:      "",
			expected: nil,
		},
		{
			name: "single ipv4 mapping",
			raw:  "0.0.0.0:8080->80/tcp",
			expected: []ContainerPortMapping{
				{HostIP: "0.0.0.0", HostPort: 8080, ContainerPort: 80, Protocol: "tcp"},
			},
		},
		{
			name: "ipv4 and ipv6 deduplicated",
			raw:  "0.0.0.0:8080->80/tcp, [::]:8080->80/tcp",
			expected: []ContainerPortMapping{
				{HostIP: "0.0.0.0", HostPort: 8080, ContainerPort: 80, Protocol: "tcp"},
			},
		},
		{
			name: "multiple mappings and exposed only",
			raw:  "0.0.0.0:3306->3306/tcp, [::]:3306->3306/tcp, 33060/tcp",
			expected: []ContainerPortMapping{
				{HostIP: "0.0.0.0", HostPort: 3306, ContainerPort: 3306, Protocol: "tcp"},
			},
		},
		{
			name: "custom host port and container port",
			raw:  "127.0.0.1:9090->3000/tcp",
			expected: []ContainerPortMapping{
				{HostIP: "127.0.0.1", HostPort: 9090, ContainerPort: 3000, Protocol: "tcp"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseDockerPorts(tt.raw)
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %d mappings, got %d: %+v", len(tt.expected), len(got), got)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("mapping %d mismatch: expected %+v, got %+v", i, tt.expected[i], got[i])
				}
			}
		})
	}
}

func TestParseDockerContainers(t *testing.T) {
	rawJSON := `{"ID":"13cbb067d0f2","Names":"phpmyadmin","Image":"phpmyadmin:latest","Ports":"0.0.0.0:8080->80/tcp, [::]:8080->80/tcp","Labels":"com.docker.compose.project=soa,com.docker.compose.service=phpmyadmin","State":"running","Status":"Up 2 hours"}
{"ID":"7c59f3358e6a","Names":"mysql_db","Image":"mysql:8.4","Ports":"0.0.0.0:3306->3306/tcp","State":"running","Status":"Up 2 hours"}
{"ID":"deadbeef1234","Names":"stopped_app","Image":"redis:alpine","Ports":"0.0.0.0:6379->6379/tcp","State":"exited","Status":"Exited (0) 5 hours ago"}
`
	containers, err := ParseDockerContainers([]byte(rawJSON))
	if err != nil {
		t.Fatalf("unexpected error parsing containers: %v", err)
	}

	// stopped_app should be filtered out
	if len(containers) != 2 {
		t.Fatalf("expected 2 running containers, got %d", len(containers))
	}

	if containers[0].PrimaryName() != "phpmyadmin" {
		t.Errorf("expected container name phpmyadmin, got %s", containers[0].PrimaryName())
	}
	if containers[0].ShortID() != "13cbb067d0f2" {
		t.Errorf("expected short ID 13cbb067d0f2, got %s", containers[0].ShortID())
	}
	if len(containers[0].Ports) != 1 || containers[0].Ports[0].HostPort != 8080 {
		t.Errorf("unexpected ports for phpmyadmin: %+v", containers[0].Ports)
	}
	if containers[0].ComposeProject() != "soa" {
		t.Errorf("expected compose project 'soa', got %q", containers[0].ComposeProject())
	}
	if containers[0].ComposeService() != "phpmyadmin" {
		t.Errorf("expected compose service 'phpmyadmin', got %q", containers[0].ComposeService())
	}
	if containers[0].ProjectDisplayName() != "soa/phpmyadmin" {
		t.Errorf("expected display name 'soa/phpmyadmin', got %q", containers[0].ProjectDisplayName())
	}

	if containers[1].PrimaryName() != "mysql_db" {
		t.Errorf("expected container name mysql_db, got %s", containers[1].PrimaryName())
	}
	if containers[1].ProjectDisplayName() != "mysql_db" {
		t.Errorf("expected display name fallback 'mysql_db', got %q", containers[1].ProjectDisplayName())
	}
}

func TestInspectDockerContainer(t *testing.T) {
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "docker" && len(args) > 1 && args[0] == "inspect" {
			target := args[1]
			if target == "phpmyadmin" {
				return []byte("13cbb067d0f2d2fe0254f6167a9990e4094ee00a74f4e7590e24ebf2c7fdc283\t/phpmyadmin\ttrue\tphpmyadmin:latest\t{\"com.docker.compose.project\":\"soa\",\"com.docker.compose.service\":\"phpmyadmin\"}"), nil
			}
			if target == "stopped" {
				return []byte("aabbccddeeff\t/stopped\tfalse\tredis:latest\tnull"), nil
			}
			return nil, errors.New("No such object")
		}
		return nil, errors.New("unexpected command")
	}

	ctx := context.Background()

	// 1. Running container with compose labels
	c, err := InspectDockerContainer(ctx, mockRunner, "phpmyadmin")
	if err != nil {
		t.Fatalf("unexpected error inspecting running container: %v", err)
	}
	if c.PrimaryName() != "phpmyadmin" {
		t.Errorf("expected phpmyadmin, got %s", c.PrimaryName())
	}
	if c.Image != "phpmyadmin:latest" {
		t.Errorf("expected phpmyadmin:latest, got %s", c.Image)
	}
	if c.ProjectDisplayName() != "soa/phpmyadmin" {
		t.Errorf("expected display name 'soa/phpmyadmin', got %q", c.ProjectDisplayName())
	}

	// 2. Stopped container
	_, err = InspectDockerContainer(ctx, mockRunner, "stopped")
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("expected error for stopped container, got %v", err)
	}

	// 3. Non-existent container
	_, err = InspectDockerContainer(ctx, mockRunner, "ghost")
	if err == nil {
		t.Errorf("expected error for non-existent container, got nil")
	}
}

func TestDefaultResolver_ResolvePort_ContainerFallback(t *testing.T) {
	dockerJSON := `{"ID":"13cbb067d0f2","Names":"phpmyadmin","Image":"phpmyadmin:latest","Ports":"0.0.0.0:8080->80/tcp","State":"running","Status":"Up 2 hours"}
`
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "lsof" {
			return nil, errors.New("lsof: command not found")
		}
		if name == "ss" {
			// Simulates WSL2 / unreadable socket without PID
			return []byte("LISTEN 0 511 0.0.0.0:8080 0.0.0.0:*\n"), nil
		}
		if name == "docker" && args[0] == "ps" {
			return []byte(dockerJSON), nil
		}
		return nil, errors.New("command not found")
	}

	res := NewDefaultResolver(mockRunner, t.TempDir())
	target, err := res.ResolvePort(context.Background(), 8080)
	if err != nil {
		t.Fatalf("unexpected error resolving container port: %v", err)
	}

	if target == nil {
		t.Fatal("expected target to be non-nil")
	}
	if target.Container != "phpmyadmin" {
		t.Errorf("expected target.Container to be phpmyadmin, got %s", target.Container)
	}
	if target.Port != 8080 {
		t.Errorf("expected target.Port 8080, got %d", target.Port)
	}
}

func TestDefaultResolver_DiscoverServices(t *testing.T) {
	ssOutput := `LISTEN 0 511 0.0.0.0:3000 0.0.0.0:* users:(("node",pid=1234,fd=19))
`
	dockerJSON := `{"ID":"13cbb067d0f2","Names":"phpmyadmin","Image":"phpmyadmin:latest","Ports":"0.0.0.0:8080->80/tcp","State":"running","Status":"Up"}
{"ID":"7c59f3358e6a","Names":"internal_worker","Image":"worker:v1","Ports":"","State":"running","Status":"Up"}
`
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "ss" {
			return []byte(ssOutput), nil
		}
		if name == "docker" && args[0] == "ps" {
			return []byte(dockerJSON), nil
		}
		return nil, errors.New("command not found")
	}

	res := NewDefaultResolver(mockRunner, t.TempDir())
	services, err := res.DiscoverServices(context.Background())
	if err != nil {
		t.Fatalf("unexpected error discovering services: %v", err)
	}

	if len(services) != 3 {
		t.Fatalf("expected 3 discovered services, got %d: %+v", len(services), services)
	}

	// Verify host service
	var foundNode, foundPMA, foundWorker bool
	for _, s := range services {
		if s.Port == 3000 && s.Kind == ServiceKindHost && s.ProcessName == "node" {
			foundNode = true
		}
		if s.Port == 8080 && s.Kind == ServiceKindDocker && s.ContainerName == "phpmyadmin" {
			foundPMA = true
		}
		if s.ContainerName == "internal_worker" && s.Kind == ServiceKindDocker && s.Port == 0 {
			foundWorker = true
		}
	}

	if !foundNode {
		t.Errorf("did not find host service node on port 3000")
	}
	if !foundPMA {
		t.Errorf("did not find docker service phpmyadmin on port 8080")
	}
	if !foundWorker {
		t.Errorf("did not find port-less docker service internal_worker")
	}
}
