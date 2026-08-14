package link

import (
	"fmt"
	"log"
	"sync"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
)

// passDef describes a single linker pass that can be independently enabled/disabled.
type passDef struct {
	name   string
	buffer int
	fn     func(*aggregate.AggregateOutput, *LinkOutput)
}

// Link executes the Interprocedural Linker pass incrementally.
// It transforms Aggregation's topology and global call queue into a Code Property Graph Delta.
// An optional LinkerConfig controls which passes run and at what granularity.
func Link(aggregateOut *aggregate.AggregateOutput, modifiedFiles []string, db GraphDB, config ...LinkerConfig) (*LinkOutput, error) {
	if aggregateOut == nil {
		return NewLinkOutput(""), nil
	}

	// Parse config (use first if provided, otherwise defaults)
	cfg := LinkerConfig{}
	if len(config) > 0 {
		cfg = config[0]
	}
	disabled := make(map[string]bool)
	for _, name := range cfg.DisabledPasses {
		disabled[name] = true
	}

	// W1-15 (§5.4.3/A-13): level-of-detail policy — architecture = structural
	// spine + calls + hierarchy only; standard = + aggregate CFG/DFG summaries;
	// full = per-branch CFG/DFG, heuristics, security.
	applyLevelPolicy(cfg.LevelOfDetail, disabled)

	// 1. Build initial GraphNodes for only the modified files
	cpg := BuildInitialNodes(aggregateOut, modifiedFiles)
	cpg.SetDB(db)
	cpg.Config = cfg
	// Build the ownership index exactly once for the whole run; linkers read
	// it from Config instead of rebuilding it (AUDIT Issue 1.7 / Phase 1C-8).
	cpg.Config.OwnershipMap = aggregate.BuildOwnershipMap(aggregateOut.GlobalDefinitionIndex, aggregateOut.WorkspaceCtx)

	// Dispatch table for all linker passes (in execution order)
	passes := []passDef{
		{name: "type", buffer: 0, fn: LinkTypesAndComposition},
		{name: "member", buffer: 1, fn: LinkMembersAndReturns},
		{name: "interface", buffer: 2, fn: LinkInterfacesAndRealizations},
		{name: "cfg", buffer: 3, fn: LinkIntraProceduralControlFlow},
		{name: "dfg", buffer: 4, fn: LinkDataFlowGraph},
		{name: "callgraph", buffer: 5, fn: LinkCallGraph},
		{name: "concurrency", buffer: 6, fn: LinkConcurrencyAndAsyncControlFlow},
		{name: "filedeps", buffer: 7, fn: LinkFileDependencies},
		{name: "semantics", buffer: 8, fn: LinkEnterpriseSemantics},
		{name: "rpc", buffer: 9, fn: LinkCrossLanguageRPC},
		{name: "constraints", buffer: 10, fn: LinkConstraints},
		{name: "ffi", buffer: 11, fn: LinkFFI},
		{name: "eventsourcing", buffer: 12, fn: LinkEventSourcing},
		{name: "di", buffer: 13, fn: LinkDependencyInjection},
		{name: "escape", buffer: 14, fn: LinkEscapeAnalysis},
		{name: "alias", buffer: 15, fn: LinkAliasAnalysis},
	}

	// Filter to only enabled passes
	var enabled []passDef
	for _, p := range passes {
		if !disabled[p.name] {
			enabled = append(enabled, p)
		}
	}

	// Allocate one buffer per enabled pass.
	// Each buffer starts with an empty private GraphNodes map and shares a
	// read-only reference to the initial nodes (baseNodes). This eliminates
	// N full map copies — memory drops from ~16× base nodes to base + per-pass delta.
	buffers := make([]*LinkOutput, len(enabled))
	for i := range buffers {
		buffers[i] = &LinkOutput{
			CommitHash:    cpg.CommitHash,
			GraphNodes:    make(map[string]*ResolvedNode), // private per-pass
			baseNodes:     cpg.GraphNodes,                 // shared read-only base
			OutboundEdges: make(map[string][]ResolvedEdge),
			InboundEdges:  make(map[string][]ResolvedEdge),
			ModifiedFiles: cpg.ModifiedFiles,
			Config:        cfg,
		}
		buffers[i].SetDB(db)
	}

	// Launch enabled passes concurrently
	var wg sync.WaitGroup
	wg.Add(len(enabled))

	for i, p := range enabled {
		i, p := i, p
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("link: pass %q panicked: %v", p.name, r)
				}
			}()
			p.fn(aggregateOut, buffers[i])
		}()
	}

	wg.Wait()

	// Merge phase: combine nodes and edges from all buffers.
	// Nodes are already in per-pass private maps — merge only new ones
	// into the final CPG (first writer wins, same semantics as before).
	var mergeWg sync.WaitGroup
	mergeWg.Add(3)

	go func() {
		defer mergeWg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("link: node merge panicked: %v", r)
			}
		}()
		for _, buf := range buffers {
			for id, node := range buf.GraphNodes {
				if _, exists := cpg.GraphNodes[id]; !exists {
					cpg.GraphNodes[id] = node
				}
			}
		}
	}()

	go func() {
		defer mergeWg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("link: outbound merge panicked: %v", r)
			}
		}()
		outboundCap := make(map[string]int)
		for _, buf := range buffers {
			for src, edges := range buf.OutboundEdges {
				outboundCap[src] += len(edges)
			}
		}
		for src, cap := range outboundCap {
			if cpg.OutboundEdges[src] == nil {
				cpg.OutboundEdges[src] = make([]ResolvedEdge, 0, cap)
			}
		}
		for _, buf := range buffers {
			for src, edges := range buf.OutboundEdges {
				cpg.OutboundEdges[src] = append(cpg.OutboundEdges[src], edges...)
			}
		}
	}()

	go func() {
		defer mergeWg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("link: inbound merge panicked: %v", r)
			}
		}()
		inboundCap := make(map[string]int)
		for _, buf := range buffers {
			for dst, edges := range buf.InboundEdges {
				inboundCap[dst] += len(edges)
			}
		}
		for dst, cap := range inboundCap {
			if cpg.InboundEdges[dst] == nil {
				cpg.InboundEdges[dst] = make([]ResolvedEdge, 0, cap)
			}
		}
		for _, buf := range buffers {
			for dst, edges := range buf.InboundEdges {
				cpg.InboundEdges[dst] = append(cpg.InboundEdges[dst], edges...)
			}
		}
	}()

	mergeWg.Wait()

	// Step 2: MaxTotalNodes check — warn or abort if the graph exceeds the limit
	if cfg.MaxTotalNodes > 0 && len(cpg.GraphNodes) > cfg.MaxTotalNodes {
		if cfg.AbortOnLimit {
			return nil, fmt.Errorf("CPG exceeded max nodes: %d > %d", len(cpg.GraphNodes), cfg.MaxTotalNodes)
		}
		log.Printf("WARNING: CPG exceeded max nodes: %d > %d. Consider --link-level=architecture", len(cpg.GraphNodes), cfg.MaxTotalNodes)
	}

	// Sequential post-merge passes
	ReasonWholeProgramPrimitives(cpg)
	detectCyclicDependencies(cpg)

	if !disabled["security"] {
		LinkSecurityVulnerabilities(cpg)
	}

	// W1-14 (§5.4.6/A-11): ext: ID mangling self-heal + gm:provenance
	// defaults. Runs after the security pass so every edge (incl. security
	// artifacts) carries evidence (§5.4.7).
	CleanupCPG(aggregateOut, cpg)

	// W1-17 (§5.4.4/A-14): synthetic-node hygiene — FileSpec derivation,
	// gm:synthetic markers, orphan CFG-node removal.
	ApplySyntheticHygiene(cpg)

	// W1-16 (V-05): deterministic ordering of all emitted edge lists.
	SortDeterministic(cpg)

	return cpg, nil
}
