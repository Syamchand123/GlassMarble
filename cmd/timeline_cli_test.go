package cmd_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// timelineTestEvent builds a valid architectural event with evidence,
// deterministic ID and a fixed title/commit for timeline assertions.
func timelineTestEvent(id string, kind archmodel.EventKind, ts time.Time, title string, components []string) archmodel.ArchEvent {
	b := evidence.NewBundle(evidence.EvidenceItem{
		Source:     evidence.SourceGit,
		Reference:  "commit-" + id,
		Confidence: 0.9,
		Timestamp:  ts,
	})
	return archmodel.ArchEvent{
		ID:         id,
		Kind:       kind,
		CommitHash: "c" + id,
		Timestamp:  ts,
		Title:      title,
		Components: components,
		Evidence:   b,
	}
}

// seedTimelineMemory ingests events into the repo's developer memory
// (.glassmarble/memory/) exactly as the Stage 6 pipeline would.
func seedTimelineMemory(t *testing.T, root string, events []archmodel.ArchEvent) {
	t.Helper()
	store := developer_memory.NewStoreForRepo(root)
	builder := developer_memory.NewMemoryBuilderWithOptions(store,
		developer_memory.WithProjectID("timeline-test"))
	n, err := builder.ProcessEvents(events)
	if err != nil {
		t.Fatalf("seed developer memory: %v", err)
	}
	if n != len(events) {
		t.Fatalf("seeded %d events, want %d", n, len(events))
	}
}

func timelineSeedEvents(t *testing.T) []archmodel.ArchEvent {
	t.Helper()
	// Both timestamps inside the default 6-month window so the unfiltered
	// command shows them.
	older := time.Now().UTC().AddDate(0, -2, 0).Truncate(time.Second)
	newer := older.AddDate(0, 1, 0).Add(10 * time.Minute)
	return []archmodel.ArchEvent{
		timelineTestEvent("e1", archmodel.EventServiceAdded, older, "billing service added", []string{"billing"}),
		timelineTestEvent("e2", archmodel.EventSmellResolved, newer, "auth smell resolved", []string{"auth"}),
	}
}

// TestTimelineCLI_EmptyMemoryHint verifies the friendly hint when no
// architectural events have been recorded yet.
func TestTimelineCLI_EmptyMemoryHint(t *testing.T) {
	root := setupAnalyzeGitRepo(t)
	if _, err := runGmbCommand(t, "init", "--dir", root); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	output, err := runGmbCommand(t, "timeline", "--dir", root)
	if err != nil {
		t.Fatalf("timeline failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Developer memory is empty") {
		t.Fatalf("expected the empty-memory hint, got:\n%s", output)
	}
}

// TestTimelineCLI_TextAndFull verifies the compact and verbose text renders.
func TestTimelineCLI_TextAndFull(t *testing.T) {
	root := setupAnalyzeGitRepo(t)
	seedTimelineMemory(t, root, timelineSeedEvents(t))

	for _, tc := range []struct {
		name   string
		args   []string
		want   []string
		not    []string
	}{
		{"text", []string{"timeline", "--dir", root}, []string{"billing service added", "auth smell resolved"}, nil},
		{"full", []string{"timeline", "--dir", root, "--full"}, []string{"billing service added", "commit: ce1", "kind:   SERVICE_ADDED", "components: billing"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, err := runGmbCommand(t, tc.args...)
			if err != nil {
				t.Fatalf("timeline failed: %v\n%s", err, output)
			}
			for _, w := range tc.want {
				if !strings.Contains(output, w) {
					t.Errorf("timeline output missing %q:\n%s", w, output)
				}
			}
			for _, n := range tc.not {
				if strings.Contains(output, n) {
					t.Errorf("timeline output unexpectedly contains %q:\n%s", n, output)
				}
			}
		})
	}
}

// TestTimelineCLI_JSON verifies the machine-readable output round-trips.
func TestTimelineCLI_JSON(t *testing.T) {
	root := setupAnalyzeGitRepo(t)
	seedTimelineMemory(t, root, timelineSeedEvents(t))

	output, err := runGmbCommand(t, "timeline", "--dir", root, "--format", "json")
	if err != nil {
		t.Fatalf("timeline --format json failed: %v\n%s", err, output)
	}
	var entries []archmodel.TimelineEntry
	if err := json.Unmarshal([]byte(output), &entries); err != nil {
		t.Fatalf("timeline JSON does not parse: %v\n%s", err, output)
	}
	if len(entries) != 2 {
		t.Fatalf("json timeline has %d entries, want 2:\n%s", len(entries), output)
	}
	if entries[0].Title != "billing service added" || entries[1].Title != "auth smell resolved" {
		t.Errorf("json timeline order wrong (want oldest first): %+v", entries)
	}
}

// TestTimelineCLI_Mermaid verifies the Mermaid timeline diagram output.
func TestTimelineCLI_Mermaid(t *testing.T) {
	root := setupAnalyzeGitRepo(t)
	seedTimelineMemory(t, root, timelineSeedEvents(t))

	output, err := runGmbCommand(t, "timeline", "--dir", root, "--format", "mermaid")
	if err != nil {
		t.Fatalf("timeline --format mermaid failed: %v\n%s", err, output)
	}
	for _, w := range []string{"timeline", "title Architecture Evolution", "section ", "billing service added", "auth smell resolved"} {
		if !strings.Contains(output, w) {
			t.Errorf("mermaid timeline missing %q:\n%s", w, output)
		}
	}
}

// TestTimelineCLI_ComponentFilter verifies the substring component filter
// only shows matching entries.
func TestTimelineCLI_ComponentFilter(t *testing.T) {
	root := setupAnalyzeGitRepo(t)
	seedTimelineMemory(t, root, timelineSeedEvents(t))

	output, err := runGmbCommand(t, "timeline", "--dir", root, "--component", "bill")
	if err != nil {
		t.Fatalf("timeline --component failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "billing service added") {
		t.Errorf("component filter dropped the billing entry:\n%s", output)
	}
	if strings.Contains(output, "auth smell resolved") {
		t.Errorf("component filter kept the auth entry:\n%s", output)
	}
}

// TestTimelineCLI_WindowFilter verifies --from/--to date bounds.
func TestTimelineCLI_WindowFilter(t *testing.T) {
	root := setupAnalyzeGitRepo(t)
	events := timelineSeedEvents(t)
	seedTimelineMemory(t, root, events)

	from := events[1].Timestamp.Add(-1 * time.Hour).Format(time.RFC3339)
	to := events[1].Timestamp.Add(1 * time.Hour).Format(time.RFC3339)

	output, err := runGmbCommand(t, "timeline", "--dir", root, "--from", from, "--to", to)
	if err != nil {
		t.Fatalf("timeline --from/--to failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "auth smell resolved") {
		t.Errorf("window excluded the newest entry:\n%s", output)
	}
	if strings.Contains(output, "billing service added") {
		t.Errorf("window kept the older entry:\n%s", output)
	}
}

// TestTimelineCLI_RefWindow verifies git refs are resolved to author
// timestamps: a ref pointing at HEAD (now) excludes both seeded entries.
func TestTimelineCLI_RefWindow(t *testing.T) {
	root := setupAnalyzeGitRepo(t)
	seedTimelineMemory(t, root, timelineSeedEvents(t))

	output, err := runGmbCommand(t, "timeline", "--dir", root, "--from", "HEAD", "--to", "HEAD")
	if err != nil {
		t.Fatalf("timeline with git refs failed: %v\n%s", err, output)
	}
	if strings.Contains(output, "billing service added") || strings.Contains(output, "auth smell resolved") {
		t.Errorf("--from HEAD should exclude older seeded entries:\n%s", output)
	}
}

// TestTimelineCLI_BadArg verifies invalid inputs fail loudly.
func TestTimelineCLI_BadArg(t *testing.T) {
	root := setupAnalyzeGitRepo(t)
	seedTimelineMemory(t, root, timelineSeedEvents(t))

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"unknown format", []string{"timeline", "--dir", root, "--format", "html"}, "unknown format"},
		{"bad date", []string{"timeline", "--dir", root, "--from", "not-a-date"}, "cannot parse"},
		{"bad ref", []string{"timeline", "--dir", root, "--from", "no-such-ref-xyz"}, "cannot parse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runGmbCommand(t, tc.args...)
			if err == nil {
				t.Fatalf("expected an error for %s, got none", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}
