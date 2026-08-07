package akg

import (
	"os"
	"path/filepath"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/git"
)

// IncrementalTracker manages append-only delta tracking and detects unchanged files (W2-06 / §6.4).
type IncrementalTracker struct {
	BaseDir      string
	LastCommit   string
	LastModified map[string]time.Time
}

// NewIncrementalTracker initializes a tracker for incremental analysis.
func NewIncrementalTracker(baseDir string) *IncrementalTracker {
	return &IncrementalTracker{
		BaseDir:      baseDir,
		LastModified: make(map[string]time.Time),
	}
}

// DetectUnchangedFiles queries git diff or filesystem timestamps to report unchanged source files.
func (it *IncrementalTracker) DetectUnchangedFiles(allFiles []string) (unchanged []string, modified []string) {
	if it.LastCommit != "" {
		changed, err := git.GetChangedFiles(it.BaseDir, it.LastCommit, "HEAD")
		if err == nil && len(changed) > 0 {
			changedMap := make(map[string]bool)
			for _, f := range changed {
				changedMap[f] = true
				changedMap[filepath.ToSlash(f)] = true
			}
			for _, f := range allFiles {
				rel, err := filepath.Rel(it.BaseDir, f)
				if err != nil {
					rel = f
				}
				rel = filepath.ToSlash(rel)
				if changedMap[rel] || changedMap[f] {
					modified = append(modified, f)
				} else {
					unchanged = append(unchanged, f)
				}
			}
			return unchanged, modified
		}
	}

	// Fallback to mtime comparison
	for _, f := range allFiles {
		info, err := os.Stat(f)
		if err != nil {
			modified = append(modified, f)
			continue
		}
		if last, ok := it.LastModified[f]; ok && info.ModTime().Equal(last) {
			unchanged = append(unchanged, f)
		} else {
			modified = append(modified, f)
		}
	}
	return unchanged, modified
}