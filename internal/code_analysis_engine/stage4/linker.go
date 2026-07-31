package stage4

import (
	"fmt"
	"log"
	"sync"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// passDef describes a single linker pass that can be independently enabled/disabled.
type passDef struct {
	name   string
	buffer int
	fn     func(*stage3.Stage3Output, *Stage4Output)
}

// Link executes Stage 4: The Interprocedural Linker incrementally.
// It transforms Stage 3's topology and global call queue into a Code Property Graph Delta.
// An optional LinkerConfig controls which passes run and at what granularity.
func Link(stage3Out *stage3.Stage3Output, modifiedFiles []string, db GraphDB, config ...LinkerConfig) (*Stage4Output, error) {
	if stage3Out == nil {
		return NewStage4Output(""), nil
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

	// "architecture" level implicitly disables CFG and DFG
	if cfg.LevelOfDetail == "architecture" {
		disabled["cfg"] = true
		disabled["dfg"] = true
	}

	// 1. Build initial GraphNodes for only the modified files
	cpg := BuildInitialNodes(stage3Out, modifiedFiles)
	cpg.SetDB(db)
	cpg.Config = cfg

	// Dispatch table for all linker passes (in execution order)
	passes := []passDef{
		{name: "type", buffer: 0, fn: LinkTypesAndComposition},
		{name: "interface", buffer: 1, fn: LinkInterfacesAndRealizations},
		{name: "cfg", buffer: 2, fn: LinkIntraProceduralControlFlow},
		{name: "dfg", buffer: 3, fn: LinkDataFlowGraph},
		{name: "callgraph", buffer: 4, fn: LinkCallGraph},
		{name: "concurrency", buffer: 5, fn: LinkConcurrencyAndAsyncControlFlow},
		{name: "filedeps", buffer: 6, fn: LinkFileDependencies},
		{name: "semantics", buffer: 7, fn: LinkEnterpriseSemantics},
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
	buffers := make([]*Stage4Output, len(enabled))
	for i := range buffers {
		buffers[i] = &Stage4Output{
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
					log.Printf("stage4: pass %q panicked: %v", p.name, r)
				}
			}()
			p.fn(stage3Out, buffers[i])
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
		defer func() { if r := recover(); r != nil { log.Printf("stage4: node merge panicked: %v", r) } }()
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
		defer func() { if r := recover(); r != nil { log.Printf("stage4: outbound merge panicked: %v", r) } }()
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
		defer func() { if r := recover(); r != nil { log.Printf("stage4: inbound merge panicked: %v", r) } }()
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

	return cpg, nil
}
