package render

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func TestThemes(t *testing.T) {
	themeNames := []string{"modern", "dark", "nordic", "forest", "mono", "unknown"}
	for _, name := range themeNames {
		theme := GetTheme(name)
		if theme == nil {
			t.Fatalf("GetTheme(%q) returned nil", name)
		}

		classDefs := theme.EmitMermaidClassDefs()
		if !strings.Contains(classDefs, "classDef") {
			t.Errorf("Theme %s Mermaid classDefs missing classDef: %s", name, classDefs)
		}
		if !strings.Contains(classDefs, "entrypoint") || !strings.Contains(classDefs, "datastore") {
			t.Errorf("Theme %s missing key archetype classDefs", name)
		}

		skinparams := theme.EmitPlantUMLSkinparams()
		if !strings.Contains(skinparams, "skinparam") {
			t.Errorf("Theme %s PlantUML skinparams invalid: %s", name, skinparams)
		}

		dotAttrs := theme.EmitDOTGraphAttrs()
		if !strings.Contains(dotAttrs, "graph [") || !strings.Contains(dotAttrs, "node [") {
			t.Errorf("Theme %s DOT attrs invalid: %s", name, dotAttrs)
		}
	}
}

func TestArchetypesClassification(t *testing.T) {
	tests := []struct {
		name      string
		node      *types.LayoutNode
		wantArch  NodeArchetype
		wantShape string
	}{
		{
			name:      "CLI Command / Entrypoint",
			node:      &types.LayoutNode{ID: "cmd/root.go::ExecuteContext", Name: "ExecuteContext", Kind: ont.PredExecutable, IsEntrypoint: true},
			wantArch:  ArchEntrypoint,
			wantShape: "([",
		},
		{
			name:      "Database Store",
			node:      &types.LayoutNode{ID: "internal/akg/store.go::AKGStore", Name: "AKGStore", Kind: ont.PredDatabase, PrimitiveType: "DATABASE"},
			wantArch:  ArchDataStore,
			wantShape: "[(",
		},
		{
			name:      "Event Queue",
			node:      &types.LayoutNode{ID: "internal/events/bus.go::EventBus", Name: "EventBus", Kind: ont.PredStruct, PrimitiveType: "MESSAGE_QUEUE"},
			wantArch:  ArchEventBus,
			wantShape: "{{",
		},
		{
			name:      "External Cloud API",
			node:      &types.LayoutNode{ID: "ext_openai", Name: "OpenAIClient", Kind: ont.PredExternalAPI},
			wantArch:  ArchExternalAPI,
			wantShape: "[[",
		},
		{
			name:      "Interface Contract",
			node:      &types.LayoutNode{ID: "internal/engine/interfaces.go::Renderer", Name: "Renderer", Kind: ont.PredInterface},
			wantArch:  ArchInterface,
			wantShape: "([",
		},
		{
			name:      "Data Model",
			node:      &types.LayoutNode{ID: "internal/archmodel/model.go::ArchEvent", Name: "ArchEvent", Kind: ont.PredStruct},
			wantArch:  ArchModel,
			wantShape: "[",
		},
		{
			name:      "Complexity Hotspot",
			node:      &types.LayoutNode{ID: "internal/complex.go::HeavyLogic", Name: "HeavyLogic", IsHotspot: true},
			wantArch:  ArchHotspot,
			wantShape: "{{",
		},
		{
			name:      "Central God Object",
			node:      &types.LayoutNode{ID: "internal/god.go::GodObject", Name: "GodObject", IsGodObject: true},
			wantArch:  ArchGodObject,
			wantShape: ">",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyNodeArchetype(tc.node)
			if got != tc.wantArch {
				t.Errorf("ClassifyNodeArchetype(%v) = %v, want %v", tc.node.Name, got, tc.wantArch)
			}
			stmt := FormatMermaidNode(got, "node1", tc.node.Name, "subtext")
			if !strings.Contains(stmt, tc.wantShape) {
				t.Errorf("FormatMermaidNode shape mismatch, stmt=%s, wantShape=%s", stmt, tc.wantShape)
			}
			if !strings.Contains(stmt, got.Stereotype()) {
				t.Errorf("FormatMermaidNode missing stereotype %s, stmt=%s", got.Stereotype(), stmt)
			}
		})
	}
}

func TestHTMLStudioGeneration(t *testing.T) {
	html := RenderHTMLStudio("flowchart TB\n    A --> B", types.CallGraph, &types.GraphSummary{NodeCount: 2, EdgeCount: 1}, "modern")
	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Errorf("RenderHTMLStudio missing doctype")
	}
	if !strings.Contains(html, "mermaid.min.js") {
		t.Errorf("RenderHTMLStudio missing mermaid script")
	}
	if !strings.Contains(html, "GlassMarble") {
		t.Errorf("RenderHTMLStudio missing GlassMarble branding")
	}
	if !strings.Contains(html, "flowchart TB") {
		t.Errorf("RenderHTMLStudio missing diagram markup")
	}
}
