package renderer

import (
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
