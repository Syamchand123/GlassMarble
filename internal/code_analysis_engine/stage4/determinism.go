package stage4

import (
	"sort"
	"strings"
)

// SortDeterministic is the W1-16 deterministic ordering pass (V-05 order
// class): after the merge + cleanup, every emitted edge list is sorted by a
// canonical key so consumers (AKG serializer, visualization extractors)
// observe a stable order regardless of Go map iteration or concurrent pass
// scheduling.
//
// Outbound edges sort by (Type, TargetID, LineNumber, SourceID, properties);
// inbound edges by (Type, SourceID, LineNumber, TargetID, properties).
// Properties are folded in as a sorted key=value suffix so parallel edges
// that legitimately coexist (registerEdge key includes gm:embedding /
// gm:provenance) are fully ordered. GraphNodes is a map and cannot be
// "sorted"; the AKG CowMap serializes it in key order already.
func SortDeterministic(cpg *Stage4Output) {
	if cpg == nil {
		return
	}

	outboundLess := func(a, b ResolvedEdge) bool {
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.TargetID != b.TargetID {
			return a.TargetID < b.TargetID
		}
		if a.LineNumber != b.LineNumber {
			return a.LineNumber < b.LineNumber
		}
		if a.SourceID != b.SourceID {
			return a.SourceID < b.SourceID
		}
		return propsKey(a) < propsKey(b)
	}
	inboundLess := func(a, b ResolvedEdge) bool {
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.SourceID != b.SourceID {
			return a.SourceID < b.SourceID
		}
		if a.LineNumber != b.LineNumber {
			return a.LineNumber < b.LineNumber
		}
		if a.TargetID != b.TargetID {
			return a.TargetID < b.TargetID
		}
		return propsKey(a) < propsKey(b)
	}

	for src, edges := range cpg.OutboundEdges {
		sort.SliceStable(edges, func(i, j int) bool { return outboundLess(edges[i], edges[j]) })
		cpg.OutboundEdges[src] = edges
	}
	for dst, edges := range cpg.InboundEdges {
		sort.SliceStable(edges, func(i, j int) bool { return inboundLess(edges[i], edges[j]) })
		cpg.InboundEdges[dst] = edges
	}
}

// propsKey renders an edge's properties as a stable, sorted key=value
// string for tie-breaking ("" when none).
func propsKey(e ResolvedEdge) string {
	if len(e.Properties) == 0 {
		return ""
	}
	keys := make([]string, 0, len(e.Properties))
	for k := range e.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(e.Properties[k])
		sb.WriteByte('\x00')
	}
	return sb.String()
}
