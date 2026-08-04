package views

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// RenderInspectDetail renders a node detail card for `gmb inspect [node_id]`
// and the interactive inspect table's Enter-detail view. It is the canonical
// detail renderer shared by the static view and the BubbleTea program.
func RenderInspectDetail(node *stage4.ResolvedNode, out, in []stage4.ResolvedEdge) string {
	lines := []string{
		tui.KV("ID", node.ID),
		tui.KV("Kind", node.Kind),
		tui.KV("Primitive", node.Primitive),
		tui.KV("File", fmt.Sprintf("%s (L%d - L%d)", node.FileSpec.Path, node.FileSpec.LineStart, node.FileSpec.LineEnd)),
	}
	for k, v := range node.Properties {
		lines = append(lines, tui.KV(k, v))
	}
	if len(out) > 0 {
		lines = append(lines, "", tui.StyleH2.Render(fmt.Sprintf("Outbound Edges (%d):", len(out))))
		for _, e := range out {
			lines = append(lines, tui.Indent(fmt.Sprintf("→ %s [%s] (L%d)", e.TargetID, e.Type, e.LineNumber), 2))
		}
	}
	if len(in) > 0 {
		lines = append(lines, "", tui.StyleH2.Render(fmt.Sprintf("Inbound Edges (%d):", len(in))))
		for _, e := range in {
			lines = append(lines, tui.Indent(fmt.Sprintf("← %s [%s] (L%d)", e.SourceID, e.Type, e.LineNumber), 2))
		}
	}
	return tui.StyleCard.Render(strings.Join(lines, "\n"))
}
