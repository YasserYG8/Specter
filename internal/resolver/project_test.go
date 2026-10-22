package resolver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectProjectName(t *testing.T) {
	// 1. Fallback to folder basename
	tmpDir := t.TempDir()
	exampleDir := filepath.Join(tmpDir, "example-app")
	if err := os.MkdirAll(exampleDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if name := DetectProjectName(exampleDir); name != "example-app" {
		t.Errorf("expected fallback 'example-app', got %q", name)
	}

	// 2. package.json detection
	pkgJSON := `{"name": "@myorg/frontend-client", "version": "1.0.0"}`
	if err := os.WriteFile(filepath.Join(exampleDir, "package.json"), []byte(pkgJSON), 0644); err != nil {
		t.Fatalf("failed to write package.json: %v", err)
	}
	if name := DetectProjectName(exampleDir); name != "frontend-client" {
		t.Errorf("expected 'frontend-client', got %q", name)
	}

	// 3. pom.xml detection
	javaDir := filepath.Join(tmpDir, "java-service")
	_ = os.MkdirAll(javaDir, 0755)
	pomXML := `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
    <modelVersion>4.0.0</modelVersion>
    <groupId>com.company</groupId>
    <artifactId>billing-service</artifactId>
    <version>0.0.1-SNAPSHOT</version>
</project>`
	if err := os.WriteFile(filepath.Join(javaDir, "pom.xml"), []byte(pomXML), 0644); err != nil {
		t.Fatalf("failed to write pom.xml: %v", err)
	}
	if name := DetectProjectName(javaDir); name != "billing-service" {
		t.Errorf("expected 'billing-service', got %q", name)
	}

	// 4. go.mod detection
	goDir := filepath.Join(tmpDir, "go-backend")
	_ = os.MkdirAll(goDir, 0755)
	goMod := `module github.com/specter-cli/specter-backend

go 1.25.0
`
	if err := os.WriteFile(filepath.Join(goDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}
	if name := DetectProjectName(goDir); name != "specter-backend" {
		t.Errorf("expected 'specter-backend', got %q", name)
	}

	// 5. pyproject.toml detection
	pyDir := filepath.Join(tmpDir, "ml-pipeline")
	_ = os.MkdirAll(pyDir, 0755)
	pyproject := `[project]
name = "data-trainer"
version = "0.1.0"
`
	if err := os.WriteFile(filepath.Join(pyDir, "pyproject.toml"), []byte(pyproject), 0644); err != nil {
		t.Fatalf("failed to write pyproject.toml: %v", err)
	}
	if name := DetectProjectName(pyDir); name != "data-trainer" {
		t.Errorf("expected 'data-trainer', got %q", name)
	}

	// 6. Root and system paths
	if name := DetectProjectName("/"); name != "" {
		t.Errorf("expected empty for root, got %q", name)
	}
	if name := DetectProjectName("/var"); name != "" {
		t.Errorf("expected empty for /var, got %q", name)
	}
}

func TestSanitizePath(t *testing.T) {
	origHome := os.Getenv("HOME")
	t.Cleanup(func() {
		os.Setenv("HOME", origHome)
	})

	os.Setenv("HOME", "/home/testuser")

	if p := SanitizePath("/home/testuser/projects/example"); p != "~/projects/example" {
		t.Errorf("expected '~/projects/example', got %q", p)
	}

	if p := SanitizePath("/mnt/c/data/app"); p != "/mnt/c/data/app" {
		t.Errorf("expected unchanged path, got %q", p)
	}
}
