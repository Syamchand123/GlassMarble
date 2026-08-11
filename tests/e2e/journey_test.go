package e2e_test

// TestEndToEndUserJourney is the full product journey through the real CLI.
// One shared sandbox keeps the expensive analyses amortized: the whole
// journey runs two `gmb analyze` invocations and every other command reads
// the resulting state. All commands execute IN PROCESS via
// cmd.RootCmdForTesting() (the harness pattern), so nothing in this file may
// call t.Parallel().

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

func TestEndToEndUserJourney(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.RequireGit()

	// --- 1. init: workspace skeleton -------------------------------------
	t.Run("init", func(t *testing.T) {
		out := gmbWant(t, sb, []string{"GlassMarble"}, "init")
		if !strings.Contains(out, "Initialized") && !strings.Contains(out, "ready") {
			t.Logf("init output (informational): %s", out)
		}
		for _, rel := range []string{
			".glassmarble/akg.json",
			".glassmarble/config.yaml",
			".glassmarble/marbles",
			".glassmarble/snapshots",
			".glassmarble/memory",
		} {
			if !sb.Exists(rel) {
				t.Errorf("init did not create %s", rel)
			}
		}
		// init is idempotent.
		gmb(t, sb, "init")
	})

	// --- 2. seed a real project and commit it -----------------------------
	t.Run("seed project and commit", func(t *testing.T) {
		sb.SampleProject()
		sb.GitInit()
		if sb.GitStatusPorcelain() != "" {
			t.Errorf("expected clean working tree, got:\n%s", sb.GitStatusPorcelain())
		}
	})

	// --- 3. first analysis: full pipeline ---------------------------------
	commit1 := ""
	t.Run("analyze", func(t *testing.T) {
		commit1 = sb.GitHead()
		gmbWant(t, sb, []string{
			"Starting GlassMarble Analysis",
			"Analyzed",
			"Stage 5:",
		}, "analyze")
		if !sb.Exists(".glassmarble/telemetry.json") {
			t.Errorf("analyze did not write telemetry.json")
		}
		if !sb.Exists(".glassmarble/intelligence/latest.json") {
			t.Errorf("analyze did not persist Stage 5 result")
		}
		akg := sb.ReadFile(".glassmarble/akg.json")
		if !strings.Contains(akg, `"schema_version"`) {
			t.Errorf("akg.json missing schema_version")
		}
		// init wrote a .gitignore that keeps .glassmarble/ out of the tree.
		if status := sb.GitStatusPorcelain(); status != "" {
			t.Errorf("expected clean working tree after analysis, got:\n%s", status)
		}
		if !sb.Exists(".gitignore") || !strings.Contains(sb.ReadFile(".gitignore"), ".glassmarble") {
			t.Errorf("init should write a .gitignore ignoring .glassmarble/")
		}
	})

	// --- 4. status / doctor / diff on the committed graph -----------------
	t.Run("status", func(t *testing.T) {
		gmbWant(t, sb, []string{
			"GlassMarble AKG Status",
			"Schema Version:",
			"Graph Version:",
			"Commit Hash:",
			"Nodes Count:",
		}, "status")
	})

	t.Run("doctor", func(t *testing.T) {
		gmbWant(t, sb, []string{"DOCTOR: OK"}, "doctor")
	})

	t.Run("diff", func(t *testing.T) {
		gmbWant(t, sb, []string{"Architectural Graph Mutation Diff", "No pending transactions"}, "diff")
	})

	// --- 5. inspect the graph ----------------------------------------------
	t.Run("inspect languages", func(t *testing.T) {
		gmbWant(t, sb, []string{"14-Language Support Matrix", "Go", "Python", "JavaScript"}, "inspect", "--languages")
	})

	t.Run("inspect list", func(t *testing.T) {
		gmbWant(t, sb, []string{"=== Entry Points & Callable Symbols ===", "service.go"}, "inspect", "--list")
	})

	t.Run("inspect search", func(t *testing.T) {
		gmbWant(t, sb, []string{"=== Search Results for 'Fetch' ===", "internal/repo/repo.go"}, "inspect", "--search", "Fetch")
	})

	t.Run("inspect node details", func(t *testing.T) {
		gmbWant(t, sb, []string{"=== Node Details:", "internal/cache/cache.go"}, "inspect", "internal/cache/cache.go::New")
	})

	// --- 6. tree ------------------------------------------------------------
	t.Run("tree", func(t *testing.T) {
		gmbWant(t, sb, []string{"=== Architecture Workspace Tree ===", "file(s) indexed", "internal/cache"}, "tree", "--depth", "2")
	})

	// --- 7. telemetry stats --------------------------------------------------
	t.Run("stats", func(t *testing.T) {
		gmbWant(t, sb, []string{"=== GlassMarble Pipeline Telemetry Spans ===", "Total Pipeline Time"}, "stats")
	})

	t.Run("stats arch", func(t *testing.T) {
		gmbWant(t, sb, []string{"=== Architecture Health (Stage 5) ===", "Components:"}, "stats", "--arch")
	})

	// --- 8. second change: evolve the codebase ------------------------------
	// A whole new package (not just a method) so the snapshot delta and the
	// Stage 6 event generator have real architectural changes to report.
	commit2 := ""
	t.Run("evolve", func(t *testing.T) {
		commit2 = sb.GitCommitFiles("add checkout service", map[string]string{
			"internal/checkout/checkout.go": `package checkout

import "example.com/shop/internal/cache"

// Processor coordinates the checkout flow through the cache layer.
type Processor struct {
	cache *cache.Cache
}

func NewProcessor(c *cache.Cache) *Processor {
	return &Processor{cache: c}
}

// Complete finishes the checkout for a customer.
func (p *Processor) Complete(customer string) string {
	p.cache.Set("checkout:"+customer, "done")
	return "ok"
}
`,
		})
		if commit2 == commit1 {
			t.Fatalf("commit 2 must differ from commit 1")
		}
	})

	t.Run("analyze incremental", func(t *testing.T) {
		out := gmbWant(t, sb, []string{
			"Analyzed",
			"Stage 8: reasoned",
			"Stage 6: recorded",
		}, "analyze")
		if !strings.Contains(out, "checkout") {
			t.Logf("incremental run did not mention the new package (informational)")
		}
	})

	// --- 9. architectural evolution: diff / snapshots / timeline -------------
	t.Run("diff after change", func(t *testing.T) {
		gmbWant(t, sb, []string{"Architectural Graph Mutation Diff"}, "diff")
	})

	t.Run("snapshot lifecycle", func(t *testing.T) {
		// analyze snapshots each analyzed commit automatically.
		out := gmbWant(t, sb, []string{"SNAPSHOT ID", "COMMIT"}, "snapshot", "--list")
		if c := strings.Count(out, "snap_"); c < 2 {
			t.Errorf("expected >= 2 snapshots, found %d\n%s", c, out)
		}

		// --at resolves a ref to the nearest snapshot.
		gmbWant(t, sb, []string{"Snapshot snap_", "commit:", "nodes"}, "snapshot", "--at", "HEAD")

		// --diff reports the architectural change between the two commits.
		gmbWant(t, sb, []string{"Architecture diff", "→"}, "snapshot", "--diff", commit1, commit2)

		// --replay renders a diagram from the snapshot's embedded graph.
		gmbWant(t, sb, []string{"graph TD"}, "snapshot", "--replay", "HEAD", "--diagram", "dependency")

		// --create at an unchanged topology skip-writes.
		out = gmb(t, sb, "snapshot", "--create")
		if !strings.Contains(out, "Snapshot") || (!strings.Contains(out, "created") && !strings.Contains(out, "unchanged")) {
			t.Errorf("unexpected snapshot --create output:\n%s", out)
		}
	})

	t.Run("timeline", func(t *testing.T) {
		// Mermaid timeline derived from developer memory events.
		gmbWant(t, sb, []string{"timeline", "title Architecture Evolution"}, "timeline", "--format", "mermaid")

		// JSON timeline is a non-empty event list.
		out := gmb(t, sb, "timeline", "--format", "json")
		var entries []map[string]any
		if err := json.Unmarshal([]byte(out), &entries); err != nil {
			t.Fatalf("timeline --format json is not valid JSON: %v\n%s", err, out)
		}
		if len(entries) == 0 {
			t.Errorf("expected timeline entries after two analyses:\n%s", out)
		}
	})

	// --- 10. developer memory -------------------------------------------------
	t.Run("memory overview", func(t *testing.T) {
		gmbWant(t, sb, []string{"Developer memory", "event(s)", "component(s)"}, "memory")
	})

	t.Run("memory ask", func(t *testing.T) {
		gmbWant(t, sb, []string{"ranked from developer memory", "Components:"}, "memory", "--ask", "what does the cache do")
	})

	t.Run("memory component history", func(t *testing.T) {
		gmbWant(t, sb, []string{"Component", "cache", "state="}, "memory", "--component", "cache")
	})

	t.Run("memory corrections", func(t *testing.T) {
		gmbWant(t, sb, []string{"Recorded correction", "INTENT", `"cache"`},
			"memory", "--correct", "cache", "--kind", "INTENT", "--value", "use LRU", "--reason", "from e2e journey")
		if !sb.Exists(".glassmarble/memory/corrections.jsonl") {
			t.Errorf("correction log not written")
		}
		gmbWant(t, sb, []string{"1 correction(s) in the audit log", "INTENT"}, "memory", "--corrections")
	})

	// --- 11. AI architect (mock LLM, no network) ----------------------------
	mock := harness.NewMockLLM(t)
	defer mock.Close()
	mockURL := mock.Start()
	mock.DefaultText("The cache serves greeting lookups.")

	t.Run("ai ask", func(t *testing.T) {
		out := gmbWant(t, sb, []string{"The cache serves greeting lookups."},
			"ai", "what is the cache for?",
			"--provider", "custom", "--base-url", mockURL, "--model", "gmb-e2e", "--root-dir", sb.Root)
		if !strings.Contains(out, "Session") && !strings.Contains(out, "ranked") {
			t.Logf("ai ask output shape (informational): %s", out)
		}
	})

	t.Run("ai chat and sessions", func(t *testing.T) {
		// chat reads from stdin until EOF; the answer is saved as a session.
		out := runGmbWithStdin(t, sb, "explain the cache architecture\nexit\n",
			"ai", "chat", "--provider", "custom", "--base-url", mockURL,
			"--model", "gmb-e2e", "--root-dir", sb.Root)
		for _, want := range []string{"Session ", "The cache serves greeting lookups."} {
			if !strings.Contains(out, want) {
				t.Errorf("ai chat output missing %q\n%s", want, out)
			}
		}

		sessDir := filepath.Join(sb.Root, ".glassmarble", "ai", "sessions")
		entries, err := os.ReadDir(sessDir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("expected one saved session, got %d (%v)", len(entries), err)
		}
		sessID := strings.TrimSuffix(entries[0].Name(), ".json")

		sessOut := gmbWant(t, sb, []string{"1 session(s)", sessID}, "ai", "sessions", "--root-dir", sb.Root)
		if !strings.Contains(sessOut, sessID) {
			t.Errorf("ai sessions should list the saved session id")
		}

		gmbWant(t, sb, []string{"Deleted session " + sessID}, "ai", "sessions", "--delete", sessID, "--root-dir", sb.Root)
		if _, err := os.Stat(filepath.Join(sessDir, entries[0].Name())); !os.IsNotExist(err) {
			t.Errorf("session file still present after --delete")
		}
	})

	// --- 12. export / import roundtrip ---------------------------------------
	t.Run("export graphjson", func(t *testing.T) {
		gmbWant(t, sb, []string{"Exported AKG snapshot", "nodes)"}, "export", "--output", "graph.json")
		if !sb.Exists("graph.json") {
			t.Fatalf("export did not write graph.json")
		}
		data := sb.ReadFile("graph.json")
		var doc map[string]any
		if err := json.Unmarshal([]byte(data), &doc); err != nil {
			t.Fatalf("exported graph.json is not valid JSON: %v", err)
		}
		if doc["schema_version"] == nil || doc["nodes"] == nil {
			t.Errorf("exported graph.json missing schema_version/nodes")
		}
	})

	t.Run("export neo4j cypher", func(t *testing.T) {
		gmbWant(t, sb, []string{"Exported AKG snapshot"}, "export", "--format", "neo4j", "--output", "dump.cypher")
		cypher := sb.ReadFile("dump.cypher")
		if !strings.Contains(cypher, "CREATE") {
			t.Errorf("cypher export has no CREATE statements")
		}
		if !strings.Contains(cypher, "GlassMarble Architecture Knowledge Graph") {
			t.Errorf("cypher export missing header")
		}
	})

	t.Run("import roundtrip", func(t *testing.T) {
		gmbWant(t, sb, []string{"Imported AKG snapshot"}, "import", "graph.json")
		// The graph survived the roundtrip.
		gmbWant(t, sb, []string{"DOCTOR: OK"}, "doctor")
	})

	// --- 13. visualize marbles --------------------------------------------------
	t.Run("visualize stream", func(t *testing.T) {
		gmbWant(t, sb, []string{"graph TD"}, "visualize", "dependency")
	})

	t.Run("visualize save", func(t *testing.T) {
		gmbWant(t, sb, []string{"Marble saved successfully to", ".glassmarble", "marbles"}, "visualize", "dependency", "--save", "dep")
		marble := sb.ReadFile(".glassmarble/marbles/dep.md")
		if !strings.Contains(marble, "```mermaid") || !strings.Contains(marble, "graph TD") {
			t.Errorf("saved marble is not a mermaid block:\n%s", marble)
		}
	})

	// --- 14. dependency / hotspot / patterns -----------------------------------
	t.Run("dependency summary", func(t *testing.T) {
		gmbWant(t, sb, []string{"Repository Dependency Summary", "Total Graph Nodes"}, "dependency")
	})

	t.Run("dependency target", func(t *testing.T) {
		gmbWant(t, sb, []string{"Dependency Analysis:", "internal/service"}, "dependency", "service")
	})

	t.Run("hotspot", func(t *testing.T) {
		gmbWant(t, sb, []string{"Top 5 Architectural Hotspots"}, "hotspot", "--top", "5")
		jsonOut := gmb(t, sb, "hotspot", "--top", "5", "--json")
		var doc struct {
			Top int `json:"top"`
		}
		if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
			t.Fatalf("hotspot --json is not valid JSON: %v\n%s", err, jsonOut)
		}
		if doc.Top != 5 {
			t.Errorf("hotspot --json top = %d, want 5", doc.Top)
		}
	})

	t.Run("patterns", func(t *testing.T) {
		gmbWant(t, sb, []string{"=== Stage 5: Architectural Intelligence ===", "Patterns:", "Components:"}, "patterns")
	})

	// --- 15. housekeeping + hooks + completion ---------------------------------
	t.Run("housekeeping", func(t *testing.T) {
		gmbWant(t, sb, []string{".glassmarble Working Set", "Total"}, "housekeeping")
	})

	t.Run("hooks install", func(t *testing.T) {
		gmbWant(t, sb, []string{"Git Hook Installed"}, "hooks", "install")
		if !sb.Exists(filepath.Join(".git", "hooks", "post-commit")) {
			t.Errorf("post-commit hook not installed")
		}
		hook := sb.ReadFile(filepath.Join(".git", "hooks", "post-commit"))
		if !strings.Contains(hook, "analyze") {
			t.Errorf("hook script does not run analyze:\n%s", hook)
		}
		gmbWant(t, sb, []string{"post-commit hook uninstalled successfully"}, "hooks", "uninstall")
		if sb.Exists(filepath.Join(".git", "hooks", "post-commit")) {
			t.Errorf("post-commit hook still present after uninstall")
		}
	})

	t.Run("completion", func(t *testing.T) {
		out := gmb(t, sb, "completion", "bash")
		if !strings.Contains(out, "__start_gmb") || !strings.Contains(out, "complete") {
			t.Errorf("completion bash missing cobra script markers")
		}
	})

	t.Run("version", func(t *testing.T) {
		gmbWant(t, sb, []string{"GlassMarble", "0.1.0"}, "version")
	})

	// --- 16. benchmark gate ----------------------------------------------------
	t.Run("stats bench", func(t *testing.T) {
		gmbWant(t, sb, []string{
			"=== GlassMarble Pipeline Benchmark Gate",
			"analyze total",
			"akg-commit",
			"state size",
			"PASS",
		}, "stats", "--bench")
	})

	// --- 17. machine-readable analysis ------------------------------------------
	t.Run("analyze json", func(t *testing.T) {
		out := gmb(t, sb, "analyze", "--json")
		// The human banner line precedes the JSON document.
		if i := strings.Index(out, "{"); i >= 0 {
			out = out[i:]
		}
		var doc struct {
			FilesAnalyzed int `json:"files_analyzed"`
			Nodes         int `json:"nodes"`
			Edges         int `json:"edges"`
			StateBytes    int64 `json:"state_bytes"`
		}
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("analyze --json is not valid JSON: %v\n%s", err, out)
		}
		if doc.Nodes == 0 || doc.Edges == 0 || doc.FilesAnalyzed == 0 {
			t.Errorf("analyze --json numbers look wrong: %+v", doc)
		}
	})

	// --- 18. uninitialized behavior of a fresh workspace -------------------------
	t.Run("fresh workspace is uninitialized", func(t *testing.T) {
		fresh := harness.NewSandbox(t)
		gmbWant(t, fresh, []string{"GlassMarble Status: Uninitialized"}, "status")
		out, err := gmbErr(t, fresh, "inspect", "--list")
		if err == nil {
			t.Errorf("inspect on empty workspace should fail:\n%s", out)
		}
	})
}
