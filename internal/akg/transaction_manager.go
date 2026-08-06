package akg

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	akgerrs "github.com/Syamchand123/GlassMarble/internal/errors"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage1"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// AKGCommitEvent represents a change event broadcasted after an atomic transaction commit.
type AKGCommitEvent struct {
	TxID          uint64    `json:"tx_id"`
	CommitHash    string    `json:"commit_hash"`
	Timestamp     time.Time `json:"timestamp"`
	ModifiedFiles []string  `json:"modified_files"`
	NodeCount     int       `json:"node_count"`
	EdgeCount     int       `json:"edge_count"`
}

// AKGTransactionManager coordinates the 4-Sub-Stage Delta Transaction Lifecycle.
type AKGTransactionManager struct {
	mu          sync.Mutex
	container   *MVCCGraphContainer
	wal         *WriteAheadLog
	storageDir  string
	subscribers []chan AKGCommitEvent
	// MaxTTLBytes is the AKG state-file budget (AUDIT Issue 4 Phase 4A-4).
	// Loading and committing are refused when the TTL would exceed it;
	// 0 means unlimited.
	MaxTTLBytes int64
}

// metadataNodeURI is the ID of the metadata node block written at the top of
// every full TTL serialization. It carries gm:commitHash, gm:schemaVersion,
// and gm:version (the WAL replay bound).
const metadataNodeURI = "http://glassmarble.org/node/metadata"

// NewAKGTransactionManager initializes the Transaction Manager, restores state from disk, and runs WAL recovery.
func NewAKGTransactionManager(storageDir string) (*AKGTransactionManager, error) {
	return NewAKGTransactionManagerWithOptions(storageDir, 0)
}

// NewAKGTransactionManagerWithOptions initializes the Transaction Manager
// with an explicit state-file byte budget (--max-ttl-mb, AUDIT Issue 4
// Phase 4A-4): oversized artifacts are refused on load and on commit.
func NewAKGTransactionManagerWithOptions(storageDir string, maxTTLBytes int64) (*AKGTransactionManager, error) {
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create AKG storage directory: %w", err)
	}

	wal, err := NewWriteAheadLog(filepath.Join(storageDir, "wal"))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize WAL logger: %w", err)
	}

	container := NewMVCCGraphContainer()
	tm := &AKGTransactionManager{
		container:   container,
		wal:         wal,
		storageDir:  storageDir,
		MaxTTLBytes: maxTTLBytes,
	}

	// Acquire startup file lock to protect recovery and loading
	if err := tm.AcquireLock(); err != nil {
		return nil, fmt.Errorf("database lock acquisition failed at startup: %w", err)
	}
	defer tm.ReleaseLock()

	// Restore persistent state from disk. Failures surface loudly: a corrupt
	// or incompatible TTL must never silently produce an empty graph
	// (AUDIT Issue 3 Phase 3B-10 / Issue 5 §5.6).
	if err := tm.loadFromDisk(); err != nil {
		return nil, fmt.Errorf("failed to restore AKG state from disk: %w", err)
	}

	// Replay and recover any committed transactions from the WAL file
	if err := tm.Recover(); err != nil {
		return nil, fmt.Errorf("WAL recovery failed: %w", err)
	}

	return tm, nil
}

// Recover checks the WAL log on disk to replay any committed transactions that weren't fully written to state.
func (tm *AKGTransactionManager) Recover() error {
	activeGraph := tm.container.GetSnapshot()

	// Replay is bounded by maxAppliedTx (the TTL metadata gm:version):
	// transactions already captured in the TTL are skipped instead of being
	// replayed from scratch on every startup (AUDIT Issue 3 Phase 3B-7).
	maxAppliedTx := activeGraph.Version

	// Streaming single-pass recovery (AUDIT Issue 4 Phase 4B-5): entries are
	// read in append order — which is transaction order — and a committed
	// delta is applied the moment its COMMITTED marker is seen. Memory is
	// bounded by the in-flight transaction's payload instead of the whole
	// log. A STARTED entry whose marker never arrived (crash mid-transaction)
	// or an ABORTED entry is simply dropped.
	pending := make(map[uint64]*WALEntry)
	replayed := false

	err := tm.wal.ForEachEntry(func(entry *WALEntry) error {
		switch entry.Status {
		case WALStatusStarted:
			pending[entry.TxID] = entry
		case WALStatusAborted:
			delete(pending, entry.TxID)
		case WALStatusCommitted:
			started, ok := pending[entry.TxID]
			delete(pending, entry.TxID)
			if !ok || started.Payload == nil || started.TxID <= maxAppliedTx {
				return nil
			}
			shadow := activeGraph.Clone()
			shadow.CommitHash = started.CommitHash
			shadow.Version = started.TxID

			if _, err := tm.applyDeltaToShadow(shadow, started.Payload, started.ModifiedFiles); err != nil {
				return fmt.Errorf("WAL recovery failed to apply transaction %d: %w", started.TxID, err)
			}

			tm.container.PromoteShadowSnapshot(shadow)
			activeGraph = shadow
			maxAppliedTx = started.TxID
			replayed = true
		}
		return nil
	})
	if err != nil {
		return err
	}

	if replayed {
		if err := tm.saveToDisk(activeGraph, nil, nil, 0); err != nil {
			return fmt.Errorf("WAL recovery failed to persist replayed state: %w", err)
		}
	}

	// After a successful recovery the TTL captures all committed state; the
	// WAL can be truncated so it stays bounded (AUDIT Issue 4 Phase 4B-8).
	if err := tm.wal.Truncate(); err != nil {
		return fmt.Errorf("WAL truncation after recovery failed: %w", err)
	}

	return nil
}

// GetActiveSnapshot returns a thread-safe read-only pointer to the active graph.
func (tm *AKGTransactionManager) GetActiveSnapshot() *CodePropertyGraph {
	if tm == nil || tm.container == nil {
		return nil
	}
	return tm.container.GetSnapshot()
}

// Subscribe returns a channel that receives events whenever an atomic transaction commits.
func (tm *AKGTransactionManager) Subscribe() chan AKGCommitEvent {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	ch := make(chan AKGCommitEvent, 10)
	tm.subscribers = append(tm.subscribers, ch)
	return ch
}

// GetActiveGraph returns a thread-safe read-only snapshot of the active AKG database.
func (tm *AKGTransactionManager) GetActiveGraph() *CodePropertyGraph {
	return tm.container.GetSnapshot()
}

// ReplaceGraph atomically swaps the active graph with the supplied graph and
// persists it as a full TTL rewrite (used by `gmb import`). The graph is
// validated before promotion: dangling edges are rejected so the persisted
// state never carries references to missing nodes. The WAL is truncated
// afterwards because the imported graph is a full snapshot, not a delta.
func (tm *AKGTransactionManager) ReplaceGraph(graph *CodePropertyGraph) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if graph == nil {
		return fmt.Errorf("cannot replace with nil graph")
	}

	// Reject dangling edges up front: the post-write verification would reject
	// the TTL anyway, but failing early with a clearer message beats a
	// serialization/rename round-trip (AUDIT Issue 5 Phase 5A-1).
	dangling := 0
	graph.OutboundEdges.Iterate(func(srcID string, edges []stage4.ResolvedEdge) {
		for _, edge := range edges {
			if _, ok := graph.Nodes.Get(srcID); !ok {
				dangling++
			}
			if _, ok := graph.Nodes.Get(edge.TargetID); !ok {
				dangling++
			}
		}
	})
	if dangling > 0 {
		return fmt.Errorf("import rejected: %d edge(s) reference nodes missing from the graph (dangling); fix the export/import document first", dangling)
	}

	// Rebuild non-serialized indexes the way loadFromDisk does.
	rebuildIndexes(graph)

	if err := tm.saveToDisk(graph, nil, nil, 0); err != nil {
		return fmt.Errorf("failed to persist imported graph: %w", err)
	}

	tm.container.PromoteShadowSnapshot(graph)

	// The WAL holds only pre-import delta transactions; they were captured by
	// the full rewrite so truncating keeps the log bounded (AUDIT Issue 4
	// Phase 4B-8).
	if err := tm.wal.Truncate(); err != nil {
		return fmt.Errorf("failed to truncate WAL after import: %w", err)
	}
	return nil
}

// rebuildIndexes reconstructs LineIndex (the only in-memory index that the
// TTL round-trip does not serialize) and re-verifies the graph for status
// reporting. Shared by import and loadFromDisk.
func rebuildIndexes(graph *CodePropertyGraph) {
	if graph.LineIndex == nil {
		graph.LineIndex = NewCowMap[string, []*stage4.ResolvedNode]()
	}
	graph.Nodes.Iterate(func(_ string, node *stage4.ResolvedNode) {
		normPath := normalizePath(node.FileSpec.Path)
		if normPath != "" {
			lineNodes, _ := graph.LineIndex.Get(normPath)
			newLineNodes := make([]*stage4.ResolvedNode, len(lineNodes)+1)
			copy(newLineNodes, lineNodes)
			newLineNodes[len(newLineNodes)-1] = node
			graph.LineIndex = graph.LineIndex.Set(normPath, newLineNodes)
		}
	})
	for _, normPath := range graph.LineIndex.Keys() {
		lineNodes, _ := graph.LineIndex.Get(normPath)
		sort.Slice(lineNodes, func(i, j int) bool {
			return lineNodes[i].FileSpec.LineStart < lineNodes[j].FileSpec.LineStart
		})
		graph.LineIndex = graph.LineIndex.Set(normPath, lineNodes)
	}

	graph.Verified = true
	graph.VerificationMsg = ""
	graph.OutboundEdges.Iterate(func(sourceID string, edges []stage4.ResolvedEdge) {
		for _, edge := range edges {
			if _, ok := graph.Nodes.Get(sourceID); !ok {
				graph.Verified = false
				graph.VerificationMsg = "dangling edge source in persisted graph"
				return
			}
			if _, ok := graph.Nodes.Get(edge.TargetID); !ok {
				graph.Verified = false
				graph.VerificationMsg = "dangling edge target in persisted graph"
				return
			}
		}
	})
}

// ExecuteDeltaTransaction executes the complete 4-Sub-Stage Delta Transaction Lifecycle on the AKG.
func (tm *AKGTransactionManager) ExecuteDeltaTransaction(payload *stage4.Stage4Output, modifiedFiles []string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if payload == nil {
		return nil
	}

	// Acquire the cross-process file lock for the ENTIRE transaction, including
	// the disk write: a second process must never observe or overwrite a
	// partially-written TTL (AUDIT Issue 3 §3.3 / Issue 4 Phase 4C-10).
	if err := tm.AcquireLock(); err != nil {
		return err
	}
	defer tm.ReleaseLock()

	// ------------------------------------------------------------------------
	// SUB-STAGE A: TRANSACTION LOGGING & ISOLATION
	// ------------------------------------------------------------------------
	// Step A.2: Allocate MVCC Shadow Snapshot for write isolation
	shadow, txID := tm.container.AllocateShadowSnapshot()
	shadow.CommitHash = payload.CommitHash

	// Step A.1: Write-Ahead Log (WAL Write) to disk before mutating in-memory shadow
	walEntry := &WALEntry{
		TxID:          txID,
		CommitHash:    payload.CommitHash,
		Timestamp:     time.Now().UTC(),
		ModifiedFiles: modifiedFiles,
		Payload:       payload,
		Status:        WALStatusStarted,
	}
	if err := tm.wal.AppendEntry(walEntry); err != nil {
		return fmt.Errorf("WAL logging failed (Sub-Stage A.1): %w", err)
	}

	// Apply delta to shadow snapshot (Sub-Stage B, C, D)
	// Capture the pre-transaction size BEFORE the sweep so delete-only deltas
	// can still take the incremental append path (AUDIT Issue 3 Phase 3B-6).
	preTxSize := tm.container.GetSnapshot().Nodes.Len()
	deletedNodeIDs, err := tm.applyDeltaToShadow(shadow, payload, modifiedFiles)
	if err != nil {
		return err
	}

	// ------------------------------------------------------------------------
	// SUB-STAGE D.3: Atomic Commit & Persistence
	// ------------------------------------------------------------------------
	if err := tm.wal.MarkCommitted(txID); err != nil {
		return fmt.Errorf("WAL commit marker failed: %w", err)
	}

	// Promote shadow snapshot to active graph snapshot
	tm.container.PromoteShadowSnapshot(shadow)

	// Persist synchronously (single writer, lock held) with tmp+fsync+rename
	// semantics: the previous good file survives any failure, and errors reach
	// the caller instead of being swallowed (AUDIT Issue 3 Phase 3B-4/3B-10).
	if err := tm.saveToDisk(shadow, payload, deletedNodeIDs, preTxSize); err != nil {
		// Roll the WAL back: the staged TTL never replaced the previous good
		// file, so the rejected transaction must not replay on the next
		// startup — recovery would hit the same verification failure and
		// block every subsequent run (zero-dangling guard, Issue 5 Phase 5A-1).
		if truncErr := tm.wal.Truncate(); truncErr != nil {
			return fmt.Errorf("failed to persist AKG state: %w (additionally failed to roll back WAL: %v)", err, truncErr)
		}
		return fmt.Errorf("failed to persist AKG state: %w", err)
	}

	// The TTL now captures every transaction up to txID; replaying the WAL
	// would be redundant. Truncate keeps .glassmarble bounded.
	if err := tm.wal.Truncate(); err != nil {
		return fmt.Errorf("failed to truncate WAL after commit: %w", err)
	}

	// Broadcast commit event to visual layout subscribers
	totalEdges := 0
	shadow.OutboundEdges.Iterate(func(_ string, edges []stage4.ResolvedEdge) {
		totalEdges += len(edges)
	})

	event := AKGCommitEvent{
		TxID:          txID,
		CommitHash:    payload.CommitHash,
		Timestamp:     time.Now().UTC(),
		ModifiedFiles: modifiedFiles,
		NodeCount:     shadow.Nodes.Len(),
		EdgeCount:     totalEdges,
	}

	for _, ch := range tm.subscribers {
		select {
		case ch <- event:
		default:
		}
	}

	return nil
}

func (tm *AKGTransactionManager) applyDeltaToShadow(shadow *CodePropertyGraph, payload *stage4.Stage4Output, modifiedFiles []string) (map[string]bool, error) {
	// If modifiedFiles is empty, infer from payload
	if len(modifiedFiles) == 0 {
		fileMap := make(map[string]bool)
		for _, node := range payload.GraphNodes {
			if node.FileSpec.Path != "" {
				fileMap[node.FileSpec.Path] = true
			}
		}
		for path := range fileMap {
			modifiedFiles = append(modifiedFiles, path)
		}
	}

	// ------------------------------------------------------------------------
	// SUB-STAGE B: TRANSACTIONAL INVALIDATION (THE SWEEP PHASE)
	// ------------------------------------------------------------------------
	// Step B.1: Node Context Sweep. Every node of every modified file is
	// invalidated (removed from the graph and its indexes); the graft phase
	// below re-adds the nodes that still exist. oldNodeIDs keeps track of the
	// full pre-transaction set so tombstones can be restricted to nodes that
	// are GENUINELY gone (deleted files / removed symbols) — blanket
	// tombstones for still-existing symbols would also erase cross-file edges
	// pointing at them (AUDIT Issue 3 Phase 3B-9).
	oldNodeIDs := make(map[string]bool)
	deletedNodeIDs := make(map[string]bool)
	for _, filePath := range modifiedFiles {
		normPath := normalizePath(filePath)
		if nodeSet, exists := shadow.FileNodeIndex.Get(normPath); exists {
			for nodeID := range nodeSet {
				oldNodeIDs[nodeID] = true
				if node, ok := shadow.Nodes.Get(nodeID); ok {
					if kindSet, kindExists := shadow.KindIndex.Get(node.Kind); kindExists {
						newKindSet := make(map[string]bool, len(kindSet))
						for k, v := range kindSet {
							newKindSet[k] = v
						}
						delete(newKindSet, nodeID)
						shadow.KindIndex = shadow.KindIndex.Set(node.Kind, newKindSet)
					}
					if node.Properties != nil {
						if h, ok := node.Properties["hash"]; ok && h != "" {
							if hashList, hashExists := shadow.HashIndex.Get(h); hashExists {
								for i, v := range hashList {
									if v == nodeID {
										newHashList := make([]string, len(hashList)-1)
										copy(newHashList, hashList[:i])
										copy(newHashList[i:], hashList[i+1:])
										shadow.HashIndex = shadow.HashIndex.Set(h, newHashList)
										break
									}
								}
							}
						}
					}
				}
				shadow.Nodes = shadow.Nodes.Delete(nodeID)
				shadow.OutboundEdges = shadow.OutboundEdges.Delete(nodeID)
				shadow.MacroRules = shadow.MacroRules.Delete(nodeID)
				shadow.FolderZones = shadow.FolderZones.Delete(nodeID)
			}
			shadow.FileNodeIndex = shadow.FileNodeIndex.Delete(normPath)
			shadow.LineIndex = shadow.LineIndex.Delete(normPath)
		}
	}

	// Clean up Entrypoints associated with deleted nodes
	var validEntrypoints []string
	for _, ep := range shadow.Entrypoints {
		if !oldNodeIDs[ep] {
			validEntrypoints = append(validEntrypoints, ep)
		}
	}
	shadow.Entrypoints = validEntrypoints

	shadow.macroCache = NewCowMap[string, []string]()

	// ------------------------------------------------------------------------
	// SUB-STAGE C: NODE AND EDGE HYDRATION (THE GRAFT PHASE)
	// ------------------------------------------------------------------------
	// Step C.1: Vertex Grafting
	updatedPaths := make(map[string]bool)
	for nodeID, node := range payload.GraphNodes {
		if node == nil {
			continue
		}
		shadow.Nodes = shadow.Nodes.Set(nodeID, node)

		existingSet, _ := shadow.KindIndex.Get(node.Kind)
		newKindSet := make(map[string]bool)
		for k, v := range existingSet {
			newKindSet[k] = v
		}
		newKindSet[nodeID] = true
		shadow.KindIndex = shadow.KindIndex.Set(node.Kind, newKindSet)

		if node.Properties != nil {
			if h, ok := node.Properties["hash"]; ok && h != "" {
				hashList, _ := shadow.HashIndex.Get(h)
				newHashList := make([]string, len(hashList)+1)
				copy(newHashList, hashList)
				newHashList[len(newHashList)-1] = nodeID
				shadow.HashIndex = shadow.HashIndex.Set(h, newHashList)
			}
		}

		normPath := normalizePath(node.FileSpec.Path)
		if normPath != "" {
			fileSet, _ := shadow.FileNodeIndex.Get(normPath)
			newFileSet := make(map[string]bool, len(fileSet))
			for k, v := range fileSet {
				newFileSet[k] = v
			}
			newFileSet[nodeID] = true
			shadow.FileNodeIndex = shadow.FileNodeIndex.Set(normPath, newFileSet)

			lineNodes, _ := shadow.LineIndex.Get(normPath)
			newLineNodes := make([]*stage4.ResolvedNode, len(lineNodes)+1)
			copy(newLineNodes, lineNodes)
			newLineNodes[len(newLineNodes)-1] = node
			shadow.LineIndex = shadow.LineIndex.Set(normPath, newLineNodes)
			updatedPaths[normPath] = true
		}
	}

	for normPath := range updatedPaths {
		lineNodes, _ := shadow.LineIndex.Get(normPath)
		sort.Slice(lineNodes, func(i, j int) bool {
			return lineNodes[i].FileSpec.LineStart < lineNodes[j].FileSpec.LineStart
		})
		shadow.LineIndex = shadow.LineIndex.Set(normPath, lineNodes)
	}

	// Step C.2: Vector Binding
	for sourceID, edges := range payload.OutboundEdges {
		newSlice := make([]stage4.ResolvedEdge, len(edges))
		copy(newSlice, edges)
		shadow.OutboundEdges = shadow.OutboundEdges.Set(sourceID, newSlice)
	}

	// Step C.2.b: Stage 3.8 and 3.9 Metadata Binding.
	// Entrypoints are deduplicated: repeated analyses must not accumulate
	// duplicate entries for the same node (AUDIT Issue 3 §3.3).
	existingEP := make(map[string]bool, len(shadow.Entrypoints))
	for _, ep := range shadow.Entrypoints {
		existingEP[ep] = true
	}
	for _, ep := range payload.EntrypointRegistry {
		if !existingEP[ep] {
			shadow.Entrypoints = append(shadow.Entrypoints, ep)
			existingEP[ep] = true
		}
	}
	for k, v := range payload.FolderZones {
		shadow.FolderZones = shadow.FolderZones.Set(k, v)
	}

	// Step C.3: Inbound Back-Linking
	for sourceID, edges := range payload.OutboundEdges {
		for _, edge := range edges {
			targetID := edge.TargetID
			inboundEdge := stage4.ResolvedEdge{
				SourceID:   sourceID,
				TargetID:   targetID,
				Type:       edge.Type,
				LineNumber: edge.LineNumber,
			}

			existingEdges, _ := shadow.InboundEdges.Get(targetID)
			exists := false
			for _, existing := range existingEdges {
				if existing.SourceID == sourceID && existing.Type == edge.Type && existing.LineNumber == edge.LineNumber {
					exists = true
					break
				}
			}
			if !exists {
				newEdges := make([]stage4.ResolvedEdge, len(existingEdges)+1)
				copy(newEdges, existingEdges)
				newEdges[len(newEdges)-1] = inboundEdge
				shadow.InboundEdges = shadow.InboundEdges.Set(targetID, newEdges)
			}
		}
	}

	// Step C.4: Post-graft invariant sweep. With the graft complete, keep
	// every edge whose endpoints exist and drop only those referencing nodes
	// that are truly gone (deleted files, symbols removed by an edit). The
	// previous pre-graft sweep removed ALL edges touching modified files —
	// including cross-file edges from UNCHANGED files to surviving symbols —
	// and the delta cannot regenerate them, so every incremental commit
	// permanently lost them (AUDIT Issue 3 Phase 3B-9 / Issue 5 Phase 5A-1).
	// Dropped edges are recorded in shadow.Errors so the loss stays visible
	// to `gmb status` / `gmb doctor` instead of disappearing silently.
	shadow.Errors = nil
	keptOutbound := NewCowMap[string, []stage4.ResolvedEdge]()
	shadow.OutboundEdges.Iterate(func(sourceID string, edges []stage4.ResolvedEdge) {
		if _, srcOK := shadow.Nodes.Get(sourceID); !srcOK {
			return
		}
		var kept []stage4.ResolvedEdge
		for _, edge := range edges {
			if _, tgtOK := shadow.Nodes.Get(edge.TargetID); tgtOK {
				kept = append(kept, edge)
				continue
			}
			shadow.Errors = append(shadow.Errors, DanglingReferenceError{
				SourceID:   sourceID,
				TargetID:   edge.TargetID,
				EdgeType:   string(edge.Type),
				LineNumber: edge.LineNumber,
				Message:    fmt.Sprintf("Dangling edge from %s to missing node %s dropped by merge sweep", sourceID, edge.TargetID),
			})
		}
		if len(kept) > 0 {
			keptOutbound = keptOutbound.Set(sourceID, kept)
		}
	})
	shadow.OutboundEdges = keptOutbound

	// Inbound edges are derived from outbound edges; rebuild them so the two
	// views stay mirror-consistent after the sweep (copy-on-write: never
	// append to a CowMap slice in place).
	newInbound := NewCowMap[string, []stage4.ResolvedEdge]()
	shadow.OutboundEdges.Iterate(func(sourceID string, edges []stage4.ResolvedEdge) {
		for _, edge := range edges {
			existing, _ := newInbound.Get(edge.TargetID)
			newEdges := make([]stage4.ResolvedEdge, len(existing)+1)
			copy(newEdges, existing)
			newEdges[len(newEdges)-1] = edge
			newInbound = newInbound.Set(edge.TargetID, newEdges)
		}
	})
	shadow.InboundEdges = newInbound

	// Tombstones are written only for nodes that are genuinely gone: nodes
	// that existed before this transaction and were NOT re-grafted by the
	// payload. Blanket-tombstoning still-existing symbols would delete their
	// incident edges from the appended file on restore (Issue 3 Phase 3B-6).
	for id := range oldNodeIDs {
		if n, ok := payload.GraphNodes[id]; !ok || n == nil {
			deletedNodeIDs[id] = true
		}
	}

	// ------------------------------------------------------------------------
	// SUB-STAGE D: GRAPH INVARIANT VERIFICATION & REASONING
	// ------------------------------------------------------------------------
	// Step D.1: Dangling Reference Audit. The post-graft sweep (Step C.4)
	// already guarantees every surviving edge has both endpoints; this pass
	// is the invariant check that catches any future regression before the
	// zero-dangling guard at write time.
	shadow.OutboundEdges.Iterate(func(sourceID string, edges []stage4.ResolvedEdge) {
		for _, edge := range edges {
			if _, targetExists := shadow.Nodes.Get(edge.TargetID); !targetExists {
				errRec := DanglingReferenceError{
					SourceID:   sourceID,
					TargetID:   edge.TargetID,
					EdgeType:   string(edge.Type),
					LineNumber: edge.LineNumber,
					Message:    fmt.Sprintf("Dangling edge from %s to missing node %s", sourceID, edge.TargetID),
				}
				shadow.Errors = append(shadow.Errors, errRec)
			}
		}
	})

	// Step D.2: Topological Macro-Inference Parsing. Delta transactions
	// re-infer ONLY the changed subgraph (modified files + their inbound
	// dependents) instead of re-walking the entire graph — the incremental
	// reasoner companion to the delta linker (AUDIT Issue 1 Phase 1C-9).
	var changedNodeIDs []string
	for id := range payload.GraphNodes {
		changedNodeIDs = append(changedNodeIDs, id)
	}
	RunIncrementalMacroInference(shadow, modifiedFiles, changedNodeIDs, payload.Config)

	return deletedNodeIDs, nil
}

// saveToDisk persists the graph to the TTL file atomically:
// tmp-file -> fsync -> post-write verification -> atomic rename.
// The previous good file is preserved on any failure (AUDIT Issue 3
// Phase 3B-4, Issue 5 Phase 5A-1).
func (tm *AKGTransactionManager) saveToDisk(graph *CodePropertyGraph, payload *stage4.Stage4Output, deletedNodeIDs map[string]bool, baseSize int) error {
	if graph == nil {
		return fmt.Errorf("cannot persist nil graph")
	}

	ttlPath := filepath.Join(tm.storageDir, "akg_state.ttl")

	tmp, err := os.CreateTemp(tm.storageDir, "akg_state.ttl.tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp TTL file: %w", err)
	}
	defer os.Remove(tmp.Name())

	// Incremental append path: stage the existing TTL and append the delta
	// (tombstones + new nodes/edges) instead of rewriting the whole graph.
	// baseSize is the PRE-transaction active graph size: measuring the
	// post-sweep shadow would see 0 nodes after a delete delta and force a
	// full rewrite, losing the tombstones entirely.
	incremental := false
	if payload != nil && len(deletedNodeIDs) != 0 {
		deltaSize := len(payload.GraphNodes) + len(deletedNodeIDs)
		if _, statErr := os.Stat(ttlPath); statErr == nil && baseSize > 0 && float64(deltaSize) <= float64(baseSize)*0.20 {
			src, openErr := os.Open(ttlPath)
			if openErr != nil {
				return fmt.Errorf("failed to open existing TTL for incremental append: %w", openErr)
			}
			if _, copyErr := io.Copy(tmp, src); copyErr != nil {
				src.Close()
				return fmt.Errorf("failed to stage existing TTL for incremental append: %w", copyErr)
			}
			src.Close()

			bw := bufio.NewWriter(tmp)
			if err := SerializeDeltaToTurtle(payload, deletedNodeIDs, graph.Version, bw); err != nil {
				return fmt.Errorf("delta serialization failed: %w", err)
			}
			if err := bw.Flush(); err != nil {
				return fmt.Errorf("failed to flush buffer: %w", err)
			}
			incremental = true
		}
	}

	// Full rewrite (compaction) path
	if !incremental {
		bw := bufio.NewWriter(tmp)
		if err := SerializeToTurtle(graph, bw); err != nil {
			return fmt.Errorf("TTL serialization failed: %w", err)
		}
		if err := bw.Flush(); err != nil {
			return fmt.Errorf("failed to flush buffer: %w", err)
		}
	}

	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("failed to fsync temp TTL file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp TTL file: %w", err)
	}

	// State-file budget guard: an oversized staged TTL is rejected BEFORE
	// verification and the atomic rename, so the previous good file stays in
	// place and the WAL can be rolled back (AUDIT Issue 4 Phase 4A-4).
	if err := tm.enforceTTLBudget(tmp.Name()); err != nil {
		return err
	}

	// Post-write verification BEFORE the atomic rename: if the staged file is
	// corrupt or lossy, the previous good file is still in place (AUDIT
	// Issue 5 Phase 5A-1).
	if err := tm.verifyTTLFile(tmp.Name(), graph); err != nil {
		return err
	}

	if err := os.Rename(tmp.Name(), ttlPath); err != nil {
		return fmt.Errorf("failed to atomically rename TTL into place: %w", err)
	}
	return nil
}

// verifyTTLFile re-reads a staged TTL with the canonical parser and asserts
// node/edge parity with the in-memory graph. Any deviation — unparseable
// file, node/edge lossiness, or a single dangling edge (source or target
// missing from the node set) — fails the write so the previous good file
// stays in place (AUDIT Issue 5 Phase 5A-1: zero dangling edges at write
// time; the engine-side producers were fixed in the Issue 1 batches).
func (tm *AKGTransactionManager) verifyTTLFile(ttlPath string, graph *CodePropertyGraph) error {
	restored, err := reconstructFromTTLFileEx(ttlPath, false)
	if err != nil {
		return fmt.Errorf("post-write verification failed: file did not parse back cleanly: %w", err)
	}

	if restored.Nodes.Len() != graph.Nodes.Len() {
		return fmt.Errorf("post-write verification failed: node count mismatch (file=%d graph=%d)", restored.Nodes.Len(), graph.Nodes.Len())
	}

	restoredEdges := 0
	restored.OutboundEdges.Iterate(func(_ string, edges []stage4.ResolvedEdge) {
		restoredEdges += len(edges)
	})
	// The TTL is triple-oriented: parallel edges sharing (source, predicate,
	// target) collapse to one canonical triple on persist (the serializer
	// dedups, keeping the max line number). Verification must therefore
	// compare against the deduplicated edge count, not the raw total.
	type keyT struct{ s, p, t string }
	seen := make(map[keyT]int)
	graph.OutboundEdges.Iterate(func(sourceID string, edges []stage4.ResolvedEdge) {
		for _, edge := range edges {
			pred := mapEdgeTypeToPredicate(edge.Type)
			if pred == "" {
				continue
			}
			seen[keyT{sourceID, pred, edge.TargetID}]++
		}
	})
	expectedEdges := len(seen)
	if restoredEdges != expectedEdges {
		return fmt.Errorf("post-write verification failed: edge count mismatch (file=%d graph=%d)", restoredEdges, expectedEdges)
	}

	// Zero-dangling guard: every persisted edge must reference real nodes.
	// The merged graph is verified as a whole (base + delta), so a dangling
	// edge is a hard integrity failure, never a tolerated warning.
	dangling := 0
	restored.OutboundEdges.Iterate(func(sourceID string, edges []stage4.ResolvedEdge) {
		for _, edge := range edges {
			if _, ok := restored.Nodes.Get(sourceID); !ok {
				dangling++
			}
			if _, ok := restored.Nodes.Get(edge.TargetID); !ok {
				dangling++
			}
		}
	})
	if dangling > 0 {
		return fmt.Errorf("post-write verification failed: %d dangling edge(s) (edges referencing missing nodes); the write was rejected and the previous good TTL was kept. Remove .glassmarble/akg_state.ttl and .glassmarble/wal/akg_transactions.wal, then re-run `gmb analyze --full` to rebuild from scratch", dangling)
	}

	graph.Verified = true
	graph.VerificationMsg = ""
	return nil
}

// enforceTTLBudget refuses to proceed when the state file at path exceeds
// the configured --max-ttl-mb budget. It is applied on load (oversized
// artifacts must not be pulled into RAM) and on save (an oversized commit is
// rejected before the atomic rename, leaving the previous good file in place).
func (tm *AKGTransactionManager) enforceTTLBudget(path string) error {
	if tm.MaxTTLBytes <= 0 {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if st.Size() > tm.MaxTTLBytes {
		return fmt.Errorf("AKG state file is %.1f MB, exceeding the --max-ttl-mb budget of %.1f MB; refused. Lower the analysis scope (--link-level=architecture), raise the budget, or rebuild with `gmb analyze --full`",
			float64(st.Size())/(1<<20), float64(tm.MaxTTLBytes)/(1<<20))
	}
	return nil
}

// loadFromDisk restores the active graph from the primary Turtle state
// database (akg_state.ttl). It automatically handles schema migration (v1/v2 -> v3) with a .bak backup.
func (tm *AKGTransactionManager) loadFromDisk() error {
	ttlPath := filepath.Join(tm.storageDir, "akg_state.ttl")
	if err := tm.enforceTTLBudget(ttlPath); err != nil {
		return err
	}
	graph, err := tm.reconstructFromTurtle()
	if err != nil {
		if os.IsNotExist(err) {
			// Fresh database: start empty. WAL replay still applies any
			// committed-but-unpersisted transactions from a previous run.
			graph = NewCodePropertyGraph("initial")
			graph.Version = 0
			graph.SchemaVersion = CurrentSchemaVersion
			tm.container.ActiveGraph = graph
			return nil
		}
		return err
	}

	// Schema v3 migration handling (K-07 / W2-08)
	if graph.SchemaVersion < CurrentSchemaVersion {
		oldVer := graph.SchemaVersion
		bakPath, _ := CreateSchemaBackup(tm.storageDir, oldVer)
		if err := MigrateToSchemaV3(graph); err != nil {
			return fmt.Errorf("schema migration failed: %w", err)
		}
		if err := tm.saveToDisk(graph, nil, nil, 0); err != nil {
			return fmt.Errorf("persisting schema migration failed: %w", err)
		}
		graph.VerificationMsg = fmt.Sprintf("Migrated AKG schema from v%d to v%d (backup created at %s)", oldVer, CurrentSchemaVersion, bakPath)
	}

	// Rebuild LineIndex since it is not serialized
	rebuildIndexes(graph)

	tm.container.ActiveGraph = graph
	return nil
}

func (tm *AKGTransactionManager) reconstructFromTurtle() (*CodePropertyGraph, error) {
	return reconstructFromTTLFileEx(filepath.Join(tm.storageDir, "akg_state.ttl"), true)
}

func reconstructFromTTLFile(ttlPath string) (*CodePropertyGraph, error) {
	return reconstructFromTTLFileEx(ttlPath, true)
}

// reconstructFromTTLFileEx rebuilds a CodePropertyGraph from a TTL file.
// If runMacros is false, topological macro inference is skipped (used by verifyTTLFile, K-03 / W2-04).
func reconstructFromTTLFileEx(ttlPath string, runMacros bool) (*CodePropertyGraph, error) {
	if _, err := os.Stat(ttlPath); os.IsNotExist(err) {
		return nil, err
	}

	// Tombstones first
	deletedIDs, err := scanDeletedNodeIDs(ttlPath)
	if err != nil {
		return nil, err
	}

	nodes, edges, err := stage1.ParseTTLFile(ttlPath)
	if err != nil {
		return nil, fmt.Errorf("TTL parse failed: %w", err)
	}

	graph := NewCodePropertyGraph("restored_from_ttl")

	mutNodes := make(map[string]*stage4.ResolvedNode, len(nodes))
	mutEntrypoints := make([]string, 0)
	mutFolderZones := make(map[string]string)
	mutKindIndex := make(map[string]map[string]bool)
	mutHashIndex := make(map[string][]string)
	mutFileNodeIndex := make(map[string]map[string]bool)

	// Convert TTLNode to stage4.ResolvedNode
	for id, tNode := range nodes {
		if id == metadataNodeURI || deletedIDs[id] {
			continue
		}
		kind := mapClassToKind(tNode.Kind)
		prim := strings.TrimPrefix(tNode.PrimitiveType, ont.PrefixGM)

		resNode := &stage4.ResolvedNode{
			ID:        id,
			Kind:      kind,
			Name:      tNode.Name,
			Primitive: prim,
			FileSpec: stage4.LocationMeta{
				Path:      strings.TrimPrefix(tNode.FileURI, "file:"),
				LineStart: tNode.LineStart,
				LineEnd:   tNode.LineEnd,
			},
			Properties: make(map[string]string),
		}
		for k, v := range tNode.Properties {
			resNode.Properties[k] = v
		}
		// K-08 write/read key symmetry: store in "content" property key only
		if tNode.Code != "" {
			if _, hasContent := resNode.Properties["content"]; !hasContent {
				resNode.Properties["content"] = tNode.Code
			}
		}
		mutNodes[id] = resNode

		// Restore Entrypoints / FolderZones
		if tNode.IsEntrypoint {
			mutEntrypoints = append(mutEntrypoints, id)
		}
		if kind == "MODULE" && tNode.PrimitiveZone != "" {
			mutFolderZones[id] = tNode.PrimitiveZone
		}

		// Rebuild KindIndex
		if mutKindIndex[kind] == nil {
			mutKindIndex[kind] = make(map[string]bool)
		}
		mutKindIndex[kind][id] = true

		// Rebuild HashIndex
		if h, ok := resNode.Properties["hash"]; ok && h != "" {
			mutHashIndex[h] = append(mutHashIndex[h], id)
		}

		// Rebuild FileNodeIndex
		normPath := normalizePath(resNode.FileSpec.Path)
		if normPath != "" {
			if mutFileNodeIndex[normPath] == nil {
				mutFileNodeIndex[normPath] = make(map[string]bool)
			}
			mutFileNodeIndex[normPath][id] = true
		}
	}

	mutOutboundEdges := make(map[string][]stage4.ResolvedEdge)
	mutInboundEdges := make(map[string][]stage4.ResolvedEdge)

	// Rebuild Outbound and Inbound Edges with canonical edge types
	for _, tEdge := range edges {
		if deletedIDs[tEdge.SourceID] || deletedIDs[tEdge.TargetID] {
			continue
		}
		if strings.HasPrefix(tEdge.Predicate, ont.PredStatus) {
			continue
		}

		resolvedEdge := stage4.ResolvedEdge{
			SourceID:   tEdge.SourceID,
			TargetID:   tEdge.TargetID,
			Type:       mapPredicateToEdgeType(tEdge.Predicate),
			LineNumber: tEdge.LineNumber,
		}

		mutOutboundEdges[tEdge.SourceID] = append(mutOutboundEdges[tEdge.SourceID], resolvedEdge)
		mutInboundEdges[tEdge.TargetID] = append(mutInboundEdges[tEdge.TargetID], resolvedEdge)
	}

	for k, v := range mutNodes {
		graph.Nodes = graph.Nodes.Set(k, v)
	}
	graph.Entrypoints = mutEntrypoints
	for k, v := range mutFolderZones {
		graph.FolderZones = graph.FolderZones.Set(k, v)
	}
	for k, v := range mutKindIndex {
		graph.KindIndex = graph.KindIndex.Set(k, v)
	}
	for k, v := range mutHashIndex {
		graph.HashIndex = graph.HashIndex.Set(k, v)
	}
	for k, v := range mutFileNodeIndex {
		graph.FileNodeIndex = graph.FileNodeIndex.Set(k, v)
	}
	for k, v := range mutOutboundEdges {
		graph.OutboundEdges = graph.OutboundEdges.Set(k, v)
	}
	for k, v := range mutInboundEdges {
		graph.InboundEdges = graph.InboundEdges.Set(k, v)
	}

	// Restore metadata node
	commitHash, schemaVersion, ttlVersion, err := scanTTLMetadata(ttlPath)
	if err != nil {
		return nil, fmt.Errorf("metadata scan failed: %w", err)
	}
	if ttlVersion > 0 {
		graph.Version = ttlVersion
	}
	if commitHash != "" {
		graph.CommitHash = commitHash
	}
	if schemaVersion > 0 {
		graph.SchemaVersion = schemaVersion
	}
	if schemaVersion > CurrentSchemaVersion {
		return nil, fmt.Errorf("%w: file schema version %d exceeds supported %d", akgerrs.ErrSchemaVersion, schemaVersion, CurrentSchemaVersion)
	}

	// Run macro inference if requested (skipped during verifyTTLFile - K-03 / W2-04)
	if runMacros {
		RunTopologicalMacroInference(graph)
	}

	return graph, nil
}

// scanDeletedNodeIDs scans a TTL file for tombstone blocks — both the modern
// node-block form (`<uri> a gm:Deleted ; gm:status "DELETED" .`) and the
// legacy bare-triple form (`<uri> gm:status "DELETED" .`) — and returns the
// set of node IDs marked for deletion. Last block wins: a node that was
// deleted and then re-added within the same delta stream keeps its latest
// (non-tombstone) block.
func scanDeletedNodeIDs(ttlPath string) (map[string]bool, error) {
	deleted := make(map[string]bool)

	f, err := os.Open(ttlPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var block []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "@prefix") || strings.HasPrefix(line, "#") {
			continue
		}
		block = append(block, line)
		if strings.HasSuffix(line, ".") {
			blockStr := strings.Join(block, " ")
			block = nil
			// Tombstone detection: `gm:status "DELETED"` as a predicate.
			// Property VALUES containing the text are escaped by the
			// serializer (\"DELETED\"), so this cannot false-positive on
			// legitimate node content.
			fields := strings.Fields(blockStr)
			if len(fields) > 0 {
				id := types.ParseNodeURI(fields[0])
				if strings.Contains(blockStr, ont.PredStatus+` "DELETED"`) {
					deleted[id] = true
				} else {
					// A later non-tombstone block for the same ID resurrects it.
					delete(deleted, id)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return deleted, nil
}

// scanTTLMetadata extracts the metadata block fields (gm:commitHash,
// gm:schemaVersion, gm:version) directly from the raw TTL text. The stage1
// parser drops the metadata node (ID "metadata") from its node map, so the
// restore path cannot rely on ParseTTLFile for it. Maxima are used for
// version/schemaVersion so duplicate blocks from incremental appends can
// never regress the WAL replay bound or smuggle an old schema past the check.
func scanTTLMetadata(ttlPath string) (commitHash string, schemaVersion int, version uint64, err error) {
	f, err := os.Open(ttlPath)
	if err != nil {
		return "", 0, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	// gm:version in metadata is written as a bare integer; tolerate quoted
	// legacy values too.
	parseInt := func(raw string) int {
		n, e := strconv.Atoi(strings.Trim(raw, `"`))
		if e != nil {
			return 0
		}
		return n
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, ont.PredCommitHash) {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				commitHash = strings.Trim(fields[1], `"`)
			}
			continue
		}
		if strings.HasPrefix(line, ont.PredSchemaVersion) {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if n := parseInt(fields[1]); n > schemaVersion {
					schemaVersion = n
				}
			}
			continue
		}
		if strings.HasPrefix(line, ont.PredVersion) {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if n := uint64(parseInt(fields[1])); n > version {
					version = n
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", 0, 0, err
	}
	return commitHash, schemaVersion, version, nil
}

// staleLockAge is how long a db.lock file may remain untouched before it is
// considered stale and stolen. Transactions hold the lock for milliseconds to
// seconds; anything older is a crashed holder.
const staleLockAge = 30 * time.Second

// lockTimeout bounds how long AcquireLock waits before failing.
const lockTimeout = 60 * time.Second

func (tm *AKGTransactionManager) AcquireLock() error {
	lockPath := filepath.Join(tm.storageDir, "db.lock")
	start := time.Now()
	sleepDur := 10 * time.Millisecond

	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
		if err == nil {
			pid := os.Getpid()
			fmt.Fprintf(file, "%d\n%d\n", pid, time.Now().Unix())
			file.Close()
			return nil
		}

		// Stale-lock detection by file age. Probing the lock holder's process
		// via os.FindProcess + Signal relies on OS error text that is
		// localized on Windows and unreliable cross-platform (AUDIT Issue 4
		// §4.5); file age is deterministic. A crashed holder's lock is
		// reclaimed once it is stale.
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > staleLockAge {
			_ = os.Remove(lockPath)
			continue
		}

		if time.Since(start) > lockTimeout {
			return akgerrs.ErrLockTimeout
		}

		time.Sleep(sleepDur)
		if sleepDur < 500*time.Millisecond {
			sleepDur *= 2
		}
	}
}

func (tm *AKGTransactionManager) ReleaseLock() {
	lockPath := filepath.Join(tm.storageDir, "db.lock")
	_ = os.Remove(lockPath)
}

func normalizePath(path string) string {
	clean := filepath.ToSlash(path)
	clean = filepath.Clean(clean)
	return clean
}

// Close blocks until all asynchronous tasks (like disk writes) are complete.
func (tm *AKGTransactionManager) Close() {
	// Persistence is synchronous within ExecuteDeltaTransaction; Close is
	// retained for API compatibility.
}
