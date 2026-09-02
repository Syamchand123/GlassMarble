package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerSamplingTools binds sampling / evidence tools (§3.2).
func (s *Server) registerSamplingTools() {
	if s.shouldRegister("gmb_summarize_evidence", "sampling") || s.shouldRegister("gmb_summarize_evidence", "memory") {
		summarizeTool := mcp.NewTool("gmb_summarize_evidence",
			mcp.WithDescription("Summarize AKG evidence bundles via host LLM sampling (sampling/createMessage) with local fallback. Builds evidence bundle from AKG + Memory and asks host LLM to summarize."),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("Topic or question to summarize evidence for"),
			),
			mcp.WithNumber("max_tokens",
				mcp.Description("Maximum tokens for LLM response (default 1024, max 4096)"),
			),
			mcp.WithString("include",
				mcp.Description("Evidence scope: akg, memory, both (default both)"),
			),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_summarize_evidence",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(false),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(summarizeTool, s.handleSummarizeEvidenceTool)
	}
	if s.shouldRegister("gmb_ask_llm", "sampling") {
		askTool := mcp.NewTool("gmb_ask_llm",
			mcp.WithDescription("Build a structured evidence prompt bundle from AKG graph and developer memory for the host LLM to execute. Returns a bundle suitable for sampling/createMessage or manual LLM invocation. Real logic builds evidence from live AKG + Memory; attempts host sampling if available, otherwise returns prompt bundle."),
			mcp.WithString("prompt",
				mcp.Required(),
				mcp.Description("User prompt / question to build evidence bundle for"),
			),
			mcp.WithString("context",
				mcp.Description("Optional additional context to include in prompt"),
			),
			mcp.WithNumber("max_evidence_items",
				mcp.Description("Maximum evidence items to include (default 10, max 50)"),
			),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:           "gmb_ask_llm",
				ReadOnlyHint:    mcp.ToBoolPtr(true),
				DestructiveHint: mcp.ToBoolPtr(false),
				IdempotentHint:  mcp.ToBoolPtr(false),
				OpenWorldHint:   mcp.ToBoolPtr(false),
			}),
		)
		s.RegisterTool(askTool, s.handleAskLLMTool)
	}
}

// requestSampling attempts to call the host LLM via sampling/createMessage (§3.2).
// Returns the sampled text on success, or an error if sampling is unavailable or fails.
func (s *Server) requestSampling(ctx context.Context, prompt string, maxTokens int) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("empty prompt")
	}
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	if maxTokens > 4096 {
		maxTokens = 4096
	}
	req := mcp.CreateMessageRequest{
		CreateMessageParams: mcp.CreateMessageParams{
			Messages: []mcp.SamplingMessage{
				{Role: mcp.RoleUser, Content: mcp.TextContent{Type: "text", Text: prompt}},
			},
			MaxTokens: maxTokens,
		},
	}
	result, err := s.mcpServer.RequestSampling(ctx, req)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", fmt.Errorf("sampling returned nil result")
	}
	// Result embeds SamplingMessage; content is in result.SamplingMessage.Content (promoted as result.Content).
	content := result.SamplingMessage.Content
	if content == nil {
		content = result.Content
	}
	switch c := content.(type) {
	case mcp.TextContent:
		return c.Text, nil
	case *mcp.TextContent:
		if c != nil {
			return c.Text, nil
		}
	case string:
		return c, nil
	case map[string]any:
		if t, ok := c["text"].(string); ok {
			return t, nil
		}
		b, _ := json.Marshal(c)
		return string(b), nil
	default:
		if c != nil {
			b, _ := json.Marshal(c)
			if len(b) > 0 && string(b) != "null" {
				// Try to extract text field if it's a TextContent marshaled as map
				var tmp map[string]any
				if json.Unmarshal(b, &tmp) == nil {
					if txt, ok := tmp["text"].(string); ok {
						return txt, nil
					}
				}
				return string(b), nil
			}
		}
		return "", fmt.Errorf("unexpected sampling result content type %T", c)
	}
	return "", fmt.Errorf("sampling result had empty content")
}

// buildEvidenceBundle constructs a real evidence bundle from AKG + Memory for a query.
func (s *Server) buildEvidenceBundle(ctx context.Context, query string, maxItems int) map[string]any {
	bundle := map[string]any{
		"query": query,
	}
	if maxItems <= 0 {
		maxItems = 10
	}
	if maxItems > 50 {
		maxItems = 50
	}
	// AKG evidence: snapshot stats
	if graph, err := s.bridge.Snapshot(); err == nil && graph != nil {
		bundle["akg"] = map[string]any{
			"nodes": graph.Nodes.Len(),
			"edges": graph.OutboundEdges.Len(),
			"inbound_mappings": graph.InboundEdges.Len(),
		}
		// Also include storage dir for provenance
		bundle["akg_storage"] = s.bridge.StorageDir()
	} else {
		bundle["akg"] = map[string]any{"error": "AKG unavailable — run 'gmb analyze' first"}
	}
	// Memory evidence
	if store, err := s.bridge.MemoryStore(); err == nil {
		if mem, err := store.LoadMemory(); err == nil && mem != nil {
			bundle["memory"] = map[string]any{
				"total_events":     mem.TotalEvents,
				"total_claims":     len(mem.GlobalMemory),
				"total_components": len(mem.ComponentMemory),
			}
			// Filter claims matching query
			lowerQ := strings.ToLower(query)
			var matchedClaims []map[string]string
			for _, c := range mem.GlobalMemory {
				if strings.Contains(strings.ToLower(c.Subject), lowerQ) ||
					strings.Contains(strings.ToLower(c.Predicate), lowerQ) ||
					strings.Contains(strings.ToLower(c.Object), lowerQ) {
					matchedClaims = append(matchedClaims, map[string]string{
						"subject":   c.Subject,
						"predicate": c.Predicate,
						"object":    c.Object,
						"kind":      string(c.ClaimKind),
					})
					if len(matchedClaims) >= maxItems {
						break
					}
				}
				select {
				case <-ctx.Done():
					break
				default:
				}
			}
			bundle["matched_claims"] = matchedClaims
		} else {
			bundle["memory"] = map[string]any{"error": "memory unavailable"}
		}
	}
	return bundle
}

// handleSummarizeEvidenceTool implements gmb_summarize_evidence (§3.2).
func (s *Server) handleSummarizeEvidenceTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if r, cancelled := checkCancellation(ctx); cancelled {
		return r, nil
	}
	query, err := requireStringArg(req, "query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	maxTokens := getIntArgClamped(req, "max_tokens", 1024, 100, 4096)
	include := getStringArg(req, "include", "both")

	// Build evidence bundle
	bundle := s.buildEvidenceBundle(ctx, query, 10)
	// Enhance bundle with include filter
	if include != "both" && include != "" {
		if include == "akg" {
			delete(bundle, "memory")
			delete(bundle, "matched_claims")
		} else if include == "memory" {
			delete(bundle, "akg")
			delete(bundle, "akg_snapshot")
		}
	}
	bundleJSON, _ := json.MarshalIndent(bundle, "", "  ")
	prompt := fmt.Sprintf("You are GlassMarble Architecture Intelligence. Summarize the following evidence bundle for query %q concisely (3-5 bullet points plus a 1-sentence executive summary). Bundle:\n%s", query, string(bundleJSON))

	// Attempt real sampling via host LLM
	if sampled, err := s.requestSampling(ctx, prompt, maxTokens); err == nil && strings.TrimSpace(sampled) != "" {
		slog.Info("sampling summarize succeeded", "query", query, "tokens", maxTokens)
		result := map[string]any{
			"query":    query,
			"summary":  sampled,
			"source":   "sampling",
			"evidence": bundle,
		}
		out, _ := json.MarshalIndent(result, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	} else {
		if err != nil {
			slog.Debug("sampling unavailable, falling back to local summarization", "query", query, "error", err)
		}
		// Local fallback: deterministic summarization from bundle
		localSummary := localSummarize(bundle, query)
		result := map[string]any{
			"query":    query,
			"summary":  localSummary,
			"source":   "local_fallback",
			"evidence": bundle,
			"note":     "Host LLM sampling not available — returned local deterministic summary. Client may re-execute prompt via sampling/createMessage.",
		}
		out, _ := json.MarshalIndent(result, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}
}

// handleAskLLMTool implements gmb_ask_llm (§3.2 fallback path).
func (s *Server) handleAskLLMTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if r, cancelled := checkCancellation(ctx); cancelled {
		return r, nil
	}
	promptArg, err := requireStringArg(req, "prompt")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	extraCtx := getStringArg(req, "context", "")
	maxItems := getIntArgClamped(req, "max_evidence_items", 10, 1, 50)

	bundle := s.buildEvidenceBundle(ctx, promptArg, maxItems)
	if extraCtx != "" {
		bundle["extra_context"] = extraCtx
	}
	bundleJSON, _ := json.MarshalIndent(bundle, "", "  ")
	structuredPrompt := fmt.Sprintf("User prompt: %q\n\nEvidence bundle (AKG + Memory) — use this as authoritative context:\n%s\n\nInstructions: Answer the user prompt using ONLY the evidence bundle above. If evidence is insufficient, state what is missing. Provide a concise executive summary followed by evidence citations.", promptArg, string(bundleJSON))
	if extraCtx != "" {
		structuredPrompt += fmt.Sprintf("\n\nAdditional context: %s", extraCtx)
	}

	// Attempt sampling first
	if sampled, err := s.requestSampling(ctx, structuredPrompt, 1024); err == nil && strings.TrimSpace(sampled) != "" {
		result := map[string]any{
			"prompt":  promptArg,
			"answer":  sampled,
			"source":  "sampling",
			"bundle":  bundle,
			"evidence_json": string(bundleJSON),
		}
		out, _ := json.MarshalIndent(result, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}
	// Fallback: return structured prompt for client to execute
	fallback := map[string]any{
		"prompt":              promptArg,
		"structured_prompt":   structuredPrompt,
		"evidence_bundle":     bundle,
		"source":              "prompt_bundle",
		"sampling_hint":       "Host LLM sampling not available in this session. Execute the structured_prompt with your LLM (sampling/createMessage) to obtain the answer.",
		"evidence_json":       string(bundleJSON),
		"next_action":         "Call sampling/createMessage with the structured_prompt, or render it to the user.",
	}
	out, _ := json.MarshalIndent(fallback, "", "  ")
	return mcp.NewToolResultText(string(out)), nil
}

// localSummarize provides deterministic fallback summarization when sampling is unavailable.
func localSummarize(bundle map[string]any, query string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Evidence summary for %q:\n", query))
	if akg, ok := bundle["akg"].(map[string]any); ok {
		sb.WriteString(fmt.Sprintf("- AKG: %v nodes / edges sample available\n", akg["nodes"]))
	}
	if mem, ok := bundle["memory"].(map[string]any); ok {
		sb.WriteString(fmt.Sprintf("- Memory: %v claims, %v components\n", mem["total_claims"], mem["total_components"]))
	}
	if claims, ok := bundle["matched_claims"].([]map[string]string); ok && len(claims) > 0 {
		sb.WriteString(fmt.Sprintf("- Matched %d relevant claims for query\n", len(claims)))
		for i, c := range claims {
			if i >= 3 {
				break
			}
			sb.WriteString(fmt.Sprintf("  • %s %s %s (%s)\n", c["subject"], c["predicate"], c["object"], c["kind"]))
		}
	} else if claimsAny, ok := bundle["matched_claims"].([]any); ok && len(claimsAny) > 0 {
		sb.WriteString(fmt.Sprintf("- Matched %d relevant claims\n", len(claimsAny)))
	}
	sb.WriteString("- Note: deterministic local summary — host LLM sampling would provide richer synthesis.")
	return sb.String()
}
