package akg

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	akgerrs "github.com/Syamchand123/GlassMarble/internal/errors"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/extract"
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

// AKGTransactionManager coordinates the 4-Sub-Phase Delta Transaction Lifecycle.
type AKGTransactionManager struct {
	mu          sync.Mutex
	container   *MVCCGraphContainer
	storageDir  string
	subscribers []chan AKGCommitEvent
	// MaxStateBytes is the AKG akg.json state-file budget (AUDIT Issue 4 Phase 4A-4).
	// Loading and committing are refused when the state file would exceed it;
	// 0 means unlimited.
	MaxStateBytes int64
}

// metadataNodeURI is the ID of the metadata node block written at the top of
// legacy Turtle serializations. It carries gm:commitHash, gm:schemaVersion,
// and gm:version.
const metadataNodeURI = "http://glassmarble.org/node/metadata"

// jsonStateFile is the canonical GraphJSON state file (v3 store) and the
// single source of truth since Phase C ended the dual-write window: commits
// persist to akg.json only. akg_state.ttl remains a read-only input for the
// one-time self-heal migration of pre-v3 repositories.
const jsonStateFile = "akg.json"

// NewAKGTransactionManager initializes the Transaction Manager and restores state from disk.
func NewAKGTransactionManager(storageDir string) (*AKGTransactionManager, error) {
	return NewAKGTransactionManagerWithOptions(storageDir, 0)
}

// NewAKGTransactionManagerWithOptions initializes the Transaction Manager
// with an explicit state-file byte budget (--max-json-mb, AUDIT Issue 4
// Phase 4A-4): oversized artifacts are refused on load and on commit.
func NewAKGTransactionManagerWithOptions(storageDir string, maxStateBytes int64) (*AKGTransactionManager, error) {
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create AKG storage directory: %w", err)
	}

	container := NewMVCCGraphContainer()
	tm := &AKGTransactionManager{
		container:     container,
		storageDir:    storageDir,
		MaxStateBytes: maxStateBytes,
	}

	// Acquire startup file lock to protect loading
	if err := tm.AcquireLock(); err != nil {
		return nil, fmt.Errorf("database lock acquisition failed at startup: %w", err)
	}
	defer tm.ReleaseLock()

	// Restore persistent state from disk. Failures surface loudly: a corrupt
	// or incompatible state file must never silently produce an empty graph
	// (AUDIT Issue 3 Phase 3B-10 / Issue 5 §5.6).
	if err := tm.loadFromDisk(); err != nil {
		return nil, fmt.Errorf("failed to restore AKG state from disk: %w", err)
	}

	// Seed the MVCC transaction counter from the restored graph version so
	// the transaction sequence continues across process runs: the first
	// commit of this run gets version+1, akg.json's graph version stays
	// monotonically increasing.
	container.txCounter = tm.container.GetSnapshot().Version

	return tm, nil
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
// persists it as a full GraphJSON rewrite (used by `gmb import`). The graph
// is validated before promotion: dangling edges are rejected so the persisted
// state never carries references to missing nodes.
func (tm *AKGTransactionManager) ReplaceGraph(graph *CodePropertyGraph) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if graph == nil {
		return fmt.Errorf("cannot replace with nil graph")
	}

	// Reject dangling edges up front: the post-write verification would reject
	// the state file anyway, but failing early with a clearer message beats a
	// serialization/rename round-trip (AUDIT Issue 5 Phase 5A-1).
	dangling := 0
	graph.OutboundEdges.Iterate(func(srcID string, edges []link.ResolvedEdge) {
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

	if err := tm.saveToDisk(graph); err != nil {
		return fmt.Errorf("failed to persist imported graph: %w", err)
	}

	tm.container.PromoteShadowSnapshot(graph)
	return nil
}

// rebuildIndexes reconstructs LineIndex (the only in-memory index that the
// TTL round-trip does not serialize) and re-verifies the graph for status
// reporting. Shared by import and loadFromDisk.
func rebuildIndexes(graph *CodePropertyGraph) {
	if graph.LineIndex == nil {
		graph.LineIndex = NewCowMap[string, []*link.ResolvedNode]()
	}
	// Bucket per path first: appending through the CowMap re-copied the whole
	// slice for every node in a file, which is O(m^2) per file and runs on
	// every load.
	byPath := make(map[string][]*link.ResolvedNode)
	graph.Nodes.Iterate(func(_ string, node *link.ResolvedNode) {
		normPath := normalizePath(node.FileSpec.Path)
		if normPath != "" {
			byPath[normPath] = append(byPath[normPath], node)
		}
	})
	for normPath, lineNodes := range byPath {
		sort.Slice(lineNodes, func(i, j int) bool {
			return lineNodes[i].FileSpec.LineStart < lineNodes[j].FileSpec.LineStart
		})
		graph.LineIndex = graph.LineIndex.Set(normPath, lineNodes)
	}

	graph.Verified = true
	graph.VerificationMsg = ""
	graph.OutboundEdges.Iterate(func(sourceID string, edges []link.ResolvedEdge) {
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

// ExecuteDeltaTransaction executes the complete 4-Sub-Phase Delta Transaction Lifecycle on the AKG.
func (tm *AKGTransactionManager) ExecuteDeltaTransaction(payload *link.LinkOutput, modifiedFiles []string) error {
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
	// SUB-PHASE A: TRANSACTION LOGGING & ISOLATION
	// ------------------------------------------------------------------------
	// Step A.2: Allocate MVCC Shadow Snapshot for write isolation
	shadow, txID := tm.container.AllocateShadowSnapshot()
	shadow.CommitHash = payload.CommitHash

	// Apply delta to shadow snapshot (Sub-Phase B, C, D)
	_, err := tm.applyDeltaToShadow(shadow, payload, modifiedFiles)
	if err != nil {
		return err
	}

	// ------------------------------------------------------------------------
	// SUB-PHASE D.3: Atomic Commit & Persistence
	// ------------------------------------------------------------------------
	// Promote shadow snapshot to active graph snapshot
	tm.container.PromoteShadowSnapshot(shadow)

	// Persist synchronously (single writer, lock held) with tmp+fsync+rename
	// semantics: the previous good file survives any failure, and errors reach
	// the caller instead of being swallowed (AUDIT Issue 3 Phase 3B-4/3B-10).
	// Since Phase C the atomic JSON write is the durability story — no WAL is
	// involved in the primary write path.
	if err := tm.saveToDisk(shadow); err != nil {
		return fmt.Errorf("failed to persist AKG state: %w", err)
	}

	// Broadcast commit event to visual layout subscribers
	totalEdges := 0
	shadow.OutboundEdges.Iterate(func(_ string, edges []link.ResolvedEdge) {
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

func (tm *AKGTransactionManager) applyDeltaToShadow(shadow *CodePropertyGraph, payload *link.LinkOutput, modifiedFiles []string) (map[string]bool, error) {
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
	// SUB-PHASE B: TRANSACTIONAL INVALIDATION (THE SWEEP PHASE)
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

	// Index maintenance is batched. The values behind KindIndex/HashIndex are
	// whole collections, so touching them once per node re-copied the entire
	// collection per node - O(N^2/K) on a full rescan (~12M map inserts for
	// 15k nodes across ~20 kinds). Instead, mutate one working copy per
	// affected key here and publish a single CowMap.Set per key afterwards.
	kindWork := make(map[string]map[string]bool)
	hashWork := make(map[string]map[string]bool) // hash -> node IDs to drop

	for _, filePath := range modifiedFiles {
		normPath := normalizePath(filePath)
		if nodeSet, exists := shadow.FileNodeIndex.Get(normPath); exists {
			for nodeID := range nodeSet {
				oldNodeIDs[nodeID] = true
				if node, ok := shadow.Nodes.Get(nodeID); ok {
					if _, staged := kindWork[node.Kind]; !staged {
						if kindSet, kindExists := shadow.KindIndex.Get(node.Kind); kindExists {
							working := make(map[string]bool, len(kindSet))
							for k, v := range kindSet {
								working[k] = v
							}
							kindWork[node.Kind] = working
						}
					}
					if working, ok := kindWork[node.Kind]; ok {
						delete(working, nodeID)
					}
					if node.Properties != nil {
						if h, ok := node.Properties["hash"]; ok && h != "" {
							drop, staged := hashWork[h]
							if !staged {
								drop = make(map[string]bool)
								hashWork[h] = drop
							}
							drop[nodeID] = true
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

	// Publish the batched kind removals (one Set per kind, not per node).
	for kind, working := range kindWork {
		shadow.KindIndex = shadow.KindIndex.Set(kind, working)
	}
	// Publish the batched hash removals (one rebuild per hash bucket).
	for h, drop := range hashWork {
		hashList, exists := shadow.HashIndex.Get(h)
		if !exists {
			continue
		}
		filtered := make([]string, 0, len(hashList))
		for _, v := range hashList {
			if !drop[v] {
				filtered = append(filtered, v)
			}
		}
		shadow.HashIndex = shadow.HashIndex.Set(h, filtered)
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
	// SUB-PHASE C: NODE AND EDGE HYDRATION (THE GRAFT PHASE)
	// ------------------------------------------------------------------------
	// Step C.1: Vertex Grafting
	updatedPaths := make(map[string]bool)

	// Same batching as the sweep: accumulate per-key working copies and publish
	// one CowMap.Set per key after the loop. Grafting node-by-node previously
	// re-copied an entire kind set / file set / hash list / line list for every
	// single node.
	graftKind := make(map[string]map[string]bool)
	graftHash := make(map[string][]string)
	graftFile := make(map[string]map[string]bool)
	graftLine := make(map[string][]*link.ResolvedNode)

	for nodeID, node := range payload.GraphNodes {
		if node == nil {
			continue
		}
		shadow.Nodes = shadow.Nodes.Set(nodeID, node)

		if _, staged := graftKind[node.Kind]; !staged {
			existingSet, _ := shadow.KindIndex.Get(node.Kind)
			working := make(map[string]bool, len(existingSet)+1)
			for k, v := range existingSet {
				working[k] = v
			}
			graftKind[node.Kind] = working
		}
		graftKind[node.Kind][nodeID] = true

		if node.Properties != nil {
			if h, ok := node.Properties["hash"]; ok && h != "" {
				if _, staged := graftHash[h]; !staged {
					hashList, _ := shadow.HashIndex.Get(h)
					working := make([]string, len(hashList), len(hashList)+1)
					copy(working, hashList)
					graftHash[h] = working
				}
				graftHash[h] = append(graftHash[h], nodeID)
			}
		}

		normPath := normalizePath(node.FileSpec.Path)
		if normPath != "" {
			if _, staged := graftFile[normPath]; !staged {
				fileSet, _ := shadow.FileNodeIndex.Get(normPath)
				working := make(map[string]bool, len(fileSet)+1)
				for k, v := range fileSet {
					working[k] = v
				}
				graftFile[normPath] = working

				lineNodes, _ := shadow.LineIndex.Get(normPath)
				lineWorking := make([]*link.ResolvedNode, len(lineNodes), len(lineNodes)+1)
				copy(lineWorking, lineNodes)
				graftLine[normPath] = lineWorking
			}
			graftFile[normPath][nodeID] = true
			graftLine[normPath] = append(graftLine[normPath], node)
			updatedPaths[normPath] = true
		}
	}

	// Publish the batched graft indexes.
	for kind, working := range graftKind {
		shadow.KindIndex = shadow.KindIndex.Set(kind, working)
	}
	for h, working := range graftHash {
		shadow.HashIndex = shadow.HashIndex.Set(h, working)
	}
	for path, working := range graftFile {
		shadow.FileNodeIndex = shadow.FileNodeIndex.Set(path, working)
	}
	for normPath, lineNodes := range graftLine {
		sort.Slice(lineNodes, func(i, j int) bool {
			return lineNodes[i].FileSpec.LineStart < lineNodes[j].FileSpec.LineStart
		})
		shadow.LineIndex = shadow.LineIndex.Set(normPath, lineNodes)
	}

	// Step C.2: Vector Binding
	for sourceID, edges := range payload.OutboundEdges {
		newSlice := make([]link.ResolvedEdge, len(edges))
		copy(newSlice, edges)
		shadow.OutboundEdges = shadow.OutboundEdges.Set(sourceID, newSlice)
	}

	// Step C.2.b: Phase 3.8 and 3.9 Metadata Binding.
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
			inboundEdge := link.ResolvedEdge{
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
				newEdges := make([]link.ResolvedEdge, len(existingEdges)+1)
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
	keptOutbound := NewCowMap[string, []link.ResolvedEdge]()
	shadow.OutboundEdges.Iterate(func(sourceID string, edges []link.ResolvedEdge) {
		if _, srcOK := shadow.Nodes.Get(sourceID); !srcOK {
			return
		}
		var kept []link.ResolvedEdge
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
	newInbound := NewCowMap[string, []link.ResolvedEdge]()
	shadow.OutboundEdges.Iterate(func(sourceID string, edges []link.ResolvedEdge) {
		for _, edge := range edges {
			existing, _ := newInbound.Get(edge.TargetID)
			newEdges := make([]link.ResolvedEdge, len(existing)+1)
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
	// SUB-PHASE D: GRAPH INVARIANT VERIFICATION & REASONING
	// ------------------------------------------------------------------------
	// Step D.1: Dangling Reference Audit. The post-graft sweep (Step C.4)
	// already guarantees every surviving edge has both endpoints; this pass
	// is the invariant check that catches any future regression before the
	// zero-dangling guard at write time.
	shadow.OutboundEdges.Iterate(func(sourceID string, edges []link.ResolvedEdge) {
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

// saveToDisk persists the graph to the canonical GraphJSON state file
// atomically: tmp-file -> fsync -> post-write verification -> atomic rename.
// The previous good file is preserved on any failure (AUDIT Issue 3
// Phase 3B-4, Issue 5 Phase 5A-1). Since Phase C the JSON store is the single
// write artifact; the legacy Turtle mirror is no longer produced (the TTL
// read path remains for one-time self-heal migration of pre-v3 repositories).
func (tm *AKGTransactionManager) saveToDisk(graph *CodePropertyGraph) error {
	if graph == nil {
		return fmt.Errorf("cannot persist nil graph")
	}
	if err := tm.writeJSONState(graph); err != nil {
		return fmt.Errorf("failed to persist state file: %w", err)
	}
	return nil
}

// writeJSONState atomically persists the graph as GraphJSON (tmp-file -> fsync
// -> parse-back verification -> rename), mirroring the former TTL write path.
// A JSON write failure never disturbs the previous good state file.
func (tm *AKGTransactionManager) writeJSONState(graph *CodePropertyGraph) error {
	jsonPath := filepath.Join(tm.storageDir, jsonStateFile)

	tmp, err := os.CreateTemp(tm.storageDir, "akg.json.tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp JSON state file: %w", err)
	}
	defer os.Remove(tmp.Name())

	// Buffer the encoder: json.Encoder issues many small writes, and an
	// unbuffered *os.File turns each into a syscall.
	bw := bufio.NewWriterSize(tmp, 1<<20)
	wantSum, err := ExportGraphJSONVerified(graph, bw)
	if err != nil {
		return fmt.Errorf("JSON serialization failed: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("failed to flush temp JSON state file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("failed to fsync temp JSON state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp JSON state file: %w", err)
	}

	if err := tm.enforceStateBudget(tmp.Name()); err != nil {
		return err
	}

	// Post-write verification BEFORE the atomic rename: if the staged file is
	// corrupt, the previous good file is still in place.
	if err := tm.verifyJSONFile(tmp.Name(), graph, wantSum); err != nil {
		return err
	}

	if err := os.Rename(tmp.Name(), jsonPath); err != nil {
		return fmt.Errorf("failed to atomically rename JSON state file into place: %w", err)
	}

	// Record the digest alongside the state so corruption at rest is
	// detectable by tooling without re-deriving the graph.
	_ = os.WriteFile(jsonPath+".sha256", []byte(wantSum+"\n"), 0o644)

	// An atomic rename is not durable until the containing directory is
	// fsynced; without this a crash can leave the directory entry pointing at
	// the old file despite a successful write. Best-effort: directory fsync is
	// not supported on all platforms.
	if dir, derr := os.Open(tm.storageDir); derr == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// verifyJSONFile proves the staged file on disk is exactly the byte stream the
// serializer produced, by streaming it back through SHA-256 and comparing with
// the digest computed while writing.
//
// This used to re-read the file, unmarshal it into a second document,
// re-serialize the graph into a third, and byte-compare — roughly the cost of
// the write again, on the hottest stage of an analysis. The invariants that
// pass was actually protecting are now enforced where they are cheaper and
// stronger: ExportGraphJSONVerified checks the zero-dangling guard against the
// document being written, and the digest detects any truncation or corruption
// between serialization and the atomic rename (which byte-comparing a
// re-serialization could not distinguish from a deterministic serializer bug).
func (tm *AKGTransactionManager) verifyJSONFile(jsonPath string, graph *CodePropertyGraph, wantSum string) error {
	f, err := os.Open(jsonPath)
	if err != nil {
		return fmt.Errorf("post-write JSON verification failed: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, bufio.NewReaderSize(f, 1<<20)); err != nil {
		return fmt.Errorf("post-write JSON verification failed: %w", err)
	}
	gotSum := hex.EncodeToString(h.Sum(nil))
	if gotSum != wantSum {
		return fmt.Errorf("post-write JSON verification failed: staged file digest %s does not match the serialized graph digest %s; the write was rejected and the previous good file was kept", gotSum, wantSum)
	}

	graph.Verified = true
	graph.VerificationMsg = ""
	return nil
}

// enforceStateBudget refuses to proceed when the state file at path exceeds
// the configured --max-json-mb budget. It is applied on load (oversized
// artifacts must not be pulled into RAM) and on save (an oversized commit is
// rejected before the atomic rename, leaving the previous good file in place).
func (tm *AKGTransactionManager) enforceStateBudget(path string) error {
	if tm.MaxStateBytes <= 0 {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if st.Size() > tm.MaxStateBytes {
		return fmt.Errorf("AKG state file is %.1f MB, exceeding the --max-json-mb budget of %.1f MB; refused. Lower the analysis scope (--link-level=architecture), raise the budget, or rebuild with `gmb analyze --full`",
			float64(st.Size())/(1<<20), float64(tm.MaxStateBytes)/(1<<20))
	}
	return nil
}

// loadFromDisk restores the active graph from the canonical GraphJSON state
// database (akg.json). A missing akg.json with a legacy akg_state.ttl
// present triggers the one-time self-heal migration (D3): the TTL is loaded
// with today's semantics (schema backup/migrate), akg.json is written from
// the restored graph, and the TTL is archived as akg_state.ttl.bak — never
// deleted.
func (tm *AKGTransactionManager) loadFromDisk() error {
	jsonPath := filepath.Join(tm.storageDir, jsonStateFile)
	if _, err := os.Stat(jsonPath); err == nil {
		return tm.loadFromJSON(jsonPath)
	} else if !os.IsNotExist(err) {
		return err
	}

	// No akg.json. Legacy TTL self-heal path.
	StatePath := filepath.Join(tm.storageDir, "akg_state.ttl")
	if _, err := os.Stat(StatePath); err == nil {
		if err := tm.loadFromLegacyTTL(); err != nil {
			return err
		}
		// Persist the canonical JSON store from the restored graph, then
		// archive the TTL out of the way (never delete user data).
		if err := tm.writeJSONState(tm.container.GetSnapshot()); err != nil {
			return fmt.Errorf("self-heal: failed to write akg.json from legacy state: %w", err)
		}
		bakPath := StatePath + ".bak"
		if err := os.Rename(StatePath, bakPath); err != nil {
			return fmt.Errorf("self-heal: failed to archive legacy state file as %s: %w", bakPath, err)
		}
		return nil
	}

	// Fresh database: start empty.
	graph := NewCodePropertyGraph("initial")
	graph.Version = 0
	graph.SchemaVersion = CurrentSchemaVersion
	tm.container.ActiveGraph = graph
	return nil
}

// loadFromJSON restores the active graph from an akg.json document.
func (tm *AKGTransactionManager) loadFromJSON(jsonPath string) error {
	if err := tm.enforceStateBudget(jsonPath); err != nil {
		return err
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", jsonPath, err)
	}

	// Schema migration with an akg.json.bak backup of the pre-migration
	// artifact. ImportGraphJSON performs the in-memory migration itself.
	var doc GraphJSON
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("failed to parse %s: %w", jsonPath, err)
	}
	if doc.SchemaVersion > 0 && doc.SchemaVersion < CurrentSchemaVersion {
		bakPath := jsonPath + ".bak"
		if err := os.WriteFile(bakPath, data, 0644); err != nil {
			return fmt.Errorf("schema migration backup failed: %w", err)
		}
	}

	graph, err := ImportGraphJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to restore AKG state from akg.json: %w", err)
	}

	// Schema v3 migration handling (K-07 / W2-08)
	if doc.SchemaVersion > 0 && doc.SchemaVersion < CurrentSchemaVersion {
		oldVer := doc.SchemaVersion
		if err := tm.writeJSONState(graph); err != nil {
			return fmt.Errorf("persisting schema migration failed: %w", err)
		}
		graph.VerificationMsg = fmt.Sprintf("Migrated AKG schema from v%d to v%d (backup created at %s)", oldVer, CurrentSchemaVersion, jsonPath+".bak")
	}

	// Rebuild LineIndex since it is not serialized
	rebuildIndexes(graph)

	tm.container.ActiveGraph = graph
	return nil
}

// loadFromLegacyTTL restores the active graph from the legacy Turtle state
// database (akg_state.ttl), preserving the previous load semantics: schema
// migration with a .bak backup, and a re-persist of the migrated state.
func (tm *AKGTransactionManager) loadFromLegacyTTL() error {
	StatePath := filepath.Join(tm.storageDir, "akg_state.ttl")
	if err := tm.enforceStateBudget(StatePath); err != nil {
		return err
	}
	graph, err := tm.reconstructFromTurtle()
	if err != nil {
		return err
	}

	// Schema v3 migration handling (K-07 / W2-08)
	if graph.SchemaVersion < CurrentSchemaVersion {
		oldVer := graph.SchemaVersion
		bakPath, _ := CreateSchemaBackup(tm.storageDir, oldVer)
		if err := MigrateToSchemaV3(graph); err != nil {
			return fmt.Errorf("schema migration failed: %w", err)
		}
		if err := tm.saveToDisk(graph); err != nil {
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

func reconstructFromTTLFile(StatePath string) (*CodePropertyGraph, error) {
	return reconstructFromTTLFileEx(StatePath, true)
}

// reconstructFromTTLFileEx rebuilds a CodePropertyGraph from a TTL file.
// If runMacros is false, topological macro inference is skipped (used by TTL
// parity verification, K-03 / W2-04).
func reconstructFromTTLFileEx(StatePath string, runMacros bool) (*CodePropertyGraph, error) {
	if _, err := os.Stat(StatePath); os.IsNotExist(err) {
		return nil, err
	}

	// Tombstones first
	deletedIDs, err := scanDeletedNodeIDs(StatePath)
	if err != nil {
		return nil, err
	}

	nodes, edges, err := extract.ParseTTLFile(StatePath)
	if err != nil {
		return nil, fmt.Errorf("TTL parse failed: %w", err)
	}

	graph := NewCodePropertyGraph("restored_from_ttl")

	mutNodes := make(map[string]*link.ResolvedNode, len(nodes))
	mutEntrypoints := make([]string, 0)
	mutFolderZones := make(map[string]string)
	mutKindIndex := make(map[string]map[string]bool)
	mutHashIndex := make(map[string][]string)
	mutFileNodeIndex := make(map[string]map[string]bool)

	// Convert TTLNode to link.ResolvedNode
	for id, tNode := range nodes {
		if id == metadataNodeURI || deletedIDs[id] {
			continue
		}
		kind := mapClassToKind(tNode.Kind)
		prim := strings.TrimPrefix(tNode.PrimitiveType, ont.PrefixGM)

		resNode := &link.ResolvedNode{
			ID:        id,
			Kind:      kind,
			Name:      tNode.Name,
			Primitive: prim,
			FileSpec: link.LocationMeta{
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

	mutOutboundEdges := make(map[string][]link.ResolvedEdge)
	mutInboundEdges := make(map[string][]link.ResolvedEdge)

	// Rebuild Outbound and Inbound Edges with canonical edge types
	for _, tEdge := range edges {
		if deletedIDs[tEdge.SourceID] || deletedIDs[tEdge.TargetID] {
			continue
		}
		if strings.HasPrefix(tEdge.Predicate, ont.PredStatus) {
			continue
		}

		resolvedEdge := link.ResolvedEdge{
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
	commitHash, schemaVersion, ttlVersion, err := scanTTLMetadata(StatePath)
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

	// Run macro inference if requested (skipped during TTL parity verification - K-03 / W2-04)
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
func scanDeletedNodeIDs(StatePath string) (map[string]bool, error) {
	deleted := make(map[string]bool)

	f, err := os.Open(StatePath)
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
// gm:schemaVersion, gm:version) directly from the raw TTL text. The ingest
// parser drops the metadata node (ID "metadata") from its node map, so the
// restore path cannot rely on ParseTTLFile for it. Maxima are used for
// version/schemaVersion so duplicate blocks from incremental appends can
// never regress the restore version or smuggle an old schema past the check.
func scanTTLMetadata(StatePath string) (commitHash string, schemaVersion int, version uint64, err error) {
	f, err := os.Open(StatePath)
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
// seconds; anything older is a crashed holder. Raised to 60s (== lockTimeout)
// so a slow disk / AV scan cannot cause a concurrent writer to steal the lock
// mid-transaction (C2b-1).
const staleLockAge = 60 * time.Second

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
