package devex

import "testing"

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
