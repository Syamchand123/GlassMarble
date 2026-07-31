package provider

import (
	"math"
	"testing"
)

func TestPricingForKnown(t *testing.T) {
	for _, model := range []string{"gpt-4o", "claude-sonnet-4-5", "gemini-2.5-flash", "deepseek-chat"} {
		p, ok := PricingFor(model)
		if !ok {
			t.Errorf("PricingFor(%q) unknown", model)
		}
		if p.InputPerM <= 0 || p.OutputPerM <= 0 {
			t.Errorf("PricingFor(%q) = %+v", model, p)
		}
	}
}

func TestPricingForVendorPrefix(t *testing.T) {
	// OpenRouter-style "openai/gpt-5" resolves via the last path segment.
	p, ok := PricingFor("openai/gpt-5")
	if !ok || p.InputPerM != 1.25 {
		t.Errorf("PricingFor(openai/gpt-5) = %+v, %v", p, ok)
	}
}

func TestPricingForUnknown(t *testing.T) {
	if _, ok := PricingFor("totally-unknown-model"); ok {
		t.Error("unknown model should not resolve")
	}
}

func TestEstimateCost(t *testing.T) {
	usd, known := EstimateCost("gpt-4o", Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000})
	if !known {
		t.Fatal("gpt-4o should be priced")
	}
	if math.Abs(usd-12.50) > 0.001 {
		t.Errorf("cost = %v, want 12.50 (2.50 in + 10.00 out)", usd)
	}

	usd, known = EstimateCost("unknown-model", Usage{PromptTokens: 100, CompletionTokens: 100})
	if known || usd != 0 {
		t.Errorf("unknown model cost = %v, %v", usd, known)
	}
}
