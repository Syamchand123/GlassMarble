package stage1

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Filter directories that never contain source code worth analysing.
// These are pattern-matched against base names so they work on every OS.
var defaultSkipDirs = map[string]struct{}{
	".git": {}, ".hg": {}, ".svn": {},
	"node_modules": {}, "bower_components": {}, "jspm_packages": {},
	"vendor": {}, "third_party": {}, "thirdparty": {}, "extern": {},
	"dist": {}, "build": {}, "out": {}, "target": {}, "bin": {}, "obj": {},
	".idea": {}, ".vscode": {}, ".gradle": {}, ".terraform": {},
	"__pycache__": {}, ".mypy_cache": {}, ".pytest_cache": {}, ".tox": {},
	"venv": {}, ".venv": {}, "env": {}, ".env": {},
	"coverage": {}, ".coverage": {}, ".nyc_output": {},
}

// generatedFileSuffixes match files produced by code generators (protobuf,
// swagger, mockgen, bindata, minified bundles). They carry no architecture
// signal and previously polluted the graph (AUDIT Issue 1.8 / Phase 1C-9).
var generatedFileSuffixes = []string{
	".pb.go", "_grpc.pb.go", ".pb.h", ".pb.cc", ".pb.c", "_pb2.py", "_pb2_grpc.py",
	".generated.go", ".gen.go", "_gen.go", "_mock.go", "_mock_test.go",
	".swagger.json", ".min.js", ".min.css", "bindata.go", ".d.ts.map", ".js.map",
}

func isGeneratedFile(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range generatedFileSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// Discover walks rootDir and streams the discovered file paths that map to a known
// language grammar to the provided channel. It respects context cancellation.
//
// Files larger than cfg.MaxFileBytes are skipped with a recorded warning so
// giant generated blobs do not stall the parser pool. Generated files and
// (optionally) untracked files are skipped as well.
func Discover(cfg Config, pathCh chan<- string, errorCh chan<- error, skipWarnCh chan<- string) {
	defer close(pathCh)
	defer close(errorCh)
	defer close(skipWarnCh)

	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	abs, err := filepath.Abs(cfg.RootDir)
	if err != nil {
		errorCh <- err
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		errorCh <- err
		return
	}
	if !info.IsDir() {
		errorCh <- fs.ErrInvalid
		return
	}

	reg := Registry()

	// Optional git-tracked filter: build the tracked-file set once before
	// walking. Falls back to scanning everything if git is unavailable.
	var tracked map[string]bool
	if cfg.GitTrackedOnly {
		tracked = collectGitTrackedFiles(abs, skipWarnCh)
	}

	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, walkErr error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if walkErr != nil {
			skipWarnCh <- p + ": " + walkErr.Error()
			return nil
		}

		if d.IsDir() {
			name := d.Name()
			if !cfg.IncludeHidden && strings.HasPrefix(name, ".") && p != abs {
				return filepath.SkipDir
			}
			if _, skip := defaultSkipDirs[name]; skip {
				return filepath.SkipDir
			}
			return nil
		}

		if !d.Type().IsRegular() {
			return nil
		}

		if !cfg.IncludeHidden && strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		if isGeneratedFile(d.Name()) {
			skipWarnCh <- p + " (generated file)"
			return nil
		}

		fi, statErr := os.Stat(p)
		if statErr == nil && cfg.MaxFileBytes > 0 && fi.Size() > cfg.MaxFileBytes {
			skipWarnCh <- p + " (exceeds MaxFileBytes)"
			return nil
		}

		if tracked != nil {
			rel, relErr := filepath.Rel(abs, p)
			if relErr == nil {
				rel = filepath.ToSlash(rel)
				if !tracked[rel] && !tracked[p] {
					skipWarnCh <- p + " (untracked by git)"
					return nil
				}
			}
		}

		if _, _, ok := DetectLanguage(p, reg); ok {
			pathCh <- p
		}
		return nil
	})

	if err != nil {
		select {
		case <-ctx.Done():
		default:
			errorCh <- err
		}
	}
}

// collectGitTrackedFiles runs `git ls-files` in rootDir and returns the set of
// tracked relative paths (slash-normalized). Returns nil when git is not
// available so the caller falls back to an unfiltered scan.
func collectGitTrackedFiles(rootDir string, skipWarnCh chan<- string) map[string]bool {
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = rootDir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		skipWarnCh <- rootDir + ": git ls-files failed (" + err.Error() + "); scanning untracked files"
		return nil
	}
	set := make(map[string]bool)
	for _, f := range strings.Split(out.String(), "\x00") {
		if f != "" {
			set[filepath.ToSlash(f)] = true
		}
	}
	return set
}
