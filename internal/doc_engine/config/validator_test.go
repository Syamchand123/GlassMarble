package config

import "testing"

func TestValidateSpecCompleteness(t *testing.T) {
	cfg := &DocsConfig{
		Version: 1,
		Documents: []DocSpec{
			{
				ID:         "test-doc",
				TargetPath: "docs/test.md",
				Sections: []SectionSpec{
					{ID: "managed-no-instruction", Title: "T", Managed: true},
				},
			},
		},
	}
	warnings := ValidateSpecCompleteness(cfg)
	if len(warnings) == 0 {
		t.Fatal("expected warnings for missing purpose/audience/instruction")
	}
	fields := map[string]bool{}
	for _, w := range warnings {
		fields[w.Field] = true
	}
	for _, want := range []string{"purpose", "audience", "instruction"} {
		if !fields[want] {
			t.Errorf("expected warning for field %q, got %+v", want, warnings)
		}
	}

	complete := &DocsConfig{
		Version: 1,
		Documents: []DocSpec{
			{
				ID:         "full-doc",
				TargetPath: "docs/full.md",
				Purpose:    "Reference",
				Audience:   "Contributors",
				Sections: []SectionSpec{
					{ID: "s1", Title: "S", Managed: true, Instruction: "Do X", GroundWith: []string{"signatures"}},
				},
			},
		},
	}
	if warnings := ValidateSpecCompleteness(complete); len(warnings) != 0 {
		t.Errorf("expected no warnings for complete spec, got %+v", warnings)
	}
}
