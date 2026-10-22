package resolver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	pomArtifactIDRegex = regexp.MustCompile(`<artifactId>\s*([^<\s]+)\s*</artifactId>`)
	tomlNameRegex      = regexp.MustCompile(`(?m)^\s*name\s*=\s*["']([^"']+)["']`)
	goModModuleRegex   = regexp.MustCompile(`(?m)^\s*module\s+([^\s\r\n]+)`)
)

// DetectProjectName extracts a developer-friendly project name from a directory.
// It checks standard manifests (package.json, pom.xml, go.mod, pyproject.toml, Cargo.toml)
// before falling back to the directory's base name.
func DetectProjectName(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" || dir == "/" || dir == "." {
		return ""
	}

	cleanDir := filepath.Clean(dir)
	base := filepath.Base(cleanDir)

	// Filter out system or generic directories
	lowerBase := strings.ToLower(base)
	if lowerBase == "root" || lowerBase == "home" || lowerBase == "tmp" || lowerBase == "var" || lowerBase == "usr" || lowerBase == "bin" || lowerBase == "etc" {
		return ""
	}

	// 1. Check package.json (Node.js)
	pkgPath := filepath.Join(cleanDir, "package.json")
	if data, err := readHead(pkgPath, 4096); err == nil {
		var pkg struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(data, &pkg) == nil && pkg.Name != "" {
			name := pkg.Name
			if idx := strings.LastIndex(name, "/"); idx != -1 {
				name = name[idx+1:]
			}
			return strings.TrimSpace(name)
		}
	}

	// 2. Check pom.xml (Java / Spring Boot)
	pomPath := filepath.Join(cleanDir, "pom.xml")
	if data, err := readHead(pomPath, 8192); err == nil {
		matches := pomArtifactIDRegex.FindSubmatch(data)
		if len(matches) >= 2 {
			return string(matches[1])
		}
	}

	// 3. Check go.mod (Go)
	goModPath := filepath.Join(cleanDir, "go.mod")
	if data, err := readHead(goModPath, 4096); err == nil {
		matches := goModModuleRegex.FindSubmatch(data)
		if len(matches) >= 2 {
			modPath := string(matches[1])
			if idx := strings.LastIndex(modPath, "/"); idx != -1 {
				return modPath[idx+1:]
			}
			return modPath
		}
	}

	// 4. Check pyproject.toml (Python)
	pyprojectPath := filepath.Join(cleanDir, "pyproject.toml")
	if data, err := readHead(pyprojectPath, 4096); err == nil {
		matches := tomlNameRegex.FindSubmatch(data)
		if len(matches) >= 2 {
			return string(matches[1])
		}
	}

	// 5. Check Cargo.toml (Rust)
	cargoPath := filepath.Join(cleanDir, "Cargo.toml")
	if data, err := readHead(cargoPath, 4096); err == nil {
		matches := tomlNameRegex.FindSubmatch(data)
		if len(matches) >= 2 {
			return string(matches[1])
		}
	}

	// Fallback to directory basename
	return base
}

// SanitizePath returns a user-friendly path display replacing home directory with '~'.
func SanitizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}

	home := os.Getenv("HOME")
	if home != "" && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}

	return path
}

func readHead(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf := make([]byte, maxBytes)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}
