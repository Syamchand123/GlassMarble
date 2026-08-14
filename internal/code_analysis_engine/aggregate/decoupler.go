package aggregate

import (
	"path/filepath"
	"strings"
)

// NormalizeRelativePath cleans and converts any host/OS file path into a clean, slash-separated relative path.
func NormalizeRelativePath(rawPath string) string {
	if rawPath == "" {
		return ""
	}

	// Replace Windows backslashes with forward slashes
	cleaned := strings.ReplaceAll(rawPath, "\\", "/")

	// Strip drive letter prefixes if present (e.g. "C:/")
	if len(cleaned) >= 2 && cleaned[1] == ':' {
		cleaned = cleaned[2:]
	}

	// Trim leading slashes and "./"
	cleaned = strings.TrimPrefix(cleaned, "/")
	cleaned = strings.TrimPrefix(cleaned, "./")
	cleaned = strings.TrimPrefix(cleaned, "/")

	// Clean path using filepath, then back to slash
	cleaned = filepath.ToSlash(filepath.Clean(cleaned))
	if cleaned == "." {
		return ""
	}
	return cleaned
}

// SplitPathToDirectories breaks a relative file path into an ordered slice of directory names and the final filename.
// Example: "src/core/database/postgres.go" -> directories: ["src", "core", "database"], fileName: "postgres.go"
// Example: "main.go" -> directories: [], fileName: "main.go"
func SplitPathToDirectories(relPath string) (dirs []string, fileName string) {
	clean := NormalizeRelativePath(relPath)
	if clean == "" {
		return nil, ""
	}

	parts := strings.Split(clean, "/")
	if len(parts) == 1 {
		return nil, parts[0]
	}

	dirs = parts[:len(parts)-1]
	fileName = parts[len(parts)-1]
	return dirs, fileName
}
