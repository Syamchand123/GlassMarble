package devex

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)
func TestExportedFuncNameReceiverQualified(t *testing.T) {
	cases := map[string]string{
		"func Run(repoRoot string) error {":                         "Run",
		"func (r *MermaidRenderer) Render(t int) (string, error) {": "MermaidRenderer.Render",
		"func (e GateError) Error() string {":                       "GateError.Error",
		"func (m *model) Init() tea.Cmd {":                          "model.Init",
		"func helper(x int) int {":                                  "",
		// Exported method on unexported receiver is still tracked: it can
		// satisfy interfaces and appear in migration-relevant diffs.
		"func (u unexported) Method() {": "unexported.Method",
	}
	for in, want := range cases {
		if got := exportedFuncName(in); got != want {
			t.Errorf("exportedFuncName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDisplayNameOf(t *testing.T) {
	if got := displayNameOf("internal/foo/bar.go::Renderer.Render"); got != "Renderer.Render" {
		t.Errorf("displayNameOf qualified = %q", got)
	}
	if got := displayNameOf("Run"); got != "Run" {
		t.Errorf("displayNameOf bare = %q", got)
	}
}

func TestNormalizeSig(t *testing.T) {
	a := normalizeSig("func  Run( x  string )  error {")
	b := normalizeSig("func Run(x string) error {")
	if a != b {
		t.Errorf("normalizeSig mismatch: %q vs %q", a, b)
	}
}

func TestLevenshtein(t *testing.T) {
	if d := levenshtein("PORT", "PORTS"); d != 1 {
		t.Errorf("levenshtein(PORT, PORTS) = %d, want 1", d)
	}
	if d := levenshtein("ABC", "ABC"); d != 0 {
		t.Errorf("levenshtein identical = %d, want 0", d)
	}
	if d := levenshtein("", "AB"); d != 2 {
		t.Errorf("levenshtein empty = %d, want 2", d)
	}
}

func TestSimilarEnvName(t *testing.T) {
	if !similarEnvName("OAUTH_CLIENT_ID", "OAUTH_CLIENT_SECRET") {
		t.Errorf("expected same-prefix OAUTH_* names to be similar")
	}
	if !similarEnvName("APP_HOST", "APP_HOSTS") {
		t.Errorf("expected levenshtein<=3 names to be similar")
	}
	if similarEnvName("DATABASE_URL", "REDIS_ADDR") {
		t.Errorf("expected unrelated names to be dissimilar")
	}
}

func initGitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init")
	for name, content := range files {
		full := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(full), 0755)
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", ".")
	run("commit", "-m", "initial")
	return dir
}

func commitAll(t *testing.T, dir, msg string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		full := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(full), 0755)
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("add", ".")
	run("commit", "-m", msg)
}

func TestDiffConfigVarRenames(t *testing.T) {
	dir := initGitRepo(t, map[string]string{
		"app.go": "package app\nimport \"os\"\nfunc A() { _ = os.Getenv(\"OAUTH_TOKEN_OLD\")\n _ = os.Getenv(\"GONE_VAR\") }\n",
	})
	commitAll(t, dir, "rename config", map[string]string{
		"app.go": "package app\nimport \"os\"\nfunc A() { _ = os.Getenv(\"OAUTH_TOKEN_NEW\") }\n",
	})

	renamed, removed := diffConfigVarRenames(dir, "HEAD~1", "HEAD")
	found := false
	for _, r := range renamed {
		if r.OldName == "OAUTH_TOKEN_OLD" && r.NewName == "OAUTH_TOKEN_NEW" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected OAUTH_TOKEN_OLD→OAUTH_TOKEN_NEW rename, got %+v", renamed)
	}
	foundGone := false
	for _, v := range removed {
		if v == "GONE_VAR" {
			foundGone = true
		}
	}
	if !foundGone {
		t.Errorf("expected GONE_VAR in pure removals, got %v", removed)
	}
}

// TestGenerateMigrationGuideBadRefIsHonest guards against a regression where
// a failed git diff (bad/unfetched ref) silently fell back to an empty
// APIDelta and rendered a confident "no breaking changes detected" claim —
// indistinguishable from a real, verified clean diff — instead of surfacing
// that the interface comparison never actually ran.
func TestGenerateMigrationGuideBadRefIsHonest(t *testing.T) {
	dir := initGitRepo(t, map[string]string{
		"api.go": "package api\nfunc Serve() {}\n",
	})
	guide, err := GenerateMigrationGuide(dir, "does-not-exist-ref", "HEAD", "")
	if err != nil {
		t.Fatalf("GenerateMigrationGuide failed: %v", err)
	}
	if strings.Contains(guide, "No breaking interface changes") {
		t.Errorf("guide falsely claims a verified clean diff after a failed git ref comparison:\n%s", guide)
	}
	if !strings.Contains(guide, "could not run") {
		t.Errorf("guide does not surface the diff failure:\n%s", guide)
	}
}

func TestGenerateMigrationGuideConfigAndExamples(t *testing.T) {
	dir := initGitRepo(t, map[string]string{
		"api.go": "package api\nimport \"os\"\nfunc Serve(host string) {}\nfunc B() { _ = os.Getenv(\"APP_HOST\") }\n",
	})
	commitAll(t, dir, "change sig and rename var", map[string]string{
		"api.go": "package api\nimport \"os\"\nfunc Serve(host string, port int) {}\nfunc B() { _ = os.Getenv(\"APP_HOSTS\") }\n",
	})

	guide, err := GenerateMigrationGuide(dir, "HEAD~1", "HEAD", "")
	if err != nil {
		t.Fatalf("GenerateMigrationGuide failed: %v", err)
	}
	if !strings.Contains(guide, "Renamed Configuration Variables") {
		t.Errorf("expected renamed config vars table:\n%s", guide)
	}
	if !strings.Contains(guide, "APP_HOST") || !strings.Contains(guide, "APP_HOSTS") {
		t.Errorf("expected APP_HOST rename pair in guide:\n%s", guide)
	}
	if !strings.Contains(guide, "Migration Code Examples") {
		t.Errorf("expected migration code examples section:\n%s", guide)
	}
	if !strings.Contains(guide, "// before:") || !strings.Contains(guide, "// after:") {
		t.Errorf("expected before/after fenced blocks:\n%s", guide)
	}
	if !strings.Contains(guide, "Rewrite note:") && !strings.Contains(guide, "rewrite note") {
		t.Errorf("expected rewrite note in guide:\n%s", guide)
	}
}
