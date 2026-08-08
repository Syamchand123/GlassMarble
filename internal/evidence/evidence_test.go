// Package evidence_test provides unit tests for the evidence package.
//
// WHY TESTS HERE:
//
//	The evidence package is foundational. Every V2 knowledge claim depends on it.
//	If aggregation math or source reliability weights are wrong, all downstream
//	confidence scores will be wrong. These tests lock down the invariants:
//	  1. Empty bundles are detectable (IsEmpty = true).
//	  2. AggConfidence is correctly computed as a weighted minimum.
//	  3. PrimarySource is the highest-reliability source in the bundle.
//	  4. Excerpts are clamped to 512 chars.
package evidence_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

func TestBundle_EmptyBundle(t *testing.T) {
	b := evidence.Bundle{}
	if !b.IsEmpty() {
		t.Error("expected empty bundle to report IsEmpty() = true")
	}
	if agg := b.Aggregate(); agg != 0.0 {
		t.Errorf("expected AggConfidence=0.0 for empty bundle, got %f", agg)
	}
}

func TestBundle_SingleItem(t *testing.T) {
	b := evidence.Bundle{}
	b.Add(evidence.EvidenceItem{
		Source:     evidence.SourceCode,
		Reference:  "internal/akg/mvcc.go:42",
		Excerpt:    "func (g *CodePropertyGraph) DetectCycles()",
		Confidence: 0.95,
		Timestamp:  time.Now(),
	})

	if b.IsEmpty() {
		t.Error("expected non-empty bundle after Add()")
	}
	if b.PrimarySource != evidence.SourceCode {
		t.Errorf("expected PrimarySource=SourceCode, got %s", b.PrimarySource)
	}
	// Weighted min = 0.95 * SourceReliability(SourceCode=1.0) = 0.95
	expected := 0.95 * evidence.SourceReliability(evidence.SourceCode)
	if b.AggConfidence != expected {
		t.Errorf("expected AggConfidence=%f, got %f", expected, b.AggConfidence)
	}
}

func TestBundle_MultipleItems_WeightedMin(t *testing.T) {
	b := evidence.Bundle{}

	// High-confidence code evidence.
	b.Add(evidence.EvidenceItem{
		Source:     evidence.SourceCode,
		Reference:  "internal/akg/mvcc.go",
		Confidence: 0.9,
		Timestamp:  time.Now(),
	})
	// Lower-confidence LLM inference — should drag down aggregate.
	b.Add(evidence.EvidenceItem{
		Source:     evidence.SourceLLM,
		Reference:  "llm-session-2026-08",
		Confidence: 0.6,
		Timestamp:  time.Now(),
	})

	// LLM item: 0.6 * 0.65 = 0.39
	// Code item: 0.9 * 1.0  = 0.90
	// Weighted min = 0.39
	expectedMin := 0.6 * evidence.SourceReliability(evidence.SourceLLM)
	if b.AggConfidence != expectedMin {
		t.Errorf("expected weighted-min AggConfidence=%f, got %f", expectedMin, b.AggConfidence)
	}

	// PrimarySource should be SourceCode (highest reliability).
	if b.PrimarySource != evidence.SourceCode {
		t.Errorf("expected PrimarySource=SourceCode, got %s", b.PrimarySource)
	}
}

func TestBundle_ExcerptClamped(t *testing.T) {
	longExcerpt := strings.Repeat("x", 600)
	b := evidence.Bundle{}
	b.Add(evidence.EvidenceItem{
		Source:     evidence.SourceGit,
		Reference:  "abc1234",
		Excerpt:    longExcerpt,
		Confidence: 0.8,
		Timestamp:  time.Now(),
	})
	if len(b.Items[0].Excerpt) > 512 {
		t.Errorf("expected excerpt clamped to 512 chars, got %d", len(b.Items[0].Excerpt))
	}
}

func TestNewBundle(t *testing.T) {
	item := evidence.EvidenceItem{
		Source:     evidence.SourceRule,
		Reference:  "rule:PR-01",
		Confidence: 0.85,
		Timestamp:  time.Now(),
	}
	b := evidence.NewBundle(item)
	if b.IsEmpty() {
		t.Error("NewBundle should produce a non-empty bundle")
	}
	if b.PrimarySource != evidence.SourceRule {
		t.Errorf("expected PrimarySource=SourceRule, got %s", b.PrimarySource)
	}
}

func TestSourceReliability_Order(t *testing.T) {
	// Enforce the documented reliability order.
	order := []evidence.Source{
		evidence.SourceCode,
		evidence.SourceUser,
		evidence.SourceDocs,
		evidence.SourcePR,
		evidence.SourceGit,
		evidence.SourceRule,
		evidence.SourceHeuristic,
		evidence.SourceLLM,
	}
	for i := 1; i < len(order); i++ {
		prev := evidence.SourceReliability(order[i-1])
		curr := evidence.SourceReliability(order[i])
		if prev < curr {
			t.Errorf("reliability order violated: %s (%f) should be >= %s (%f)",
				order[i-1], prev, order[i], curr)
		}
	}
}
