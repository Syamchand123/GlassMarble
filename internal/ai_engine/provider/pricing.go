package provider

// ModelPrice is a rough per-1M-token list price in USD (input/output) used by
// the cost guardrails. Prices change frequently; treat them as estimates and
// rely on the provider-reported usage. Models absent from the table report
// cost 0 (unknown), so a configured cost cap cannot be enforced for them.
type ModelPrice struct {
	InputPerM  float64
	OutputPerM float64
}

// modelPrices maps well-known model identifiers to list prices. Unknown
// models fall back to the last path segment so vendor prefixes (e.g.
// OpenRouter's "openai/gpt-5") still resolve.
var modelPrices = map[string]ModelPrice{
	"gpt-5":                         {InputPerM: 1.25, OutputPerM: 10.00},
	"gpt-5-mini":                    {InputPerM: 0.25, OutputPerM: 2.00},
	"gpt-4o":                        {InputPerM: 2.50, OutputPerM: 10.00},
	"gpt-4o-mini":                   {InputPerM: 0.15, OutputPerM: 0.60},
	"gpt-4.1":                       {InputPerM: 2.00, OutputPerM: 8.00},
	"gpt-4.1-mini":                  {InputPerM: 0.40, OutputPerM: 1.60},
	"o3":                            {InputPerM: 2.00, OutputPerM: 8.00},
	"o3-mini":                       {InputPerM: 1.10, OutputPerM: 4.40},
	"claude-opus-4-1":               {InputPerM: 15.00, OutputPerM: 75.00},
	"claude-sonnet-4-5":             {InputPerM: 3.00, OutputPerM: 15.00},
	"claude-haiku-4-5":              {InputPerM: 1.00, OutputPerM: 5.00},
	"gemini-2.5-pro":                {InputPerM: 1.25, OutputPerM: 10.00},
	"gemini-2.5-flash":              {InputPerM: 0.30, OutputPerM: 2.50},
	"gemini-2.5-flash-lite":         {InputPerM: 0.10, OutputPerM: 0.40},
	"gemini-2.0-flash":              {InputPerM: 0.10, OutputPerM: 0.40},
	"deepseek-chat":                 {InputPerM: 0.27, OutputPerM: 1.10},
	"deepseek-reasoner":             {InputPerM: 0.55, OutputPerM: 2.19},
	"deepseek-ai/deepseek-r1":       {InputPerM: 0.55, OutputPerM: 2.19},
	"mistral-large-latest":          {InputPerM: 2.00, OutputPerM: 6.00},
	"mistral-small-latest":          {InputPerM: 0.10, OutputPerM: 0.30},
	"codestral-latest":              {InputPerM: 0.30, OutputPerM: 0.90},
	"glm-4.6":                       {InputPerM: 0.60, OutputPerM: 2.00},
	"glm-4.5":                       {InputPerM: 0.30, OutputPerM: 1.00},
	"glm-4.5-air":                   {InputPerM: 0.15, OutputPerM: 0.50},
	"llama-3.3-70b-versatile":       {InputPerM: 0.59, OutputPerM: 0.79},
	"llama-3.1-8b-instant":          {InputPerM: 0.05, OutputPerM: 0.08},
	"nvidia/llama-3.3-70b-instruct": {InputPerM: 0.50, OutputPerM: 0.70},
}

// PricingFor returns the estimated list price for a model. An empty result
// with known=false means the model is not in the table.
func PricingFor(model string) (ModelPrice, bool) {
	if p, ok := modelPrices[model]; ok {
		return p, true
	}
	// Vendor-prefixed ids such as "openai/gpt-5" or "deepseek-ai/deepseek-r1".
	for i := len(model) - 1; i >= 0; i-- {
		if model[i] == '/' {
			if p, ok := modelPrices[model[i+1:]]; ok {
				return p, true
			}
			break
		}
	}
	return ModelPrice{}, false
}

// EstimateCost converts a usage report into a USD estimate using the model's
// list price. known=false when the model is unpriced (cost is then 0).
func EstimateCost(model string, usage Usage) (usd float64, known bool) {
	p, known := PricingFor(model)
	if !known {
		return 0, false
	}
	return float64(usage.PromptTokens)/1e6*p.InputPerM +
		float64(usage.CompletionTokens)/1e6*p.OutputPerM, true
}
