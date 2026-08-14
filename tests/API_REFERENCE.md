# GlassMarble Test Authoring Reference (internal)

Compact API reference for the test suites under tests/. All paths are
relative to G:\GlassMarble. Verified against source on 2026-08-10.

## Rules

- CLI tests: `harness.RunGmb(t, sb, args...)` runs IN PROCESS via
  `cmd.RootCmdForTesting()`. It chdirs to the sandbox root, swaps os.Stdout,
  resets all cobra flags, and appends `--dir <root>` automatically when the
  target command declares a --dir flag. Returns (stdout, error).
- NEVER call t.Parallel() in a test that uses RunGmb (process-global state).
- Build the real binary once per process with `harness.BuildBinary(t)` and
  run it with `harness.RunBinary(t, bin, workdir, env, args...)` →
  (stdout, stderr, exitCode). Use for watch/hooks/exit-code/cross-process
  tests.
- AI commands: `harness.NewMockLLM(t)` (OpenAI-compatible /chat/completions,
  SSE streaming, tool calls, scriptable). Point the sandbox at it via
  `sb.SeedAIConfig(baseURL)`, or use flags `--provider custom --base-url
  <url> --key x --model m --no-stream`.
- `gmb ai` resolves rootDir from the persistent `--root-dir` flag (default
  ".") — RunGmb chdirs into the sandbox so "." resolves there.
- git repo: `sb.GitInit()` then `sb.GitCommitFiles(msg, map[string]string)`
  or `sb.GitCommit(msg)` after writing files. `sb.GitHead()` for hashes.
- Sandbox helpers: `sb.WriteFile(rel, content)`, `sb.ReadFile(rel)`,
  `sb.Exists(rel)`, `sb.Path(rel...)`, `sb.SampleProject()`,
  `sb.WriteAKGState(*akg.GraphJSON)`, `sb.WriteEmptyAKGState()`,
  `sb.SeedMemory(projectID)`, `sb.SeedCorrections()`,
  `sb.SeedConfigYAML(content)`, `sb.SeedAIConfig(baseURL)`,
  `sb.GitStatusPorcelain()`, `sb.TreeContents(rel)`.
- Fixture graphs: `harness.TinyGraph() *akg.GraphJSON` (2 nodes/1 edge),
  `harness.BigGraph(n) *akg.GraphJSON` (chain of n nodes).

## Key real APIs (verified signatures)

### internal/config
- `config.Load(flagConfig config.Config) (*Config, error)` — reads flags >
  GLASSMARBLE_* env > .glassmarble/config.yaml (CWD!) > ~/.glassmarble/
  config.yaml > defaults (WorkerCount 4, MaxFileBytes 10MiB).
- `config.Config{RootDir, WorkerCount, MaxFileBytes, Debug, StorageDir,
  OutputFormat, IncludeHidden}` + nested Drift/Intelligence/Fusion/Learning/
  Aging sections; `DefaultIntelligenceConfig()` etc.

### internal/code_analysis_engine
- ingest: `RunIngestion(cfg Config) (*IngestOutput, error)`,
  `RunIngestionForDelta(cfg Config, diff []FileTask) (*IngestOutput, error)`,
  `CollectGitDiff(rootDir, commitHash string) ([]FileTask, error)`,
  `DefaultConfig(root string) Config`. IngestOutput{Updated, Deleted,
  Skipped, Warnings, ...}. Config has WithWorkers, MaxFileBytes,
  GitTrackedOnly.
- normalize: `Normalize(ingestOut *ingest.IngestOutput, commitHash string)
  (*NormalizeOutput, error)`.
- aggregate: `Aggregate(payload *normalize.NormalizeOutput, existingState
  *AggregateOutput, rootDir string) (*AggregateOutput, error)`.
- link: `Link(aggregateOut *aggregate.AggregateOutput, modifiedFiles []string, db
  GraphDB, config ...LinkerConfig) (*LinkOutput, error)`; db from
  `akg.NewCodePropertyGraph(commitHash string) *CodePropertyGraph`;
  `link.LinkerConfig{LevelOfDetail: link.LevelArchitecture|LevelFull,
  ...}`, `link.LevelOfDetail` constants; ResolvedNode{ID, Name, Kind,
  FileSpec{Path,LineStart,LineEnd}, Properties map[string]string};
  `link.NewLinkOutput()`, `out.AddEdge(...)`, QualityMetrics via
  `link.MeasureQuality`.

### internal/akg (transaction manager + state)
- `akg.NewAKGTransactionManager(storageDir string) (*AKGTransactionManager,
  error)`; `NewAKGTransactionManagerWithOptions(storageDir string,
  maxStateBytes int64)`.
- `tm.GetActiveGraph() *akg.CodePropertyGraph`;
  `tm.ExecuteDeltaTransaction(payload *link.LinkOutput, modifiedFiles
  []string) error`; `tm.ReplaceGraph(...)`; `tm.Subscribe() chan
  AKGCommitEvent`; `tm.AcquireLock()/ReleaseLock()/Close()`.
- `akg.StateMetadata(storageDir) (commitHash, schemaVersion string, version
  uint64, err error)`; `akg.RunDoctor(storageDir) DoctorReport{LoadOK,
  ...}`; `akg.MeasureGraphQuality(graph) link.QualityMetrics`;
  `akg.StreamGraphStats`, `akg.QueryNode`, `akg.StreamNodes`;
  `akg.DiffGraphs(base, head *CodePropertyGraph) *GraphDiff`;
  `akg.ImportGraphJSON(r io.Reader)`, `akg.ExportGraphJSON(graph, w)`.
- GraphJSON: `SchemaVersion (akg.CurrentSchemaVersion=3), Version uint64,
  CommitHash, Nodes []GraphNodeJSON{ID,Kind,Name,FileSpec
  LocationMetaJSON{Path,LineStart,LineEnd},Properties}, Edges
  []GraphEdgeJSON{SourceID,TargetID,Type,LineNumber}`.
- Lock: `.glassmarble/db.lock`, stale after 30s, 60s timeout. State file:
  `.glassmarble/akg.json` (atomic tmp+rename+fsync).

### internal/arch_intelligence (Architecture Intelligence)
- `arch_intelligence.NewEngine(graph *akg.CodePropertyGraph) *Engine`;
  `NewEngineWithOptions(graph, opts...)`; `engine.Run() IntelligenceResult`;
  `IntelligenceResult{Metrics, Components, Patterns, Smells, GraphHash, ...}`;
  `LoadLatestResult(storageDir string) (*IntelligenceResult, error)`;
  `MetricSummary(metrics archmodel.ArchMetrics) string`.
- Config: `config.IntelligenceConfig{...}`; defaults via
  `config.DefaultIntelligenceConfig()`.
- archmodel types: `DetectedPattern{Name, Kind PatternKind, Components
  []string, Confidence float64}`, `ArchSmell{Title, Kind SmellKind,
  Severity Severity, AffectedIDs []string, Description}`,
  `DetectedComponent{Name, Kind, Directories, NodeIDs, Confidence}`,
  `Severity (SeverityCritical/High/Medium/Low)`,
  `ArchMetrics{...}` (fields: node/edge counts, coupling, cohesion,
  cycles, instability...).

### internal/developer_memory (Phases 6-8)
- `developer_memory.NewStoreForRepo(repoDir string) *MemoryStore`;
  `store.LoadMemory() (*DeveloperMemory, error)`; `AppendEvent(archmodel.
  ArchEvent) error`; `AppendClaim(KnowledgeClaim) error`; `Rebuild()`;
  `SaveMemory(mem)`; `SaveMemoryAndTimeline(mem)`; `Dir()`.
- WAL: `.glassmarble/memory/events.jsonl`, `claims.jsonl`; aggregate
  `memory.json`, `timeline.json`.
- `QueryMemory(store, query string) *MemoryQueryResult` (top 25);
  `QueryMemoryFromMemory(mem, query, topK)`; `QueryTerms(query) []string`;
  `MemoryQueryResult{Query, Components []ComponentHistory, Claims
  []KnowledgeClaim, Events []archmodel.ArchEvent, Timeline
  []archmodel.TimelineEntry}`.
- `KnowledgeClaim{ID, Subject, SubjectID, Predicate, Object, ObjectID,
  ClaimKind, Evidence evidence.Bundle, State, ValidFrom time.Time,
  ValidUntil *time.Time, FreshnessScore float64}`; ClaimKind = FACT |
  EXPLICIT_REASON | INFERENCE | SPECULATION; KnowledgeState = CURRENT |
  DEPRECATED | REMOVED | HISTORICAL | EXPERIMENTAL | UNKNOWN (consts
  StateActive, StateDeprecated, ...).
- `DeveloperMemory{ProjectID, LastUpdated, TotalEvents, Timeline,
  ComponentMemory map[string]ComponentHistory, GlobalMemory
  []KnowledgeClaim, Events []archmodel.ArchEvent}`.
- archmodel.ArchEvent{ID, Kind EventKind, CommitHash, Timestamp, Title,
  Description, AffectedIDs []string, Components []string, Evidence
  evidence.Bundle, Intent, IntentSrc, Tags []string, RelatedPRs,
  RelatedIssues, ValidFrom, ValidUntil}; EventKind consts e.g.
  archmodel.EventCachingAdded, EventServiceAdded, EventStateChanged;
  `archmodel.StateTag(state)`, `archmodel.StateFromTags`.
- archmodel.TimelineEntry{Timestamp, CommitHash, Version, Title,
  Description, EventKind, Components []string, Intent, Tags}.

### internal/knowledge_aging (knowledge aging)
- `knowledge_aging.FreshenMemoryWithSnapshot(mem *developer_memory.
  DeveloperMemory, snap *archmodel.ArchSnapshot, now time.Time, cfg
  *config.AgingConfig) *developer_memory.DeveloperMemory`;
- `knowledge_aging.Age(mem, now, cfg)` (persists), config.AgingConfig with
  CodeHalfLifeDays etc. (defaults 365/270/180/90); `DetectStaleEntities`.

### internal/learning (convention learning)
- `learning.NewLearnerForRepo(repoDir) *Learner`;
  `learner.Correct(c Correction, mem *developer_memory.DeveloperMemory)
  (Correction, error)`; `learner.List() ([]Correction, error)`;
  `learner.OverlayQuery(res *developer_memory.MemoryQueryResult)
  (*CorrectedResult, error)`; `learner.OverlayMemory(mem) (*DeveloperMemory,
  []AppliedCorrection, error)`; `learner.Undo(id)`;
  `learner.PatternFeedback(mem) (preferred, rejected []string, err)`.
- `Correction{ID, Kind (INTENT|LABEL|STATE|CONFIDENCE|REJECT|ACCEPT),
  TargetType (claim|pattern|component|...), Target string, Value string,
  Reason string, CreatedAt, ...}`. Store: `.glassmarble/memory/
  corrections.jsonl`; `learning.NewStoreForRepo` also used by `gmb memory
  --correct`.

### internal/knowledge_fusion (knowledge fusion)
- `knowledge_fusion.NewFusionStore(dir) *FusionStore`; `Fuse(ctx, verify)`;
  `Put(FusedClaim)`; `Store()`; `Missing(ids...)`; `PurgeStale(olderThan)`.
  File: `<dir>/fusion.json`.

### internal/commit_reasoning (commit reasoning)
- `commit_reasoning.NewReasoner(repoPath) *Reasoner`;
  `ReasonAboutCommit(commit string) (CommitReason, error)` — categories
  BUILD | FIX | RESOURCE | MISC; `CommitMatchesCategory(category, commit)`;
  `ContainsMergedPR(commit)`; `ContainsDeployment(commit)`; `BuildContext()`.

### internal/drift
- `drift.Analyze(graph *akg.CodePropertyGraph, cfg config.DriftConfig)
  (*drift.Report, error)` (verify — check cmd/drift.go:72 usage);
  `drift.DetectCategories(snapshot)`, `TrackEntryTrends(snapshot, memory)`,
  `CompareEntryTrends(prev, cur)`.

### internal/arch_timeline (snapshots)
- `arch_timeline.NewSnapshotStore(dir) (*SnapshotStore, error)` —
  `.glassmarble/snapshots` (index.json + snap_<8hex>.json, content
  addressed, self-healing).
- `store.Create(snap *archmodel.ArchSnapshot) (bool, error)` (false =
  unchanged topology skip), `store.List() []SnapshotIndexEntry`, `store.Get(
  commitHashPrefix)`, `store.GetBySnapshotID(id)`, `store.NearestAt(ts)`,
  `store.Latest()`; `arch_timeline.Diff(base, head) *DiffResult`;
  `arch_timeline.Replay(snap) (*akg.CodePropertyGraph, error)`.
- archmodel.ArchSnapshot{ID, CapturedAt, Components []Component};
  `archmodel.BuildSnapshot(graph arch.Graph, now time.Time) ArchSnapshot`.

### internal/ai_engine (evidence retrieval evidence retrieval + agent)
- `ai_engine.NewRetriever(rootDir string) *Retriever`; `RetrieveForQuestion(
  question string, opts RetrieveOptions) *EvidenceContext`;
  `RetrieveOptions{TopK, MaxTokens, MinConfidence}`;
  `EvidenceContext{Question, Nodes []NodeEvidence, Claims, Timeline,
  Patterns, Smells, Components, MetricSummary, Corrections, TokenCount}`
  with `Empty() bool`, `BuildPrompt() string`, `TrimToBudget(tokens)`,
  `Citations() []string`.
- `ai_engine.New(cfg *aiconfig.Config, rootDir string) (*Engine, error)`;
  `engine.Provider.Complete(ctx, provider.Request{Model, System, Messages,
  Temperature, MaxOutputTokens, OnStream})`; `engine.AskAgent(ctx, query,
  opts agent... )`; `ai_engine.Doctor(cfg, rootDir)`.
- aiconfig: `aiconfig.Load(flagConfig Config) (*Config, error)` (precedence
  flags > GLASSMARBLE_AI_* env > .glassmarble/ai.yaml > ~/.glassmarble/
  ai.yaml > defaults); `aiconfig.Save(path, cfg)` (0600);
  `aiconfig.Default()`; Config fields incl. Provider, Model, APIKey,
  BaseURL, Temperature, MaxTurns, MaxOutputTokens, Stream,
  MaxTotalTokens, MaxCostUSD, MaxSessionMessages.
- provider registry: `provider.Get(name) (Meta, bool)`; providers incl.
  openai, anthropic, gemini, deepseek, mistral, groq, openrouter, glm,
  nvidia, ollama, custom; `provider.NewOpenAICompatProvider(apiKey, baseURL,
  timeout)`.
- agent loop: `agent.Run(ctx, query, history)`; `agent.Event{Type
  ("tool_call"|"tool_result"|"answer"), ...}`; `agent.Result{Text, Turns,
  StoppedReason ("turn_limit"|"token_budget"|"cost_budget"), CostUSD}`.
- tools: `tools.All() []Tool` — names include akg/summary, akg/query,
  code/read, diagram/flowchart, memory/query, memory/claims, system/
  now, timeline/recent...; `tools.Select(all, restrict)`.

### internal/tui/views (renderers — golden output targets)
- `views.RenderInitSuccess(...)`, `views.RenderVersion(version)`,
  `views.RenderStatus(...)`, `views.RenderDoctor(rep)` /
  `RenderDoctorUninitialized()`, `views.RenderDiff(...)`,
  `views.RenderHotspot(...)`, `views.RenderDependencySummary(...)`,
  `views.RenderCompare(diff)`, `views.RenderExportSuccess(...)`,
  `views.RenderImportSuccess(...)`, `views.RenderHooksInstalled(...)`,
  `views.RenderHousekeepingReport(...)`, `views.RenderDrift(rep)`,
  `views.RenderSessions(...)`, `views.RenderModels(...)`,
  `views.RenderAIDoctor(rep, maskedKey)`.

### cmd/ flags to remember
- `analyze`: --dir, --commit, --full, --workers, --link-level
  (architecture|standard|full), --macro-inference, --max-nodes,
  --abort-on-limit, --verbose, --store-code, --json, --bench, --intelligence,
  --include-docs.
- `visualize`: --dir, --entry, --depth (7), --unused, --save, --format
  (mermaid|plantuml|dot), --scope, --output, --summary, --pagerank,
  --community, --scc, --render (.svg/.png via Kroki), --max-nodes,
  --changed-files, --relative, --link-level. Diagram names: list of 31
  (dependency, flowchart, sequence, layer, component, class, package,
  context, container, deployment, c4_*, state, activity, usecase, er,
  timeline, scc, community, pagerank, unused, ...). `visualize list` prints
  the catalog. Exit codes documented in visualize.go header: 0 ok, 1
  validation, 2 entry missing, 3 empty subgraph, 4 render limit.
- `export`: -o (required), -f graphjson|neo4j (or infer from .cypher).
- `import <file.json>`: replaces the graph.
- `compare [base.json head.json]`: 0 args = working-tree snapshots (runs
  full analysis!). 2 args = diff two exported files.
- `snapshot`: --create | --list | --at <ref> | --diff <base> <head> |
  --replay <ref> (mutually exclusive), --diagram, --format, --no-graph,
  --json.
- `memory`: --ask <q> | --component <c> | --correct <v> ... | --corrections
  | (default overview); --kind, --value, --reason, --author, --json.
- `timeline`: --component, --from, --to, --format text|json|mermaid, --full.
- `stats`: --last, --bench, --arch.
- `ai`: --provider, --model, --key, --base-url, --temperature, --max-turns,
  --timeout, --tools, --no-tools, --no-stream, --max-total-tokens,
  --max-cost, --max-session-messages, --save; subcommands ai chat (--session,
  --new), ai sessions (--delete), ai configure (--scope global|project,
  --dir), ai models, ai doctor.
- `why <question>`: no flags; reads aiconfig from CWD project config; hard
  root ".".  Requires ai.yaml present or errors "AI is not configured".
- `hooks install|uninstall`, `housekeeping --prune --older-than N`,
  `dev rebase-goldens --dir --golden-dir`, `completion bash|zsh|fish|
  powershell`, `watch --interval`, `tree --depth`, `inspect --file --line
  | --list | --search | --type | --languages`, `dependency [target]`,
  `hotspot --top`, `status --json`, `doctor --dir`, `diff --dir`,
  `drift --json`, `patterns --smells --json`.

## MockLLM usage

```go
m := harness.NewMockLLM(t)
url := m.Start()
defer m.Close()
sb.SeedAIConfig(url)               // writes .glassmarble/ai.yaml
m.Script(harness.MockResponse{Text: "answer one"})
out, err := harness.RunGmb(t, sb, "ai", "--no-stream", "question here")
// streaming default: m.Script(harness.MockResponse{Text: "..."})
// tool call: MockResponse{ToolCalls: []harness.MockToolCall{{ID:"call_1",
//   Name:"akg/query", Arguments:`{"query":"service"}`}}}
// then a second scripted response with the final answer.
m.LastRequest()  // inspect what the client sent
m.FailNext()     // next request -> HTTP 500
```
