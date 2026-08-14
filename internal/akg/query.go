package akg

import (
	"regexp"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// Query returns all nodes matching the given filter. Filters are AND-ed together.
// Empty/zero fields are ignored. Uses KindIndex for O(1) pre-filtering when Kind is set.
func (c *CodePropertyGraph) Query(filter link.QueryFilter) []*link.ResolvedNode {
	var candidates []*link.ResolvedNode
	if filter.Kind != "" {
		if c.KindIndex != nil {
			if nodeSet, exists := c.KindIndex.Get(filter.Kind); exists {
				for nodeID := range nodeSet {
					if node, ok := c.Nodes.Get(nodeID); ok {
						candidates = append(candidates, node)
					}
				}
			}
		} else {
			// KindIndex is not built (e.g. a graph constructed directly in
			// tests): fall back to a linear scan filtered by Kind so the
			// query still returns correct results (AUDIT Issue 3 Phase 3D-13).
			for _, node := range c.Nodes.Values() {
				if node != nil && node.Kind == filter.Kind {
					candidates = append(candidates, node)
				}
			}
		}
	} else {
		for _, node := range c.Nodes.Values() {
			candidates = append(candidates, node)
		}
	}

	var nameRegex *regexp.Regexp
	if filter.NameRegex != "" {
		var err error
		nameRegex, err = regexp.Compile(filter.NameRegex)
		if err != nil {
			return nil
		}
	}

	propRegexes := make(map[string]*regexp.Regexp)
	for k, pattern := range filter.PropertyRegex {
		re, err := regexp.Compile(pattern)
		if err == nil {
			propRegexes[k] = re
		}
	}

	var results []*link.ResolvedNode
	for _, node := range candidates {
		if !matchFilter(node, filter, nameRegex, propRegexes, c) {
			continue
		}
		results = append(results, node)
	}

	if filter.Offset > 0 && filter.Offset < len(results) {
		results = results[filter.Offset:]
	} else if filter.Offset >= len(results) {
		return nil
	}
	if filter.Limit > 0 && filter.Limit < len(results) {
		results = results[:filter.Limit]
	}

	return results
}

func matchFilter(node *link.ResolvedNode, filter link.QueryFilter, nameRegex *regexp.Regexp, propRegexes map[string]*regexp.Regexp, graph *CodePropertyGraph) bool {
	if node == nil {
		return false
	}

	if filter.NameContains != "" && !strings.Contains(strings.ToLower(node.Name), strings.ToLower(filter.NameContains)) {
		return false
	}

	if nameRegex != nil && !nameRegex.MatchString(node.Name) {
		return false
	}

	if filter.Primitive != "" && node.Primitive != filter.Primitive {
		return false
	}

	for k, v := range filter.Properties {
		if node.Properties == nil {
			return false
		}
		if val, ok := node.Properties[k]; !ok || val != v {
			return false
		}
	}

	for k, re := range propRegexes {
		if node.Properties == nil {
			return false
		}
		if val, ok := node.Properties[k]; !ok || !re.MatchString(val) {
			return false
		}
	}

	if filter.MinEdges > 0 || filter.MaxEdges > 0 {
		totalEdges := len(graph.GetOutboundEdges(node.ID)) + len(graph.GetInboundEdges(node.ID))
		if filter.MinEdges > 0 && totalEdges < filter.MinEdges {
			return false
		}
		if filter.MaxEdges > 0 && totalEdges > filter.MaxEdges {
			return false
		}
	}

	return true
}

// GetNodesByPattern returns all source node IDs that have an outbound edge of the
// given predicate type pointing to the given objectID.
// If objectID is empty, returns all source node IDs with any edge of that type.
func (c *CodePropertyGraph) GetNodesByPattern(predicate link.RelationshipType, objectID string) []string {
	var results []string
	seen := make(map[string]bool)

	c.OutboundEdges.Iterate(func(sourceID string, edges []link.ResolvedEdge) {
		for _, edge := range edges {
			if edge.Type != predicate {
				continue
			}
			if objectID != "" && edge.TargetID != objectID {
				continue
			}
			if !seen[sourceID] {
				seen[sourceID] = true
				results = append(results, sourceID)
			}
		}
	})
	return results
}

// Match parses a simple pattern string and delegates to Query.
// Pattern syntax:
//
//	"kind:STRUCT"          -> filter by Kind
//	"name:Service"         -> filter by NameContains
//	"primitive:DATABASE"   -> filter by Primitive
//	"prop:key=val"         -> filter by Properties key=val
//	Multiple patterns can be space-separated (AND-ed).
func (c *CodePropertyGraph) Match(pattern string) []*link.ResolvedNode {
	filter := link.QueryFilter{}
	parts := strings.Fields(pattern)
	for _, part := range parts {
		switch {
		case strings.HasPrefix(part, "kind:"):
			filter.Kind = strings.TrimPrefix(part, "kind:")
		case strings.HasPrefix(part, "name:"):
			filter.NameContains = strings.TrimPrefix(part, "name:")
		case strings.HasPrefix(part, "primitive:"):
			filter.Primitive = strings.TrimPrefix(part, "primitive:")
		case strings.HasPrefix(part, "prop:"):
			kv := strings.TrimPrefix(part, "prop:")
			if eqIdx := strings.Index(kv, "="); eqIdx > 0 {
				if filter.Properties == nil {
					filter.Properties = make(map[string]string)
				}
				filter.Properties[kv[:eqIdx]] = kv[eqIdx+1:]
			}
		}
	}
	return c.Query(filter)
}

// SafeQuery is a concurrency-safe wrapper that acquires the read lock before querying.
func (c *CodePropertyGraph) SafeQuery(filter link.QueryFilter) []*link.ResolvedNode {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Query(filter)
}

// SafeGetNodesByPattern is a concurrency-safe wrapper.
func (c *CodePropertyGraph) SafeGetNodesByPattern(predicate link.RelationshipType, objectID string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.GetNodesByPattern(predicate, objectID)
}
