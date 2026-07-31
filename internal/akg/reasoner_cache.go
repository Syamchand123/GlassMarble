package akg

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

// RuleDefinition defines a single macro-inference rule.
type RuleDefinition struct {
	ID      string
	Name    string
	Tier    string
	Enabled func(node *stage4.ResolvedNode, graph *CodePropertyGraph, disabledRules map[string]bool, primitivesFound map[string]bool, flags ruleFlags) bool
	Apply   func(node *stage4.ResolvedNode, graph *CodePropertyGraph, primitivesFound map[string]bool, flags ruleFlags) string
}

type ruleFlags struct {
	hasSecurityGate         bool
	hasAsyncProcessing      bool
	hasContextPass          bool
	hasEventPubSub          bool
	hasDependencyInjection  bool
	hasHeapEscape           bool
	hasFFI                  bool
	hasConstraint           bool
}

// RuleRegistry contains all 28 heuristic/structural macro-inference rules.
var RuleRegistry []RuleDefinition

func init() {
	RuleRegistry = []RuleDefinition{
		{
			ID: "rule_01", Name: "Web-to-Storage Traffic", Tier: RuleTierStructural,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_01"] { return false }
				lowerName := strings.ToLower(n.Name)
				return (pf["NETWORK_IO"] || strings.Contains(lowerName, "router") || strings.Contains(lowerName, "api") || strings.Contains(lowerName, "http")) &&
					(pf["DISK_IO"] || pf["DATABASE"])
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s handles Web-to-Storage traffic [%s]", n.Name, RuleTierStructural)
			},
		},
		{
			ID: "rule_02", Name: "Security Gate Audit", Tier: RuleTierStructural,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_02"] { return false }
				return f.hasSecurityGate && (pf["DISK_IO"] || pf["DATABASE"])
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s enforces Security Validation before Storage Persistence [%s]", n.Name, RuleTierStructural)
			},
		},
		{
			ID: "rule_03", Name: "Async Background Tasks", Tier: RuleTierStructural,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_03"] { return false }
				return f.hasAsyncProcessing
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s executes Asynchronous Background Processing [%s]", n.Name, RuleTierStructural)
			},
		},
		{
			ID: "rule_04", Name: "External API Integrator", Tier: RuleTierStructural,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_04"] { return false }
				return pf["NETWORK_IO"]
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s communicates over Remote Network Protocols [%s]", n.Name, RuleTierStructural)
			},
		},
		{
			ID: "rule_05", Name: "Repository Pattern", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_05"] { return false }
				lowerName := strings.ToLower(n.Name)
				return (strings.Contains(lowerName, "repository") || strings.Contains(lowerName, "repo") || strings.Contains(lowerName, "store")) && pf["DATABASE"]
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s implements the Repository Data Access Pattern [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_06", Name: "Service Layer", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_06"] { return false }
				lowerName := strings.ToLower(n.Name)
				return strings.Contains(lowerName, "service") || strings.Contains(lowerName, "usecase")
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s functions as a Business Logic Service [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_07", Name: "Controller / Handler Layer", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_07"] { return false }
				lowerName := strings.ToLower(n.Name)
				return strings.Contains(lowerName, "controller") || strings.Contains(lowerName, "handler") || strings.Contains(lowerName, "endpoint")
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s serves as an Inbound API Controller [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_08", Name: "Gateway Pattern", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_08"] { return false }
				lowerName := strings.ToLower(n.Name)
				return strings.Contains(lowerName, "gateway") || strings.Contains(lowerName, "client")
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s acts as an Integration Gateway [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_09", Name: "Event Publisher / Producer", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_09"] { return false }
				lowerName := strings.ToLower(n.Name)
				return strings.Contains(lowerName, "publisher") || strings.Contains(lowerName, "producer") || strings.Contains(lowerName, "emitter") || f.hasEventPubSub
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s publishes Domain Events to Queue/Broker [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_10", Name: "Event Consumer / Subscriber", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_10"] { return false }
				lowerName := strings.ToLower(n.Name)
				return strings.Contains(lowerName, "consumer") || strings.Contains(lowerName, "subscriber") || strings.Contains(lowerName, "listener")
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s consumes Asynchronous Message Queue Events [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_11", Name: "CQRS Command Handler", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_11"] { return false }
				lowerName := strings.ToLower(n.Name)
				return strings.Contains(lowerName, "command") || strings.Contains(lowerName, "mutation")
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s handles State Mutation Commands (CQRS) [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_12", Name: "CQRS Query Handler", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_12"] { return false }
				lowerName := strings.ToLower(n.Name)
				return strings.Contains(lowerName, "query") || strings.Contains(lowerName, "reader")
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s handles Read-Only Query Projections (CQRS) [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_13", Name: "Cache Layer", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_13"] { return false }
				lowerName := strings.ToLower(n.Name)
				return strings.Contains(lowerName, "cache") || strings.Contains(lowerName, "redis") || strings.Contains(lowerName, "memcached")
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s acts as an In-Memory Cache Tier [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_14", Name: "Authentication / Authorization Middleware", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_14"] { return false }
				lowerName := strings.ToLower(n.Name)
				return strings.Contains(lowerName, "auth") || strings.Contains(lowerName, "jwt") || strings.Contains(lowerName, "token")
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s executes Identity Authentication/Authorization [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_15", Name: "Input Validation Gate", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_15"] { return false }
				lowerName := strings.ToLower(n.Name)
				return strings.Contains(lowerName, "validator") || strings.Contains(lowerName, "sanitizer")
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s validates and sanitizes incoming payload data [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_16", Name: "Secret Manager Access", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_16"] { return false }
				lowerName := strings.ToLower(n.Name)
				return strings.Contains(lowerName, "secret") || strings.Contains(lowerName, "vault") || strings.Contains(lowerName, "keyvault")
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s interacts with Key/Secret Management Services [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_17", Name: "Metrics Emitter", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_17"] { return false }
				lowerName := strings.ToLower(n.Name)
				return strings.Contains(lowerName, "metric") || strings.Contains(lowerName, "prometheus") || strings.Contains(lowerName, "statsd")
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s emits Telemetry Metrics [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_18", Name: "Distributed Tracer", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_18"] { return false }
				lowerName := strings.ToLower(n.Name)
				return strings.Contains(lowerName, "trace") || strings.Contains(lowerName, "opentelemetry") || strings.Contains(lowerName, "jaeger")
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s participates in Distributed Tracing Spans [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_19", Name: "Structured Logger", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_19"] { return false }
				lowerName := strings.ToLower(n.Name)
				return strings.Contains(lowerName, "logger") || strings.Contains(lowerName, "log")
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s emits Structured Audit Logs [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_20", Name: "Circuit Breaker / Resilience", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_20"] { return false }
				lowerName := strings.ToLower(n.Name)
				return strings.Contains(lowerName, "circuit") || strings.Contains(lowerName, "retry") || strings.Contains(lowerName, "fallback")
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s implements Resilience Fault-Tolerance Mechanisms [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_21", Name: "Cache-Aside Pattern", Tier: RuleTierStructural,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_21"] { return false }
				lowerName := strings.ToLower(n.Name)
				return (strings.Contains(lowerName, "cache") || pf["CACHE"]) && pf["DATABASE"]
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s implements the Cache-Aside Pattern [%s]", n.Name, RuleTierStructural)
			},
		},
		{
			ID: "rule_22", Name: "Saga Orchestrator", Tier: RuleTierHeuristic,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_22"] { return false }
				lowerName := strings.ToLower(n.Name)
				return strings.Contains(lowerName, "saga") || strings.Contains(lowerName, "orchestrator")
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s acts as a Saga Orchestrator for Distributed Transactions [%s]", n.Name, RuleTierHeuristic)
			},
		},
		{
			ID: "rule_23", Name: "Event-Driven Architecture (Pub/Sub)", Tier: RuleTierStructural,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_23"] { return false }
				return f.hasEventPubSub
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s participates in Event-Driven Pub/Sub architecture [%s]", n.Name, RuleTierStructural)
			},
		},
		{
			ID: "rule_24", Name: "Dependency Injection", Tier: RuleTierStructural,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_24"] { return false }
				return f.hasDependencyInjection || pf["DI_CONTAINER"]
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s utilizes Dependency Injection [%s]", n.Name, RuleTierStructural)
			},
		},
		{
			ID: "rule_25", Name: "Context Cancellation/Propagation", Tier: RuleTierStructural,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_25"] { return false }
				return f.hasContextPass
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s handles Context Propagation (Timeouts/Cancellation) [%s]", n.Name, RuleTierStructural)
			},
		},
		{
			ID: "rule_26", Name: "Memory-Intensive / Escape Analysis", Tier: RuleTierStructural,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_26"] { return false }
				return f.hasHeapEscape
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s performs Memory Allocations that Escape to the Heap [%s]", n.Name, RuleTierStructural)
			},
		},
		{
			ID: "rule_27", Name: "FFI / CGO Bridge", Tier: RuleTierStructural,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_27"] { return false }
				return f.hasFFI
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s interfaces with Native/C code via FFI [%s]", n.Name, RuleTierStructural)
			},
		},
		{
			ID: "rule_28", Name: "Security / Bounds Checking", Tier: RuleTierStructural,
			Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool, pf map[string]bool, f ruleFlags) bool {
				if dr["rule_28"] { return false }
				return f.hasConstraint
			},
			Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph, pf map[string]bool, f ruleFlags) string {
				return fmt.Sprintf("Component %s enforces Branch Constraints for Security/Bounds Checking [%s]", n.Name, RuleTierStructural)
			},
		},
	}
}

// nodeMacroKey computes a content-addressable hash for a node's macro-inference inputs.
func nodeMacroKey(node *stage4.ResolvedNode, graph *CodePropertyGraph) string {
	h := sha256.New()
	h.Write([]byte(node.ID))
	h.Write([]byte(node.Kind))
	h.Write([]byte(node.Name))
	h.Write([]byte(node.Primitive))
	for _, e := range graph.GetOutboundEdges(node.ID) {
		h.Write([]byte(e.TargetID))
		h.Write([]byte(e.Type))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
