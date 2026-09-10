# GlassMarble Documentation Intelligence Engine — Master Improvement Plan

**Version:** 1.0
**Status:** Proposed — Ready for Prioritization
**Scope:** The 20-pillar v1.2.0 engine (8 stages, deterministic core + LLM actuator).
**Target repos:** All 17 GAST languages. **Scale:** single repo → 50k-file monorepo.
**Effort model:** Balanced across deterministic core and LLM track. Phased P0 → P3.

---

## 0. Where the Engine Stands (v1.2.0, verified)

The final audit confirmed the kept scope is fully implemented and functional:
zero-churn loop proven live (re-run → 0 dirty sections, byte-identical),
Appendix B freshness live, branch policy + `--bg` + `--write` enforced,
exit-code contract tested, 5 gates on both tracks, atomic MVCC writes,
per-package unit tests green.

This plan is therefore **not a rescue plan**. It is a reliability,
accuracy, and scale plan: everything below makes a working engine
trustworthy in real repos it has never seen, under adversarial inputs,
at monorepo scale, across 17 languages.

### How to read this document

Each area follows the same shape:

- **Current weakness** — what breaks today, with the mechanism.
- **Proposed improvement** — the concrete change.
- **Why better** — the engineering argument (not hype).
- **Industry precedent** — who proved this works, with sources at the end.
- **Effort** — S (days), M (1–3 weeks), L (month+). **Phase** — P0..P3.

**Phases:** P0 = correctness foundations (do first — bugs hide here).
P1 = grounding accuracy (the 95% core earns its name). P2 = QA at scale
(gates that work on repos you don't control). P3 = intelligence and
feedback loops (compounding value).

---

## P0 — Correctness Foundations

### A1. Replace the custom markdown line-scanner with a real Markdown AST

- **Current weakness:** `patcher/parser.go` is a hand-rolled line scanner.
  Hand scanners mis-handle nested fences, indented code blocks, CRLF edge
  cases (we already fixed one CRLF blindness in snippets — the same bug
  class lives anywhere regexes meet line endings), setext headings, and
  HTML-comment directives inside code spans. Every anchor-zone bug
  destroys human prose, the one thing P4/Human-Sovereignty forbids losing.
- **Proposed improvement:** Parse with **goldmark** (pure Go, CommonMark
  0.31.2-compliant, AST preserves source positions, fuzz-tested upstream,
  stdlib-only dependency) with the GFM extension (tables, strikethrough,
  task lists). Keep the zone model, but implement it as an AST walk:
  HTML-comment directives become locatable nodes; `Reconstruct` becomes a
  renderer over the zone map instead of string splicing. Keep the current
  scanner as a fallback behind a flag for one release.
- **Why better:** A spec-compliant parser converts an unbounded bug class
  (every weird-but-valid markdown file in the wild) into a bounded one.
  Position-preserving AST nodes make anchor isolation exact instead of
  heuristic, and goldmark's own `go test --fuzz` corpus is free QA.
- **Industry precedent:** goldmark powers Hugo (yuin/goldmark); the JS
  ecosystem converged on the same architecture with remark/mdast +
  remark-stringify (parse → transform → serialize), used by ~300k projects
  including Gatsby and Prettier.
- **Effort:** M. **Phase:** P0.

### A2. Delegate 3-way merge to battle-tested merge code

- **Current weakness:** `patcher/merger.go` is a custom LCS line-merge.
  Merge is famously easy to get 95% right and brutal to get 100% right
  (CRLF vs LF, no-trailing-newline files, UTF-8 boundaries, conflicting
  adjacent insertions). A merge bug silently eats human or machine text.
- **Proposed improvement:** Shell out to `git merge-file -p -L base -L ours
  -L theirs` (present wherever git is, which the engine already requires)
  with the custom LCS as offline fallback. Add property-based tests:
  `merge(base, ours, theirs)` must satisfy `merge(x, x, y) == y`,
  `merge(x, y, x) == y` (human-wins), and idempotence on already-merged
  output, plus Go fuzzing (`go test -fuzz`) over random triplets.
- **Why better:** `git merge-file` has 20 years of adversarial exposure;
  matching its conflict markers also means conflicts render correctly in
  every GitHub/GitLab UI. Property tests + fuzzing prove the fallback
  instead of hoping.
- **Industry precedent:** git's `merge-file`/`xdiff` backend; goldmark's
  own fuzz-tested robustness culture.
- **Effort:** S (delegation) + S (property/fuzz tests). **Phase:** P0.

### A3. SQLite WAL state store behind the current JSON interface

- **Current weakness:** `docs_state.json` is a whole-file read-modify-write
  with an `O_EXCL` lock file. Under concurrent `gmb doc` + CI + hook runs,
  lock contention fails the run (fail-open) or, worse, two writers
  interleave Load/Save cycles and lose section hashes — resurrecting
  already-fixed drift. JSON also forces full-file parses on a 50k-file
  monorepo's state.
- **Proposed improvement:** Move persistence to **SQLite in WAL mode**
  behind the existing `StateManager` interface: one row per
  (doc, section), indexed lookups instead of full scans, real transactions
  instead of lock files, concurrent readers for `check`/`status`/`diff`
  while `doc` writes. Keep JSON export (`doc export --format state`) for
  debuggability. Migrate automatically on first run (JSON → SQLite import).
- **Why better:** WAL gives real MVCC (the plan already promises MVCC
  semantics — this is how you actually get them), O(indexed-row) instead
  of O(state-file) access, and eliminates an entire class of lost-update
  races that no lock-file retry loop fully fixes.
- **Industry precedent:** SQLite WAL is the default embedded durability
  story (used by Litestream-backed edge apps, countless CLIs); the
  docs-as-code world converged on "state in a queryable store, JSON only
  for interchange."
- **Effort:** M. **Phase:** P0 (after A1/A2 — it changes the same files).

### A4. Cross-platform crash-safety harness

- **Current weakness:** Atomic-write correctness is asserted by unit tests,
  never by killing the process mid-write. `fsync` semantics differ across
  Linux/macOS/Windows, and `.gmb.bak` self-healing has no recovery test.
- **Proposed improvement:** A crash harness: spawn `doc --write` on a
  fixture, `SIGKILL` at randomized points (pre-write, mid-tmp, post-rename
  pre-state-save), then assert (a) target file is either old or new bytes,
  never torn, (b) rerun converges, (c) `.gmb.bak` restores. Run on all three
  OSes in CI. Replace the lock file with `flock`-style advisory locking
  (e.g. `gofrs/flock`) for correct cross-process semantics.
- **Why better:** Durability you haven't crash-tested is a hope, not a
  property. The harness turns "atomic writes" from a code comment into a
  CI-enforced invariant — exactly what makes engineers trust automation
  with their docs.
- **Industry precedent:** SQLite's own crash testing; filesystem
  durability testing culture (Jepsen-style fault injection, scaled down).
- **Effort:** S–M. **Phase:** P0.

### A5. Fuzz the parser, merger, sanitizer, and snippet extractor

- **Current weakness:** Four components parse untrusted input (repo
  markdown, LLM output, source files, doc snippets). Unit tests cover
  known cases; attackers and weird repos supply unknown ones (ReDoS in
  snippet/permalink regexes, pathological fences, adversarial secrets
  shaped to dodge patterns).
- **Proposed improvement:** Go native fuzzing (`testing.F`) with seed
  corpora from real repos: parser must never panic and must round-trip
  (`Parse → Reconstruct` is byte-identical on valid input); merger must
  satisfy the A2 properties; sanitizer must redact a generated adversarial
  corpus; snippet regex must complete within a time budget (ReDoS guard).
  Run fuzzing nightly, not per-PR.
- **Why better:** Fuzzing finds the inputs humans never think of — the
  exact inputs that cause data loss in production. goldmark itself is
  fuzz-tested; matching that bar is cheap.
- **Industry precedent:** goldmark (`go test --fuzz`), Go stdlib fuzzing
  workflow, ReDoS-aware regex review culture.
- **Effort:** S. **Phase:** P0.

---

## P1 — Grounding Accuracy (earning the "95% deterministic" claim)

### B1. SCIP/LSP-backed symbol resolution (the single biggest accuracy win)

- **Current weakness:** Grounding resolves symbols by AKG node iteration,
  name-substring matching (`isConfigVar`, crypto keyword lists), and Go
  `go/parser` fallbacks. This mis-resolves across the 17 languages:
  re-exports, overloads, method-vs-function collisions, and anything where
  the name lies. Every mis-resolution is a potential hallucination the
  gates must then catch downstream.
- **Proposed improvement:** Layer precision under the existing AKG:
  (1) **SCIP indexes** — consume the open Sourcegraph SCIP protocol
  (protobuf schema, Go bindings available) so `scip-go`,
  `scip-typescript`, `scip-python`, etc. provide compiler-accurate
  definitions/references per language; (2) **LSP spot-queries** (`gopls`
  first) for same-language disambiguation where no SCIP index exists;
  (3) keep tree-sitter/AST extraction as the universal fallback so all 17
  GAST languages keep working with zero setup. Resolution order:
  SCIP → LSP → AST-heuristic, with the source recorded per fact.
- **Why better:** Compiler-accurate references convert grounding from
  "probably the right symbol" to "the symbol the compiler means" —
  definition lookup, find-references, and cross-repo jumps become exact.
  Graceful degradation preserves zero-config behavior on day one while
  precision ratchets up wherever indexes exist.
- **Industry precedent:** SCIP (Sourcegraph, Mozilla Searchfox,
  rust-analyzer, Glean) replaced LSIF as the standard precisely because
  text search returns matches while indexes return meaning; Aider's
  tree-sitter symbol graph is the same insight at a different layer.
- **Effort:** L (SCIP consumer + LSP client + fallback orchestration).
  **Phase:** P1, highest priority in phase.

### B2. PageRank-ranked, budget-aware grounding context (repo map for docs)

- **Current weakness:** FactSheets ground the dirty section's scope only.
  Prose about an interface routinely needs one hop further (the caller
  that motivates a parameter, the type that constrains a field), and today
  that context arrives by luck or not at all — while token budget is spent
  flatly, with no notion of relevance.
- **Proposed improvement:** Build a file→file reference graph from the
  AKG/CPG once per run and run **personalized PageRank** biased toward
  the dirty symbols (mirroring Aider's repomap: files are nodes, symbol
  references are edges, 10x for user-named identifiers, 50x for in-scope
  neighborhoods). Fill the FactSheet's context allowance top-down until
  the per-section token budget is hit (default ~1k tokens of context, a la
  Aider's `--map-tokens`). Emit the ranked map deterministically so
  identical repos produce identical prompts.
- **Why better:** Relevance-ranked context raises prose accuracy per token
  more than any prompt wording change: the model sees the neighborhood
  that explains *why*, not just the signature that states *what* — while
  the hard budget keeps the F6/F10 cost story intact at monorepo scale.
- **Industry precedent:** Aider's tree-sitter + PageRank repomap
  (deterministic, inspectable, proven at hundreds of kLOC).
- **Effort:** M. **Phase:** P1.

### B3. Real arch-event and impact signals (retire the keyword proxies)

- **Current weakness:** `ArchEvents` derive from commit-message keywords
  ("split", "database"), and callgraph grounding follows only outbound
  entry-point edges. Bland messages and inbound-blind callgraphs silently
  degrade P6 cascade gating, P8 decay, P16 triggers, and P33 callers.
- **Proposed improvement:** Compute structural events deterministically:
  component added/removed (package set diff), dependency cycle
  introduced/resolved (tarjan on the module graph — GlassMarble already
  detects smells), layer-violation delta, public-surface delta; feed all
  four into the dossier, cascade gates, freshness base-decay, and ADR
  triggers. Resolve **inbound** CPG edges for callers everywhere
  (`GetInboundEdges` exists — thread it through collector, facts, and
  error-catalog callers uniformly).
- **Why better:** Keyword proxies fail exactly when they matter (terse
  messages on big refactors). Structural events are message-independent,
  making cascade/freshness/ADR behavior predictable and testable.
- **Industry precedent:** ArchUnit-style structural fitness functions;
  Sourcegraph "compiler beats grep" principle applied to change events.
- **Effort:** M. **Phase:** P1.

### B4. Per-language grounding depth matrix (17 languages, honestly graded)

- **Current weakness:** "17 GAST languages" is a claim without a scorecard:
  Go gets AST parsing while other languages get whatever the AKG happens
  to contain. Unknown per-language quality means unknown doc quality.
- **Proposed improvement:** Publish an explicit matrix
  (language × {signatures, doc-comments, call-edges, sentinels/errors,
  concurrency, config, tests}) graded A/B/C per language, generated by a
  fixture corpus (one idiomatic sample file per language with known
  symbols). CI fails if any grade regresses. Drive B1/B2 work by lowest
  grade first.
- **Why better:** You can't improve what you don't measure per language;
  the matrix turns "polyglot" from marketing into a release gate and tells
  contributors exactly where help is needed.
- **Industry precedent:** LlamaIndex `tree-sitter-language-pack`
  per-language support lists; Aider's 130+-language tree-sitter stance
  with graceful degradation.
- **Effort:** S (harness) + ongoing per-language M's. **Phase:** P1.

---

## P2 — QA at Scale (gates that work on repos you don't control)

### C1. In-process prose/style QA: Vale-class rules + cspell + markdownlint

- **Current weakness:** P11 style lives only in the LLM prompt (unenforced
  on Track B, unverified on Track A output), and Gate 1 checks syntax, not
  quality. On a stranger's repo, generated prose drifts in voice,
  terminology, and spelling with no automatic check.
- **Proposed improvement:** Add deterministic Gate 6 (prose quality):
  active-voice, sentence-length, terminology-allowlist, and
  jargon-blacklist rules compiled from `docs.yaml` style into an
  in-process rule engine (Vale's architecture: YAML rules over prose AST,
  stdlib-only implementation — no Vale binary dependency required but Vale
  rule files as the authoring format for familiarity); **cspell**-style
  spell checking with a code-aware dictionary (identifiers split on
  camel/snake boundaries before lookup, project vocabulary learned from
  the repo); **markdownlint**-class structural rules (heading hierarchy,
  fence language tags, list consistency, no trailing whitespace).
  Gate 6 warns by default, fails when `strict_prose: true`.
- **Why better:** Deterministic prose QA works on both tracks, costs zero
  tokens, runs offline, and catches the exact decay (voice drift, typos in
  generated tables, malformed lists) that erodes trust in generated docs.
  It is also the enforcement arm P11 always lacked.
- **Industry precedent:** Vale (Datadog/GitLab/Microsoft/Mozilla; Go
  single binary; reviewdog PR annotations), markdownlint +
  markdownlint-cli2, cspell with custom vocabularies, lychee for links —
  the standard `docs-lint.yml` CI quartet, all blocking on PRs in mature
  setups.
- **Effort:** M (rule engine) + S (each linter family). **Phase:** P2.

### C2. Link, anchor, and frontmatter integrity (lychee-class checking)

- **Current weakness:** No verification that `[text](path#L..)` links
  resolve, that heading anchors match GitHub's slug algorithm, that TOCs
  match headings, or that frontmatter validates against a schema. Generated
  docs link to moved symbols constantly (that is what P7 heals — but
  nothing *checks* the healing worked, including external URLs).
- **Proposed improvement:** Gate 7 (reference integrity): resolve every
  internal link against the healed file set, every heading anchor against
  the GitHub slugger algorithm, every code permalink against the symbol
  table; validate frontmatter against a JSON Schema per
  `target_platform`; check external URLs with lychee-style cached,
  retried, timeout-bounded HEAD requests (offline-skippable). Regenerate
  TOCs deterministically (remark-toc behavior) instead of trusting LLM
  output.
- **Why better:** Broken links are the most visible doc QA failure and
  the easiest to check deterministically. Slug-correct anchors are what
  make permalinks actually clickable on github.com.
- **Industry precedent:** lychee (fast, cached link checking),
  markdown-link-check, remark-toc, frontmatter validators in docs-quality
  CI pipelines.
- **Effort:** S–M. **Phase:** P2.

### C3. Executable snippets: compile AND run, per language (rustdoc model)

- **Current weakness:** P14 verifies syntax + arity + names. A snippet can
  still be semantically wrong (wrong argument order of matching arity,
  stale builder pattern, panics at runtime) — the exact failure users
  punish most ("copied from official docs, doesn't work").
- **Proposed improvement:** Execute snippets in sandboxes with per-language
  runners: Go via `go run` on synthesized `Example`-style mains (extend
  the existing `go test` runnable-Example convention); TypeScript via
  `tsc --noEmit` (compile gate) + `deno run --allow-none` or node-vm
  (run gate); Python via `py_compile` + subprocess with timeout and no
  network; Rust via `rustdoc --test` where applicable. Adopt rustdoc
  semantics: `no_run` (compile only), `should_panic`, hidden setup lines
  (`# `-prefixed, compiled but not shown), merged compilation units for
  speed (rustdoc 2024 merges doctests — compilation dominates runtime).
  `--fix` graduates from signature-rewrite to lead-maintainer-reviewed
  semantic repair proposals.
- **Why better:** Execution is the only check that answers "does the doc
  work." Rust made `cargo test` run doc examples by default and thereby
  made stale examples structurally impossible — the same guarantee is
  available to every language with a compiler.
- **Industry precedent:** rustdoc documentation tests (`cargo test`
  runs them; `no_run`/`should_panic`/`compile_fail`/hidden lines/merged
  compilation); Go runnable Examples; Python doctest.
- **Effort:** L (runners × languages + sandboxing + CI time budget).
  **Phase:** P2 (compile gates first, run gates second).

### C4. Adversarial gate testing (mutation testing for the firewall)

- **Current weakness:** Gates are tested on valid output. Nothing proves
  Gate 3 catches a hallucinated symbol, Gate 4 catches an exfiltrated key,
  or Gate 5 catches a churn-only rewrite — the firewall's entire reason
  to exist is unproven under attack.
- **Proposed improvement:** Mutation harness: inject hallucinated
  identifiers (Gate 3 must fail), AWS keys/JWTs/emails (Gate 4 must
  hard-fail with no retry), paraphrase-only rewrites (Gate 5 must mark
  no-op), broken mermaid (Gate 2 must fail), unclosed fences (Gate 1 must
  fail). Each mutation asserts the exact gate fires and the prescribed
  recovery runs (repair-once → Track B → keep-prior). Run the corpus on
  every gate change; publish the mutation score in CI.
- **Why better:** A firewall with untested alarms is theater. Mutation
  testing converts "5 gates exist" into "5 gates demonstrably fire,"
  which is the claim enterprise security review will actually probe.
- **Industry precedent:** Stryker/mutation-testing culture; RAGAS-style
  adversarial evaluation (noise sensitivity); Swimm's verify-check
  philosophy (fail the PR rather than ship doubt).
- **Effort:** S. **Phase:** P2, immediately after C1.

### C5. Monorepo performance envelope (measure, then buy headroom)

- **Current weakness:** Only fast-bail has a latency budget. AKG loads,
  grounding scans, permalink sweeps, and state saves are O(repo) with no
  measured envelope — a 50k-file monorepo discovers this in production.
- **Proposed improvement:** Benchmark-gated envelope per stage
  (catalog <100ms, grounding <50ms/section, gates <10ms, write <30ms —
  the plan's own §13.4 table, currently 6/7 unmeasured). Then buy
  headroom: persistent **daemon mode** (`gmb doc serve`: loads AKG once,
  watches files via fsnotify/Watchman, debounced priority queue with
  backpressure, singleflight per file); scope-index caching across runs;
  parallel section rendering with a worker pool sized to the token budget.
  CI asserts benchmark ceilings (fail on regression, like a bloat guard).
- **Why better:** Latency budgets are the difference between "runs on my
  laptop" and "runs on the monorepo." A daemon amortizes the dominant
  cost (AKG load) across events instead of paying it per commit.
- **Industry precedent:** Watchman-triggered incremental pipelines;
  CocoIndex's incremental re-embedding (only changed files re-processed);
  benchmark-gated CI (Go benchmark regression checks).
- **Effort:** M (benchmarks + daemon) + S (worker pool). **Phase:** P2.

---

## P3 — Intelligence and Feedback Loops

### D1. Faithfulness evaluation harness (RAGAS-style, deterministic-first)

- **Current weakness:** Prose quality is judged by vibes. Gate 3 checks
  identifiers, but no metric checks *claims*: "retries with backoff"
  could be invented even when every backticked symbol exists.
- **Proposed improvement:** Nightly eval corpus (sample of managed
  sections × recent commits): (1) decompose generated prose into atomic
  claims; (2) verify each claim against the FactSheet + AKG —
  deterministically first (symbol/relation entailment over the CPG; most
  doc claims reduce to checkable relations), LLM-judge only for the
  residue (or a small open NLI classifier in the Vectara HHEM spirit for
  offline use); (3) score faithfulness = supported/total (RAGAS formula),
  answer-relevance = does the section satisfy its `instruction`
  (reverse-check: which instruction would this prose fulfill?). Gate
  releases on thresholds; track trends per archetype.
- **Why better:** Reference-free (no gold docs needed — FactSheet *is*
  the ground truth), so it scales to every section. It measures the exact
  failure users feel (invented facts) instead of its proxy (bad
  identifiers), and it tells you whether a *prompt* change or a
  *grounding* change is at fault.
- **Industry precedent:** RAGAS faithfulness/answer-relevancy/context
  metrics (arxiv:2309.15217) + `FaithfulnesswithHHEM` offline pattern;
  DeepEval/Promptfoo eval-loop culture.
- **Effort:** M. **Phase:** P3 (needs B2 context + stable prompts first).

### D2. Diátaxis-aligned archetypes (quadrant-specific prompts and QA)

- **Current weakness:** 10 archetypes differ in sections but share one
  prose contract. A reference table written like a tutorial (chatty,
  incomplete) and a how-to written like reference (dry, no steps) both
  pass every gate while failing their readers.
- **Proposed improvement:** Map each archetype onto the Diátaxis
  quadrants (tutorial / how-to / reference / explanation) and give each
  quadrant its own system prompt, QA rules, and acceptance tests:
  *reference* must be dry, complete (every exported symbol present —
  mechanically checkable), and mirror the code's structure;
  *tutorials* must be end-to-end executable (checked by C3 runners);
  *how-tos* must be goal-shaped with verifiable outcomes;
  *explanation* must link claims to ADRs/timeline. Add a Diátaxis compass
  check to `doc check`: flag sections whose form contradicts their
  quadrant.
- **Why better:** Diátaxis is the industry's best-tested answer to "what
  makes docs good," and its quadrants are *mechanically distinguishable*
  — which means an engine can enforce them, not just aspire to them.
  Reference-completeness alone (every public symbol documented, checked)
  is a killer enterprise feature.
- **Industry precedent:** Diátaxis (Canonical, Gatsby, Django); its core
  warning applies directly: don't create empty quadrant structures —
  let them form from content, which is exactly what archetype-driven
  scaffolding plus completeness checks does.
- **Effort:** M. **Phase:** P3.

### D3. RAG export 2.0: AST-aware chunks with stable IDs

- **Current weakness:** P37 chunks by section with backtick-heuristic
  symbols. Retrieved chunks can still orphan (a method without its class,
  a table row without its header context) and chunk IDs shift across runs,
  breaking downstream embedding caches.
- **Proposed improvement:** Chunk on the tree-sitter AST (function/class/
  method boundaries, never mid-node — LlamaIndex `CodeSplitter` pattern),
  cap by tokens with method-boundary splits for oversized classes, and
  *enrich every chunk* with file path + parent signature + imports before
  export (enrichment matters more than chunk size for retrieval quality).
  Add hierarchical parent links (section → document, AutoMergingRetriever
  pattern) and content-hash-stable chunk IDs so re-exports don't churn
  vector stores. Keep the existing manifest/jsonl shapes additive.
- **Why better:** Structure-aware, self-explaining chunks with stable IDs
  are the difference between "RAG over our docs kind of works" and
  "Cursor/Copilot answers from our docs reliably" — which is P37's entire
  strategic point.
- **Industry precedent:** LlamaIndex `CodeSplitter` (tree-sitter,
  token/char budgets, hierarchical nodes + `AutoMergingRetriever`);
  LangChain language-aware splitters as the floor; CocoIndex incremental
  tree-sitter chunking.
- **Effort:** M. **Phase:** P3.

### D4. ADR lifecycle (MADR + supersession graph)

- **Current weakness:** P16 writes one-shot ADR files. Real decision logs
  need status transitions (proposed → accepted → deprecated →
  superseded-by), immutable history, an index, and links between decisions
  — otherwise the `docs/adr/` folder rots into contradicted fragments.
- **Proposed improvement:** Adopt the **MADR** template (Context, Decision
  Drivers, Considered Options, Decision Outcome, Consequences — richer
  than Nygard-classic and machine-parseable); auto-maintain `index.md`
  plus a supersession graph (when a new ADR on the same subsystem lands,
  link `superseded-by` and flip the old status — never rewrite history,
  per log4brains' immutability rule); validate transitions in
  `doc check`; emit timeline-menu data for the TUI/web.
- **Why better:** Immutability + explicit supersession is what makes ADRs
  trustworthy over years: old decisions stay true *as history* while the
  live set stays unambiguous. Status validation turns the ADR folder into
  a governed log instead of a pile.
- **Industry precedent:** MADR (Sustainable Architectural Decisions),
  log4brains (immutability, no-numbering to avoid merge conflicts,
  git-log-guessed metadata, timeline UI), adr-tools lineage.
- **Effort:** S–M. **Phase:** P3.

### D5. Observability and cost attribution

- **Current weakness:** Runs emit human text. No structured record of
  per-stage latency, per-section tokens, per-doc cost, gate-fire rates, or
  repair frequency — so regressions, cost spikes, and quality drops are
  discovered by users, not dashboards.
- **Proposed improvement:** Structured JSONL run ledger
  (`.glassmarble/runs/`): per-stage spans with durations (OpenTelemetry
  conventions, no collector required), per-section tokens + model +
  track + gates-fired + repair-used, per-doc cost attribution, plus a
  `doc ledger --summary` rollup. Alert hooks on budget burn rate and gate
  failure spikes. This data also feeds D1 baselines and model-routing
  decisions (route table sections to cheap models, narrative sections to
  strong ones).
- **Why better:** You cannot manage token spend or doc quality without
  measuring them per unit of work. The ledger turns every run into eval
  data and every cost question into a query.
- **Industry precedent:** OpenTelemetry span conventions; LLM-cost
  attribution culture (per-request usage accounting in every provider
  SDK); Aider's token-budget discipline made observable.
- **Effort:** S. **Phase:** P3 (or late P2 — cheap and unlocks D1).

### D6. Human review queue (approve, reject, learn)

- **Current weakness:** The loop is write-only: conflicting merges append
  notes, repairs happen silently, and rejections (human reverts of machine
  text) teach the engine nothing. Trust in automation comes from visible,
  reversible, learning behavior.
- **Proposed improvement:** `doc review` surface (TUI first): pending
  items (merge conflicts, Gate-5 discards above a size threshold, snippet
  fixes, ADR drafts) with approve/reject; rejections recorded with reason
  codes into the run ledger; monthly prompt/style tuning driven by
  rejection clusters (Swimm's lesson: prefer asking over strange
  suggestions; make review cheap and the engine earns auto-approve).
  Long-term: per-repo acceptance model that tightens budgets where humans
  keep reverting.
- **Why better:** Swimm's core product insight — auto-sync what is safe,
  ask for the rest, and make asking frictionless — is the trust model that
  gets documentation automation *kept* instead of disabled. Rejection data
  is the highest-signal prompt-improvement fuel available.
- **Industry precedent:** Swimm's auto-sync vs needs-review triage
  (up-to-date / out-of-sync / outdated) with one-click approve-and-commit;
  GitHub's suggested-changes review flow.
- **Effort:** M. **Phase:** P3.

---

## Roadmap Summary

| Phase | Areas | Theme | Exit signal |
|---|---|---|---|
| **P0** | A1 goldmark AST · A2 merge delegation + property/fuzz tests · A3 SQLite WAL state · A4 crash harness + flock · A5 fuzzing | Correctness foundations | Zero data-loss-class bugs; crash harness green on 3 OSes; state races gone |
| **P1** | B1 SCIP/LSP resolution · B2 PageRank context budgets · B3 structural events + inbound edges · B4 language depth matrix | Grounding accuracy | Per-language grades published; context relevance measured; keyword proxies retired |
| **P2** | C1 prose/style QA gates · C2 link/anchor/frontmatter integrity · C3 executable snippets · C4 adversarial gate mutation · C5 monorepo envelope + daemon | QA at scale | Gates fire provably; snippets execute; 50k-file envelope benchmarked |
| **P3** | D1 faithfulness eval · D2 Diátaxis quadrants · D3 RAG 2.0 chunks · D4 ADR lifecycle · D5 observability ledger · D6 review queue | Compounding intelligence | Release-gated eval scores; quadrant-checked docs; stable chunk IDs; cost attribution |

Suggested sequencing within phases follows dependency order (A1→A2→A3;
B1→B2; C1→C4; everything→D1/D5 as consumers). Total rough envelope:
P0 ~5–7 weeks, P1 ~7–10 weeks, P2 ~8–11 weeks, P3 ~8–12 weeks for a
small focused team; individual areas are independently shippable.

## Explicit Non-Goals (previously descoped — stay out)

Cross-repo federation, SBOM/license compliance, threat modeling, i18n
inventory, versioned doc portals, domain glossaries, test-topology docs,
concurrency contracts, API-boundary enforcement, debt analytics,
polyglot call-tracing, time-machine narratives, AI gap telemetry.
Revisit only with dedicated ownership — each is a product unto itself.

## KPI Dashboard (what "reliable" means, numerically)

- **Zero-churn rate:** % of no-change runs producing empty diffs (target: 100%).
- **Faithfulness (D1):** supported-claims ratio per archetype (target: ≥0.95 reference, ≥0.90 others).
- **Gate mutation score (C4):** % of injected faults caught (target: 100% Gate 4, ≥95% others).
- **Snippet pass rate (C3):** % executable snippets green (target: 100% blocking).
- **Freshness lag:** median commits from code-change to doc-sync (target: ≤1).
- **p99 run latency** at 50k files (target: budgeted per §13.4 × repo factor).
- **Human revert rate (D6):** % machine-written lines reverted within 30 days (target: trending down).

## Sources & Further Reading

- Aider repo maps: tree-sitter + PageRank context selection —
  aider.chat/docs/repomap.html; aider.chat/2023/10/22/repomap.html;
  anishgandhi.com (PageRank tour, 2026).
- Diátaxis documentation framework — diataxis.fr (map, compass, workflow,
  quality).
- goldmark (yuin/goldmark) — CommonMark-compliant, position-preserving,
  fuzz-tested Go Markdown AST; remark/unified/mdast ecosystem
  (remark.js.org, unifiedjs/handbook).
- Rust documentation tests — doc.rust-lang.org/rustdoc (executable
  examples, `no_run`/`should_panic`/hidden lines/merged doctests);
  `cargo test` runs doc-tests by default.
- Vale (docs.vale.sh; errata-ai), markdownlint + markdownlint-cli2,
  cspell, lychee — the docs-as-code QA quartet; example enforced
  `docs-lint.yml` pipelines ( ambient-code, intentional-cognition-os ).
- RAGAS (arxiv:2309.15217; docs.ragas.io) — faithfulness, answer
  relevancy, context precision/recall; `FaithfulnesswithHHEM` offline
  judging (Vectara HHEM-2.1-Open).
- SCIP (scip-code.org; sourcegraph docs) — language-agnostic precise
  code intelligence (scip-go, scip-typescript, scip-python, …),
  successor to LSIF.
- Swimm auto-sync & verify (swimm.io/blog) — signal-histogram
  auto-sync vs needs-review triage; approve-and-commit; verify-only
  changed files; docs-in-repo Markdown.
- LlamaIndex CodeSplitter (tree-sitter AST chunking, token budgets,
  hierarchical nodes, AutoMergingRetriever); AST-vs-fixed-size analysis
  (dreaming.press, 2026): enrich chunks with path/parent/imports.
- Mintlify + Stainless + Fern — OpenAPI-driven reference docs with
  generated SDK examples and AI-filled samples (mintlify.com,
  stainless.com, fern-api/docs).
- log4brains / MADR / adr-tools lineage — immutable ADRs, supersession,
  no-numbering filenames, timeline UI, metadata from git history.
