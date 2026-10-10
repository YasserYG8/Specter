package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractContainerID_CgroupsV2(t *testing.T) {
	sample := `0::/system.slice/docker-b27f4803d297a82649a7852c00d41804ee9d53c7a31b439e6a0d01d4a0a03b51.scope
`
	id := ExtractContainerID(sample)
	expected := "b27f4803d297a82649a7852c00d41804ee9d53c7a31b439e6a0d01d4a0a03b51"
	if id != expected {
		t.Errorf("expected %q, got %q", expected, id)
	}
}

func TestExtractContainerID_CgroupsV1(t *testing.T) {
	sample := `12:memory:/docker/112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00
11:cpu,cpuacct:/docker/112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00
10:net_cls,net_prio:/docker/112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00
`
	id := ExtractContainerID(sample)
	expected := "112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00"
	if id != expected {
		t.Errorf("expected %q, got %q", expected, id)
	}
}

func TestExtractContainerID_Containerd(t *testing.T) {
	sample := `0::/system.slice/containerd.service/io.containerd.runtime.v2.task/moby/abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890
`
	id := ExtractContainerID(sample)
	expected := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	if id != expected {
		t.Errorf("expected %q, got %q", expected, id)
	}
}

func TestExtractContainerID_NonContainer(t *testing.T) {
	sample := `0::/user.slice/user-1000.slice/session-2.scope
`
	id := ExtractContainerID(sample)
	if id != "" {
		t.Errorf("expected empty string for host process, got %q", id)
	}
}

func TestDetectContainer_MockProc(t *testing.T) {
	tmpDir := t.TempDir()
	pidDir := filepath.Join(tmpDir, "1234")
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		t.Fatalf("failed to create mock proc dir: %v", err)
	}

	cgroupContent := "0::/system.slice/docker-e8a49c25f1712a3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d.scope\n"
	if err := os.WriteFile(filepath.Join(pidDir, "cgroup"), []byte(cgroupContent), 0644); err != nil {
		t.Fatalf("failed to write mock cgroup: %v", err)
	}

	id, err := DetectContainer(1234, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "e8a49c25f1712a3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d"
	if id != expected {
		t.Errorf("expected %q, got %q", expected, id)
	}
}
