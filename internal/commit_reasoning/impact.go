package commit_reasoning

import (
	"path"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// ResolveImpact maps the files a commit touched onto the components of the
// head snapshot, returning the affected component names. Resolution order:
//
//  1. Exact match of a file path against a component's Directory.
//  2. Longest directory prefix match (nested trees resolve to their deepest
//     owner).
//  3. Component whose NodeIDs share the file's node ID (graph-level).
//  4. Component-name token overlap with the file path ("auth-service" and
//     "internal/auth/..." both contain "auth") — a heuristic, never the
//     deciding factor on its own.
//
// The result is sorted and deduplicated, and empty for unknown files.
func ResolveImpact(files []string, snap *archmodel.ArchSnapshot) []string {
	if snap == nil || len(files) == 0 {
		return nil
	}
	byDir := make(map[string]string) // directory -> component name
	for _, c := range snap.Components {
		for _, d := range c.Directories {
			byDir[strings.TrimSuffix(path.Clean(d), "/")] = c.Name
		}
	}
	byNode := make(map[string]string)
	for _, c := range snap.Components {
		for _, id := range c.NodeIDs {
			byNode[id] = c.Name
		}
	}
	byToken := make(map[string]string)
	for _, c := range snap.Components {
		for _, tok := range nameTokensOf(c.Name) {
			if _, dup := byToken[tok]; !dup {
				byToken[tok] = c.Name
			}
		}
	}

	hit := make(map[string]bool)
	var out []string
	record := func(name string) {
		if name != "" && !hit[name] {
			hit[name] = true
			out = append(out, name)
		}
	}
	for _, f := range files {
		clean := path.Clean(f)
		if name, ok := byDir[clean]; ok {
			record(name)
			continue
		}
		if name, ok := longestPrefixDir(byDir, clean); ok {
			record(name)
			continue
		}
		if name, ok := byNode[f]; ok {
			record(name)
			continue
		}
		for _, tok := range nameTokensOf(clean) {
			if name, ok := byToken[tok]; ok {
				record(name)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// longestPrefixDir finds the component whose directory is the longest prefix
// of the file path (deepest owner wins).
func longestPrefixDir(byDir map[string]string, file string) (string, bool) {
	best, bestLen := "", 0
	for dir := range byDir {
		if strings.HasPrefix(file, dir) && len(dir) > bestLen &&
			(len(file) == len(dir) || file[len(dir)] == '/' || file[len(dir)] == '\\') {
			best, bestLen = byDir[dir], len(dir)
		}
	}
	return best, bestLen > 0
}

// nameTokensOf splits a path/name into lowercase alphanumeric tokens
// ("internal/auth/v1" → [internal auth v1]).
func nameTokensOf(s string) []string {
	var out []string
	var cur strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
