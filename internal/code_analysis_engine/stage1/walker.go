package stage1

import (
	"context"
	"io/fs"
	"os"
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

// Discover walks rootDir and streams the discovered file paths that map to a known
// language grammar to the provided channel. It respects context cancellation.
//
// Files larger than cfg.MaxFileBytes are skipped with a recorded warning so
// giant generated blobs do not stall the parser pool.
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

		fi, statErr := os.Stat(p)
		if statErr == nil && cfg.MaxFileBytes > 0 && fi.Size() > cfg.MaxFileBytes {
			skipWarnCh <- p + " (exceeds MaxFileBytes)"
			return nil
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
