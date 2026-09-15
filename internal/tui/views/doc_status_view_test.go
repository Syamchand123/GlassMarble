package views

import (
	"strings"
	"testing"

	doc_engine "github.com/Syamchand123/GlassMarble/internal/doc_engine"
)

// TestRenderDocStatus_AdvisoryWarningsDoNotDrift guards against a regression
// where the dashboard's top banner turned "DRIFTING" whenever d.Warnings was
// non-empty, even though doc_engine.Check appends purely advisory messages
// (diataxis compass suggestions, frontmatter/TOC lint, reference
// completeness hints) into that same slice for documents that are otherwise
// 100% fresh. A document at 100% freshness with only advisory warnings
// attached must render a FRESH banner, matching its own FRESH row.
func TestRenderDocStatus_AdvisoryWarningsDoNotDrift(t *testing.T) {
	out := RenderDocStatus(DocStatusData{
		GlobalFreshness: 100,
		AllFresh:        true,
		Documents: []doc_engine.DocumentCheckResult{
			{ID: "api", TargetPath: "docs/api.md", Freshness: 100, Status: "fresh"},
		},
		Warnings: []string{"docs/api.md [health-checks]: diataxis: compass: consider adding a Troubleshooting section"},
	})

	if !strings.Contains(out, "FRESH") {
		t.Errorf("expected FRESH banner, got:\n%s", out)
	}
	if strings.Contains(out, "DRIFTING") {
		t.Errorf("advisory-only warnings must not drift the banner, got:\n%s", out)
	}
}

// TestRenderDocStatus_WarnStatusDrifts confirms a document whose OWN Status
// is "warn" (a real freshness-threshold dip) still shows the DRIFTING
// banner — the fix must not silence genuine drift signals, only decouple
// the banner from unrelated advisory warning text.
func TestRenderDocStatus_WarnStatusDrifts(t *testing.T) {
	out := RenderDocStatus(DocStatusData{
		GlobalFreshness: 60,
		Documents: []doc_engine.DocumentCheckResult{
			{ID: "api", TargetPath: "docs/api.md", Freshness: 60, Status: "warn"},
		},
	})

	if !strings.Contains(out, "DRIFTING") {
		t.Errorf("expected DRIFTING banner for a warn-status document, got:\n%s", out)
	}
}

// TestRenderDocStatus_StaleStatusDrifted confirms a stale/missing document
// still shows the more severe DRIFTED banner.
func TestRenderDocStatus_StaleStatusDrifted(t *testing.T) {
	out := RenderDocStatus(DocStatusData{
		Documents: []doc_engine.DocumentCheckResult{
			{ID: "api", TargetPath: "docs/api.md", Freshness: 0, Status: "stale"},
		},
	})

	if !strings.Contains(out, "DRIFTED") {
		t.Errorf("expected DRIFTED banner for a stale document, got:\n%s", out)
	}
}
