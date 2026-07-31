package akg

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage1"
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
	wg          sync.WaitGroup
}

// NewAKGTransactionManager initializes the Transaction Manager, restores state from disk, and runs WAL recovery.
func NewAKGTransactionManager(storageDir string) (*AKGTransactionManager, error) {
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create AKG storage directory: %w", err)
	}

	wal, err := NewWriteAheadLog(filepath.Join(storageDir, "wal"))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize WAL logger: %w", err)
	}

	container := NewMVCCGraphContainer()
	tm := &AKGTransactionManager{
		container:  container,
		wal:        wal,
		storageDir: storageDir,
	}

	// Acquire startup file lock to protect recovery and loading
	if err := tm.AcquireLock(); err != nil {
		return nil, fmt.Errorf("database lock acquisition failed at startup: %w", err)
	}
	defer tm.ReleaseLock()

	// Restore persistent state from disk if present
	_ = tm.loadFromDisk()

	// Replay and recover any committed transactions from the WAL file
	_ = tm.Recover()

	return tm, nil
}

// Recover checks the WAL log on disk to replay any committed transactions that weren't fully written to state.
func (tm *AKGTransactionManager) Recover() error {
	entries, err := tm.wal.ReadAllEntries()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}

	startedMap := make(map[uint64]*WALEntry)
	committedMap := make(map[uint64]bool)

	for _, entry := range entries {
		if entry.Status == WALStatusStarted {
			startedMap[entry.TxID] = entry
		} else if entry.Status == WALStatusCommitted {
			committedMap[entry.TxID] = true
		}
	}

	activeGraph := tm.container.GetSnapshot()
	maxAppliedTx := activeGraph.Version

	replayed := false
	for txID, entry := range startedMap {
		if committedMap[txID] && txID > maxAppliedTx {
			shadow := activeGraph.Clone()
			shadow.CommitHash = entry.CommitHash
			shadow.Version = txID

			if _, err := tm.applyDeltaToShadow(shadow, entry.Payload, entry.ModifiedFiles); err != nil {
				return fmt.Errorf("WAL recovery failed to apply transaction %d: %w", txID, err)
			}

			tm.container.PromoteShadowSnapshot(shadow)
			activeGraph = shadow
			maxAppliedTx = txID
			replayed = true
		}
	}

	if replayed {
		_ = tm.saveToDisk(activeGraph, nil, nil)
	}

	_, err = tm.wal.Checkpoint()
	return err
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

// ExecuteDeltaTransaction executes the complete 4-Sub-Stage Delta Transaction Lifecycle on the AKG.
func (tm *AKGTransactionManager) ExecuteDeltaTransaction(payload *stage4.Stage4Output, modifiedFiles []string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if payload == nil {
		return nil
	}

	// Acquire cross-process file lock during delta transaction
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
	deletedNodeIDs, err := tm.applyDeltaToShadow(shadow, payload, modifiedFiles)
	if err != nil {
		return err
	}

	// ------------------------------------------------------------------------
	// SUB-STAGE D.3: Atomic Commit & Persistence
	// ------------------------------------------------------------------------
	_ = tm.wal.MarkCommitted(txID)

	// Promote shadow snapshot to active graph snapshot
	tm.container.PromoteShadowSnapshot(shadow)

	// Save active graph to disk asynchronously
	tm.wg.Add(1)
	go func(g *CodePropertyGraph) {
		defer tm.wg.Done()
		if err := tm.saveToDisk(g, payload, deletedNodeIDs); err != nil {
			// Optional: log error here
		}
	}(shadow.Clone())

	// Checkpoint WAL log file from disk if it exceeds limits
	rotated, _ := tm.wal.Checkpoint()
	if rotated {
		tm.wg.Add(1)
		go func(g *CodePropertyGraph) {
			defer tm.wg.Done()
			_ = tm.saveBaseSnapshot(g)
		}(shadow.Clone())
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
	// Step B.1: Node Context Sweep
	deletedNodeIDs := make(map[string]bool)
	for _, filePath := range modifiedFiles {
		normPath := normalizePath(filePath)
		if nodeSet, exists := shadow.FileNodeIndex.Get(normPath); exists {
			for nodeID := range nodeSet {
				deletedNodeIDs[nodeID] = true
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
		if !deletedNodeIDs[ep] {
			validEntrypoints = append(validEntrypoints, ep)
		}
	}
	shadow.Entrypoints = validEntrypoints

	// Step B.2: Dangling Edge Eradication
	var sweepWg sync.WaitGroup
	sweepWg.Add(2)

	go func() {
		defer sweepWg.Done()
		for _, sourceID := range shadow.OutboundEdges.Keys() {
			edges, _ := shadow.OutboundEdges.Get(sourceID)
			n := 0
			for _, edge := range edges {
				if !deletedNodeIDs[edge.TargetID] {
					edges[n] = edge
					n++
				}
			}
			if n == 0 {
				shadow.OutboundEdges = shadow.OutboundEdges.Delete(sourceID)
			} else {
				shadow.OutboundEdges = shadow.OutboundEdges.Set(sourceID, edges[:n])
			}
		}
	}()

	go func() {
		defer sweepWg.Done()
		for _, targetID := range shadow.InboundEdges.Keys() {
			if deletedNodeIDs[targetID] {
				shadow.InboundEdges = shadow.InboundEdges.Delete(targetID)
				continue
			}
			edges, _ := shadow.InboundEdges.Get(targetID)
			n := 0
			for _, edge := range edges {
				if !deletedNodeIDs[edge.SourceID] {
					edges[n] = edge
					n++
				}
			}
			shadow.InboundEdges = shadow.InboundEdges.Set(targetID, edges[:n])
		}
	}()

	sweepWg.Wait()

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

	// Step C.2.b: Stage 3.8 and 3.9 Metadata Binding
	shadow.Entrypoints = append(shadow.Entrypoints, payload.EntrypointRegistry...)
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

	// ------------------------------------------------------------------------
	// SUB-STAGE D: GRAPH INVARIANT VERIFICATION & REASONING
	// ------------------------------------------------------------------------
	// Step D.1: Dangling Reference Audit
	shadow.Errors = nil
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

	// Step D.2: Topological Macro-Inference Parsing
	RunTopologicalMacroInference(shadow, payload.Config)

	return deletedNodeIDs, nil
}

func (tm *AKGTransactionManager) saveBaseSnapshot(graph *CodePropertyGraph) error {
	gzPath := filepath.Join(tm.storageDir, "akg_state.json.gz")
	f, err := os.Create(gzPath)
	if err == nil {
		gw := gzip.NewWriter(f)
		enc := json.NewEncoder(gw)
		if err := enc.Encode(graph); err != nil {
			return err
		}
		gw.Close()
		f.Close()
	}
	os.Remove(filepath.Join(tm.storageDir, "akg_state.json"))
	return nil
}

func (tm *AKGTransactionManager) saveToDisk(graph *CodePropertyGraph, payload *stage4.Stage4Output, deletedNodeIDs map[string]bool) error {
	// 1. Base snapshots (.json.gz) are now exclusively handled by saveBaseSnapshot during WAL rotation.

	// 2. Save W3C RDF Turtle representation as primary persistent graph database
	ttlPath := filepath.Join(tm.storageDir, "akg_state.ttl")

	// Check if we can do an incremental append
	if payload != nil && len(deletedNodeIDs) != 0 {
		deltaSize := len(payload.GraphNodes) + len(deletedNodeIDs)
		baseSize := graph.Nodes.Len()

		// Compact when delta exceeds 20% of base, or file doesn't exist
		_, statErr := os.Stat(ttlPath)
		if statErr == nil && baseSize > 0 && float64(deltaSize) <= float64(baseSize)*0.20 {
			// Incremental append
			f, err := os.OpenFile(ttlPath, os.O_APPEND|os.O_WRONLY, 0644)
			if err == nil {
				err = SerializeDeltaToTurtle(payload, deletedNodeIDs, f)
				f.Close()
				if err == nil {
					return nil // Success incremental write
				}
			}
		}
	}

	// Fallback to full rewrite (compaction)
	f, err := os.OpenFile(ttlPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	return SerializeToTurtle(graph, f)
}

func (tm *AKGTransactionManager) loadFromDisk() error {
	var graph *CodePropertyGraph

	jsonPath := filepath.Join(tm.storageDir, "akg_state.json")
	gzPath := filepath.Join(tm.storageDir, "akg_state.json.gz")
	var loaded CodePropertyGraph
	var readErr error
	var successfullyLoaded bool

	if _, err := os.Stat(gzPath); err == nil {
		f, err := os.Open(gzPath)
		if err == nil {
			defer f.Close()
			gr, err := gzip.NewReader(f)
			if err == nil {
				decoder := json.NewDecoder(gr)
				if err := decoder.Decode(&loaded); err == nil {
					successfullyLoaded = true
				} else {
					readErr = err
				}
				gr.Close()
			}
		}
	} else if _, err := os.Stat(jsonPath); err == nil {
		f, err := os.Open(jsonPath)
		if err == nil {
			defer f.Close()
			decoder := json.NewDecoder(f)
			if err := decoder.Decode(&loaded); err == nil {
				successfullyLoaded = true
			} else {
				readErr = err
			}
		}
	}

	if successfullyLoaded {
		if loaded.SchemaVersion < CurrentSchemaVersion {
			// Migration Logic for older schemas
			loaded.SchemaVersion = CurrentSchemaVersion
			if loaded.InboundEdges == nil {
				loaded.InboundEdges = NewCowMap[string, []stage4.ResolvedEdge]()
			}
			if loaded.OutboundEdges == nil {
				loaded.OutboundEdges = NewCowMap[string, []stage4.ResolvedEdge]()
			}
		}
		graph = &loaded
	} else {
		if readErr == nil {
			readErr = fmt.Errorf("no cache file found")
		}
	}

	// Self-healing fallback if JSON cache is missing or corrupt
	if graph == nil {
		// Attempt to restore from primary Turtle state database
		restored, err := tm.reconstructFromTurtle()
		if err == nil {
			graph = restored
			// Repair the cache file immediately
			_ = tm.saveToDisk(graph, nil, nil)
		} else {
			if readErr != nil {
				return fmt.Errorf("JSON cache load failed (%v) and Turtle fallback failed: %w", readErr, err)
			}
			return err
		}
	}

	// Rebuild LineIndex since it is not serialized
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

	tm.container.ActiveGraph = graph
	return nil
}

func (tm *AKGTransactionManager) reconstructFromTurtle() (*CodePropertyGraph, error) {
	ttlPath := filepath.Join(tm.storageDir, "akg_state.ttl")
	if _, err := os.Stat(ttlPath); os.IsNotExist(err) {
		return nil, err
	}

	nodes, edges, err := stage1.ParseTTLFile(ttlPath)
	if err != nil {
		return nil, err
	}

	graph := NewCodePropertyGraph("restored_from_ttl")

	// Convert TTLNode to stage4.ResolvedNode
	for id, tNode := range nodes {
		kind := mapClassToKind(tNode.Kind)
		prim := strings.TrimPrefix(tNode.PrimitiveType, "gm:")

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
		graph.Nodes = graph.Nodes.Set(id, resNode)

		// Rebuild FileNodeIndex
		normPath := normalizePath(resNode.FileSpec.Path)
		if normPath != "" {
			fileSet, _ := graph.FileNodeIndex.Get(normPath)
			newFileSet := make(map[string]bool, len(fileSet))
			for k, v := range fileSet {
				newFileSet[k] = v
			}
			newFileSet[id] = true
			graph.FileNodeIndex = graph.FileNodeIndex.Set(normPath, newFileSet)
		}
	}

	// Rebuild Outbound and Inbound Edges
	for _, tEdge := range edges {
		pred := stage4.RelationshipType(strings.ToUpper(strings.TrimPrefix(tEdge.Predicate, "gm:")))

		resolvedEdge := stage4.ResolvedEdge{
			SourceID:   tEdge.SourceID,
			TargetID:   tEdge.TargetID,
			Type:       pred,
			LineNumber: tEdge.LineNumber,
		}

		outEdges, _ := graph.OutboundEdges.Get(tEdge.SourceID)
		newOutEdges := make([]stage4.ResolvedEdge, len(outEdges)+1)
		copy(newOutEdges, outEdges)
		newOutEdges[len(newOutEdges)-1] = resolvedEdge
		graph.OutboundEdges = graph.OutboundEdges.Set(tEdge.SourceID, newOutEdges)

		inEdges, _ := graph.InboundEdges.Get(tEdge.TargetID)
		newInEdges := make([]stage4.ResolvedEdge, len(inEdges)+1)
		copy(newInEdges, inEdges)
		newInEdges[len(newInEdges)-1] = resolvedEdge
		graph.InboundEdges = graph.InboundEdges.Set(tEdge.TargetID, newInEdges)
	}

	// Run macro inference on restored graph
	RunTopologicalMacroInference(graph)

	return graph, nil
}

func (tm *AKGTransactionManager) AcquireLock() error {
	lockPath := filepath.Join(tm.storageDir, "db.lock")
	start := time.Now()
	sleepDur := 10 * time.Millisecond

	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
		if err == nil {
			pid := os.Getpid()
			fmt.Fprintf(file, "%d\n", pid)
			file.Close()
			return nil
		}

		if lockData, readErr := os.ReadFile(lockPath); readErr == nil {
			var lockedPid int
			if n, _ := fmt.Sscanf(strings.TrimSpace(string(lockData)), "%d", &lockedPid); n == 1 {
				proc, err := os.FindProcess(lockedPid)
				if err != nil {
					_ = os.Remove(lockPath)
				} else {
					if sigErr := proc.Signal(syscall.Signal(0)); sigErr != nil {
						if strings.Contains(sigErr.Error(), "already finished") || strings.Contains(sigErr.Error(), "no such process") {
							_ = os.Remove(lockPath)
						}
					}
				}
			}
		}

		if time.Since(start) > 10*time.Second {
			return fmt.Errorf("failed to acquire database lock within 10 seconds (another transaction is active)")
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
	tm.wg.Wait()
}
