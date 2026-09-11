// Package renderer — engine.go
// Dual-track orchestrator (Track A LLM + Track B Deterministic Fallback).
// Implements Stage 6, coordinates Stage 7 (Quality Firewall) and Stage 8 (Atomic MVCC Write).
package renderer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/grounding"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/invalidator"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/patcher"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/review"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/verifier"
)

// OrchestratorOptions configures the dual-track renderer orchestrator.
type OrchestratorOptions struct {
	NoLLM    bool
	Model    string
	Provider provider.Provider
	Verbose  bool
	Out      io.Writer
	// GlobalStyle is the DocsConfig.Style fallback merged under the
	// per-document style: per-doc Voice/JargonBlacklist win when non-empty.
	GlobalStyle config.StyleSpec
}

// Orchestrator coordinates dual-track rendering, quality gates, and MVCC writes.
type Orchestrator struct {
	opts     OrchestratorOptions
	det      *DeterministicRenderer
	actuator *LLMActuator

	// D5 observability: usage counters accumulated across ProcessDocument
	// calls (guarded for parallel section rendering).
	countsMu   sync.Mutex
	tracksUsed map[string]int
	repairs    int
	fallbacks  int
}

// SnapshotCounters returns D5 observability counters: per-track render
// counts, repair uses, and Track A→B fallbacks. Safe for concurrent use.
func (o *Orchestrator) SnapshotCounters() (tracks map[string]int, repairs, fallbacks int) {
	o.countsMu.Lock()
	defer o.countsMu.Unlock()
	tracks = make(map[string]int, len(o.tracksUsed))
	for k, v := range o.tracksUsed {
		tracks[k] = v
	}
	return tracks, o.repairs, o.fallbacks
}

func (o *Orchestrator) countTrack(track string) {
	o.countsMu.Lock()
	defer o.countsMu.Unlock()
	if o.tracksUsed == nil {
		o.tracksUsed = make(map[string]int)
	}
	o.tracksUsed[track]++
}

func (o *Orchestrator) countRepair() {
	o.countsMu.Lock()
	defer o.countsMu.Unlock()
	o.repairs++
}

func (o *Orchestrator) countFallback() {
	o.countsMu.Lock()
	defer o.countsMu.Unlock()
	o.fallbacks++
}

// NewOrchestrator creates a new Dual-Track Orchestrator.
func NewOrchestrator(opts OrchestratorOptions) *Orchestrator {
	if opts.Out == nil {
		opts.Out = io.Discard
	}

	det := NewDeterministicRenderer()
	var actuator *LLMActuator

	if opts.Provider != nil && !opts.NoLLM {
		actuator = NewLLMActuator(DefaultLLMActuatorConfig(opts.Provider, opts.Model))
	}

	return &Orchestrator{
		opts:     opts,
		det:      det,
		actuator: actuator,
	}
}

// RenderResult captures the outcome of rendering a single section.
type SectionRenderOutcome struct {
	Content      string
	RenderMode   string // "llm" | "deterministic"
	TokensUsed   int
	DurationMs   int64
	RepairUsed   bool
	FallbackUsed bool
	Warning      string
}

// RenderSection executes dual-track rendering with Quality Firewall gates and repair retries.
func (o *Orchestrator) RenderSection(ctx context.Context, fs *config.FactSheet, graph *akg.CodePropertyGraph) (SectionRenderOutcome, error) {
	if fs == nil {
		return SectionRenderOutcome{}, fmt.Errorf("doc_engine/orchestrator: nil FactSheet")
	}

	symIndex := buildSymbolIndex(fs, graph)

	// Determine if we should attempt Track A (LLM)
	useLLM := o.actuator != nil && !o.opts.NoLLM && fs.RenderMode != "deterministic"

	if useLLM {
		outcome, err := o.renderTrackA(ctx, fs, symIndex)
		if err == nil {
			o.countTrack("llm")
			if outcome.RepairUsed {
				o.countRepair()
			}
			return outcome, nil
		}
		// Log warning and fall back to Track B
		warn := fmt.Sprintf("Track A failed for section %q: %v — falling back to deterministic renderer", fs.SectionID, err)
		if o.opts.Verbose {
			fmt.Fprintln(o.opts.Out, "doc_engine: "+warn)
		}

		detOutcome, detErr := o.renderTrackB(fs, symIndex)
		if detErr != nil {
			return SectionRenderOutcome{}, detErr
		}
		detOutcome.FallbackUsed = true
		detOutcome.Warning = warn
		o.countTrack("deterministic")
		o.countFallback()
		if detOutcome.RepairUsed {
			o.countRepair()
		}
		return detOutcome, nil
	}

	// Track B directly
	outcome, err := o.renderTrackB(fs, symIndex)
	if err == nil {
		o.countTrack("deterministic")
	}
	return outcome, err
}

// renderTrackA executes Track A with 1 repair retry on Gate 1/2/3 failure.
func (o *Orchestrator) renderTrackA(ctx context.Context, fs *config.FactSheet, symIndex verifier.AKGSymbolIndex) (SectionRenderOutcome, error) {
	resp, err := o.actuator.Render(ctx, fs)
	if err != nil {
		return SectionRenderOutcome{}, err
	}

	candidate := resp.Text
	gateRes := verifier.RunGates(fs.PriorSectionMarkdown, candidate, symIndex)

	if gateRes.Pass {
		content := candidate
		if gateRes.SemanticNoOp && fs.PriorSectionMarkdown != "" {
			// Zero-git-churn: discard candidate if only cosmetic diff
			content = fs.PriorSectionMarkdown
		}
		return SectionRenderOutcome{
			Content:    content,
			RenderMode: "llm",
			TokensUsed: resp.TotalTokens,
			DurationMs: resp.Duration.Milliseconds(),
		}, nil
	}

	// Gate failure inspection
	if gateRes.FailedGate == 4 {
		// Gate 4: Hard fail on secret pattern — do not retry LLM
		return SectionRenderOutcome{}, fmt.Errorf("gate 4 hard failure: %w", gateRes.Error)
	}

	// Gate 1, 2, or 3: Attempt 1 repair retry
	repairResp, repairErr := o.actuator.Repair(ctx, fs, candidate, gateRes.Error)
	if repairErr != nil {
		return SectionRenderOutcome{}, fmt.Errorf("repair attempt failed: %w", repairErr)
	}

	repairedCandidate := repairResp.Text
	repairGateRes := verifier.RunGates(fs.PriorSectionMarkdown, repairedCandidate, symIndex)

	if repairGateRes.Pass {
		content := repairedCandidate
		if repairGateRes.SemanticNoOp && fs.PriorSectionMarkdown != "" {
			content = fs.PriorSectionMarkdown
		}
		return SectionRenderOutcome{
			Content:    content,
			RenderMode: "llm",
			TokensUsed: resp.TotalTokens + repairResp.TotalTokens,
			DurationMs: (resp.Duration + repairResp.Duration).Milliseconds(),
			RepairUsed: true,
		}, nil
	}

	return SectionRenderOutcome{}, fmt.Errorf("gate %d failed after repair: %w", repairGateRes.FailedGate, repairGateRes.Error)
}

// renderTrackB executes deterministic generation directly.
func (o *Orchestrator) renderTrackB(fs *config.FactSheet, symIndex verifier.AKGSymbolIndex) (SectionRenderOutcome, error) {
	content, err := o.det.RenderSection(fs)
	if err != nil {
		return SectionRenderOutcome{}, fmt.Errorf("deterministic render: %w", err)
	}

	gateRes := verifier.RunGates(fs.PriorSectionMarkdown, content, symIndex)
	if !gateRes.Pass && gateRes.FailedGate == 4 {
		return SectionRenderOutcome{}, fmt.Errorf("deterministic output triggered gate 4 secret scan: %w", gateRes.Error)
	}

	outcome := SectionRenderOutcome{
		Content:    content,
		RenderMode: "deterministic",
		TokensUsed: 0,
		DurationMs: 0,
	}
	if !gateRes.Pass {
		// All 5 gates observe Track B output (plan Stage 7: no exceptions).
		// Deterministic output is grounded by construction, so a non-secret
		// gate failure is reported, not fatal: keep prior content when the
		// failure is churn-only, otherwise ship with a warning.
		if gateRes.SemanticNoOp && fs.PriorSectionMarkdown != "" {
			outcome.Content = fs.PriorSectionMarkdown
		}
		outcome.Warning = fmt.Sprintf("deterministic output failed gate %d (%v); shipped with warning", gateRes.FailedGate, gateRes.Error)
	}
	return outcome, nil
}

// enforceMaxWords verifies the SectionSpec.MaxWords cap post-render.
// Prompt text alone is not enforcement: on violation Track A gets one
// repair retry asking for brevity; otherwise (or on Track B) a warning is
// recorded and the content ships unchanged — truncation would corrupt
// markdown structure.
func (o *Orchestrator) enforceMaxWords(ctx context.Context, fs *config.FactSheet, sec *config.SectionSpec, graph *akg.CodePropertyGraph, outcome SectionRenderOutcome, warnings *[]string) string {
	if sec == nil || sec.MaxWords <= 0 || wordCount(outcome.Content) <= sec.MaxWords {
		return outcome.Content
	}
	if o.actuator != nil && !outcome.RepairUsed && outcome.RenderMode == "llm" {
		repairResp, repairErr := o.actuator.Repair(ctx, fs, outcome.Content,
			fmt.Errorf("section is %d words, exceeding the MaxWords cap of %d: shorten it without losing any factual statements", wordCount(outcome.Content), sec.MaxWords))
		if repairErr == nil {
			repairGateRes := verifier.RunGates(fs.PriorSectionMarkdown, repairResp.Text, buildSymbolIndex(fs, graph))
			if repairGateRes.Pass && wordCount(repairResp.Text) <= sec.MaxWords {
				*warnings = append(*warnings, fmt.Sprintf("section %s shortened to MaxWords cap (%d words)", sec.ID, sec.MaxWords))
				o.countRepair()
				return repairResp.Text
			}
		}
	}
	*warnings = append(*warnings, fmt.Sprintf("section %s exceeds MaxWords cap (%d words > %d max)", sec.ID, wordCount(outcome.Content), sec.MaxWords))
	return outcome.Content
}

func wordCount(s string) int {
	return len(strings.Fields(s))
}

// queueConflictReview records a 3-way merge conflict as a D6 human-review
// item (kind "conflict"). Best-effort: any queue error is swallowed by the
// caller contract — review must never fail a doc run.
func queueConflictReview(repoRoot, docPath, sectionID, base, ours, theirs string) error {
	oursLine, theirsLine := firstDiffLine(base, ours), firstDiffLine(base, theirs)
	_, err := review.Queue(repoRoot, review.ReviewItem{
		Kind:      "conflict",
		DocPath:   docPath,
		SectionID: sectionID,
		Summary:   fmt.Sprintf("merge conflict in %s [%s]: human edits preserved, machine update appended as note", docPath, sectionID),
		Detail:    fmt.Sprintf("first human divergence at line %d; first machine divergence at line %d", oursLine, theirsLine),
	})
	return err
}

func firstDiffLine(base, other string) int {
	bl, ol := strings.Split(base, "\n"), strings.Split(other, "\n")
	for i := 0; i < len(bl) && i < len(ol); i++ {
		if bl[i] != ol[i] {
			return i + 1
		}
	}
	if len(bl) != len(ol) {
		if len(bl) < len(ol) {
			return len(bl) + 1
		}
		return len(ol) + 1
	}
	return 0
}

// ProcessDocument coordinates the full read -> ground -> render -> merge -> write pipeline for a single document.
func (o *Orchestrator) ProcessDocument(
	ctx context.Context,
	repoRoot string,
	doc *config.DocSpec,
	dirtySectionIDs []string,
	graph *akg.CodePropertyGraph,
	dossier *config.GlobalCommitDossier,
	sm *storage.StateManager,
	commitHash string,
) (docUpdated bool, tokensUsed int, warnings []string, err error) {
	if doc == nil {
		return false, 0, nil, fmt.Errorf("doc_engine/orchestrator: nil DocSpec")
	}

	// Apply archetype defaults if needed
	if err := ApplyArchetype(doc); err != nil {
		warnings = append(warnings, fmt.Sprintf("could not apply archetype %q: %v", doc.Archetype, err))
	}

	absTarget := filepath.Join(repoRoot, doc.TargetPath)

	// Read or scaffold target markdown
	rawBytes, readErr := os.ReadFile(absTarget)
	var rawContent string
	if readErr == nil {
		rawContent = string(rawBytes)
	} else {
		// Document file does not exist yet; scaffold initial skeleton
		rawContent = scaffoldDocSkeleton(doc)
	}

	parsedDoc := patcher.ParseMarkdown(rawContent)

	dirtySet := make(map[string]bool)
	for _, id := range dirtySectionIDs {
		dirtySet[id] = true
	}
	// If no specific dirty sections specified, consider all managed sections in doc
	if len(dirtySectionIDs) == 0 {
		for _, s := range doc.Sections {
			if s.Managed {
				dirtySet[s.ID] = true
			}
		}
	}

	docModified := false

	for i := range doc.Sections {
		sec := &doc.Sections[i]
		if !sec.Managed || sec.Freeze {
			continue
		}
		if !dirtySet[sec.ID] {
			continue
		}

		// Check if zone exists in parsed doc
		zone := patcher.ManagedZone(parsedDoc, sec.ID)
		if zone != nil && zone.Kind == patcher.ZoneFrozen {
			continue
		}

		priorBody := ""
		if zone != nil {
			priorBody = patcher.ExtractBody(zone)
		}

		// Stage 4: Grounding
		fs := grounding.AssembleFactSheet(doc, sec, graph, dossier, priorBody, repoRoot)
		if fs == nil {
			continue
		}
		fs.CommitHash = commitHash
		fs.DocPurpose = doc.Purpose
		fs.DocAudience = doc.Audience
		// Global style is the fallback; per-doc style wins on non-empty fields.
		fs.Style = o.opts.GlobalStyle
		if doc.Style != nil {
			if doc.Style.Standard != "" {
				fs.Style.Standard = doc.Style.Standard
			}
			if doc.Style.Voice != "" {
				fs.Style.Voice = doc.Style.Voice
			}
			if doc.Style.Tone != "" {
				fs.Style.Tone = doc.Style.Tone
			}
			if doc.Style.CodeBlockFormat != "" {
				fs.Style.CodeBlockFormat = doc.Style.CodeBlockFormat
			}
			if len(doc.Style.JargonBlacklist) > 0 {
				fs.Style.JargonBlacklist = doc.Style.JargonBlacklist
			}
		}
		// Thread the section's MaxWords cap through to the system prompt.
		fs.MaxWords = sec.MaxWords
		if dossier != nil {
			fs.CommitIntent = dossier.CommitIntent
			fs.CommitReason = dossier.CommitReason
		}
		if zone != nil && zone.Directives != nil {
			if inst, ok := zone.Directives["instruction"]; ok && inst != "" {
				fs.SectionInstruction = inst
			}
		}
		// D2: Diátaxis quadrant guidance. The archetype's quadrant shapes
		// the prose contract (reference/how-to/tutorial/explanation) without
		// touching user content — prompt payload only.
		if q := ArchetypeQuadrant(doc.Archetype); q != "" {
			if qp := QuadrantPrompt(q); qp != "" {
				if fs.SectionInstruction != "" {
					fs.SectionInstruction += "\n\n"
				}
				fs.SectionInstruction += "Documentation quadrant (" + q + "): " + qp
			}
		}

	// Stage 6 & 7: Render + Quality Firewall. Default RenderMode from the
	// orchestrator decision so the contract field is never decorative:
	// deterministic when no LLM is available, llm otherwise.
	if fs.RenderMode == "" {
		if o.actuator != nil && !o.opts.NoLLM {
			fs.RenderMode = "llm"
		} else {
			fs.RenderMode = "deterministic"
		}
	}
	outcome, rErr := o.RenderSection(ctx, fs, graph)
	if rErr != nil {
		warnings = append(warnings, fmt.Sprintf("failed rendering section %s/%s: %v", doc.ID, sec.ID, rErr))
		continue
	}
	tokensUsed += outcome.TokensUsed
	if outcome.Warning != "" {
		warnings = append(warnings, outcome.Warning)
	}
	outcome.Content = o.enforceMaxWords(ctx, fs, sec, graph, outcome, &warnings)

		// P9: living diagram directive — best-effort. After a managed
		// section renders, if the zone directives contain `diagram`
		// (parsed via patcher.ProcessDirectives), generate the diagram
		// markdown via the grounding diagram path and inject it into the
		// section body before merge. On error, warning only.
		if zone != nil && len(zone.Directives) > 0 {
			pd := patcher.ProcessDirectives(zone.Directives)
			if pd.DiagramType != "" {
				ref := config.DiagramRef{Type: pd.DiagramType, Scope: pd.DiagramScope}
				if (strings.EqualFold(ref.Type, "callgraph") || strings.EqualFold(ref.Type, "sequence")) && ref.Entry == "" && len(doc.Scope.EntryPoints) > 0 {
					ref.Entry = doc.Scope.EntryPoints[0]
				}
				if diag, dErr := grounding.GenerateDiagram(ref, graph); dErr != nil {
					warnings = append(warnings, fmt.Sprintf("diagram directive %q for section %s/%s failed: %v", pd.DiagramType, doc.ID, sec.ID, dErr))
				} else if strings.TrimSpace(diag) != "" {
					outcome.Content = strings.TrimRight(outcome.Content, "\n") + "\n\n" + strings.TrimSpace(diag) + "\n"
				}
			}
		}

		// P9: todo population — best-effort. When the managed zone contains
		// a `gmb:todo:` marker, append a deterministic draft line derived
		// from grounding via patcher.PopulateTodos.
		if zone != nil && strings.Contains(zone.Content, "gmb:todo:") {
			outcome.Content = patcher.PopulateTodos(outcome.Content, exportedShortNames(fs))
		}

		// Gate 6: prose quality (plan C1, both tracks). Strict mode fails;
		// non-strict only reports. On strict LLM failure: one repair retry,
		// then deterministic fallback (mirrors the Stage 7 recovery ladder).
		// On strict deterministic failure: warn + ship (tables cannot be
		// re-voiced; failing would discard grounded facts).
		prosePass, proseFailures := verifier.CheckProseGate(outcome.Content, fs.Style, fs.Style.StrictProse)
		if !prosePass {
			proseErr := fmt.Errorf("prose quality gate failed: %s", strings.Join(proseFailures, "; "))
			if outcome.RenderMode == "llm" && o.actuator != nil && !outcome.RepairUsed {
				repairResp, repairErr := o.actuator.Repair(ctx, fs, outcome.Content, proseErr)
				if repairErr == nil {
					symIndex := buildSymbolIndex(fs, graph)
					if rg := verifier.RunGates(fs.PriorSectionMarkdown, repairResp.Text, symIndex); rg.Pass {
						if rp, rf := verifier.CheckProseGate(repairResp.Text, fs.Style, true); rp {
							outcome.Content = repairResp.Text
							outcome.RepairUsed = true
							outcome.TokensUsed += repairResp.TotalTokens
							tokensUsed += repairResp.TotalTokens
							o.countRepair()
							warnings = append(warnings, fmt.Sprintf("section %s/%s prose repaired to strict style", doc.ID, sec.ID))
							goto gate7
						} else {
							_ = rf
						}
					}
				}
				detOutcome, detErr := o.renderTrackB(fs, buildSymbolIndex(fs, graph))
				if detErr != nil {
					warnings = append(warnings, fmt.Sprintf("section %s/%s strict prose failed (%v); skipping update", doc.ID, sec.ID, proseErr))
					continue
				}
				detOutcome.FallbackUsed = true
				detOutcome.Warning = fmt.Sprintf("strict prose failed on LLM output; deterministic fallback used (%v)", proseErr)
				outcome = detOutcome
				tokensUsed += outcome.TokensUsed
			} else {
				warnings = append(warnings, fmt.Sprintf("section %s/%s prose quality: %s", doc.ID, sec.ID, strings.Join(proseFailures, "; ")))
			}
		} else if len(proseFailures) > 0 {
			warnings = append(warnings, fmt.Sprintf("section %s/%s prose suggestions: %s", doc.ID, sec.ID, strings.Join(proseFailures, "; ")))
		}

	gate7:
		// Gate 7: reference integrity (plan C2). Engine-side it reports
		// warnings only — hard failures belong to `doc check` (CI), where
		// missing files and bad anchors fail the gate without blocking
		// generation here.
		{
			var symFn func(string) bool
			if symIndex := buildSymbolIndex(fs, graph); symIndex != nil {
				symFn = func(s string) bool { return symIndex.HasSymbol(s) }
			}
			refRep := verifier.CheckReferences(repoRoot, doc.TargetPath, outcome.Content, symFn, false)
			for _, br := range refRep.Broken {
				warnings = append(warnings, fmt.Sprintf("section %s/%s reference integrity: %s (line %d: %s)", doc.ID, sec.ID, br.Reason, br.Line, br.Target))
			}
		}

		// Stage 8: 3-way merge. BASE is the last rendered body from state
		// (not priorBody): it records what the machine wrote last run, while
		// priorBody (OURS) holds current human edits. Falls back to priorBody
		// when no stored base exists (first run).
		base := priorBody
		if sm != nil {
			if st, lErr := sm.Load(); lErr == nil && st != nil {
				if ds, ok := st.Documents[doc.TargetPath]; ok && ds != nil {
					if ss, ok := ds.Sections[sec.ID]; ok && ss != nil && ss.LastRenderedBody != "" {
						base = ss.LastRenderedBody
					}
				}
			}
		}
		mergeRes := patcher.MergeSection(base, priorBody, outcome.Content)
		if mergeRes.Conflicted {
			patcher.WarnConflict(o.opts.Out, doc.TargetPath, sec.ID)
			// D6 review queue: conflicts are human-decision items, recorded
			// best-effort (never fail the run on queue errors).
			_ = queueConflictReview(repoRoot, doc.TargetPath, sec.ID, base, priorBody, outcome.Content)
		}

		// Apply to in-memory parsed doc
		newFullText := patcher.ApplyToDoc(parsedDoc, sec.ID, mergeRes.Content)
		parsedDoc = patcher.ParseMarkdown(newFullText)
		docModified = true

	// Update section hash in state manager. Recompute the exact Stage-3
	// hash (same inputs FindDirtySections compares) so the next run can
	// hit "clean → 0 tokens". Persisting "" here would defeat zero-churn.
	if sm != nil {
		astHash := invalidator.SectionHash(doc, sec, graph)
		_ = storage.WriteSectionHash(sm, doc.TargetPath, sec.ID, astHash, outcome.RenderMode, commitHash, outcome.TokensUsed, outcome.DurationMs)
		// Persist the merged body as the BASE for the next run's 3-way merge.
		_ = storage.SetLastRenderedBody(sm, doc.TargetPath, sec.ID, mergeRes.Content)
	}
	}

	if !docModified {
		return false, tokensUsed, warnings, nil
	}

	// Reconstruct and write file. absTarget is the filesystem path;
	// doc.TargetPath is the docs_state.json key (relative). They must not
	// be conflated or state lookups miss under a second document entry.
	finalMarkdown := patcher.Reconstruct(parsedDoc.Zones)
	writeRes, wErr := storage.WriteDoc(sm, absTarget, doc.TargetPath, []byte(finalMarkdown), doc.ID, "", commitHash, "managed")
	if wErr != nil {
		return false, tokensUsed, warnings, fmt.Errorf("writing document %s: %w", doc.TargetPath, wErr)
	}

	return writeRes.Changed, tokensUsed, warnings, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

func scaffoldDocSkeleton(doc *config.DocSpec) string {
	var sb strings.Builder
	title := doc.Title
	if title == "" {
		title = doc.ID
	}
	sb.WriteString(fmt.Sprintf("# %s\n\n", title))
	if doc.Purpose != "" {
		sb.WriteString(fmt.Sprintf("%s\n\n", doc.Purpose))
	}

	for _, sec := range doc.Sections {
		sb.WriteString(fmt.Sprintf("## %s\n\n", sec.Title))
		if sec.Managed {
			sb.WriteString(patcher.BuildBeginMarker(sec.ID) + "\n")
			sb.WriteString(patcher.BuildEndMarker(sec.ID) + "\n\n")
		}
	}
	return sb.String()
}

type memorySymbolIndex struct {
	symbols map[string]bool
}

func (m *memorySymbolIndex) HasSymbol(id string) bool {
	if m.symbols[id] {
		return true
	}
	// Also test short name after "::" or "."
	parts := strings.Split(id, "::")
	if len(parts) > 1 && m.symbols[parts[len(parts)-1]] {
		return true
	}
	subparts := strings.Split(id, ".")
	if len(subparts) > 1 && m.symbols[subparts[len(subparts)-1]] {
		return true
	}
	return false
}

// exportedShortNames returns the sorted, deduplicated short names of exported
// symbols known to the FactSheet (used for P9 todo population).
func exportedShortNames(fs *config.FactSheet) []string {
	if fs == nil {
		return nil
	}
	seen := make(map[string]bool)
	var names []string
	add := func(fqn string) {
		short := fqn
		if idx := strings.LastIndex(short, "::"); idx >= 0 {
			short = short[idx+2:]
		} else if idx := strings.LastIndex(short, "."); idx >= 0 {
			short = short[idx+1:]
		}
		short = strings.TrimSpace(short)
		if short == "" || seen[short] {
			return
		}
		if r := []rune(short); len(r) == 0 || !unicode.IsUpper(r[0]) {
			return
		}
		seen[short] = true
		names = append(names, short)
	}
	for _, s := range fs.GroundTruth.Symbols {
		add(s.FQN)
	}
	for _, s := range fs.GroundTruth.AddedSymbols {
		add(s.FQN)
	}
	sort.Strings(names)
	return names
}

func buildSymbolIndex(fs *config.FactSheet, graph *akg.CodePropertyGraph) verifier.AKGSymbolIndex {
	m := make(map[string]bool)

	addSym := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		m[s] = true
		parts := strings.Split(s, "::")
		if len(parts) > 1 {
			m[parts[len(parts)-1]] = true
		}
		subparts := strings.Split(s, ".")
		if len(subparts) > 1 {
			m[subparts[len(subparts)-1]] = true
		}
	}

	if fs != nil {
		for _, s := range fs.GroundTruth.Symbols {
			addSym(s.FQN)
		}
		for _, s := range fs.GroundTruth.AllSymbols {
			addSym(s.FQN)
		}
		for _, s := range fs.GroundTruth.AddedSymbols {
			addSym(s.FQN)
		}
		for _, s := range fs.GroundTruth.ModifiedSymbols {
			addSym(s.FQN)
		}
		for _, s := range fs.GroundTruth.Sentinels {
			addSym(s.FQN)
		}
		for _, s := range fs.GroundTruth.AddedSentinels {
			addSym(s.FQN)
		}
		for _, s := range fs.GroundTruth.ConfigVars {
			addSym(s.Name)
		}
		for _, cf := range fs.GroundTruth.CallFlow {
			addSym(cf)
		}
		for _, c := range fs.GroundTruth.Callers {
			addSym(c)
		}
	}

	if graph != nil && graph.Nodes != nil {
		graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
			addSym(id)
			if n != nil && n.Name != "" {
				addSym(n.Name)
			}
		})
	}

	return &memorySymbolIndex{symbols: m}
}
