package arch_intelligence

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// componentMergeThreshold is the minimum node count a directory group must
// have to survive as its own component; smaller groups merge into their
// nearest ancestor, which keeps component IDs stable across file moves.
const componentMergeThreshold = 3

// dominantFraction is the share of total nodes above which a single
// component is considered dominating and gets split by community detection.
const dominantFraction = 0.6

// InferComponents groups nodes into directory-anchored components with stable
// IDs (comp_{directory}). Compatibility wrapper for graph callers.
func InferComponents(graph *akg.CodePropertyGraph) []archmodel.DetectedComponent {
	if graph == nil {
		return nil
	}
	return InferComponentsFromSnapshot(NewGraphSnapshot(graph), nil, time.Now)
}

// InferComponentsFromSnapshot builds components from a snapshot using the
// given config (nil cfg means defaults). Components are anchored on canonical
// directories; small groups merge into their nearest ancestor; a dominating
// directory is split by Louvain communities. All output is deterministic.
func InferComponentsFromSnapshot(snap *GraphSnapshot, cfg *config.IntelligenceConfig, clock func() time.Time) []archmodel.DetectedComponent {
	if snap == nil {
		return nil
	}
	if clock == nil {
		clock = time.Now
	}
	excluded := make(map[string]bool)
	if cfg != nil {
		for _, d := range cfg.ArchExcludedDirs {
			excluded[normalizeDir(d)] = true
		}
	}

	// 1. Partition nodes by canonical directory (relative to the common
	// project root so IDs are stable across machines).
	root := commonProjectRoot(snap)
	dirNodes := make(map[string][]string)
	for _, id := range snap.NodeIDs {
		node := snap.Nodes[id]
		if node == nil || node.FileSpec.Path == "" {
			continue
		}
		dir := canonicalDir(node.FileSpec.Path, root)
		if dir == "" || excluded[dir] || dirExcluded(excluded, dir) {
			continue
		}
		dirNodes[dir] = append(dirNodes[dir], id)
	}

	// 2. Merge small groups into their nearest existing ancestor.
	mergeSmallGroups(dirNodes)

	// 3. Build components in sorted directory order.
	dirs := make([]string, 0, len(dirNodes))
	for d := range dirNodes {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	var components []archmodel.DetectedComponent
	for _, d := range dirs {
		ids := dirNodes[d]
		sort.Strings(ids)
		components = append(components, newComponent(d, d, ids, clock))
	}

	// 4. Dominance split: one directory holding the bulk of the graph is
	// probably several logical units — split it by community detection.
	if total := snap.Len(); total >= 20 {
		largest, largestIdx := 0, -1
		for i, c := range components {
			if len(c.NodeIDs) > largest {
				largest = len(c.NodeIDs)
				largestIdx = i
			}
		}
		if largestIdx >= 0 && float64(largest)/float64(total) > dominantFraction {
			components = splitDominantComponent(snap, components, largestIdx, clock)
		}
	}

	// 5. Compute component-level dependencies (needed by event generation).
	computeComponentDependencies(snap, components)

	return components
}

func dirExcluded(excluded map[string]bool, dir string) bool {
	for prefix := range excluded {
		if strings.HasPrefix(dir, prefix+"/") {
			return true
		}
	}
	return false
}

// isAbsPath reports whether p is an absolute (machine-specific) path.
// Relative paths are already project-relative and must not be stripped.
func isAbsPath(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	return len(p) >= 3 && p[1] == ':' && (p[2] == '/' || p[2] == '\\')
}

// commonProjectRoot finds the longest directory prefix shared by all file
// paths ("" when paths have no common directory). Only absolute paths are
// reduced to a common root so IDs stay stable across machines; relative
// paths keep their full project-relative directory in component IDs.
func commonProjectRoot(snap *GraphSnapshot) string {
	var common string
	first := true
	for _, id := range snap.NodeIDs {
		node := snap.Nodes[id]
		if node == nil {
			continue
		}
		p := normalizeDir(node.FileSpec.Path)
		if p == "" {
			continue
		}
		if !isAbsPath(p) {
			// Relative paths are already project-relative; stripping a
			// shared prefix (e.g. "internal") would destabilize IDs.
			return ""
		}
		if first {
			common = p
			first = false
			continue
		}
		for !strings.HasPrefix(p, common) {
			idx := strings.LastIndex(common, "/")
			if idx <= 0 {
				common = ""
				break
			}
			common = common[:idx]
		}
		if common == "" {
			break
		}
	}
	// Only treat the prefix as a root when at least two paths share it;
	// a single path has no meaningful common root.
	shared := 0
	for _, id := range snap.NodeIDs {
		node := snap.Nodes[id]
		if node != nil && common != "" && strings.HasPrefix(normalizeDir(node.FileSpec.Path), common) {
			shared++
		}
	}
	if shared < 2 || common == "" {
		return ""
	}
	return common
}

// canonicalDir returns the directory of path relative to root, or "" when the
// path lies outside the root. "." is returned for root-level files.
func canonicalDir(path, root string) string {
	clean := normalizeDir(path)
	if root != "" {
		if clean == root {
			return "."
		}
		if !strings.HasPrefix(clean, root+"/") {
			return ""
		}
		clean = strings.TrimPrefix(clean, root+"/")
	}
	return dirSlash(clean)
}

// dirSlash returns the directory of a forward-slash path without ever going
// through filepath (which converts separators back to OS-native backslashes
// on Windows and would break component IDs).
func dirSlash(p string) string {
	idx := strings.LastIndex(p, "/")
	if idx < 0 {
		return "."
	}
	if idx == 0 {
		return "."
	}
	return p[:idx]
}

// normalizeDir converts a path to a forward-slash path without leading "./".
func normalizeDir(p string) string {
	clean := strings.ReplaceAll(p, "\\", "/")
	clean = strings.TrimPrefix(clean, "./")
	clean = strings.TrimSuffix(clean, "/")
	return clean
}

// parentDir returns the parent of dir ("" when dir has no parent).
func parentDir(dir string) string {
	if dir == "" || dir == "." {
		return ""
	}
	idx := strings.LastIndex(dir, "/")
	if idx < 0 {
		return "."
	}
	if idx == 0 {
		return "."
	}
	return dir[:idx]
}

// mergeSmallGroups merges directory groups with fewer than
// componentMergeThreshold nodes into their nearest ancestor group.
func mergeSmallGroups(dirNodes map[string][]string) {
	changed := true
	for changed {
		changed = false
		dirs := make([]string, 0, len(dirNodes))
		for d := range dirNodes {
			dirs = append(dirs, d)
		}
		sort.Strings(dirs)
		for _, d := range dirs {
			if len(dirNodes[d]) >= componentMergeThreshold {
				continue
			}
			anc := nearestAncestor(dirNodes, d)
			if anc == "" {
				continue
			}
			if anc == d {
				continue
			}
			dirNodes[anc] = append(dirNodes[anc], dirNodes[d]...)
			delete(dirNodes, d)
			changed = true
			break
		}
	}
}

// nearestAncestor returns the nearest ancestor of dir that either already has
// a group or would become one (immediate parent). "" means no parent exists.
func nearestAncestor(dirNodes map[string][]string, dir string) string {
	if dir == "" || dir == "." {
		return ""
	}
	p := parentDir(dir)
	if p == "" {
		return ""
	}
	for {
		if _, ok := dirNodes[p]; ok {
			return p
		}
		if p == "." {
			return ""
		}
		np := parentDir(p)
		if np == p {
			return ""
		}
		p = np
	}
}

// sanitizeID converts a directory path into a stable identifier fragment.
var idSanitizer = regexp.MustCompile(`[^A-Za-z0-9_\-]+`)

func sanitizeID(dir string) string {
	parts := strings.Split(dir, "/")
	cleanParts := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		p = idSanitizer.ReplaceAllString(p, "_")
		if p != "" {
			cleanParts = append(cleanParts, p)
		}
	}
	return strings.Join(cleanParts, "_")
}

func newComponent(idDir, nameDir string, ids []string, clock func() time.Time) archmodel.DetectedComponent {
	name := nameDir
	if name == "." {
		name = "root"
	}
	componentID := "comp_" + sanitizeID(idDir)
	b := evidence.Bundle{PrimarySource: evidence.SourceRule}
	b.Add(evidence.EvidenceItem{
		Source:     evidence.SourceRule,
		Reference:  "COMPONENT_INFERENCE",
		Excerpt:    "Directory-anchored component (" + name + ") with " + strconv.Itoa(len(ids)) + " nodes.",
		Confidence: 0.8,
		Timestamp:  clock(),
	})
	return archmodel.DetectedComponent{
		ID:          componentID,
		Name:        name,
		Kind:        archmodel.ComponentModule,
		NodeIDs:     ids,
		Directories: []string{nameDir},
		Confidence:  0.8,
		Evidence:    b,
	}
}

// splitDominantComponent replaces the dominant component with
// community-based sub-components whose names keep the directory prefix.
func splitDominantComponent(snap *GraphSnapshot, components []archmodel.DetectedComponent, dominantIdx int, clock func() time.Time) []archmodel.DetectedComponent {
	dom := components[dominantIdx]
	comms := LouvainCommunityDetectionSnapshot(snap, 2, 2)

	byComm := make(map[string][]string)
	for _, id := range dom.NodeIDs {
		c := comms[id]
		byComm[c] = append(byComm[c], id)
	}
	commsSorted := make([]string, 0, len(byComm))
	for c := range byComm {
		commsSorted = append(commsSorted, c)
	}
	sort.Strings(commsSorted)

	var remaining []string
	var subs []archmodel.DetectedComponent
	for _, c := range commsSorted {
		ids := byComm[c]
		sort.Strings(ids)
		if len(ids) < 2 {
			remaining = append(remaining, ids...)
			continue
		}
		idDir := dom.Directories[0] + "/__comm_" + c
		sub := newComponent(idDir, dom.Directories[0]+" ["+c+"]", ids, clock)
		sub.Directories = []string{dom.Directories[0]}
		subs = append(subs, sub)
	}
	if len(subs) == 0 {
		return components
	}
	// Keep the original component for its remaining nodes only if non-empty.
	out := make([]archmodel.DetectedComponent, 0, len(components)+len(subs))
	out = append(out, components[:dominantIdx]...)
	if len(remaining) > 0 {
		dom.NodeIDs = remaining
		out = append(out, dom)
	}
	out = append(out, subs...)
	out = append(out, components[dominantIdx+1:]...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// computeComponentDependencies fills each component's Dependencies list with
// the IDs of the distinct components it depends on.
func computeComponentDependencies(snap *GraphSnapshot, components []archmodel.DetectedComponent) {
	nodeToComp := make(map[string]string, snap.Len())
	for _, c := range components {
		for _, id := range c.NodeIDs {
			nodeToComp[id] = c.ID
		}
	}
	deps := make(map[string]map[string]bool, len(components))
	for _, id := range snap.NodeIDs {
		srcComp, ok := nodeToComp[id]
		if !ok {
			continue
		}
		for _, e := range snap.structuralOutbound(id) {
			tgtComp, ok := nodeToComp[e.TargetID]
			if !ok || tgtComp == srcComp {
				continue
			}
			if deps[srcComp] == nil {
				deps[srcComp] = make(map[string]bool)
			}
			deps[srcComp][tgtComp] = true
		}
	}
	for i := range components {
		ids := make([]string, 0, len(deps[components[i].ID]))
		for d := range deps[components[i].ID] {
			ids = append(ids, d)
		}
		sort.Strings(ids)
		components[i].Dependencies = ids
	}
}
