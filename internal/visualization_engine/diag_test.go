package visualization_engine

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage1"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func TestDiagAllTypes(t *testing.T) {
	ttl := "G:/GlassMarble/.glassmarble/akg_state.ttl"
	if _, err := os.Stat(ttl); err != nil {
		t.Skip("skipping integration test: .glassmarble/akg_state.ttl not present on disk")
	}
	nodes, edges, err := stage1.ParseTTLFile(ttl)
	if err != nil {
		t.Fatal(err)
	}
	full := &types.NativeGraph{Nodes: nodes, Edges: edges}
	all := []types.DiagramType{
		types.UMLClass, types.UMLObject, types.UMLComponent, types.UMLDeployment,
		types.UMLPackage, types.UMLComposite, types.UMLProfile, types.UMLUsecase,
		types.UMLActivity, types.UMLState, types.UMLSequence, types.UMLCommunication,
		types.UMLInteractionOverview, types.UMLTiming,
		types.C4Context, types.C4Container, types.C4Component, types.C4Code,
		types.C4Landscape, types.C4Dynamic, types.C4Deployment,
		types.DataFlow, types.ERDiagram, types.Mindmap, types.Flowchart,
		types.DependencyGraph, types.HotspotComplexity, types.CallGraph,
		types.LayeredArchitecture, types.ChangeImpact, types.Infrastructure,
	}
	for _, dt := range all {
		cfg := stage1.GetExtractionConfig(dt, types.QueryOptions{})
		sub, _, err := stage1.ExtractFromSubgraph(full, cfg, types.QueryOptions{})
		if err != nil {
			fmt.Printf("%-22s ERROR: %v\n", dt, err)
			continue
		}
		edgeKinds := map[string]int{}
		for _, e := range sub.Edges {
			edgeKinds[e.Predicate]++
		}
		kindCounts := map[string]int{}
		for _, n := range sub.Nodes {
			kindCounts[n.Kind]++
		}
		fmt.Printf("%-22s nodes=%-6d edges=%-6d nodeKinds=%v edgeKinds=%v\n", dt, len(sub.Nodes), len(sub.Edges), kindCounts, edgeKinds)
	}
}

func TestDiagEndpoints(t *testing.T) {
	ttl := "G:/GlassMarble/.glassmarble/akg_state.ttl"
	if _, err := os.Stat(ttl); err != nil {
		t.Skip("skipping integration test: .glassmarble/akg_state.ttl not present on disk")
	}
	nodes, edges, err := stage1.ParseTTLFile(ttl)
	if err != nil {
		t.Fatal(err)
	}
	preds := []string{"gm:dataFlowTo", "gm:escapesToHeap", "gm:instantiatesGeneric", "gm:contains", "gm:queriesDatabase", "gm:contextualCall", "gm:callsCloudAPI", "gm:dependsOn", "gm:branchConstraint", "gm:defersExecution", "gm:controlFlowTo", "gm:aliasType", "gm:aliasesType", "gm:spawnsConcurrent", "gm:exposesEndpoint", "gm:securitySink"}
	for _, p := range preds {
		combos := map[string]int{}
		for _, e := range edges {
			if e.Predicate != p {
				continue
			}
			srcK, tgtK := "?", "?"
			if n := nodes[e.SourceID]; n != nil {
				srcK = n.Kind
			}
			if n := nodes[e.TargetID]; n != nil {
				tgtK = n.Kind
			}
			combos[srcK+" -> "+tgtK]++
		}
		if len(combos) == 0 {
			fmt.Printf("%-24s --- none ---\n", p)
			continue
		}
		fmt.Printf("%-24s (total %d)\n", p, len(combos))
		for c, n := range combos {
			fmt.Printf("    %s: %d\n", c, n)
		}
	}
	ep := 0
	mainNames := 0
	mainSample := ""
	for id, n := range nodes {
		if n.IsEntrypoint {
			ep++
		}
		if (n.Kind == "gm:Function" || n.Kind == "gm:Method" || n.Kind == "gm:Executable") && strings.EqualFold(n.Name, "main") {
			mainNames++
			if mainSample == "" {
				mainSample = id
			}
		}
	}
	fmt.Printf("ENTRYPOINT-FLAGGED: %d\n", ep)
	fmt.Printf("FUNCTIONS-NAMED-MAIN: %d sample=%s\n", mainNames, mainSample)
}

func TestDiagEntry(t *testing.T) {
	ttl := "G:/GlassMarble/.glassmarble/akg_state.ttl"
	if _, err := os.Stat(ttl); err != nil {
		t.Skip("skipping integration test: .glassmarble/akg_state.ttl not present on disk")
	}
	nodes, _, err := stage1.ParseTTLFile(ttl)
	if err != nil {
		t.Fatal(err)
	}
	var mSample, fSample, sSample, iSample []string
	methods, funcs, structs, ifaces := 0, 0, 0, 0
	for id, n := range nodes {
		switch n.Kind {
		case "gm:Method":
			methods++
			if len(mSample) < 5 {
				mSample = append(mSample, id+"  name="+n.Name)
			}
		case "gm:Function":
			funcs++
			if len(fSample) < 5 {
				fSample = append(fSample, id+"  name="+n.Name)
			}
		case "gm:Struct":
			structs++
			if len(sSample) < 5 {
				sSample = append(sSample, id+"  name="+n.Name)
			}
		case "gm:Interface":
			ifaces++
			if len(iSample) < 5 {
				iSample = append(iSample, id+"  name="+n.Name)
			}
		}
	}
	fmt.Println("METHOD ids:", mSample)
	fmt.Println("FUNCTION ids:", fSample)
	fmt.Println("STRUCT ids:", sSample)
	fmt.Println("INTERFACE ids:", iSample)
	fmt.Printf("counts: methods=%d funcs=%d structs=%d ifaces=%d\n", methods, funcs, structs, ifaces)
}

func TestDiagComponent(t *testing.T) {
	ttl := "G:/GlassMarble/.glassmarble/akg_state.ttl"
	if _, err := os.Stat(ttl); err != nil {
		t.Skip("skipping integration test: .glassmarble/akg_state.ttl not present on disk")
	}
	t0 := nowMs()
	nodes, edges, err := stage1.ParseTTLFile(ttl)
	if err != nil {
		t.Fatal(err)
	}
	t1 := nowMs()
	fmt.Printf("PARSE: %d ms\n", t1-t0)
	cfg := stage1.GetExtractionConfig(types.UMLComponent, types.QueryOptions{})
	sub, _, err := stage1.ExtractFromSubgraph(&types.NativeGraph{Nodes: nodes, Edges: edges}, cfg, types.QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t2 := nowMs()
	fmt.Printf("EXTRACT: %d ms (nodes=%d edges=%d)\n", t2-t1, len(sub.Nodes), len(sub.Edges))
	_ = stage2.ComputeAllMetrics(sub)
	t3 := nowMs()
	fmt.Printf("METRICS: %d ms\n", t3-t2)
	_ = stage2.DetectCommunities(sub)
	t4 := nowMs()
	fmt.Printf("CLUSTER: %d ms\n", t4-t3)
	_ = stage2.BuildLayoutTreeEx(sub, &stage2.DiagramMetrics{}, nil, types.QueryOptions{}, types.UMLComponent)
	t5 := nowMs()
	fmt.Printf("LAYOUT: %d ms\n", t5-t4)
}

func TestDiagTiming(t *testing.T) {
	ttl := "G:/GlassMarble/.glassmarble/akg_state.ttl"
	if _, err := os.Stat(ttl); err != nil {
		t.Skip("skipping integration test: .glassmarble/akg_state.ttl not present on disk")
	}
	t0 := nowMs()
	nodes, edges, err := stage1.ParseTTLFile(ttl)
	if err != nil {
		t.Fatal(err)
	}
	t1 := nowMs()
	fmt.Printf("PARSE: %d ms\n", t1-t0)
	cfg := stage1.GetExtractionConfig(types.UMLClass, types.QueryOptions{})
	sub, _, err := stage1.ExtractFromSubgraph(&types.NativeGraph{Nodes: nodes, Edges: edges}, cfg, types.QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t2 := nowMs()
	fmt.Printf("EXTRACT: %d ms (nodes=%d edges=%d)\n", t2-t1, len(sub.Nodes), len(sub.Edges))
	_ = stage2.ComputeAllMetrics(sub)
	t3 := nowMs()
	fmt.Printf("METRICS: %d ms\n", t3-t2)
	_ = stage2.DetectCommunities(sub)
	t4 := nowMs()
	fmt.Printf("CLUSTER: %d ms\n", t4-t3)
	_ = stage2.BuildLayoutTreeEx(sub, &stage2.DiagramMetrics{}, nil, types.QueryOptions{}, types.UMLClass)
	t5 := nowMs()
	fmt.Printf("LAYOUT: %d ms\n", t5-t4)
}

var _t0 = time.Now()

func nowMs() int64 {
	return time.Since(_t0).Milliseconds()
}

func TestDiagParse(t *testing.T) {
	ttl := "G:/GlassMarble/.glassmarble/akg_state.ttl"
	if _, err := os.Stat(ttl); err != nil {
		t.Skip("skipping integration test: .glassmarble/akg_state.ttl not present on disk")
	}
	nodes, edges, err := stage1.ParseTTLFile(ttl)
	if err != nil {
		t.Fatal("PARSE ERROR:", err)
	}
	fmt.Printf("PARSED: %d nodes, %d edges\n", len(nodes), len(edges))

	kindCount := map[string]int{}
	for _, n := range nodes {
		kindCount[n.Kind]++
	}
	var kinds []string
	for k := range kindCount {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	fmt.Println("NODE KINDS:")
	for _, k := range kinds {
		fmt.Printf("  %s: %d\n", k, kindCount[k])
	}

	predCount := map[string]int{}
	danglingSrc, danglingTgt := 0, 0
	for _, e := range edges {
		predCount[e.Predicate]++
		if _, ok := nodes[e.SourceID]; !ok {
			danglingSrc++
		}
		if _, ok := nodes[e.TargetID]; !ok {
			danglingTgt++
		}
	}
	var preds []string
	for p := range predCount {
		preds = append(preds, p)
	}
	sort.Strings(preds)
	fmt.Println("EDGE PREDICATES:")
	for _, p := range preds {
		fmt.Printf("  %s: %d\n", p, predCount[p])
	}
	fmt.Printf("DANGLING src=%d tgt=%d\n", danglingSrc, danglingTgt)

	cfg := stage1.GetExtractionConfig(types.UMLClass, types.QueryOptions{})
	fmt.Printf("UMLClass cfg: kinds=%v groups=%v strategy=%v maxDepth=%d dir=%v includeUnused=%v\n",
		cfg.NodeKindFilter, cfg.PredicateGroup, cfg.EntryStrategy, cfg.MaxDepth, cfg.Direction, cfg.IncludeUnused)
	sub, _, err := stage1.ExtractFromSubgraph(&types.NativeGraph{Nodes: nodes, Edges: edges}, cfg, types.QueryOptions{})
	if err != nil {
		t.Fatal("UMLExtract error:", err)
	}
	fmt.Printf("UMLClass SUBGRAPH: %d nodes, %d edges\n", len(sub.Nodes), len(sub.Edges))
	if len(sub.Edges) > 0 {
		ep := map[string]int{}
		for _, e := range sub.Edges {
			ep[e.Predicate]++
		}
		for p, c := range ep {
			fmt.Printf("  sub-edge %s: %d\n", p, c)
		}
	}
	stage2.ComputeAllMetrics(sub)
	s := stage2.ComputeGraphSummary(sub)
	fmt.Printf("SUMMARY: nodes=%d edges=%d clusters=%d comps=%d\n", s.NodeCount, s.EdgeCount, s.ClusterCount, s.ConnectedComponents)

	// Dump sample edges with endpoint kinds
	fmt.Println("gm:contains samples (src kind -> tgt kind):")
	n := 0
	for _, e := range edges {
		if e.Predicate == "gm:contains" {
			src := nodes[e.SourceID]
			tgt := nodes[e.TargetID]
			srcK, tgtK := "?", "?"
			if src != nil {
				srcK = src.Kind
			}
			if tgt != nil {
				tgtK = tgt.Kind
			}
			fmt.Printf("  %s(%s) -> %s(%s)\n", e.SourceID[:min(len(e.SourceID), 60)], srcK, e.TargetID[:min(len(e.TargetID), 60)], tgtK)
			n++
			if n >= 12 {
				break
			}
		}
	}
	fmt.Println("gm:calls samples (src kind -> tgt kind):")
	n = 0
	for _, e := range edges {
		if e.Predicate == "gm:calls" {
			src := nodes[e.SourceID]
			tgt := nodes[e.TargetID]
			srcK, tgtK := "?", "?"
			if src != nil {
				srcK = src.Kind
			}
			if tgt != nil {
				tgtK = tgt.Kind
			}
			fmt.Printf("  %s(%s) -> %s(%s)\n", e.SourceID[:min(len(e.SourceID), 60)], srcK, e.TargetID[:min(len(e.TargetID), 60)], tgtK)
			n++
			if n >= 12 {
				break
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}