package ai_engine

// This file holds the evidence retrieval prompt templates (master plan §10.3 / §10.5).
// The templates are the ONLY place prompt text is authored: the context
// builder and the CLI reference these constants, so the grounding discipline
// ("answer from evidence only, never invent history") can never drift between
// call paths.

// Evidence section headers. Shared with the agent loop's injected context so
// every grounded prompt speaks the same section vocabulary.
const (
	EvidenceSectionAKG        = "=== ARCHITECTURE KNOWLEDGE ==="
	EvidenceSectionHistory    = "=== KNOWLEDGE CLAIMS ==="
	EvidenceSectionTimeline   = "=== HISTORY & TIMELINE ==="
	EvidenceSectionComponents = "=== DETECTED COMPONENTS ==="
	EvidenceSectionPatterns   = "=== PATTERNS & SMELLS ==="
	EvidenceSectionMetrics    = "=== METRICS ==="
	EvidenceSectionQuestion   = "=== QUESTION ==="
)

// GroundingInstructions is the fixed instruction block appended to every
// grounded prompt. It guides the LLM to synthesize the architectural evidence,
// explain design motivations and tradeoffs, and cite real repository facts.
const GroundingInstructions = `INSTRUCTIONS:
- Answer the architectural question by synthesizing the Architecture Knowledge Graph (AKG), developer memory, timeline history, and codebase facts provided above.
- Explain the architectural purpose, engineering rationale, design tradeoffs, and component relationships clearly and thoroughly.
- When commit messages, pull requests, files, components, or patterns are available in the evidence, cite them directly to support your explanation.
- Distinguish between explicit recorded human decisions (commit messages, PRs, ADRs) and architectural inferences derived from the codebase structure.
- Provide a clear, actionable, and insightful architectural answer based on the real repository architecture.`

// GroundedSystemPrompt extends the base persona with the evidence retrieval evidence
// discipline (master plan §10.5). It is used for `gmb why` and for `gmb ai`
// runs where the deterministic evidence retriever found material.
const GroundedSystemPrompt = `You are GlassMarble AI Architect, an expert systems architect with deep knowledge of this repository's codebase and architecture.

You have access to:
1. Real-time Architecture Knowledge Graph (AKG) tracking all code symbols, dependencies, and calls
2. Developer Memory: historical records of architectural evolution, commits, and design decisions
3. Detected Patterns & Smells: DDD contexts, clean architecture, coupling, and modularity metrics
4. Architecture Timeline: chronological log of architectural changes

Working principles:
- Provide clear, insightful, and comprehensive explanations of "why" components, technologies, dependencies, and changes exist.
- Ground your analysis in the real code structure, package relationships, commit history, and design patterns.
- Cite specific files, structs, functions, components, or commits when explaining architecture.
- Explain the engineering motivations and architectural roles of components within the system.`