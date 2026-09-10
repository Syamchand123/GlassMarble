package invalidator

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

func TestDirtyQueuePriorityAndBudget(t *testing.T) {
	refs := []config.DirtySectionRef{
		{DocID: "mod", SectionID: "interface"},
		{DocID: "sec", SectionID: "threat-model"},
		{DocID: "cfg", SectionID: "env-vars"},
		{DocID: "arch", SectionID: "overview"},
		{DocID: "int", SectionID: "internals"},
	}

	specs := map[string]*config.SectionSpec{
		SpecKey("mod", "interface"):    {ID: "interface", GroundWith: []string{"exported_symbols", "signatures"}},
		SpecKey("sec", "threat-model"): {ID: "threat-model", GroundWith: []string{"sentinels", "error_returns"}},
		SpecKey("cfg", "env-vars"):     {ID: "env-vars", GroundWith: []string{"config_vars"}},
		SpecKey("arch", "overview"):    {ID: "overview", GroundWith: []string{"arch_intelligence"}},
		SpecKey("int", "internals"):    {ID: "internals", GroundWith: []string{"signatures"}},
	}

	// Dossier with sentinel changes + arch events + exported symbol changes.
	dossier := &config.GlobalCommitDossier{
		AddedSentinels: []config.SentinelFact{{FQN: "internal/auth.ErrBoom"}},
		ArchEvents:     []string{"SERVICE_ADDED"},
		AddedSymbols: []config.SymbolFact{
			{FQN: "internal/mod.go::NewService"},
		},
	}

	q := NewDirtyQueue(refs)
	q.AssignPriorities(dossier, specs)
	q.Sort()
	items := q.Items()
	if len(items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(items))
	}

	got := map[string]int{}
	for _, it := range items {
		got[it.DocID] = it.Priority
	}
	if got["sec"] != 1 {
		t.Errorf("sentinel-grounded section with sentinel changes should be priority 1, got %d", got["sec"])
	}
	if got["arch"] != 2 {
		t.Errorf("section with arch events in dossier should be priority 2, got %d", got["arch"])
	}
	if got["cfg"] != 5 {
		t.Errorf("config-only section should be priority 5, got %d", got["cfg"])
	}
	// Security first, config last.
	if items[0].DocID != "sec" {
		t.Errorf("expected security section first, got %+v", items[0])
	}
	if items[len(items)-1].DocID != "cfg" {
		t.Errorf("expected config section last, got %+v", items[len(items)-1])
	}

	q.EnforceBudget(2)
	if q.Len() != 2 {
		t.Fatalf("expected budget truncation to 2, got %d", q.Len())
	}
	if q.Items()[0].DocID != "sec" {
		t.Errorf("budget must keep highest priority, got %+v", q.Items()[0])
	}
}

func TestPriorityForSection_SignalOrdering(t *testing.T) {
	apiSpec := &config.SectionSpec{ID: "iface", GroundWith: []string{"exported_symbols"}}
	internalSpec := &config.SectionSpec{ID: "impl", GroundWith: []string{"signatures"}}
	configSpec := &config.SectionSpec{ID: "env", GroundWith: []string{"config_vars"}}

	// API surface change, no arch events, no sentinel changes.
	// Rule 3 keys off the dossier (not the section), so any non-config
	// section in this dossier queues at 3.
	dossier := &config.GlobalCommitDossier{
		AddedSymbols: []config.SymbolFact{{FQN: "internal/mod.go::NewService"}},
	}
	ref := config.DirtySectionRef{DocID: "d", SectionID: "iface"}
	if p := PriorityForSection(ref, dossier, apiSpec); p != 3 {
		t.Errorf("exported symbol change should be priority 3, got %d", p)
	}
	if p := PriorityForSection(ref, dossier, internalSpec); p != 3 {
		t.Errorf("any non-config section with API changes should be priority 3, got %d", p)
	}
	if p := PriorityForSection(ref, dossier, configSpec); p != 5 {
		t.Errorf("config-only section should be priority 5, got %d", p)
	}

	// Dossier with only unexported changes: no rule fires → default 4.
	unexported := &config.GlobalCommitDossier{
		AddedSymbols: []config.SymbolFact{{FQN: "internal/mod.go::parseRaw"}},
	}
	if p := PriorityForSection(ref, unexported, apiSpec); p != 4 {
		t.Errorf("unexported-only change should be priority 4, got %d", p)
	}

	// Nil dossier degrades to defaults.
	if p := PriorityForSection(ref, nil, internalSpec); p != 4 {
		t.Errorf("nil dossier should default to priority 4, got %d", p)
	}
	if p := PriorityForSection(ref, nil, configSpec); p != 5 {
		t.Errorf("nil dossier with config-only spec should be priority 5, got %d", p)
	}
	if p := PriorityForSection(ref, nil, nil); p != 4 {
		t.Errorf("nil dossier and nil spec should default to priority 4, got %d", p)
	}
}
