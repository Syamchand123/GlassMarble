package render

import (
	"fmt"
	"sort"
	"strings"
)

// ArchetypeStyle defines the visual styling tokens for a specific architectural archetype.
type ArchetypeStyle struct {
	Fill        string
	Stroke      string
	TextColor   string
	StrokeWidth string
	StrokeDash  string // e.g. "4 4" or empty
	Accent      string
}

// Theme defines the complete color palette and styling for all diagram targets (Mermaid, PlantUML, DOT).
type Theme struct {
	Name            string
	CanvasBg        string
	SubgraphFill    string
	SubgraphStroke  string
	SubgraphText    string
	EdgeColor       string
	DataFlowEdge    string
	CycleEdge       string
	AsyncEdge       string
	ArchetypeStyles map[string]ArchetypeStyle
}

// Built-in theme instances
var (
	ThemeModern = &Theme{
		Name:           "modern",
		CanvasBg:       "#FFFFFF",
		SubgraphFill:   "#F8FAFC",
		SubgraphStroke: "#CBD5E1",
		SubgraphText:   "#0F172A",
		EdgeColor:      "#475569",
		DataFlowEdge:   "#059669",
		CycleEdge:      "#DC2626",
		AsyncEdge:      "#7C3AED",
		ArchetypeStyles: map[string]ArchetypeStyle{
			"entrypoint": {Fill: "#ECFDF5", Stroke: "#059669", TextColor: "#064E3B", StrokeWidth: "2px"},
			"service":    {Fill: "#EEF2FF", Stroke: "#4F46E5", TextColor: "#1E1B4B", StrokeWidth: "1.5px"},
			"datastore":  {Fill: "#FEF3C7", Stroke: "#D97706", TextColor: "#78350F", StrokeWidth: "1.5px"},
			"eventbus":   {Fill: "#F5F3FF", Stroke: "#7C3AED", TextColor: "#4C1D95", StrokeWidth: "1.5px"},
			"external":   {Fill: "#F8FAFC", Stroke: "#64748B", TextColor: "#334155", StrokeWidth: "1.5px", StrokeDash: "4 4"},
			"interface":  {Fill: "#F0FDFA", Stroke: "#0D9488", TextColor: "#134E4A", StrokeWidth: "1.5px", StrokeDash: "3 3"},
			"model":      {Fill: "#F0FDF4", Stroke: "#16A34A", TextColor: "#14532D", StrokeWidth: "1.5px"},
			"parser":     {Fill: "#FFFBEB", Stroke: "#B45309", TextColor: "#78350F", StrokeWidth: "1.5px"},
			"renderer":   {Fill: "#EFF6FF", Stroke: "#2563EB", TextColor: "#1E3A8A", StrokeWidth: "1.5px"},
			"gateway":    {Fill: "#F0F9FF", Stroke: "#0284C7", TextColor: "#075985", StrokeWidth: "1.5px"},
			"hotspot":    {Fill: "#FEF2F2", Stroke: "#DC2626", TextColor: "#7F1D1D", StrokeWidth: "2.5px", Accent: "#EF4444"},
			"godobject":  {Fill: "#FFF7ED", Stroke: "#EA580C", TextColor: "#7C2D12", StrokeWidth: "2.5px", Accent: "#F97316"},
		},
	}

	ThemeDark = &Theme{
		Name:           "dark",
		CanvasBg:       "#0F172A",
		SubgraphFill:   "#1E293B",
		SubgraphStroke: "#334155",
		SubgraphText:   "#F8FAFC",
		EdgeColor:      "#94A3B8",
		DataFlowEdge:   "#34D399",
		CycleEdge:      "#F87171",
		AsyncEdge:      "#A78BFA",
		ArchetypeStyles: map[string]ArchetypeStyle{
			"entrypoint": {Fill: "#064E3B", Stroke: "#34D399", TextColor: "#ECFDF5", StrokeWidth: "2px"},
			"service":    {Fill: "#1E1B4B", Stroke: "#818CF8", TextColor: "#EEF2FF", StrokeWidth: "1.5px"},
			"datastore":  {Fill: "#451A03", Stroke: "#FBBF24", TextColor: "#FEF3C7", StrokeWidth: "1.5px"},
			"eventbus":   {Fill: "#3B0764", Stroke: "#A78BFA", TextColor: "#F5F3FF", StrokeWidth: "1.5px"},
			"external":   {Fill: "#1E293B", Stroke: "#94A3B8", TextColor: "#CBD5E1", StrokeWidth: "1.5px", StrokeDash: "4 4"},
			"interface":  {Fill: "#134E4A", Stroke: "#2DD4BF", TextColor: "#F0FDFA", StrokeWidth: "1.5px", StrokeDash: "3 3"},
			"model":      {Fill: "#14532D", Stroke: "#4ADE80", TextColor: "#F0FDF4", StrokeWidth: "1.5px"},
			"parser":     {Fill: "#78350F", Stroke: "#FCD34D", TextColor: "#FFFBEB", StrokeWidth: "1.5px"},
			"renderer":   {Fill: "#1E3A8A", Stroke: "#60A5FA", TextColor: "#EFF6FF", StrokeWidth: "1.5px"},
			"gateway":    {Fill: "#075985", Stroke: "#38BDF8", TextColor: "#F0F9FF", StrokeWidth: "1.5px"},
			"hotspot":    {Fill: "#450A0A", Stroke: "#F87171", TextColor: "#FEE2E2", StrokeWidth: "2.5px", Accent: "#EF4444"},
			"godobject":  {Fill: "#431407", Stroke: "#FB923C", TextColor: "#FFEDD5", StrokeWidth: "2.5px", Accent: "#F97316"},
		},
	}

	ThemeNordic = &Theme{
		Name:           "nordic",
		CanvasBg:       "#FFFFFF",
		SubgraphFill:   "#F1F5F9",
		SubgraphStroke: "#94A3B8",
		SubgraphText:   "#0F172A",
		EdgeColor:      "#475569",
		DataFlowEdge:   "#0284C7",
		CycleEdge:      "#E11D48",
		AsyncEdge:      "#6366F1",
		ArchetypeStyles: map[string]ArchetypeStyle{
			"entrypoint": {Fill: "#E0F2FE", Stroke: "#0284C7", TextColor: "#0369A1", StrokeWidth: "2px"},
			"service":    {Fill: "#F8FAFC", Stroke: "#475569", TextColor: "#0F172A", StrokeWidth: "1.5px"},
			"datastore":  {Fill: "#F1F5F9", Stroke: "#0EA5E9", TextColor: "#0369A1", StrokeWidth: "1.5px"},
			"eventbus":   {Fill: "#EEF2FF", Stroke: "#6366F1", TextColor: "#3730A3", StrokeWidth: "1.5px"},
			"external":   {Fill: "#FFFFFF", Stroke: "#64748B", TextColor: "#334155", StrokeWidth: "1.5px", StrokeDash: "4 4"},
			"interface":  {Fill: "#F0FDFA", Stroke: "#0D9488", TextColor: "#115E59", StrokeWidth: "1.5px", StrokeDash: "3 3"},
			"model":      {Fill: "#F8FAFC", Stroke: "#38BDF8", TextColor: "#0C4A6E", StrokeWidth: "1.5px"},
			"parser":     {Fill: "#F1F5F9", Stroke: "#64748B", TextColor: "#1E293B", StrokeWidth: "1.5px"},
			"renderer":   {Fill: "#E0F2FE", Stroke: "#38BDF8", TextColor: "#075985", StrokeWidth: "1.5px"},
			"gateway":    {Fill: "#F0F9FF", Stroke: "#0284C7", TextColor: "#0C4A6E", StrokeWidth: "1.5px"},
			"hotspot":    {Fill: "#FFF1F2", Stroke: "#E11D48", TextColor: "#9F1239", StrokeWidth: "2.5px", Accent: "#F43F5E"},
			"godobject":  {Fill: "#FFF7ED", Stroke: "#EA580C", TextColor: "#9A3412", StrokeWidth: "2.5px", Accent: "#FB923C"},
		},
	}

	ThemeForest = &Theme{
		Name:           "forest",
		CanvasBg:       "#FFFFFF",
		SubgraphFill:   "#F7FEE7",
		SubgraphStroke: "#A3E635",
		SubgraphText:   "#1A2E05",
		EdgeColor:      "#3F6212",
		DataFlowEdge:   "#15803D",
		CycleEdge:      "#B91C1C",
		AsyncEdge:      "#D97706",
		ArchetypeStyles: map[string]ArchetypeStyle{
			"entrypoint": {Fill: "#ECFDF5", Stroke: "#059669", TextColor: "#064E3B", StrokeWidth: "2px"},
			"service":    {Fill: "#F0FDF4", Stroke: "#16A34A", TextColor: "#14532D", StrokeWidth: "1.5px"},
			"datastore":  {Fill: "#FEF3C7", Stroke: "#D97706", TextColor: "#78350F", StrokeWidth: "1.5px"},
			"eventbus":   {Fill: "#FFFBEB", Stroke: "#CA8A04", TextColor: "#713F12", StrokeWidth: "1.5px"},
			"external":   {Fill: "#F9FAFB", Stroke: "#4B5563", TextColor: "#1F2937", StrokeWidth: "1.5px", StrokeDash: "4 4"},
			"interface":  {Fill: "#F0FDFA", Stroke: "#0D9488", TextColor: "#134E4A", StrokeWidth: "1.5px", StrokeDash: "3 3"},
			"model":      {Fill: "#F7FEE7", Stroke: "#65A30D", TextColor: "#365314", StrokeWidth: "1.5px"},
			"parser":     {Fill: "#FEF3C7", Stroke: "#B45309", TextColor: "#78350F", StrokeWidth: "1.5px"},
			"renderer":   {Fill: "#ECFDF5", Stroke: "#10B981", TextColor: "#064E3B", StrokeWidth: "1.5px"},
			"gateway":    {Fill: "#ECFCCB", Stroke: "#84CC16", TextColor: "#365314", StrokeWidth: "1.5px"},
			"hotspot":    {Fill: "#FEF2F2", Stroke: "#DC2626", TextColor: "#7F1D1D", StrokeWidth: "2.5px", Accent: "#EF4444"},
			"godobject":  {Fill: "#FFF7ED", Stroke: "#EA580C", TextColor: "#7C2D12", StrokeWidth: "2.5px", Accent: "#F97316"},
		},
	}

	ThemeMono = &Theme{
		Name:           "mono",
		CanvasBg:       "#FFFFFF",
		SubgraphFill:   "#F9FAFB",
		SubgraphStroke: "#9CA3AF",
		SubgraphText:   "#111827",
		EdgeColor:      "#374151",
		DataFlowEdge:   "#111827",
		CycleEdge:      "#000000",
		AsyncEdge:      "#4B5563",
		ArchetypeStyles: map[string]ArchetypeStyle{
			"entrypoint": {Fill: "#F3F4F6", Stroke: "#111827", TextColor: "#111827", StrokeWidth: "2.5px"},
			"service":    {Fill: "#FFFFFF", Stroke: "#374151", TextColor: "#111827", StrokeWidth: "1.5px"},
			"datastore":  {Fill: "#F9FAFB", Stroke: "#111827", TextColor: "#111827", StrokeWidth: "1.5px"},
			"eventbus":   {Fill: "#F3F4F6", Stroke: "#374151", TextColor: "#111827", StrokeWidth: "1.5px"},
			"external":   {Fill: "#FFFFFF", Stroke: "#4B5563", TextColor: "#111827", StrokeWidth: "1.5px", StrokeDash: "4 4"},
			"interface":  {Fill: "#FFFFFF", Stroke: "#111827", TextColor: "#111827", StrokeWidth: "1.5px", StrokeDash: "3 3"},
			"model":      {Fill: "#F9FAFB", Stroke: "#4B5563", TextColor: "#111827", StrokeWidth: "1.5px"},
			"parser":     {Fill: "#F3F4F6", Stroke: "#374151", TextColor: "#111827", StrokeWidth: "1.5px"},
			"renderer":   {Fill: "#FFFFFF", Stroke: "#111827", TextColor: "#111827", StrokeWidth: "1.5px"},
			"gateway":    {Fill: "#F3F4F6", Stroke: "#111827", TextColor: "#111827", StrokeWidth: "1.5px"},
			"hotspot":    {Fill: "#E5E7EB", Stroke: "#000000", TextColor: "#000000", StrokeWidth: "3px"},
			"godobject":  {Fill: "#E5E7EB", Stroke: "#000000", TextColor: "#000000", StrokeWidth: "3px"},
		},
	}
)

// GetTheme returns a Theme by name, defaulting to ThemeModern if not found.
func GetTheme(name string) *Theme {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "dark", "darknebula", "midnight":
		return ThemeDark
	case "nordic", "frost", "arctic":
		return ThemeNordic
	case "forest", "warmforest", "sage":
		return ThemeForest
	case "mono", "monochrome", "print":
		return ThemeMono
	default:
		return ThemeModern
	}
}

// EmitMermaidClassDefs outputs Mermaid classDef declarations for all archetypes.
// C3-3: ArchetypeStyles is a map; iteration order is randomized per run.
// Sorting keys makes diagram markup deterministic (required for TestGoldenParity).
func (t *Theme) EmitMermaidClassDefs() string {
	if t == nil {
		t = ThemeModern
	}
	keys := make([]string, 0, len(t.ArchetypeStyles))
	for k := range t.ArchetypeStyles {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, name := range keys {
		style := t.ArchetypeStyles[name]
		var parts []string
		if style.Fill != "" {
			parts = append(parts, fmt.Sprintf("fill:%s", style.Fill))
		}
		if style.Stroke != "" {
			parts = append(parts, fmt.Sprintf("stroke:%s", style.Stroke))
		}
		if style.StrokeWidth != "" {
			parts = append(parts, fmt.Sprintf("stroke-width:%s", style.StrokeWidth))
		}
		if style.TextColor != "" {
			parts = append(parts, fmt.Sprintf("color:%s", style.TextColor))
		}
		if style.StrokeDash != "" {
			parts = append(parts, fmt.Sprintf("stroke-dasharray:%s", style.StrokeDash))
		}
		if len(parts) > 0 {
			sb.WriteString(fmt.Sprintf("    classDef %s %s;\n", name, strings.Join(parts, ",")))
		}
	}
	return sb.String()
}

// EmitPlantUMLSkinparams outputs modern PlantUML skinparameters.
func (t *Theme) EmitPlantUMLSkinparams() string {
	if t == nil {
		t = ThemeModern
	}
	var sb strings.Builder
	sb.WriteString("skinparam roundcorner 10\n")
	sb.WriteString("skinparam shadowing false\n")
	sb.WriteString("skinparam defaultFontName \"Inter, -apple-system, Roboto, sans-serif\"\n")
	sb.WriteString("skinparam defaultFontSize 11\n")
	sb.WriteString(fmt.Sprintf("skinparam backgroundColor %s\n", t.CanvasBg))
	sb.WriteString(fmt.Sprintf("skinparam packageBackgroundColor %s\n", t.SubgraphFill))
	sb.WriteString(fmt.Sprintf("skinparam packageBorderColor %s\n", t.SubgraphStroke))
	sb.WriteString(fmt.Sprintf("skinparam packageFontColor %s\n", t.SubgraphText))
	sb.WriteString(fmt.Sprintf("skinparam arrowColor %s\n", t.EdgeColor))
	sb.WriteString("skinparam sequenceMessageAlign center\n")
	sb.WriteString("skinparam classAttributeIconSize 0\n")
	return sb.String()
}

// EmitDOTGraphAttrs outputs modern Graphviz DOT digraph attributes.
func (t *Theme) EmitDOTGraphAttrs() string {
	if t == nil {
		t = ThemeModern
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("    graph [fontname=\"Inter, -apple-system, sans-serif\", fontsize=11, bgcolor=\"%s\", pad=0.5, splines=ortho, nodesep=0.6, ranksep=0.8];\n", t.CanvasBg))
	sb.WriteString(fmt.Sprintf("    node [fontname=\"Inter, -apple-system, sans-serif\", fontsize=10, shape=box, style=\"filled,rounded\", fillcolor=\"%s\", color=\"%s\", fontcolor=\"%s\", penwidth=1.2, margin=\"0.2,0.1\"];\n",
		t.ArchetypeStyles["service"].Fill, t.ArchetypeStyles["service"].Stroke, t.ArchetypeStyles["service"].TextColor))
	sb.WriteString(fmt.Sprintf("    edge [fontname=\"Inter, -apple-system, sans-serif\", fontsize=9, color=\"%s\", penwidth=1.2, fontcolor=\"%s\", arrowhead=vee];\n",
		t.EdgeColor, t.EdgeColor))
	return sb.String()
}
