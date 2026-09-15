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
