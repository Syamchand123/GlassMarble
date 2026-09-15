package renderer

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/verifier"
)

func TestDeterministicRenderer_Basic(t *testing.T) {
	r := NewDeterministicRenderer()

	fs := &config.FactSheet{
		DocID:              "docs/auth.md",
		SectionID:          "interface",
		SectionInstruction: "Document exported authentication interface.",
		GroundTruth: config.GroundTruthPayload{
			Symbols: []config.SymbolFact{
				{
					FQN:       "internal/auth.Authenticate",
					Kind:      "func",
					Signature: "func Authenticate(token string) bool",
					Doc:       "Authenticate validates the user token.",
					File:      "internal/auth/auth.go",
					Line:      42,
					Permalink: "internal/auth/auth.go#L42-L50",
				},
				{
					FQN:  "internal/auth.SessionManager",
					Kind: "struct",
					Doc:  "SessionManager coordinates active sessions.",
					File: "internal/auth/session.go",
					Line: 12,
				},
			},
			Sentinels: []config.SentinelFact{
				{
					FQN:     "internal/auth.ErrInvalidToken",
					Doc:     "returned when token is malformed",
					File:    "internal/auth/auth.go",
					Line:    15,
					Callers: []string{"Authenticate"},
				},
			},
			ConfigVars: []config.ConfigVarFact{
				{
					Name:     "AUTH_SECRET",
					Source:   "os.Getenv",
					Required: true,
					Doc:      "HMAC secret for signing tokens.",
					File:     "internal/auth/config.go",
					Line:     8,
				},
			},
			// DiagramMermaid is a Track-A-only prompt convenience (see
			// facts.go: "injected verbatim by the LLM actuator") that
			// production code always sets as a copy of Diagrams[0].Content
			// — never populated alone. Track B (this renderer) renders
			// Diagrams directly and must NOT also render DiagramMermaid,
			// or every section with a diagram would show it twice.
			DiagramMermaid: "graph TD\n  A --> B\n",
			Diagrams: []config.DiagramFact{
				{Type: "callgraph", Content: "graph TD\n  A --> B\n"},
			},
			CallFlow: []string{"cmd.Login", "internal/auth.Authenticate"},
			Callers:  []string{"cmd.Login"},
		},
	}

	out, err := r.RenderSection(fs)
	if err != nil {
		t.Fatalf("RenderSection failed: %v", err)
	}

	// 1. Must have deterministic mode tag
	if !strings.Contains(out, "<!-- gmb:mode:deterministic -->") {
		t.Errorf("missing deterministic mode tag")
	}

	// 2. Must contain Mermaid block
	if !strings.Contains(out, "```mermaid\ngraph TD\n  A --> B\n```") {
		t.Errorf("missing or malformed diagram block")
	}

	// 3. Must contain function table with valid columns
	if !strings.Contains(out, "| Name | Signature | Description | File |") {
		t.Errorf("missing function table header")
	}
	if !strings.Contains(out, "`Authenticate`") {
		t.Errorf("missing Authenticate function in output")
	}
	if !strings.Contains(out, "[auth.go#L42](internal/auth/auth.go#L42-L50)") {
		t.Errorf("missing permalink link in output")
	}

	// 4. Must contain types table
	if !strings.Contains(out, "| Type | Kind | Description | File |") {
		t.Errorf("missing types table header")
	}
	if !strings.Contains(out, "`SessionManager`") {
		t.Errorf("missing SessionManager type in output")
	}

	// 5. Must contain error catalog
	if !strings.Contains(out, "| Error | Trigger Condition | Caller |") {
		t.Errorf("missing error catalog table header")
	}
	if !strings.Contains(out, "`ErrInvalidToken`") {
		t.Errorf("missing ErrInvalidToken in output")
	}

	// 6. Must contain config vars table
	if !strings.Contains(out, "| Variable | Source | Default | Required | Description |") {
		t.Errorf("missing config table header")
	}
	if !strings.Contains(out, "`AUTH_SECRET`") {
		t.Errorf("missing AUTH_SECRET in output")
	}

	// 7. Must pass Quality Firewall Gates 1, 2, 4
	symIndex := buildSymbolIndex(fs, nil)
	gateRes := verifier.RunGates("", out, symIndex)
	if !gateRes.Pass {
		t.Fatalf("deterministic output failed Quality Firewall Gate %d: %v", gateRes.FailedGate, gateRes.Error)
	}
}

// TestDeterministicRenderer_EndpointsTable guards against a regression
// where a section's HTTP endpoints (method, path, handler) had nowhere to
// render at all — GroundTruthPayload had no Endpoints field, so a route's
// method and path were structurally invisible and the handler only ever
// appeared in the generic Functions and Methods table.
func TestDeterministicRenderer_EndpointsTable(t *testing.T) {
	r := NewDeterministicRenderer()
	fs := &config.FactSheet{
		DocID:     "docs/api.md",
		SectionID: "endpoints",
		GroundTruth: config.GroundTruthPayload{
			Endpoints: []config.EndpointFact{
				{
					Method:    "GET",
					Path:      "/tasks",
					Handler:   "ListTasksHandler",
					Doc:       "ListTasksHandler lists all tasks.",
					Permalink: "pkg/api/handler.go#L10",
				},
			},
		},
	}

	out, err := r.RenderSection(fs)
	if err != nil {
		t.Fatalf("RenderSection failed: %v", err)
	}
	if !strings.Contains(out, "### Endpoints") {
		t.Fatalf("missing Endpoints heading:\n%s", out)
	}
	if !strings.Contains(out, "GET") || !strings.Contains(out, "`/tasks`") {
		t.Errorf("missing method/path in rendered table:\n%s", out)
	}
	if !strings.Contains(out, "ListTasksHandler lists all tasks.") {
		t.Errorf("missing handler description:\n%s", out)
	}
}

// TestDeterministicRenderer_EndpointsTable_NoDoubledWordFalsePositive
// guards against a regression confirmed against the real Gate 6 prose
// checker: the Endpoints table's Handler column used to be a markdown link
// whose visible text was the handler's own name — [CreateTaskHandler](url)
// — immediately followed by a Description that (correctly) opens with
// that same name ("CreateTaskHandler handles POST /tasks..."). Gate 6
// strips markdown link/code syntax down to plain words before prose
// checking, so the two occurrences collapsed into what reads as the same
// bare word twice in a row, failing verifier.CheckProse's doubled-word
// rule on perfectly correct, non-doubled content.
func TestDeterministicRenderer_EndpointsTable_NoDoubledWordFalsePositive(t *testing.T) {
	r := NewDeterministicRenderer()
	fs := &config.FactSheet{
		DocID:     "docs/api.md",
		SectionID: "endpoints",
		GroundTruth: config.GroundTruthPayload{
			Endpoints: []config.EndpointFact{
				{
					Method:    "ANY",
					Path:      "/tasks",
					Handler:   "CreateTaskHandler",
					Doc:       "CreateTaskHandler handles POST /tasks and creates a new task.",
					File:      "pkg/api/handler.go",
					Line:      21,
					Permalink: "pkg/api/handler.go#L21-L36",
				},
			},
		},
	}

	out, err := r.RenderSection(fs)
	if err != nil {
		t.Fatalf("RenderSection failed: %v", err)
	}

	report := verifier.CheckProse(out, config.StyleSpec{}, false)
	for _, v := range report.Violations {
		if v.Rule == "doubled-word" {
			t.Errorf("false doubled-word violation on correct content: %+v\nrendered:\n%s", v, out)
		}
	}
}

func TestDeterministicRenderer_All10Archetypes(t *testing.T) {
	r := NewDeterministicRenderer()

	for _, name := range ArchetypeNames {
		arch, ok := GetArchetype(name)
		if !ok {
			t.Fatalf("could not retrieve archetype %q", name)
		}

		for _, sec := range arch.Sections {
			fs := &config.FactSheet{
				DocID:              arch.Title,
				SectionID:          sec.ID,
				SectionInstruction: sec.Instruction,
				GroundTruth: config.GroundTruthPayload{
					Symbols: []config.SymbolFact{
						{
							FQN:       "example/pkg.SampleService",
							Kind:      "struct",
							Signature: "type SampleService struct",
							Doc:       "SampleService handles domain operations.",
							File:      "example/pkg/service.go",
							Line:      10,
							Permalink: "example/pkg/service.go#L10-L30",
						},
						{
							FQN:       "example/pkg.ExecuteOperation",
							Kind:      "func",
							Signature: "func ExecuteOperation() error",
							Doc:       "ExecuteOperation runs the primary logic.",
							File:      "example/pkg/service.go",
							Line:      35,
						},
					},
					Sentinels: []config.SentinelFact{
						{
							FQN:     "example/pkg.ErrOpFailed",
							Doc:     "failed to execute",
							File:    "example/pkg/service.go",
							Line:    5,
							Callers: []string{"ExecuteOperation"},
						},
					},
					ConfigVars: []config.ConfigVarFact{
						{
							Name:     "MAX_RETRIES",
							Source:   "os.Getenv",
							Default:  "3",
							Required: false,
							Doc:      "Maximum retry count.",
							File:     "example/pkg/config.go",
							Line:     12,
						},
					},
					DiagramMermaid: "graph TD\n  Service --> Store\n",
					CallFlow:       []string{"pkg.Handler", "pkg.ExecuteOperation"},
				},
			}

			out, err := r.RenderSection(fs)
			if err != nil {
				t.Fatalf("archetype %s section %s failed: %v", name, sec.ID, err)
			}

			if !strings.Contains(out, "<!-- gmb:mode:deterministic -->") {
				t.Errorf("archetype %s section %s missing mode tag", name, sec.ID)
			}

			// Validate with Quality Firewall
			symIndex := buildSymbolIndex(fs, nil)
			gateRes := verifier.RunGates("", out, symIndex)
			if !gateRes.Pass {
				t.Errorf("archetype %s section %s failed gate %d: %v", name, sec.ID, gateRes.FailedGate, gateRes.Error)
			}
		}
	}
}

// TestDeterministicRenderer_PromptGuidanceNotLeaked guards against a
// regression where the D2 Diátaxis quadrant contract (renderer/engine.go)
// was appended directly onto FactSheet.SectionInstruction — a field Track B
// renders verbatim as the section's visible blockquote header. Every
// section whose archetype has a quadrant then showed an internal
// LLM-steering paragraph ("Documentation quadrant (reference): Reference
// documentation must be dry and complete...") as if it were real section
// content. PromptGuidance exists specifically so this text reaches only
// Track A's prompt (see BuildUserPrompt) and never a reader.
func TestDeterministicRenderer_PromptGuidanceNotLeaked(t *testing.T) {
	r := NewDeterministicRenderer()
	fs := &config.FactSheet{
		SectionInstruction: "Document the exported interface.",
		PromptGuidance:     "Documentation quadrant (reference): Reference documentation must be dry and complete.",
	}
	out, err := r.RenderSection(fs)
	if err != nil {
		t.Fatalf("RenderSection failed: %v", err)
	}
	if !strings.Contains(out, "Document the exported interface.") {
		t.Errorf("missing real section instruction:\n%s", out)
	}
	if strings.Contains(out, "Documentation quadrant") || strings.Contains(out, "PromptGuidance") {
		t.Errorf("PromptGuidance leaked into Track B's rendered output:\n%s", out)
	}
}

// TestDeterministicRenderer_DiagramNotDuplicated guards against a
// regression where the renderer embedded a section's first diagram twice:
// once via GroundTruth.DiagramMermaid (a Track-A-only prompt convenience
// that facts.go always sets to a copy of Diagrams[0].Content) and again via
// the Diagrams list itself. Every section configured with 1+ diagrams
// showed its first diagram rendered twice in the generated markdown.
func TestDeterministicRenderer_DiagramNotDuplicated(t *testing.T) {
	r := NewDeterministicRenderer()
	fs := &config.FactSheet{
		SectionInstruction: "Document the call graph.",
		GroundTruth: config.GroundTruthPayload{
			// Mirrors real production population (facts.go): DiagramMermaid
			// is always Diagrams[0].Content, never set independently.
			DiagramMermaid: "graph TD\n  A --> B\n",
			Diagrams: []config.DiagramFact{
				{Type: "callgraph", Content: "graph TD\n  A --> B\n"},
				{Type: "dependency", Content: "graph LR\n  X --> Y\n"},
			},
		},
	}
	out, err := r.RenderSection(fs)
	if err != nil {
		t.Fatalf("RenderSection failed: %v", err)
	}
	if got := strings.Count(out, "A --> B"); got != 1 {
		t.Errorf("callgraph diagram rendered %d time(s), want exactly 1:\n%s", got, out)
	}
	if got := strings.Count(out, "X --> Y"); got != 1 {
		t.Errorf("dependency diagram rendered %d time(s), want exactly 1:\n%s", got, out)
	}
}

// TestSymbolShortName_FileAndModulePrefixes guards against a real bug found
// via live end-to-end testing: the old implementation split on "." with no
// regard for what the string actually was, so a "file:" pseudo-symbol whose
// path happens to end in ".go" — the overwhelmingly common case for a Go
// repo — collapsed to the nonsensical short name "go" instead of the
// filename. This showed up verbatim in a real generated doc's "Recent
// Symbol Changes" list as "- `go`: `config.go`".
func TestSymbolShortName_FileAndModulePrefixes(t *testing.T) {
	cases := []struct {
		fqn  string
		want string
	}{
		{"pkg/config/config.go::envInt::param:def", "param:def"},
		{"internal/auth/jwt.go::ValidateToken", "ValidateToken"},
		{"file:pkg/config/config.go", "config.go"},
		{"module:cmd/server", "server"},
		{"pkg/config/config.go", "config.go"},
		{"config.go", "config.go"},
		{"pkg.SubPkg.Type", "Type"},
		{"Plain", "Plain"},
	}
	for _, c := range cases {
		if got := symbolShortName(c.fqn); got != c.want {
			t.Errorf("symbolShortName(%q) = %q, want %q", c.fqn, got, c.want)
		}
	}
}

// TestStripPseudoSymbolPrefix_KeepsCallerQualification guards against a
// real bug found via live end-to-end testing: a "Direct Callers" bullet
// list rendered a "file:" pseudo-symbol raw — "- `file:pkg/config/
// config.go`" — because that render path never called symbolShortName at
// all. The fix must NOT reuse symbolShortName directly, though: a real
// "::"-chained caller like "pkg/api/handler.go::Handler::CreateTaskHandler"
// needs to keep its receiver-type qualification (that's what tells one
// caller apart from another same-named method elsewhere) — symbolShortName
// would collapse it down to just "CreateTaskHandler", losing exactly the
// context this list exists to show.
func TestStripPseudoSymbolPrefix_KeepsCallerQualification(t *testing.T) {
	cases := []struct {
		ref  string
		want string
	}{
		{"file:pkg/config/config.go", "config.go"},
		{"module:cmd/server", "server"},
		{"pkg/api/handler.go::Handler::CreateTaskHandler", "pkg/api/handler.go::Handler::CreateTaskHandler"},
		{"pkg/store/store.go::New", "pkg/store/store.go::New"},
	}
	for _, c := range cases {
		if got := stripPseudoSymbolPrefix(c.ref); got != c.want {
			t.Errorf("stripPseudoSymbolPrefix(%q) = %q, want %q", c.ref, got, c.want)
		}
	}
}

// TestHumanizeCompoundIdentifiers_Backstop guards against a real bug found
// via live end-to-end testing: despite BuildSystemPrompt rule 3 explicitly
// forbidding it (with this exact string as the negative example), a live
// model run still wrote "the remaining symbols (`file:pkg/config/
// config.go`, ...)" in generated prose — raw compound machine syntax
// leaking through prompt non-compliance. This deterministic backstop must
// catch what the prompt rule alone did not, without disturbing sentences
// that were already clean.
func TestHumanizeCompoundIdentifiers_Backstop(t *testing.T) {
	in := "The remaining symbols (`file:pkg/config/config.go`, `envInt`, its parameters `def` and `name`) are not environment-based. See `pkg/config/config.go::envInt::param:def` and `module:cmd/server` too."
	want := "The remaining symbols (`config.go`, `envInt`, its parameters `def` and `name`) are not environment-based. See `param:def` and `server` too."
	if got := humanizeCompoundIdentifiers(in); got != want {
		t.Errorf("humanizeCompoundIdentifiers() =\n%q\nwant\n%q", got, want)
	}

	// A plain, legitimately-quoted file path or short name must pass through
	// untouched — this backstop targets unambiguous machine-key shapes only.
	clean := "You define `MAX_TASKS` in `pkg/config/config.go`, read via `envInt`."
	if got := humanizeCompoundIdentifiers(clean); got != clean {
		t.Errorf("humanizeCompoundIdentifiers() must not touch clean prose:\ngot  %q\nwant %q", got, clean)
	}
}
