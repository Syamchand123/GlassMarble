package akg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/git"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
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

// SerializeGraphDiffToTurtle converts a GraphDiff delta into a clean append-only RDF-star Turtle string (W2-06 / §6.4).
// It emits metadata only when version advances, avoiding header duplication.
func SerializeGraphDiffToTurtle(diff *GraphDiff, newVersion uint64) string {
	if diff == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n# --- Delta Append for Version %d ---\n", newVersion))
	for _, n := range diff.NodesRemoved {
		sb.WriteString(fmt.Sprintf("<http://glassmarble.org/node/%s> a <%sDeleted> .\n", types.ParseNodeURI(n.ID), ont.PrefixGM))
	}
	for _, n := range diff.NodesAdded {
		sb.WriteString(fmt.Sprintf("<http://glassmarble.org/node/%s> a <%s%s> ;\n", types.ParseNodeURI(n.ID), ont.PrefixGM, n.Kind))
		sb.WriteString(fmt.Sprintf("    <%sname> %q .\n", ont.PrefixGM, n.Name))
	}
	for _, e := range diff.EdgesAdded {
		sb.WriteString(fmt.Sprintf("<< <http://glassmarble.org/node/%s> <%s%s> <http://glassmarble.org/node/%s> >> <%slineNumber> %d .\n",
			types.ParseNodeURI(e.SourceID), ont.PrefixGM, strings.TrimPrefix(e.Type, ont.PrefixGM), types.ParseNodeURI(e.TargetID), ont.PrefixGM, e.Line))
	}
	return sb.String()
}
