package context

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

func addTestNode(g *akg.CodePropertyGraph, id, file string, line int) {
	name := id
	if i := strings.LastIndex(id, "::"); i >= 0 {
		name = id[i+2:]
	}
	g.Nodes = g.Nodes.Set(id, &link.ResolvedNode{
		ID:       id,
		Kind:     "FUNCTION",
		Name:     name,
		FileSpec: link.LocationMeta{Path: file, LineStart: line},
	})
}

func addTestEdge(g *akg.CodePropertyGraph, src, tgt string) {
	e := link.ResolvedEdge{SourceID: src, TargetID: tgt, Type: link.EdgeCalls}
	g.OutboundEdges = g.OutboundEdges.Set(src, append(g.GetOutboundEdges(src), e))
	g.InboundEdges = g.InboundEdges.Set(tgt, append(g.GetInboundEdges(tgt), e))
}

// buildDiamondGraph returns seed.go::Alpha -> {b.go::Beta, c.go::Gamma} ->
// d.go::Delta, the classic fan-out/fan-in diamond with the seed at the top.
func buildDiamondGraph() *akg.CodePropertyGraph {
	g := akg.NewCodePropertyGraph("diamond")
	addTestNode(g, "seed.go::Alpha", "seed.go", 1)
	addTestNode(g, "b.go::Beta", "b.go", 2)
	addTestNode(g, "c.go::Gamma", "c.go", 3)
	addTestNode(g, "d.go::Delta", "d.go", 4)
	addTestEdge(g, "seed.go::Alpha", "b.go::Beta")
	addTestEdge(g, "seed.go::Alpha", "c.go::Gamma")
	addTestEdge(g, "b.go::Beta", "d.go::Delta")
	addTestEdge(g, "c.go::Gamma", "d.go::Delta")
	return g
}

// buildDiamondGraphShuffled inserts the same diamond nodes/edges in a
// different order; output must still be identical (no insertion-order leak).
func buildDiamondGraphShuffled() *akg.CodePropertyGraph {
	g := akg.NewCodePropertyGraph("diamond")
	addTestNode(g, "d.go::Delta", "d.go", 4)
	addTestNode(g, "c.go::Gamma", "c.go", 3)
	addTestNode(g, "b.go::Beta", "b.go", 2)
	addTestNode(g, "seed.go::Alpha", "seed.go", 1)
	addTestEdge(g, "c.go::Gamma", "d.go::Delta")
	addTestEdge(g, "b.go::Beta", "d.go::Delta")
	addTestEdge(g, "seed.go::Alpha", "c.go::Gamma")
	addTestEdge(g, "seed.go::Alpha", "b.go::Beta")
	return g
}

func sumScores(t *testing.T, ctx RankedContext) float64 {
	t.Helper()
	sum := 0.0
	for _, f := range ctx.Files {
		sum += f.Score
	}
	return sum
}

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 1},
		{"a", 1},
		{"abc", 1},
		{"abcd", 1},
		{"abcdefgh", 2},
		{strings.Repeat("x", 400), 100},
		{strings.Repeat("x", 401), 100},
		{strings.Repeat("x", 404), 101},
	}
	for _, c := range cases {
		if got := EstimateTokens(c.in); got != c.want {
			t.Errorf("EstimateTokens(len=%d) = %d, want %d", len(c.in), got, c.want)
		}
	}
}

func TestSelectContextNilAndEmpty(t *testing.T) {
	got := SelectContext(nil, []string{"a.go::A"}, 100)
	if got.Files == nil {
		t.Error("nil graph: Files must be non-nil")
	}
	if len(got.Files) != 0 || got.TotalTokens != 0 || got.BudgetTokens != 100 {
		t.Errorf("nil graph: got %+v", got)
	}

	empty := akg.NewCodePropertyGraph("empty")
	got = SelectContext(empty, nil, 0)
	if got.Files == nil {
		t.Error("empty graph: Files must be non-nil")
	}
	if len(got.Files) != 0 || got.TotalTokens != 0 {
		t.Errorf("empty graph: got %+v", got)
	}
	if got.BudgetTokens != 1000 {
		t.Errorf("empty graph default budget: got %d, want 1000", got.BudgetTokens)
	}

	// Nodes without file coordinates contribute no files.
	ghost := akg.NewCodePropertyGraph("ghost")
	ghost.Nodes = ghost.Nodes.Set("ghost", &link.ResolvedNode{ID: "ghost", Kind: "FUNCTION", Name: "ghost"})
	got = SelectContext(ghost, []string{"ghost"}, 250)
	if got.Files == nil || len(got.Files) != 0 {
		t.Errorf("file-less nodes: got %+v", got)
	}
	if got.BudgetTokens != 250 {
		t.Errorf("budget passthrough: got %d, want 250", got.BudgetTokens)
	}

	// Non-positive budgets select the 1000-token default.
	for _, b := range []int{0, -5} {
		got = SelectContext(empty, nil, b)
		if got.BudgetTokens != 1000 {
			t.Errorf("budget %d: got BudgetTokens %d, want 1000", b, got.BudgetTokens)
		}
	}
}

func TestSelectContextDeterministic(t *testing.T) {
	seeds := []string{"seed.go::Alpha", "Beta"}
	a := SelectContext(buildDiamondGraph(), seeds, 1000)
	b := SelectContext(buildDiamondGraph(), seeds, 1000)
	if !reflect.DeepEqual(a, b) {
		t.Error("same graph run twice: results differ")
	}
	// Different insertion order must not leak into output either.
	c := SelectContext(buildDiamondGraphShuffled(), seeds, 1000)
	if !reflect.DeepEqual(a, c) {
		t.Errorf("insertion order leaked:\n%+v\nvs\n%+v", a, c)
	}
}

func TestSelectContextBudgetRespected(t *testing.T) {
	seeds := []string{"seed.go::Alpha"}
	for _, budget := range []int{1, 5, 20, 100, 1000} {
		got := SelectContext(buildDiamondGraph(), seeds, budget)
		if got.TotalTokens > budget {
			t.Errorf("budget %d: TotalTokens %d exceeds budget", budget, got.TotalTokens)
		}
		// TotalTokens must equal the recomputed sum of per-file costs.
		recomputed := 0
		for _, f := range got.Files {
			fqns := make([]string, len(f.Symbols))
			for i, s := range f.Symbols {
				fqns[i] = s.FQN
			}
			recomputed += EstimateTokens(f.Path + "\n" + strings.Join(fqns, "\n"))
		}
		if recomputed != got.TotalTokens {
			t.Errorf("budget %d: TotalTokens %d != recomputed %d", budget, got.TotalTokens, recomputed)
		}
	}
	// A tiny budget still returns a well-formed (possibly empty) package.
	tiny := SelectContext(buildDiamondGraph(), seeds, 1)
	if tiny.Files == nil {
		t.Error("tiny budget: Files must be non-nil")
	}
}

func TestSelectContextSeedsBias(t *testing.T) {
	got := SelectContext(buildDiamondGraph(), []string{"seed.go::Alpha"}, 1000)
	if len(got.Files) == 0 {
		t.Fatal("diamond: no files selected")
	}
	if got.Files[0].Path != "seed.go" {
		paths := make([]string, len(got.Files))
		for i, f := range got.Files {
			paths[i] = f.Path
		}
		t.Errorf("seed file should rank first, order: %v", paths)
	}
	// Plain (file-less) seed form must bias identically.
	plain := SelectContext(buildDiamondGraph(), []string{"Alpha"}, 1000)
	if len(plain.Files) == 0 || plain.Files[0].Path != "seed.go" {
		t.Errorf("plain-name seed lost bias: %+v", plain.Files)
	}
}

func TestSelectContextTiebreak(t *testing.T) {
	g := akg.NewCodePropertyGraph("tie")
	addTestNode(g, "b.go::Bee", "b.go", 1)
	addTestNode(g, "a.go::Aye", "a.go", 1)
	got := SelectContext(g, nil, 1000)
	if len(got.Files) != 2 {
		t.Fatalf("tie: got %d files, want 2", len(got.Files))
	}
	if got.Files[0].Score != got.Files[1].Score {
		t.Fatalf("tie: scores differ (%v vs %v), not a tiebreak test",
			got.Files[0].Score, got.Files[1].Score)
	}
	if got.Files[0].Path != "a.go" || got.Files[1].Path != "b.go" {
		t.Errorf("tie: order [%s %s], want [a.go b.go]",
			got.Files[0].Path, got.Files[1].Path)
	}
	again := SelectContext(g, nil, 1000)
	if !reflect.DeepEqual(got, again) {
		t.Error("tie: repeated run differs")
	}
}

func TestSelectContextCycleConverges(t *testing.T) {
	g := akg.NewCodePropertyGraph("cycle")
	addTestNode(g, "a.go::A", "a.go", 1)
	addTestNode(g, "b.go::B", "b.go", 2)
	addTestNode(g, "c.go::C", "c.go", 3)
	addTestEdge(g, "a.go::A", "b.go::B")
	addTestEdge(g, "b.go::B", "c.go::C")
	addTestEdge(g, "c.go::C", "a.go::A")
	got := SelectContext(g, []string{"a.go::A"}, 1000)
	if len(got.Files) != 3 {
		t.Fatalf("cycle: got %d files, want 3", len(got.Files))
	}
	for _, f := range got.Files {
		if math.IsNaN(f.Score) || math.IsInf(f.Score, 0) || f.Score <= 0 {
			t.Errorf("cycle: bad score %v for %s", f.Score, f.Path)
		}
	}
	if sum := sumScores(t, got); math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("cycle: scores sum to %v, want ~1.0", sum)
	}
	again := SelectContext(g, []string{"a.go::A"}, 1000)
	if !reflect.DeepEqual(got, again) {
		t.Error("cycle: repeated run differs")
	}
}

func TestSelectContextSymbolsCappedAndSorted(t *testing.T) {
	g := akg.NewCodePropertyGraph("big")
	for i := 9; i >= 0; i-- {
		addTestNode(g, fmt.Sprintf("big.go::S%02d", i), "big.go", i+1)
	}
	addTestNode(g, "other.go::Z", "other.go", 1)
	got := SelectContext(g, nil, 100000)
	var big *RankedFile
	for i := range got.Files {
		if got.Files[i].Path == "big.go" {
			big = &got.Files[i]
		}
	}
	if big == nil {
		t.Fatal("big.go missing from output")
	}
	if len(big.Symbols) != 8 {
		t.Fatalf("big.go symbols = %d, want cap of 8", len(big.Symbols))
	}
	for i := 1; i < len(big.Symbols); i++ {
		if big.Symbols[i-1].FQN >= big.Symbols[i].FQN {
			t.Errorf("symbols not sorted by FQN: %q >= %q",
				big.Symbols[i-1].FQN, big.Symbols[i].FQN)
		}
	}
	for _, s := range big.Symbols {
		if s.File != "big.go" || s.Line <= 0 {
			t.Errorf("bad symbol ref: %+v", s)
		}
	}
}

// buildBenchGraph creates a synthetic 500-file graph for the benchmark.
func buildBenchGraph() *akg.CodePropertyGraph {
	g := akg.NewCodePropertyGraph("bench")
	const nFiles = 500
	for i := 0; i < nFiles; i++ {
		file := fmt.Sprintf("pkg/f%03d.go", i)
		for k := 0; k < 3; k++ {
			addTestNode(g, fmt.Sprintf("%s::Sym%d", file, k), file, k+1)
		}
	}
	for i := 0; i < nFiles; i++ {
		src := fmt.Sprintf("pkg/f%03d.go::Sym0", i)
		addTestEdge(g, src, fmt.Sprintf("pkg/f%03d.go::Sym0", (i+1)%nFiles))
		addTestEdge(g, src, fmt.Sprintf("pkg/f%03d.go::Sym1", (i+2)%nFiles))
		addTestEdge(g, fmt.Sprintf("pkg/f%03d.go::Sym2", i), fmt.Sprintf("pkg/f%03d.go::Sym0", (i+7)%nFiles))
	}
	return g
}

func BenchmarkSelectContext(b *testing.B) {
	g := buildBenchGraph()
	seeds := []string{"pkg/f000.go::Sym0", "pkg/f250.go::Sym1"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SelectContext(g, seeds, 1000)
	}
}
