package archfeatures

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateADR(t *testing.T) {
	tempDir := t.TempDir()

	event := ADREvent{
		Type:        "COMPONENT_SPLIT",
		Title:       "Split Monolith into Subsystems",
		Context:     "High coupling between ingest and link layers.",
		Decision:    "Split packages into autonomous bounded contexts.",
		Consequence: "Clean architectural boundaries and isolated unit tests.",
		CommitHash:  "abc1234",
		Timestamp:   time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
	}

	relPath, err := GenerateADR(tempDir, event)
	if err != nil {
		t.Fatalf("GenerateADR failed: %v", err)
	}

	if !strings.HasPrefix(relPath, "docs/adr/0001-") {
		t.Errorf("expected 0001 ADR path, got %q", relPath)
	}

	fullPath := filepath.Join(tempDir, filepath.FromSlash(relPath))
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("failed to read generated ADR: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "id: adr-0001") {
		t.Errorf("missing frontmatter id in ADR:\n%s", content)
	}
	if !strings.Contains(content, "COMPONENT_SPLIT") {
		t.Errorf("missing event type in ADR:\n%s", content)
	}

	// Test sequential numbering on second ADR
	event2 := ADREvent{
		Type:       "NEW_DATABASE_LAYER",
		Title:      "Add Atomic Storage Engine",
		CommitHash: "def5678",
		Timestamp:  time.Now(),
	}
	relPath2, err := GenerateADR(tempDir, event2)
	if err != nil {
		t.Fatalf("GenerateADR 2 failed: %v", err)
	}
	if !strings.HasPrefix(relPath2, "docs/adr/0002-") {
		t.Errorf("expected 0002 ADR path, got %q", relPath2)
	}
}

func TestScanArchEvents(t *testing.T) {
	logs := []string{
		"feat: split package into submodules to reduce coupling",
		"fix: typo in readme",
		"refactor: break cycle between auth and user layers",
		"feat: add database storage layer with atomic locking",
	}

	events := ScanArchEvents(logs)
	if len(events) != 3 {
		t.Fatalf("expected 3 detected arch events, got %d", len(events))
	}
	if events[0].Type != "COMPONENT_SPLIT" {
		t.Errorf("expected COMPONENT_SPLIT, got %s", events[0].Type)
	}
	if events[1].Type != "CYCLE_RESOLVED" {
		t.Errorf("expected CYCLE_RESOLVED, got %s", events[1].Type)
	}
	if events[2].Type != "NEW_DATABASE_LAYER" {
		t.Errorf("expected NEW_DATABASE_LAYER, got %s", events[2].Type)
	}
}

func TestScanArchEventsDefaultsNonEmpty(t *testing.T) {
	logs := []string{
		"feat: split package into submodules to reduce coupling",
		"feat: introduce cycle between a and b",
		"fix: layer violation in billing boundary",
		"feat: add new service for notifications",
		"BREAKING: rename public API signature",
		"feat: add oauth login flow",
		"feat: create new subsystem for search",
		"feat: add database storage layer",
		"fix: typo in readme",
	}
	events := ScanArchEvents(logs)
	if len(events) != 8 {
		t.Fatalf("expected 8 events (9 types coverable), got %d: %+v", len(events), events)
	}
	seen := map[string]bool{}
	for _, e := range events {
		seen[e.Type] = true
		if strings.TrimSpace(e.Context) == "" || strings.TrimSpace(e.Decision) == "" || strings.TrimSpace(e.Consequence) == "" {
			t.Errorf("event %s has empty defaults: %+v", e.Type, e)
		}
	}
	for _, want := range []string{"COMPONENT_SPLIT", "CYCLE_INTRODUCED", "LAYER_VIOLATION", "SERVICE_ADDED", "INTERFACE_CHANGED", "AUTH_ARCHITECTURE", "NEW_SUBSYSTEM", "NEW_DATABASE_LAYER"} {
		if !seen[want] {
			t.Errorf("expected event type %s to be detected", want)
		}
	}
}

func TestAutoGenerateADRs(t *testing.T) {
	tempDir := t.TempDir()

	paths, err := AutoGenerateADRs(tempDir, "abc1234", "feat: split package into submodules to reduce coupling")
	if err != nil {
		t.Fatalf("AutoGenerateADRs failed: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 ADR path, got %v", paths)
	}
	if !strings.HasPrefix(paths[0], "docs/adr/0001-") {
		t.Errorf("expected 0001 ADR path, got %q", paths[0])
	}
	data, err := os.ReadFile(filepath.Join(tempDir, filepath.FromSlash(paths[0])))
	if err != nil {
		t.Fatalf("reading generated ADR: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "abc1234") {
		t.Errorf("expected commit hash in ADR:\n%s", content)
	}
	if !strings.Contains(content, "## Context") || !strings.Contains(content, "## Decision") || !strings.Contains(content, "## Consequences") {
		t.Errorf("expected Context/Decision/Consequences sections:\n%s", content)
	}

	// Non-architectural message produces no ADRs and no error.
	paths, err = AutoGenerateADRs(tempDir, "def5678", "fix: typo in readme")
	if err != nil {
		t.Fatalf("AutoGenerateADRs (no-op) failed: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected 0 ADR paths for non-arch commit, got %v", paths)
	}
}

func TestEventsFromDossier(t *testing.T) {
	events := EventsFromDossier([]string{
		"COMPONENT_SPLIT", "COMPONENT_ADDED", "COMPONENT_REMOVED",
		"CYCLE_INTRODUCED", "cycle_resolved", "LAYER_VIOLATION",
		"NEW_DATABASE_LAYER", "SERVICE_ADDED", "INTERFACE_CHANGED",
		"PUBLIC_SURFACE_CHANGED", "SECURITY_BOUNDARY_CHANGED",
		"BOGUS_EVENT", "", "CYCLE_INTRODUCED",
	}, "abc123")
	if len(events) != 11 {
		t.Fatalf("expected 11 mapped events (unknown/empty/dup skipped), got %d: %+v", len(events), events)
	}
	seen := map[string]bool{}
	for _, e := range events {
		seen[e.Type] = true
		if strings.TrimSpace(e.Title) == "" || strings.TrimSpace(e.Context) == "" ||
			strings.TrimSpace(e.Decision) == "" || strings.TrimSpace(e.Consequence) == "" {
			t.Errorf("event %s has empty defaults: %+v", e.Type, e)
		}
		if e.CommitHash != "abc123" {
			t.Errorf("event %s missing commit hash: %+v", e.Type, e)
		}
		if e.Timestamp.IsZero() {
			t.Errorf("event %s missing timestamp", e.Type)
		}
	}
	for _, want := range []string{
		"COMPONENT_SPLIT", "COMPONENT_ADDED", "COMPONENT_REMOVED",
		"CYCLE_INTRODUCED", "CYCLE_RESOLVED", "LAYER_VIOLATION",
		"NEW_DATABASE_LAYER", "SERVICE_ADDED", "INTERFACE_CHANGED",
		"PUBLIC_SURFACE_CHANGED", "SECURITY_BOUNDARY_CHANGED",
	} {
		if !seen[want] {
			t.Errorf("expected mapped event type %s", want)
		}
	}
	if seen["BOGUS_EVENT"] {
		t.Errorf("unknown event names must not map")
	}

	if got := EventsFromDossier(nil, "abc123"); len(got) != 0 {
		t.Errorf("expected no events for nil input, got %v", got)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// D4: ADR lifecycle tests (APPEND ONLY — existing tests above untouched)
// ────────────────────────────────────────────────────────────────────────────

func TestRenderMADR(t *testing.T) {
	event := ADREvent{
		Type:        "COMPONENT_SPLIT",
		Title:       "Split Monolith into Subsystems",
		Context:     "High coupling between ingest and link layers.",
		Decision:    "Split packages into autonomous bounded contexts.",
		Consequence: "Clean architectural boundaries and isolated unit tests.",
		CommitHash:  "abc1234",
		Timestamp:   time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
	}
	out := RenderMADR(event)
	for _, section := range []string{
		"# Split Monolith into Subsystems",
		"Status",
		"Context",
		"Decision Drivers",
		"Considered Options",
		"Decision Outcome",
		"Consequences",
		"2026-09-09",
		"abc1234",
		"Split packages into autonomous bounded contexts.",
		"Clean architectural boundaries",
	} {
		if !strings.Contains(out, section) {
			t.Errorf("rendered MADR missing %q:\n%s", section, out)
		}
	}
	// Empty event still renders every section (defaults, never blank skeleton).
	empty := RenderMADR(ADREvent{})
	for _, section := range []string{
		"Context", "Decision Drivers", "Considered Options",
		"Decision Outcome", "Consequences",
	} {
		if !strings.Contains(empty, section) {
			t.Errorf("empty-event MADR missing section %q:\n%s", section, empty)
		}
	}
}

func TestMarkSuperseded(t *testing.T) {
	tempDir := t.TempDir()
	rel, err := GenerateADR(tempDir, ADREvent{
		Type: "COMPONENT_SPLIT", Title: "Old Decision",
		Context: "ctx", Decision: "dec", Consequence: "con",
		CommitHash: "aaa1111", Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("GenerateADR failed: %v", err)
	}
	full := filepath.Join(tempDir, filepath.FromSlash(rel))

	if err := MarkSuperseded(tempDir, rel, "docs/adr/0002-new-decision.md"); err != nil {
		t.Fatalf("MarkSuperseded failed: %v", err)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("reading superseded ADR: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "status: superseded") {
		t.Errorf("frontmatter status not flipped to superseded:\n%s", content)
	}
	want := "> Superseded by [docs/adr/0002-new-decision.md](docs/adr/0002-new-decision.md)"
	if !strings.Contains(content, want) {
		t.Errorf("supersession note missing:\n%s", content)
	}
	if !strings.Contains(content, "Old Decision") || !strings.Contains(content, "\ndec\n") || !strings.Contains(content, "\ncon\n") {
		t.Errorf("history must be preserved:\n%s", content)
	}

	// Idempotent rerun: byte-identical.
	before := content
	if err := MarkSuperseded(tempDir, rel, "docs/adr/0002-new-decision.md"); err != nil {
		t.Fatalf("MarkSuperseded rerun failed: %v", err)
	}
	after, _ := os.ReadFile(full)
	if string(after) != before {
		t.Errorf("MarkSuperseded not idempotent:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
	if n := strings.Count(string(after), "Superseded by"); n != 1 {
		t.Errorf("expected exactly 1 supersession note, found %d", n)
	}

	// Missing target → error.
	if err := MarkSuperseded(tempDir, "docs/adr/9999-nope.md", "docs/adr/0002-x.md"); err == nil {
		t.Errorf("expected error for missing oldFile")
	}
}

func TestValidateStatusTransition(t *testing.T) {
	allowed := [][2]string{
		{"", "proposed"}, {"", "accepted"},
		{"proposed", "accepted"}, {"proposed", "rejected"}, {"proposed", "deprecated"},
		{"accepted", "deprecated"}, {"accepted", "superseded"},
		{"deprecated", "superseded"},
		{"proposed", "proposed"}, {"accepted", "accepted"}, {"superseded", "superseded"},
	}
	for _, tc := range allowed {
		if err := ValidateStatusTransition(tc[0], tc[1]); err != nil {
			t.Errorf("transition %q → %q should be allowed: %v", tc[0], tc[1], err)
		}
	}
	rejected := [][2]string{
		{"proposed", "superseded"}, {"accepted", "rejected"}, {"accepted", "proposed"},
		{"deprecated", "accepted"}, {"deprecated", "proposed"},
		{"superseded", "accepted"}, {"superseded", "deprecated"}, {"rejected", "accepted"},
		{"", "superseded"}, {"", "rejected"}, {"", ""},
	}
	for _, tc := range rejected {
		if err := ValidateStatusTransition(tc[0], tc[1]); err == nil {
			t.Errorf("transition %q → %q should be rejected", tc[0], tc[1])
		}
	}
	if err := ValidateStatusTransition("bogus", "accepted"); err == nil {
		t.Errorf("unknown old status should error")
	}
	if err := ValidateStatusTransition("accepted", "bogus"); err == nil {
		t.Errorf("unknown new status should error")
	}
}

func writeADRFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "docs", "adr"), 0755); err != nil {
		t.Fatalf("mkdir adr: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "adr", name), []byte(content), 0644); err != nil {
		t.Fatalf("writing fixture %s: %v", name, err)
	}
}

func TestRegenerateIndex(t *testing.T) {
	tempDir := t.TempDir()
	writeADRFixture(t, tempDir, "0001-old.md", `---
id: adr-0001
title: "0001 - Old Decision"
status: superseded
date: 2026-09-01
commit: "aaa1111"
---

# 0001. Old Decision

## Status

Superseded

## Context

Old context.

> Superseded by [docs/adr/0002-new.md](docs/adr/0002-new.md)
`)
	writeADRFixture(t, tempDir, "0002-new.md", `---
id: adr-0002
title: "0002 - New Decision"
status: accepted
date: 2026-09-09
commit: "bbb2222"
---

# 0002. New Decision

## Status

Accepted

## Context

New context.
`)
	writeADRFixture(t, tempDir, "template.md", "# ADR template — must be skipped\n")

	if err := RegenerateIndex(tempDir); err != nil {
		t.Fatalf("RegenerateIndex failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tempDir, "docs", "adr", "index.md"))
	if err != nil {
		t.Fatalf("reading index: %v", err)
	}
	index := string(data)
	if !strings.Contains(index, "| ADR | Title | Status | Date |") {
		t.Errorf("index missing table header:\n%s", index)
	}
	if !strings.Contains(index, "0001-old.md") || !strings.Contains(index, "0002-new.md") {
		t.Errorf("index missing ADR rows:\n%s", index)
	}
	if strings.Contains(index, "template.md") {
		t.Errorf("index must skip template.md:\n%s", index)
	}
	if !strings.Contains(index, "Superseded by") || !strings.Contains(index, "0001-old.md") {
		t.Errorf("index missing supersession note:\n%s", index)
	}
	// Deterministic filename-ascending order.
	if strings.Index(index, "0001-old.md") > strings.Index(index, "0002-new.md") {
		t.Errorf("index rows not in filename order:\n%s", index)
	}
	// Rerun is byte-identical.
	before := index
	if err := RegenerateIndex(tempDir); err != nil {
		t.Fatalf("RegenerateIndex rerun failed: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(tempDir, "docs", "adr", "index.md"))
	if string(after) != before {
		t.Errorf("RegenerateIndex not deterministic")
	}
}

func TestRegenerateIndexMissingDir(t *testing.T) {
	tempDir := t.TempDir()
	if err := RegenerateIndex(tempDir); err != nil {
		t.Fatalf("RegenerateIndex on empty root failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tempDir, "docs", "adr", "index.md"))
	if err != nil {
		t.Fatalf("reading empty index: %v", err)
	}
	index := string(data)
	if !strings.Contains(index, "| ADR | Title | Status | Date |") {
		t.Errorf("empty index missing header-only table:\n%s", index)
	}
}

func TestValidateADRLifecycle(t *testing.T) {
	// Clean fixture.
	clean := t.TempDir()
	writeADRFixture(t, clean, "0001-ok.md", `---
status: accepted
date: 2026-09-09
---

# 0001. Fine

## Status

Accepted
`)
	writeADRFixture(t, clean, "0002-ok.md", `---
status: superseded
---

# 0002. Replaced

> Superseded by [docs/adr/0003-next.md](docs/adr/0003-next.md)
`)
	if got := ValidateADRLifecycle(clean); len(got) != 0 {
		t.Errorf("clean fixture should validate, got %v", got)
	}

	// Dirty fixture: unknown status + dangling superseded.
	dirty := t.TempDir()
	writeADRFixture(t, dirty, "0001-mystery.md", "# 0001. Mystery\n\nNo status anywhere.\n")
	writeADRFixture(t, dirty, "0002-dangling.md", `---
status: superseded
---

# 0002. Dangling

No supersession link here.
`)
	writeADRFixture(t, dirty, "index.md", "# stale index — must be skipped\n")
	failures := ValidateADRLifecycle(dirty)
	if len(failures) != 2 {
		t.Fatalf("expected 2 lifecycle failures, got %v", failures)
	}
	if !strings.Contains(failures[0], "0001-mystery.md") || !strings.Contains(failures[1], "0002-dangling.md") {
		t.Errorf("failures not deterministic filename-ordered: %v", failures)
	}

	// Missing docs/adr → nil (opt-in feature).
	if got := ValidateADRLifecycle(t.TempDir()); got != nil {
		t.Errorf("missing docs/adr should yield nil, got %v", got)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// D4 supersession wiring + timeline tests (APPEND ONLY — above untouched)
// ────────────────────────────────────────────────────────────────────────────

func TestAutoGenerateADRsSupersedesPriorAccepted(t *testing.T) {
	tempDir := t.TempDir()
	oldRel, err := GenerateADR(tempDir, ADREvent{
		Type: "COMPONENT_SPLIT", Title: "Subsystem Decoupling & Component Split",
		Context: "ctx", Decision: "dec", Consequence: "con",
		CommitHash: "aaa1111", Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("GenerateADR failed: %v", err)
	}

	paths, err := AutoGenerateADRs(tempDir, "bbb2222", "feat: split package into submodules to reduce coupling")
	if err != nil {
		t.Fatalf("AutoGenerateADRs failed: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 ADR path, got %v", paths)
	}
	if paths[0] == oldRel {
		t.Fatalf("new ADR must differ from the superseded file")
	}

	oldData, err := os.ReadFile(filepath.Join(tempDir, filepath.FromSlash(oldRel)))
	if err != nil {
		t.Fatalf("reading old ADR: %v", err)
	}
	oldContent := string(oldData)
	if !strings.Contains(oldContent, "status: superseded") {
		t.Errorf("prior accepted ADR with overlapping title must be superseded:\n%s", oldContent)
	}
	if !strings.Contains(oldContent, paths[0]) {
		t.Errorf("supersession note must link the new ADR %q:\n%s", paths[0], oldContent)
	}

	// The new ADR itself stays accepted and unmarked.
	newData, err := os.ReadFile(filepath.Join(tempDir, filepath.FromSlash(paths[0])))
	if err != nil {
		t.Fatalf("reading new ADR: %v", err)
	}
	if !strings.Contains(string(newData), "status: accepted") {
		t.Errorf("new ADR must stay accepted:\n%s", newData)
	}
	if strings.Contains(string(newData), "Superseded by") {
		t.Errorf("new ADR must not carry a supersession note:\n%s", newData)
	}
}

func TestAutoGenerateADRsNoFalseSupersession(t *testing.T) {
	tempDir := t.TempDir()
	oldRel, err := GenerateADR(tempDir, ADREvent{
		Type: "AUTH_ARCHITECTURE", Title: "Authentication Architecture Change",
		Context: "ctx", Decision: "dec", Consequence: "con",
		CommitHash: "aaa1111", Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("GenerateADR failed: %v", err)
	}
	before, _ := os.ReadFile(filepath.Join(tempDir, filepath.FromSlash(oldRel)))

	paths, err := AutoGenerateADRs(tempDir, "bbb2222", "feat: split package into submodules to reduce coupling")
	if err != nil {
		t.Fatalf("AutoGenerateADRs failed: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 ADR path, got %v", paths)
	}
	after, _ := os.ReadFile(filepath.Join(tempDir, filepath.FromSlash(oldRel)))
	if string(after) != string(before) {
		t.Errorf("unrelated accepted ADR must be left untouched:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

func TestValidateADRLifecycleDeprecatedLink(t *testing.T) {
	// Deprecated without any deprecation link → failure.
	bad := t.TempDir()
	writeADRFixture(t, bad, "0001-old.md", `---
status: deprecated
date: 2026-09-01
---

# 0001. Old

No link anywhere.
`)
	if got := ValidateADRLifecycle(bad); len(got) != 1 {
		t.Fatalf("expected 1 deprecated-link failure, got %v", got)
	} else if !strings.Contains(got[0], "0001-old.md") || !strings.Contains(got[0], "deprecated") {
		t.Errorf("failure must name the file and status, got %v", got)
	}

	// Deprecated with a deprecation link → clean.
	good := t.TempDir()
	writeADRFixture(t, good, "0001-old.md", `---
status: deprecated
date: 2026-09-01
---

# 0001. Old

> Deprecated by [docs/adr/0002-new.md](docs/adr/0002-new.md)
`)
	if got := ValidateADRLifecycle(good); len(got) != 0 {
		t.Errorf("deprecated ADR with link should validate, got %v", got)
	}
}

func TestValidateADRLifecycleUnknownVocabulary(t *testing.T) {
	dir := t.TempDir()
	writeADRFixture(t, dir, "0001-weird.md", `---
status: draft-pending
---

# 0001. Weird
`)
	failures := ValidateADRLifecycle(dir)
	if len(failures) != 1 {
		t.Fatalf("expected 1 unknown-vocabulary failure, got %v", failures)
	}
	if !strings.Contains(failures[0], "0001-weird.md") {
		t.Errorf("failure must name the file, got %v", failures)
	}
}

func TestWriteTimelineData(t *testing.T) {
	tempDir := t.TempDir()
	writeADRFixture(t, tempDir, "0002-new.md", `---
status: accepted
date: 2026-09-09
commit: "bbb2222"
---

# 0002. New Decision
`)
	writeADRFixture(t, tempDir, "0001-old.md", `---
status: superseded
date: 2026-09-01
commit: "aaa1111"
---

# 0001. Old Decision

> Superseded by [docs/adr/0002-new.md](docs/adr/0002-new.md)
`)
	if err := WriteTimelineData(tempDir); err != nil {
		t.Fatalf("WriteTimelineData failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tempDir, "docs", "adr", "timeline.json"))
	if err != nil {
		t.Fatalf("reading timeline: %v", err)
	}
	// Decode structurally: array of {file,title,status,date,commit}.
	var raw []ADRTimelineEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("timeline is not a JSON array: %v\n%s", err, data)
	}
	if len(raw) != 2 {
		t.Fatalf("expected 2 timeline entries, got %v", raw)
	}
	if raw[0].File != "0001-old.md" || raw[1].File != "0002-new.md" {
		t.Errorf("timeline must be filename-ascending, got %v", raw)
	}
	if raw[0].Status != "superseded" || raw[1].Status != "accepted" {
		t.Errorf("timeline statuses wrong: %+v", raw)
	}
	if raw[0].Date != "2026-09-01" || raw[0].Commit != "aaa1111" {
		t.Errorf("timeline date/commit wrong: %+v", raw[0])
	}
	if !strings.Contains(raw[0].Title, "Old Decision") {
		t.Errorf("timeline title wrong: %+v", raw[0])
	}
}

func TestRegenerateIndexWritesTimeline(t *testing.T) {
	tempDir := t.TempDir()
	writeADRFixture(t, tempDir, "0001-solo.md", `---
status: accepted
date: 2026-09-09
commit: "ccc3333"
---

# 0001. Solo
`)
	if err := RegenerateIndex(tempDir); err != nil {
		t.Fatalf("RegenerateIndex failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tempDir, "docs", "adr", "timeline.json"))
	if err != nil {
		t.Fatalf("RegenerateIndex must emit timeline.json: %v", err)
	}
	if !strings.Contains(string(data), "0001-solo.md") {
		t.Errorf("timeline missing ADR entry:\n%s", data)
	}
	// Empty adr dir → empty array, not null.
	empty := t.TempDir()
	if err := WriteTimelineData(empty); err != nil {
		t.Fatalf("WriteTimelineData on empty root failed: %v", err)
	}
	emptyData, _ := os.ReadFile(filepath.Join(empty, "docs", "adr", "timeline.json"))
	if strings.TrimSpace(string(emptyData)) != "[]" {
		t.Errorf("empty timeline must be [], got:\n%s", emptyData)
	}
}
