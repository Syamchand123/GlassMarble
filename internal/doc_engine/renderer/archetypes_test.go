package renderer

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

func TestArchetypes_All10Present(t *testing.T) {
	if len(ArchetypeNames) != 10 {
		t.Fatalf("expected exactly 10 archetypes, got %d", len(ArchetypeNames))
	}

	expected := []string{
		"architecture",
		"module",
		"runbook",
		"onboarding",
		"migration",
		"adr",
		"api",
		"security",
		"database",
		"config",
	}

	for _, name := range expected {
		spec, ok := GetArchetype(name)
		if !ok {
			t.Errorf("archetype %q not found", name)
			continue
		}
		if spec.Title == "" {
			t.Errorf("archetype %q missing Title", name)
		}
		if spec.Purpose == "" {
			t.Errorf("archetype %q missing Purpose", name)
		}
		if spec.Audience == "" {
			t.Errorf("archetype %q missing Audience", name)
		}
		if spec.Mode == "" {
			t.Errorf("archetype %q missing Mode", name)
		}
		if len(spec.Sections) < 4 {
			t.Errorf("archetype %q has fewer than 4 sections: %d", name, len(spec.Sections))
		}
		for _, s := range spec.Sections {
			if s.ID == "" {
				t.Errorf("archetype %q has section with empty ID", name)
			}
			if s.Title == "" {
				t.Errorf("archetype %q has section with empty Title", name)
			}
			if s.Instruction == "" {
				t.Errorf("archetype %q has section with empty Instruction", name)
			}
			if len(s.GroundWith) == 0 {
				t.Errorf("archetype %q section %q has empty GroundWith", name, s.ID)
			}
		}
	}
}

func TestApplyArchetype(t *testing.T) {
	spec := &config.DocSpec{
		ID:        "docs/storage.md",
		Archetype: "module",
	}

	if err := ApplyArchetype(spec); err != nil {
		t.Fatalf("ApplyArchetype failed: %v", err)
	}

	if spec.Title != "Module & Package Reference" {
		t.Errorf("expected module title, got %q", spec.Title)
	}
	if len(spec.Sections) != 4 {
		t.Errorf("expected 4 sections, got %d", len(spec.Sections))
	}

	// Custom override should be preserved
	specCustom := &config.DocSpec{
		ID:        "docs/custom.md",
		Archetype: "runbook",
		Title:     "Custom Runbook Title",
	}
	if err := ApplyArchetype(specCustom); err != nil {
		t.Fatalf("ApplyArchetype failed: %v", err)
	}
	if specCustom.Title != "Custom Runbook Title" {
		t.Errorf("custom title overwritten: %q", specCustom.Title)
	}

	// Unknown archetype should return error
	specBad := &config.DocSpec{
		Archetype: "unknown-archetype",
	}
	if err := ApplyArchetype(specBad); err == nil {
		t.Errorf("expected error for unknown archetype")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// D2: Diátaxis quadrant tests (APPEND ONLY — existing tests above untouched)
// ────────────────────────────────────────────────────────────────────────────

func TestArchetypeQuadrant_All10Assigned(t *testing.T) {
	expected := map[string]string{
		"architecture": "explanation",
		"module":       "reference",
		"runbook":      "how-to",
		"onboarding":   "tutorial",
		"migration":    "how-to",
		"adr":          "explanation",
		"api":          "reference",
		"security":     "explanation",
		"database":     "reference",
		"config":       "reference",
	}
	if len(ArchetypeNames) != len(expected) {
		t.Fatalf("quadrant map covers %d archetypes, want %d", len(expected), len(ArchetypeNames))
	}
	for _, name := range ArchetypeNames {
		q := ArchetypeQuadrant(name)
		if q == "" {
			t.Errorf("archetype %q has no quadrant assigned", name)
			continue
		}
		if q != expected[name] {
			t.Errorf("archetype %q quadrant = %q, want %q", name, q, expected[name])
		}
		switch q {
		case "tutorial", "how-to", "reference", "explanation":
		default:
			t.Errorf("archetype %q has invalid quadrant %q", name, q)
		}
	}
	if got := ArchetypeQuadrant("no-such-archetype"); got != "" {
		t.Errorf("unknown archetype quadrant = %q, want empty", got)
	}
	if got := ArchetypeQuadrant(""); got != "" {
		t.Errorf("empty archetype quadrant = %q, want empty", got)
	}
	if got := ArchetypeQuadrant("  API  "); got != "reference" {
		t.Errorf("case/space-insensitive lookup failed, got %q", got)
	}
}

func TestQuadrantPrompt(t *testing.T) {
	keywords := map[string]string{
		"tutorial":    "end-to-end",
		"how-to":      "goal",
		"reference":   "complete",
		"explanation": "why",
	}
	for quadrant, keyword := range keywords {
		p := QuadrantPrompt(quadrant)
		if p == "" {
			t.Errorf("QuadrantPrompt(%q) empty", quadrant)
			continue
		}
		if !strings.Contains(strings.ToLower(p), keyword) {
			t.Errorf("QuadrantPrompt(%q) missing keyword %q:\n%s", quadrant, keyword, p)
		}
		if n := strings.Count(strings.TrimSpace(p), "."); n < 2 || n > 4 {
			t.Errorf("QuadrantPrompt(%q) has %d sentences, want 2-4", quadrant, n)
		}
	}
	if got := QuadrantPrompt("bogus-quadrant"); got != "" {
		t.Errorf("unknown quadrant prompt = %q, want empty", got)
	}
	if got := QuadrantPrompt(""); got != "" {
		t.Errorf("empty quadrant prompt = %q, want empty", got)
	}
}

func TestCompassCheck(t *testing.T) {
	// 1. Reference instruction with tutorial verbs → flag.
	if got := CompassCheck("Endpoints", "Table of routes. Learn the API with this step-by-step walk through.", "reference"); len(got) == 0 {
		t.Errorf("expected flag for reference section with tutorial verbs")
	}
	// 2. Clean reference section → no flag.
	if got := CompassCheck("Endpoints", "Table of route paths, HTTP methods, and handler functions.", "reference"); len(got) != 0 {
		t.Errorf("unexpected flags for clean reference section: %v", got)
	}
	// 3. Tutorial with no action verb → flag.
	if got := CompassCheck("System Overview", "Describe the high-level architecture and component topology.", "tutorial"); len(got) == 0 {
		t.Errorf("expected flag for tutorial section with no action verb")
	}
	// 4. Tutorial with action verb → no flag.
	if got := CompassCheck("Run Your First Migration", "Run the migration tool and verify the schema upgrade.", "tutorial"); len(got) != 0 {
		t.Errorf("unexpected flags for actionable tutorial section: %v", got)
	}
	// 5. How-to with no action verb → flag; with verb → clean.
	if got := CompassCheck("Overview", "Background and history of the service.", "how-to"); len(got) == 0 {
		t.Errorf("expected flag for how-to section with no action verb")
	}
	if got := CompassCheck("Deploy the Service", "Deploy the service and verify health checks pass.", "how-to"); len(got) != 0 {
		t.Errorf("unexpected flags for actionable how-to section: %v", got)
	}
	// 6. Explanation with purely tabular instruction → flag; clean → no flag.
	if got := CompassCheck("Decisions", "Table of all configuration keys and defaults.", "explanation"); len(got) == 0 {
		t.Errorf("expected flag for tabular explanation section")
	}
	if got := CompassCheck("Why Layering", "Explain why upper layers depend downward only, with trade-offs.", "explanation"); len(got) != 0 {
		t.Errorf("unexpected flags for clean explanation section: %v", got)
	}
	// 7. Empty quadrant → nil.
	if got := CompassCheck("Anything", "Learn step-by-step with tables of everything.", ""); got != nil {
		t.Errorf("empty quadrant should yield nil, got %v", got)
	}
}

func TestReferenceCompleteness(t *testing.T) {
	prose := "The `TokenBucket` limiter uses `Refill` to add tokens; see `pkg/limiter.TokenBucket` for details."
	// All symbols documented (plain, backticked, and short-name forms).
	if got := ReferenceCompleteness([]string{"TokenBucket", "Refill", "pkg/limiter.TokenBucket"}, prose); len(got) != 0 {
		t.Errorf("expected no missing symbols, got %v", got)
	}
	// Missing symbols flagged by exact name.
	got := ReferenceCompleteness([]string{"TokenBucket", "MissingSymbol", "AnotherMissing"}, prose)
	if len(got) != 2 {
		t.Fatalf("expected 2 missing symbols, got %v", got)
	}
	if !strings.Contains(got[0], `"MissingSymbol"`) || !strings.Contains(got[1], `"AnotherMissing"`) {
		t.Errorf("failure strings must quote symbol names, got %v", got)
	}
	// Case-sensitive: lowercase variant does not satisfy CamelCase symbol.
	if got := ReferenceCompleteness([]string{"tokenbucket"}, prose); len(got) != 1 {
		t.Errorf("matching must be case-sensitive, got %v", got)
	}
	// Empty symbol list → nil.
	if got := ReferenceCompleteness(nil, prose); got != nil {
		t.Errorf("empty symbols should yield nil, got %v", got)
	}
	if got := ReferenceCompleteness([]string{}, ""); got != nil {
		t.Errorf("empty symbols should yield nil, got %v", got)
	}
}
