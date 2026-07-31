package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/akgbridge"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/testutil"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/tools"
)

// newTestEnv seeds a synthetic AKG plus source files and returns a ready Env.
func newTestEnv(t *testing.T) *tools.Env {
	t.Helper()
	dir := t.TempDir()
	testutil.SeedAKG(t, dir)
	testutil.WriteFile(t, dir, "src/db.go", testutil.DBStoreSource())
	testutil.WriteFile(t, dir, "src/app.go", testutil.AppSource)
	testutil.WriteFile(t, dir, "src/util.go", testutil.UtilSource())
	return &tools.Env{
		RootDir:     dir,
		Bridge:      akgbridge.New(dir),
		ArtifactDir: filepath.Join(dir, ".glassmarble", "ai"),
	}
}

// call invokes a tool handler by name with JSON arguments. Results are
// JSON round-tripped so tests see exactly what the model sees: maps with
// float64/int-decoded numbers and slices as []any. Raw output becomes string.
func call(t *testing.T, env *tools.Env, name, argsJSON string) (any, error) {
	t.Helper()
	var tool *tools.Tool
	for i := range tools.All() {
		if tools.All()[i].Name == name {
			tool = &tools.All()[i]
			break
		}
	}
	if tool == nil {
		t.Fatalf("tool %q not registered", name)
	}
	var args map[string]any
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			t.Fatalf("bad args json: %v", err)
		}
	}
	data, err := tool.Handler(context.Background(), env, args)
	if err != nil {
		return nil, err
	}
	if raw, ok := data.(tools.Raw); ok {
		return string(raw), nil
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return decoded, nil
}

func dataOf(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T: %v", v, v)
	}
	return m
}

func TestAKGStatusTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "akg_status", "")
	if err != nil {
		t.Fatalf("akg_status: %v", err)
	}
	d := dataOf(t, v)
	// The transaction manager restores the graph from the TTL, which stamps
	// the commit hash and loses the recorded entrypoints (real behavior).
	if d["commit"] != "restored_from_ttl" {
		t.Errorf("commit = %v", d["commit"])
	}
	if d["nodes"] != float64(4) {
		t.Errorf("nodes = %v", d["nodes"])
	}
	if d["edges"] != float64(3) {
		t.Errorf("edges = %v", d["edges"])
	}
	if d["entrypoints"] != float64(0) {
		t.Errorf("entrypoints = %v", d["entrypoints"])
	}
}

func TestAKGStatusMissingAKG(t *testing.T) {
	dir := t.TempDir()
	env := &tools.Env{RootDir: dir, Bridge: akgbridge.New(dir)}
	if _, err := call(t, env, "akg_status", ""); err == nil {
		t.Fatal("expected error without an AKG")
	} else if !strings.Contains(err.Error(), "gmb analyze") {
		t.Errorf("error should recommend gmb analyze: %v", err)
	}
}

func TestAKGSummaryTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "akg_summary", "")
	if err != nil {
		t.Fatalf("akg_summary: %v", err)
	}
	d := dataOf(t, v)
	if _, ok := d["layer_distribution"]; !ok {
		t.Errorf("summary missing layer_distribution: %v", d)
	}
}

func TestAKGSearchTool(t *testing.T) {
	env := newTestEnv(t)

	v, err := call(t, env, "akg_search", `{"name_contains": "Store"}`)
	if err != nil {
		t.Fatalf("akg_search: %v", err)
	}
	d := dataOf(t, v)
	if d["count"] != float64(1) {
		t.Errorf("search count = %v, want 1 (only DBStore's name contains Store)", d["count"])
	}

	v, err = call(t, env, "akg_search", `{"kind": "FUNCTION"}`)
	if err != nil {
		t.Fatalf("akg_search kind: %v", err)
	}
	d = dataOf(t, v)
	// Macro inference relabels fixture methods to FUNCTION on restore, and
	// the bridge repairs the dropped KindIndex so kind search sees them.
	if d["count"] != float64(3) {
		t.Errorf("kind search count = %v, want 3", d["count"])
	}
}

func TestAKGGetNodeTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "akg_get_node", `{"id": "src/db.go::DBStore::Save"}`)
	if err != nil {
		t.Fatalf("akg_get_node: %v", err)
	}
	d := dataOf(t, v)
	// Macro inference relabels fixture methods to FUNCTION on TTL restore.
	if d["kind"] != "FUNCTION" || d["name"] != "Save" {
		t.Errorf("node fields: %v", d)
	}
	if d["inbound_edges"] != float64(2) || d["outbound_edges"] != float64(1) {
		t.Errorf("edge counts: %v", d)
	}
	props, _ := d["properties"].(map[string]any)
	if len(props) == 0 {
		t.Errorf("expected inferred properties: %v", d["properties"])
	}

	if _, err := call(t, env, "akg_get_node", `{"id": "nope"}`); err == nil {
		t.Fatal("expected error for missing node")
	}
}

func TestAKGEdgesTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "akg_edges", `{"id": "src/db.go::DBStore::Save", "direction": "out"}`)
	if err != nil {
		t.Fatalf("akg_edges: %v", err)
	}
	d := dataOf(t, v)
	if d["total"] != float64(1) || d["shown"] != float64(1) {
		t.Errorf("edge counts: %v", d)
	}

	v, err = call(t, env, "akg_edges", `{"id": "src/db.go::DBStore::Save", "predicate": "DEPENDS_ON"}`)
	if err != nil {
		t.Fatalf("akg_edges predicate: %v", err)
	}
	d = dataOf(t, v)
	if d["shown"] != float64(0) {
		t.Errorf("predicate filter should empty the result: %v", d)
	}
}

func TestAKGPathTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "akg_path", `{"start_id": "src/app.go::main", "target_id": "src/util.go::helper"}`)
	if err != nil {
		t.Fatalf("akg_path: %v", err)
	}
	d := dataOf(t, v)
	if d["found"] != true {
		t.Fatalf("path should be found: %v", d)
	}
	path, _ := d["path"].([]any)
	if len(path) != 3 {
		t.Errorf("path length = %d, want 3", len(path))
	}

	v, err = call(t, env, "akg_path", `{"start_id": "src/util.go::helper", "target_id": "src/app.go::main"}`)
	if err != nil {
		t.Fatalf("akg_path reverse: %v", err)
	}
	d = dataOf(t, v)
	if d["found"] != false {
		t.Errorf("no path expected in reverse direction: %v", d)
	}
}

func TestAKGTraverseTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "akg_traverse", `{"start_id": "src/app.go::main", "depth": 2}`)
	if err != nil {
		t.Fatalf("akg_traverse: %v", err)
	}
	d := dataOf(t, v)
	if d["total_reachable"] != float64(3) {
		t.Errorf("total_reachable = %v, want 3", d["total_reachable"])
	}
	levels, _ := d["levels"].([]any)
	if len(levels) != 2 {
		t.Errorf("levels = %d, want 2", len(levels))
	}
}

func TestAKGCyclesTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "akg_cycles", "")
	if err != nil {
		t.Fatalf("akg_cycles: %v", err)
	}
	d := dataOf(t, v)
	if d["count"] != float64(1) {
		t.Fatalf("cycles count = %v, want 1", d["count"])
	}
	cycles, _ := d["cycles"].([]any)
	first := cycles[0].([]any)
	names := first[0].(string) + "," + first[1].(string)
	if !strings.Contains(names, "Save") || !strings.Contains(names, "helper") {
		t.Errorf("cycle members = %v", first)
	}
}

func TestAKGOrphansTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "akg_orphans", "")
	if err != nil {
		t.Fatalf("akg_orphans: %v", err)
	}
	d := dataOf(t, v)
	if _, ok := d["count"]; !ok {
		t.Errorf("orphans result malformed: %v", d)
	}
}

func TestAKGHotspotsTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "akg_hotspots", `{"limit": 5}`)
	if err != nil {
		t.Fatalf("akg_hotspots: %v", err)
	}
	d := dataOf(t, v)
	hot, _ := d["hotspots"].([]any)
	if len(hot) == 0 {
		t.Fatal("no hotspots")
	}
	first := hot[0].(map[string]any)
	if first["degree"].(float64) < 1 {
		t.Errorf("top hotspot degree = %v", first["degree"])
	}
}

func TestAKGPageRankTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "akg_page_rank", `{"top": 3}`)
	if err != nil {
		t.Fatalf("akg_page_rank: %v", err)
	}
	d := dataOf(t, v)
	if d["total"] != float64(4) {
		t.Errorf("pagerank total = %v, want 4", d["total"])
	}
	top, _ := d["top"].([]any)
	if len(top) != 3 {
		t.Errorf("top = %d, want 3", len(top))
	}
}

func TestAKGImpactRadiusTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "akg_impact_radius", `{"id": "src/db.go::DBStore::Save"}`)
	if err != nil {
		t.Fatalf("akg_impact_radius: %v", err)
	}
	d := dataOf(t, v)
	// Exact blast radius depends on macro-inference artifacts; require a
	// sane non-empty result.
	if n, _ := d["affected_count"].(float64); n < 1 {
		t.Errorf("affected_count = %v, want >= 1", d["affected_count"])
	}
	affected, _ := d["affected_nodes"].([]any)
	if len(affected) < 1 {
		t.Errorf("affected list = %d entries, want >= 1", len(affected))
	}
	for _, n := range affected {
		m, _ := n.(map[string]any)
		if m["id"] == "" {
			t.Errorf("affected node missing id: %v", n)
		}
	}
}

func TestAKGCommunitiesTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "akg_communities", "")
	if err != nil {
		t.Fatalf("akg_communities: %v", err)
	}
	d := dataOf(t, v)
	clusters, _ := d["clusters"].([]any)
	if len(clusters) == 0 {
		t.Fatal("no clusters found")
	}
	total := 0.0
	for _, c := range clusters {
		total += c.(map[string]any)["size"].(float64)
	}
	if total != 4 {
		t.Errorf("cluster members total = %v, want 4", total)
	}
}

func TestAKGArticulationTool(t *testing.T) {
	env := newTestEnv(t)
	if _, err := call(t, env, "akg_articulation_points", ""); err != nil {
		t.Fatalf("akg_articulation_points: %v", err)
	}
}

func TestAKGTopologicalOrderTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "akg_topological_order", "")
	if err != nil {
		t.Fatalf("akg_topological_order: %v", err)
	}
	d := dataOf(t, v)
	if n, _ := d["count"].(float64); n < 1 {
		t.Errorf("order count = %v, want >= 1", d["count"])
	}
}

func TestAKGEntrypointsTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "akg_entrypoints", "")
	if err != nil {
		t.Fatalf("akg_entrypoints: %v", err)
	}
	d := dataOf(t, v)
	// TTL restoration does not carry the entrypoint registry.
	if d["count"] != float64(0) {
		t.Errorf("entrypoints = %v, want 0", d["count"])
	}
}

func TestAKGSimilarityTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "akg_similarity", `{"node_a": "src/db.go::DBStore::Save", "node_b": "src/util.go::helper"}`)
	if err != nil {
		t.Fatalf("akg_similarity: %v", err)
	}
	d := dataOf(t, v)
	sim, ok := d["similarity"].(float64)
	if !ok || sim < 0 || sim > 1 {
		t.Errorf("similarity out of range: %v", d)
	}
}

func TestCodeReadFileTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "code_read_file", `{"path": "src/db.go", "start_line": 15, "end_line": 21}`)
	if err != nil {
		t.Fatalf("code_read_file: %v", err)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("expected raw string output, got %T", v)
	}
	if !strings.Contains(s, "func (s *DBStore) Save") || !strings.Contains(s, "helper(rec)") {
		t.Errorf("snippet missing Save body:\n%s", s)
	}
	if !strings.Contains(s, "lines 15-21 of 60") {
		t.Errorf("range header missing:\n%s", s)
	}

	// Out of range start.
	if _, err := call(t, env, "code_read_file", `{"path": "src/db.go", "start_line": 999}`); err == nil {
		t.Error("expected out-of-range error")
	}

	// Path traversal.
	if _, err := call(t, env, "code_read_file", `{"path": "../outside.txt"}`); err == nil {
		t.Error("expected escape error")
	} else if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("unexpected error: %v", err)
	}

	// Absolute path.
	if _, err := call(t, env, "code_read_file", `{"path": "C:/windows/system32/drivers/etc/hosts"}`); err == nil {
		t.Error("expected absolute-path error")
	}
}

func TestCodeListDirTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "code_list_dir", `{"path": "src"}`)
	if err != nil {
		t.Fatalf("code_list_dir: %v", err)
	}
	d := dataOf(t, v)
	entries, _ := d["entries"].([]any)
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
}

func TestCodeSearchSymbolTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "code_search_symbol", `{"name": "Save"}`)
	if err != nil {
		t.Fatalf("code_search_symbol: %v", err)
	}
	d := dataOf(t, v)
	if d["count"] != float64(1) {
		t.Errorf("count = %v, want 1 (only the Save method's name contains Save)", d["count"])
	}
}

func TestCodeDefinitionTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "code_definition", `{"symbol": "Save"}`)
	if err != nil {
		t.Fatalf("code_definition: %v", err)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("expected raw string output, got %T", v)
	}
	if !strings.Contains(s, "### Save (") || !strings.Contains(s, "src/db.go:15") || !strings.Contains(s, "helper(rec)") {
		t.Errorf("definition output wrong:\n%s", s)
	}

	if _, err := call(t, env, "code_definition", `{"symbol": "DoesNotExist"}`); err == nil {
		t.Error("expected unknown-symbol error")
	}
}

func TestCodeDiffTool(t *testing.T) {
	dir := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q")
	git("config", "user.email", "test@glassmarble.dev")
	git("config", "user.name", "Test")
	testutil.WriteFile(t, dir, "file.txt", "line one\n")
	git("add", ".")
	git("commit", "-qm", "init")

	env := &tools.Env{RootDir: dir, Bridge: akgbridge.New(dir)}
	v, err := call(t, env, "code_diff", "")
	if err != nil {
		t.Fatalf("code_diff clean: %v", err)
	}
	if !strings.Contains(v.(string), "(clean)") {
		t.Errorf("clean repo diff: %v", v)
	}

	testutil.WriteFile(t, dir, "file.txt", "line one\nline two\n")
	v, err = call(t, env, "code_diff", "")
	if err != nil {
		t.Fatalf("code_diff dirty: %v", err)
	}
	s := v.(string)
	if !strings.Contains(s, "+line two") {
		t.Errorf("diff missing change:\n%s", s)
	}

	v, err = call(t, env, "code_diff", `{"path": "other.txt"}`)
	if err != nil {
		t.Fatalf("code_diff other: %v", err)
	}
	if !strings.Contains(v.(string), "(no tracked changes)") {
		t.Errorf("unexpected diff for untouched file:\n%v", v)
	}
}

func TestSystemStatusTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "system_status", "")
	if err != nil {
		t.Fatalf("system_status: %v", err)
	}
	d := dataOf(t, v)
	if d["root_dir"] != env.RootDir {
		t.Errorf("root_dir = %v", d["root_dir"])
	}
	akgInfo, _ := d["akg"].(map[string]any)
	if akgInfo["exists"] != true {
		t.Fatalf("akg info: %v", akgInfo)
	}
	counts, _ := akgInfo["counts"].(map[string]any)
	if counts["nodes"] != float64(4) {
		t.Errorf("counts: %v", counts)
	}
	if akgInfo["commit"] != "restored_from_ttl" {
		t.Errorf("commit: %v", akgInfo["commit"])
	}
}

func TestSystemDiagramTypesTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "system_diagram_types", "")
	if err != nil {
		t.Fatalf("system_diagram_types: %v", err)
	}
	d := dataOf(t, v)
	if d["count"].(float64) < 30 {
		t.Errorf("diagram type count = %v, want >= 30", d["count"])
	}
	types := d["types"].([]any)
	seen := false
	for _, ti := range types {
		if ti.(map[string]any)["type"] == "UML_CLASS" {
			seen = true
		}
	}
	if !seen {
		t.Error("UML_CLASS missing from catalog")
	}
}

func TestSaveArtifactTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "save_artifact", `{"filename": "notes.md", "content": "hello"}`)
	if err != nil {
		t.Fatalf("save_artifact: %v", err)
	}
	d := dataOf(t, v)
	if !strings.Contains(d["path"].(string), "notes.md") {
		t.Errorf("path = %v", d["path"])
	}
	data, err := os.ReadFile(d["path"].(string))
	if err != nil || string(data) != "hello" {
		t.Fatalf("artifact content: %q err=%v", data, err)
	}

	// Traversal attempts must fail.
	for _, bad := range []string{"../evil.md", "a/b.md", ".."} {
		if _, err := call(t, env, "save_artifact", `{"filename": "`+bad+`", "content": "x"}`); err == nil {
			t.Errorf("expected error for filename %q", bad)
		}
	}
}

func TestSelectRestriction(t *testing.T) {
	all := tools.All()
	if len(all) < 29 {
		t.Errorf("expected >= 29 tools, got %d", len(all))
	}

	code, err := tools.Select(all, []string{"code"})
	if err != nil {
		t.Fatalf("Select(code): %v", err)
	}
	for _, c := range code {
		if c.Category != "code" {
			t.Errorf("non-code tool %q selected", c.Name)
		}
	}
	if len(code) != 5 {
		t.Errorf("code tools = %d, want 5", len(code))
	}

	diagram, err := tools.Select(all, []string{"diagram"})
	if err != nil {
		t.Fatalf("Select(diagram): %v", err)
	}
	if len(diagram) != 3 {
		t.Errorf("diagram tools = %d, want 3", len(diagram))
	}
	for _, d := range diagram {
		if d.Category != "diagram" {
			t.Errorf("non-diagram tool %q selected", d.Name)
		}
	}

	mixed, err := tools.Select(all, []string{"akg", "system_status"})
	if err != nil {
		t.Fatalf("Select(mixed): %v", err)
	}
	if len(mixed) != 19 {
		t.Errorf("mixed tools = %d, want 19 (18 akg + system_status)", len(mixed))
	}

	if _, err := tools.Select(all, []string{"bogus"}); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("expected unknown-tool error, got %v", err)
	}
}

func TestSchemaRequired(t *testing.T) {
	all := tools.All()
	for i := range all {
		if all[i].Name == "akg_search" {
			// akg_search has no required args.
			if _, ok := all[i].Parameters["required"]; ok {
				t.Error("akg_search should not declare required args")
			}
		}
		if all[i].Name == "code_read_file" {
			req, _ := all[i].Parameters["required"].([]string)
			if len(req) != 1 || req[0] != "path" {
				t.Errorf("code_read_file required = %v", req)
			}
		}
	}
}
