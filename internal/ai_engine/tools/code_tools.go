package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// codeTools builds the source-reading tools that ground the agent in real
// code. Paths are always resolved against the workspace root and can never
// escape it.
func codeTools() []Tool {
	return []Tool{
		{
			Name:        "code_read_file",
			Description: "Read a source file (or a line range of it) from the workspace, with line numbers.",
			Category:    CategoryCode,
			Parameters: Schema(map[string]Prop{
				"path":       {Type: "string", Description: "Repo-relative file path, e.g. \"src/db.go\"", Required: true},
				"start_line": {Type: "integer", Description: "First line to read (1-based, default 1)", Default: float64(1)},
				"end_line":   {Type: "integer", Description: "Last line to read (default: to end of file, max 300 lines per call)"},
				"max_lines":  {Type: "integer", Description: "Max lines to return (default 300, max 1000)", Default: float64(300)},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				rel := strArg(args, "path", "")
				full, err := safeJoin(env.RootDir, rel)
				if err != nil {
					return nil, err
				}
				fi, err := os.Stat(full)
				if err != nil {
					return nil, fmt.Errorf("cannot read %q: %v", rel, err)
				}
				if fi.IsDir() {
					return nil, fmt.Errorf("%q is a directory — use code_list_dir", rel)
				}
				data, err := os.ReadFile(full)
				if err != nil {
					return nil, fmt.Errorf("cannot read %q: %v", rel, err)
				}
				lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
				if len(lines) > 0 && lines[len(lines)-1] == "" {
					lines = lines[:len(lines)-1]
				}
				start := intArg(args, "start_line", 1, 1, 1<<30)
				if start > len(lines) {
					return nil, fmt.Errorf("start_line %d is out of range — %q has %d lines", start, rel, len(lines))
				}
				maxLines := intArg(args, "max_lines", 300, 1, 1000)
				end := len(lines)
				if _, ok := args["end_line"]; ok {
					end = intArg(args, "end_line", end, 1, 1<<30)
					if end < start {
						return nil, fmt.Errorf("end_line %d is before start_line %d", end, start)
					}
					if end > len(lines) {
						end = len(lines)
					}
				}
				if end-start+1 > maxLines {
					end = start + maxLines - 1
				}

				var sb strings.Builder
				fmt.Fprintf(&sb, "### %s (lines %d-%d of %d)\n", rel, start, end, len(lines))
				for i := start; i <= end; i++ {
					fmt.Fprintf(&sb, "%6d | %s\n", i, lines[i-1])
				}
				return Raw(sb.String()), nil
			},
		},
		{
			Name:        "code_list_dir",
			Description: "List the entries of a workspace directory (directories first, then files).",
			Category:    CategoryCode,
			Parameters: Schema(map[string]Prop{
				"path": {Type: "string", Description: "Repo-relative directory path (default \".\" = workspace root)", Default: "."},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				rel := strArg(args, "path", ".")
				full, err := safeJoin(env.RootDir, rel)
				if err != nil {
					return nil, err
				}
				entries, err := os.ReadDir(full)
				if err != nil {
					return nil, fmt.Errorf("cannot list %q: %v", rel, err)
				}
				type entry struct {
					Name  string `json:"name"`
					IsDir bool   `json:"is_dir"`
					Size  int64  `json:"size_bytes,omitempty"`
				}
				out := make([]entry, 0, len(entries))
				for _, e := range entries {
					if len(out) >= 200 {
						break
					}
					info, err := e.Info()
					if err != nil {
						continue
					}
					out = append(out, entry{Name: e.Name(), IsDir: e.IsDir(), Size: info.Size()})
				}
				sort.Slice(out, func(i, j int) bool {
					if out[i].IsDir != out[j].IsDir {
						return out[i].IsDir
					}
					return out[i].Name < out[j].Name
				})
				return map[string]any{"path": rel, "count": len(out), "entries": out}, nil
			},
		},
		{
			Name:        "code_search_symbol",
			Description: "Search the AKG for nodes whose name contains the given text (case-insensitive). Use when you need the exact node ID or location of a symbol.",
			Category:    CategoryCode,
			Parameters: Schema(map[string]Prop{
				"name":  {Type: "string", Description: "Symbol name substring, e.g. \"Save\" or \"Postgres\"", Required: true},
				"kind":  {Type: "string", Description: "Optional exact node kind filter"},
				"limit": {Type: "integer", Description: "Max results (default 20, max 50)", Default: float64(20)},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					name := strArg(args, "name", "")
					filter := link.QueryFilter{
						NameContains: name,
						Kind:         strArg(args, "kind", ""),
					}
					all := snap.Query(filter)
					if all == nil {
						all = []*link.ResolvedNode{}
					}
					limit := intArg(args, "limit", 20, 1, 50)
					out := make([]nodeBrief, 0, min(limit, len(all)))
					for _, n := range all {
						if len(out) >= limit {
							break
						}
						out = append(out, brief(n))
					}
					return map[string]any{"count": len(all), "nodes": out}, nil
				})
			},
		},
		{
			Name:        "code_definition",
			Description: "Locate a symbol in the AKG and read its defining source lines from disk.",
			Category:    CategoryCode,
			Parameters: Schema(map[string]Prop{
				"symbol": {Type: "string", Description: "Exact symbol name, e.g. \"Save\" or \"DBStore\"", Required: true},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				symbol := strArg(args, "symbol", "")
				var node *link.ResolvedNode
				_, err := withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					all := snap.Query(link.QueryFilter{NameContains: symbol, Limit: 50})
					if all == nil {
						all = []*link.ResolvedNode{}
					}
					var exact []*link.ResolvedNode
					for _, n := range all {
						if n.Name == symbol {
							exact = append(exact, n)
						}
					}
					if len(exact) == 0 {
						return nil, fmt.Errorf("no AKG node named %q — try code_search_symbol", symbol)
					}
					// Prefer the most specific kinds.
					for _, kind := range []string{"FUNCTION", "METHOD", "STRUCT", "CLASS", "INTERFACE", "MODULE"} {
						for _, n := range exact {
							if n.Kind == kind {
								node = n
								return nil, nil
							}
						}
					}
					node = exact[0]
					return nil, nil
				})
				if err != nil {
					return nil, err
				}
				if node.FileSpec.Path == "" || node.FileSpec.LineStart <= 0 {
					return nil, fmt.Errorf("node %q has no source location in the AKG", symbol)
				}
				full, err := safeJoin(env.RootDir, node.FileSpec.Path)
				if err != nil {
					return nil, err
				}
				data, err := os.ReadFile(full)
				if err != nil {
					return nil, fmt.Errorf("cannot read source of %q at %s: %v", symbol, node.FileSpec.Path, err)
				}
				lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
				start := node.FileSpec.LineStart
				end := node.FileSpec.LineEnd
				if end <= start {
					end = start
				}
				if end-start+1 > 100 {
					end = start + 99
				}
				if end > len(lines) {
					end = len(lines)
				}
				var sb strings.Builder
				fmt.Fprintf(&sb, "### %s (%s)\n", node.Name, node.Kind)
				fmt.Fprintf(&sb, "ID: %s\n", node.ID)
				fmt.Fprintf(&sb, "Path: %s:%d\n", node.FileSpec.Path, node.FileSpec.LineStart)
				if len(node.Properties) > 0 {
					fmt.Fprintf(&sb, "Properties: %s\n", sortedProps(node.Properties))
				}
				for i := start; i <= end; i++ {
					fmt.Fprintf(&sb, "%6d | %s\n", i, lines[i-1])
				}
				return Raw(sb.String()), nil
			},
		},
		{
			Name:        "code_diff",
			Description: "Working-tree changes: `git status` plus `git diff` for the whole repo or one file. Use for impact and change questions.",
			Category:    CategoryCode,
			Parameters: Schema(map[string]Prop{
				"path":    {Type: "string", Description: "Optional repo-relative path to diff only that file"},
				"context": {Type: "integer", Description: "Diff context lines (default 3)", Default: float64(3)},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				context := intArg(args, "context", 3, 0, 50)
				diffArgs := []string{"-C", env.RootDir, "diff", "--no-color", "--unified=" + fmt.Sprintf("%d", context)}
				if rel := strArg(args, "path", ""); rel != "" {
					if _, err := safeJoin(env.RootDir, rel); err != nil {
						return nil, err
					}
					diffArgs = append(diffArgs, "--", filepath.FromSlash(rel))
				}
				diff, err := exec.CommandContext(ctx, "git", diffArgs...).Output()
				if err != nil {
					return nil, fmt.Errorf("git diff failed: %v (is %s a git repository?)", err, env.RootDir)
				}
				status, err := exec.CommandContext(ctx, "git", "-C", env.RootDir, "status", "--porcelain").Output()
				if err != nil {
					status = []byte{}
				}
				var sb strings.Builder
				sb.WriteString("WORKING TREE STATUS (git status --porcelain):\n")
				statusText := strings.TrimSpace(string(status))
				if statusText == "" {
					statusText = "(clean)"
				}
				sb.WriteString(statusText)
				sb.WriteString("\n\nDIFF (git diff):\n")
				diffText := strings.TrimSpace(string(diff))
				if diffText == "" {
					diffText = "(no tracked changes)"
				}
				sb.WriteString(diffText)
				sb.WriteString("\n")
				return Raw(sb.String()), nil
			},
		},
	}
}

// safeJoin resolves rel (a repo-relative path) against root and rejects any
// path that would escape the workspace.
func safeJoin(root, rel string) (string, error) {
	if rel == "" {
		rel = "."
	}
	cleaned := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("absolute paths are not allowed: %q", rel)
	}
	joined := filepath.Join(root, cleaned)
	r, err := filepath.Rel(root, joined)
	if err != nil {
		return "", fmt.Errorf("invalid path %q: %v", rel, err)
	}
	if r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the workspace", rel)
	}
	return joined, nil
}

func sortedProps(props map[string]string) string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+props[k])
	}
	return strings.Join(parts, ", ")
}
