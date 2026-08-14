package developer_memory

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// DefaultTopK is the per-section result cap used by QueryMemory.
const DefaultTopK = 25

// MemoryQueryResult holds the ranked results of a memory query. Every item
// is deterministic — this layer never invokes an LLM.
type MemoryQueryResult struct {
	Query string `json:"query"`

	// Components matched by name, ranked by match quality × state weight.
	Components []ComponentHistory `json:"components"`

	// Claims matched on subject/object/predicate, ranked by
	// match × confidence × freshness.
	Claims []KnowledgeClaim `json:"claims"`

	// Events matched on components/title/description/tags, ranked by
	// match × evidence confidence.
	Events []archmodel.ArchEvent `json:"events"`

	// Timeline entries relevant to the query, deduplicated and ordered
	// most-recent first.
	Timeline []archmodel.TimelineEntry `json:"timeline"`
}

// QueryMemory answers questions like "what do we know about Redis?" against
// the store, returning the top DefaultTopK ranked items per section. This is
// a deterministic query — no LLM involved (master plan §4.4).
func QueryMemory(store *MemoryStore, query string) *MemoryQueryResult {
	if store == nil {
		return &MemoryQueryResult{Query: query}
	}
	mem, err := store.LoadMemory()
	if err != nil || mem == nil {
		return &MemoryQueryResult{Query: query}
	}
	return QueryMemoryFromMemory(mem, query, DefaultTopK)
}

// QueryMemoryFromMemory runs the query against an in-memory aggregate with an
// explicit result cap. Used by callers that already hold the memory (e.g.
// evidence retrieval evidence retrieval) and by tests.
func QueryMemoryFromMemory(mem *DeveloperMemory, query string, topK int) *MemoryQueryResult {
	result := &MemoryQueryResult{Query: query}
	if mem == nil || topK <= 0 {
		return result
	}

	tokens := tokenize(query)
	if len(tokens) == 0 {
		return result
	}

	type scoredComponent struct {
		history ComponentHistory
		score   float64
	}
	components := make([]scoredComponent, 0, len(mem.ComponentMemory))
	for _, history := range mem.ComponentMemory {
		m := matchScore(history.Name, tokens)
		if m == 0 {
			continue
		}
		components = append(components, scoredComponent{
			history: history,
			score:   m * stateWeight(history.State),
		})
	}
	sort.SliceStable(components, func(i, j int) bool { return components[i].score > components[j].score })
	for _, c := range components {
		if len(result.Components) >= topK {
			break
		}
		result.Components = append(result.Components, c.history)
	}

	type scoredClaim struct {
		claim KnowledgeClaim
		score float64
	}
	claims := make([]scoredClaim, 0, len(mem.GlobalMemory))
	for _, claim := range mem.GlobalMemory {
		text := strings.Join([]string{claim.Subject, claim.Predicate, claim.Object}, " ")
		m := matchScore(text, tokens)
		if m == 0 {
			continue
		}
		claims = append(claims, scoredClaim{
			claim: claim,
			score: m * claimConfidence(claim) * claimFreshness(claim),
		})
	}
	sort.SliceStable(claims, func(i, j int) bool {
		if claims[i].score != claims[j].score {
			return claims[i].score > claims[j].score
		}
		return claims[i].claim.ID < claims[j].claim.ID
	})
	for _, c := range claims {
		if len(result.Claims) >= topK {
			break
		}
		result.Claims = append(result.Claims, c.claim)
	}

	type scoredEvent struct {
		event archmodel.ArchEvent
		score float64
	}
	events := make([]scoredEvent, 0, len(mem.Events))
	for _, ev := range mem.Events {
		text := strings.ToLower(strings.Join(ev.Components, " ") + " " + ev.Title + " " +
			ev.Description + " " + ev.Intent + " " + strings.Join(ev.Tags, " "))
		m := matchScore(text, tokens)
		if m == 0 {
			continue
		}
		events = append(events, scoredEvent{event: ev, score: m * eventConfidence(ev)})
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].score != events[j].score {
			return events[i].score > events[j].score
		}
		return events[i].event.ID < events[j].event.ID
	})
	for _, e := range events {
		if len(result.Events) >= topK {
			break
		}
		result.Events = append(result.Events, e.event)
	}

	result.Timeline = GetRelatedTimeline(mem, tokens)
	if len(result.Timeline) > topK {
		result.Timeline = result.Timeline[:topK]
	}
	return result
}

// QueryTerms returns the normalized, stopword-filtered query tokens used by
// the memory query layer. Exported for consumers that need identical
// tokenization for their own matching (e.g. evidence retrieval evidence retrieval), so
// entity extraction never drifts from the ranking logic.
func QueryTerms(query string) []string {
	return tokenize(query)
}

// GetComponentTimeline returns all timeline entries mentioning a component,
// matched case-insensitively by substring (so "redis" finds "RedisCache").
// Entries are ordered oldest first.
func GetComponentTimeline(store *MemoryStore, componentName string) []archmodel.TimelineEntry {
	if store == nil {
		return nil
	}
	mem, err := store.LoadMemory()
	if err != nil || mem == nil {
		return nil
	}
	return GetComponentTimelineFromMemory(mem, componentName)
}

// GetComponentTimelineFromMemory is the in-memory variant of
// GetComponentTimeline.
func GetComponentTimelineFromMemory(mem *DeveloperMemory, componentName string) []archmodel.TimelineEntry {
	if mem == nil {
		return nil
	}
	needle := strings.ToLower(componentName)
	var entries []archmodel.TimelineEntry
	for _, entry := range mem.Timeline {
		for _, comp := range entry.Components {
			if strings.Contains(strings.ToLower(comp), needle) {
				entries = append(entries, entry)
				break
			}
		}
	}
	return entries
}

// GetFullTimeline returns all timeline entries within the time window
// [from, to]. A zero from means the beginning; a zero to means now. Entries
// are ordered oldest first.
func GetFullTimeline(store *MemoryStore, from, to time.Time) []archmodel.TimelineEntry {
	if store == nil {
		return nil
	}
	mem, err := store.LoadMemory()
	if err != nil || mem == nil {
		return nil
	}
	return GetFullTimelineFromMemory(mem, from, to)
}

// GetFullTimelineFromMemory is the in-memory variant of GetFullTimeline.
func GetFullTimelineFromMemory(mem *DeveloperMemory, from, to time.Time) []archmodel.TimelineEntry {
	if mem == nil {
		return nil
	}
	if to.IsZero() {
		to = time.Now()
	}
	var entries []archmodel.TimelineEntry
	for _, entry := range mem.Timeline {
		if (from.IsZero() || !entry.Timestamp.Before(from)) && !entry.Timestamp.After(to) {
			entries = append(entries, entry)
		}
	}
	return entries
}

// GetRelatedTimeline returns the timeline entries relevant to the given
// entities (already-lowercased query tokens or entity names). An entry is
// relevant when any entity appears in its components, description or title.
// Entries are deduplicated and ordered most-recent first (recency matters
// for evidence retrieval).
func GetRelatedTimeline(mem *DeveloperMemory, entities []string) []archmodel.TimelineEntry {
	if mem == nil || len(entities) == 0 {
		return nil
	}
	needles := make([]string, 0, len(entities))
	for _, e := range entities {
		if e = strings.ToLower(e); e != "" {
			needles = append(needles, e)
		}
	}
	if len(needles) == 0 {
		return nil
	}

	var entries []archmodel.TimelineEntry
	seen := make(map[string]bool)
	for _, entry := range mem.Timeline {
		if seen[entryKey(entry)] {
			continue
		}
		if timelineMatches(entry, needles) {
			seen[entryKey(entry)] = true
			entries = append(entries, entry)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if !entries[i].Timestamp.Equal(entries[j].Timestamp) {
			return entries[i].Timestamp.After(entries[j].Timestamp)
		}
		return entries[i].CommitHash > entries[j].CommitHash
	})
	return entries
}

// timelineMatches reports whether an entry mentions any of the needles in its
// components, description, title or tags.
func timelineMatches(entry archmodel.TimelineEntry, needles []string) bool {
	text := strings.ToLower(strings.Join(entry.Components, " ") + " " +
		entry.Description + " " + entry.Title + " " + strings.Join(entry.Tags, " "))
	for _, n := range needles {
		if strings.Contains(text, n) {
			return true
		}
	}
	return false
}

// entryKey deduplicates timeline entries (same commit, kind and title).
func entryKey(entry archmodel.TimelineEntry) string {
	return entry.CommitHash + "\x00" + string(entry.EventKind) + "\x00" + entry.Title
}

// --- scoring helpers ---

// tokenize lowercases the query, splits on non-alphanumerics and drops
// stopwords and single-character tokens.
func tokenize(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var out []string
	for _, f := range fields {
		if len(f) < 2 || stopwords[f] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// stopwords are the query noise words that carry no architectural meaning.
var stopwords = map[string]bool{
	"the": true, "and": true, "was": true, "were": true, "why": true,
	"what": true, "when": true, "how": true, "where": true, "who": true,
	"which": true, "does": true, "did": true, "do": true, "is": true,
	"are": true, "for": true, "from": true, "with": true, "this": true,
	"that": true, "have": true, "has": true, "had": true, "been": true,
	"being": true, "not": true, "can": true, "could": true, "would": true,
	"should": true, "will": true, "about": true, "into": true, "than": true,
	"then": true, "there": true, "their": true, "they": true, "you": true,
	"your": true, "its": true, "it": true, "of": true, "on": true, "in": true,
	"at": true, "to": true, "by": true, "as": true, "or": true, "if": true,
	"but": true, "so": true, "up": true, "out": true, "get": true, "use": true,
	"used": true, "using": true, "know": true, "tell": true, "explain": true,
	"introduce": true, "introduced": true, "added": true, "remove": true,
	"removed": true, "change": true, "changed": true, "happen": true,
	"happened": true, "still": true, "ever": true, "since": true,
}

// matchScore scores a text against the query tokens: exact word match 1.0,
// prefix match 0.9, substring match 0.8, otherwise 0.
func matchScore(text string, tokens []string) float64 {
	t := strings.ToLower(text)
	best := 0.0
	for _, tok := range tokens {
		if t == tok {
			return 1.0
		}
		score := 0.0
		switch {
		case strings.HasPrefix(t, tok):
			score = 0.9
		case strings.Contains(t, tok) || strings.Contains(tok, t):
			score = 0.8
		}
		if score > best {
			best = score
		}
	}
	return best
}

// stateWeight weights components by their temporal state so CURRENT
// knowledge outranks REMOVED/HISTORICAL in query results.
func stateWeight(state KnowledgeState) float64 {
	switch state {
	case StateActive:
		return 1.0
	case StateExperimental:
		return 0.8
	case StateDeprecated:
		return 0.6
	case StateHistorical:
		return 0.4
	case StateRemoved:
		return 0.3
	default:
		return 0.5
	}
}

// claimConfidence returns the claim's aggregate evidence confidence,
// defaulting to a neutral 0.5 when the bundle is empty.
func claimConfidence(c KnowledgeClaim) float64 {
	if c.Evidence.AggConfidence > 0 {
		return c.Evidence.AggConfidence
	}
	return 0.5
}

// claimFreshness returns the claim's freshness score, defaulting to 1.0
// before knowledge aging aging has run.
func claimFreshness(c KnowledgeClaim) float64 {
	if c.FreshnessScore > 0 {
		return c.FreshnessScore
	}
	return 1.0
}

// eventConfidence returns an event's aggregate evidence confidence,
// defaulting to a neutral 0.7 for events without evidence.
func eventConfidence(e archmodel.ArchEvent) float64 {
	if e.Evidence.AggConfidence > 0 {
		return e.Evidence.AggConfidence
	}
	return 0.7
}
