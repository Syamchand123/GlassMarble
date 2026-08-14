// Package knowledge_fusion implements knowledge fusion of the GlassMarble V2
// pipeline: Multi-Source Knowledge Fusion (v2_master_implementaion_plan.md
// §7).
//
// WHAT THIS IS:
//
//	Architecture knowledge does not live only in source code. READMEs
//	describe design decisions, ADRs (Architecture Decision Records) explain
//	WHY things are the way they are, and git history ties code changes to
//	PRs and issues. knowledge fusion ingests all of these and fuses them with the
//	AKG (JSON graph) into one provenance-tagged picture.
//
// PIPELINE (FusionEngine.Run):
//
//	discover docs → parse ADRs/READMEs → fetch PRs/issues (adapters)
//	    → build claims → entity-link against the AKG
//	    → resolve conflicts → append new claims to developer memory
//
// NON-NEGOTIABLE PRINCIPLES:
//
//   - Preserve provenance. Every claim keeps its own evidence bundle
//     (source, reference, excerpt, timestamp, confidence). A claim from a
//     PR description is never flattened into a claim from code. Claims are
//     never deleted; the loser of a contradiction is marked HISTORICAL
//     with a ValidUntil.
//   - Never fabricate. ClaimKind labels how each claim was established
//     (FACT for directly-observed changes, EXPLICIT_REASON for human
//     documentation). ValidFrom uses the source's real timestamp (doc
//     mtime, commit author time) — never the analysis run time.
//   - Deterministic. Claim IDs are content hashes, output is sorted, and
//     re-running fusion on the same sources appends nothing: the engine
//     checks the memory claim WAL before appending, so the WAL stays
//     append-only AND bounded.
//   - LLM intensity: Medium by design, and zero LLM calls in this
//     implementation — semantic extraction here is deterministic keyword
//     matching with word boundaries. The LLM is reserved for evidence retrieval and
//     only ever interprets what this phase has grounded.
//
// DEPENDENCY DIRECTION (strict, cycle-free):
//
//	knowledge_fusion imports config (FusionConfig), akg, developer_memory,
//	evidence, git and commit_reasoning (ExtractRelatedRefs only). No
//	package imports knowledge_fusion except cmd/. FusionConfig lives in
//	internal/config (not here) because commit_reasoning already imports
//	config — this type placement is what keeps the import graph acyclic.
//
// PERSISTENCE: fused claims are appended to the developer-memory claims
// WAL (.glassmarble/memory/claims.jsonl) through MemoryStore.AppendClaim
// (only claim IDs not already present), followed by a rebuild of the memory
// aggregate. This makes fused claims immediately queryable via
// `gmb memory --ask` and keeps them in the same dedup/aging pipeline as
// event-derived claims. (Approved deviation from the plan's literal
// .glassmarble/fusion/ layout, which predates the developer memory refactor that
// consolidated all claims into the memory store.)
package knowledge_fusion
