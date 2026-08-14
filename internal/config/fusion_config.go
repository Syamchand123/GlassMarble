package config

import "strings"

// FusionConfig controls the knowledge fusion multi-source knowledge fusion pipeline
// (v2_master_implementaion_plan.md §7). It is the single canonical definition
// of the "fusion:" section in .glassmarble/config.yaml.
//
// It lives in the config package (not in knowledge_fusion) for the same
// reason IntelligenceConfig does: knowledge_fusion imports commit_reasoning,
// and commit_reasoning imports config — placing this type in knowledge_fusion
// would create an import cycle. Phase packages consume config types; they do
// not define them.
//
// All fields are optional; zero values fall back to DefaultFusionConfig via
// ApplyDefaults. Whether the phase runs at all is controlled by the
// `--include-docs` flag on `gmb analyze` (opt-in by design — doc scanning and
// git-history walks are not free on large repositories).
type FusionConfig struct {
	// ADRGlobs lists file globs (relative to the repo root, "**" allowed)
	// matched for ADR parsing. See DefaultFusionConfig for the defaults.
	ADRGlobs []string `json:"adr_globs,omitempty" yaml:"adr_globs,omitempty"`

	// ReadmeFiles lists README files (relative to the repo root) parsed
	// for technology mentions.
	ReadmeFiles []string `json:"readme_files,omitempty" yaml:"readme_files,omitempty"`

	// TechLexicon adds technology names beyond the built-in lexicon
	// (Redis, PostgreSQL, Kafka, ...). Matching is case-insensitive and
	// word-boundary based.
	TechLexicon []string `json:"tech_lexicon,omitempty" yaml:"tech_lexicon,omitempty"`

	// IncludeGitSources enables PR/issue claim extraction from local git
	// history through the configured adapters. Tri-state: nil means the
	// default (true) — a plain bool cannot distinguish "unset" from an
	// explicit "false", which is why this is a pointer.
	IncludeGitSources *bool `json:"include_git_sources,omitempty" yaml:"include_git_sources,omitempty"`

	// MaxCommits caps how many most-recent commits the git adapter scans
	// per run (0 = the built-in default of 500). Bounds cost on large
	// repositories; the scan is a git-log walk over recent history.
	MaxCommits int `json:"max_commits,omitempty" yaml:"max_commits,omitempty"`

	// DocMaxSizeBytes caps how large a documentation file may be before
	// it is skipped (0 = 1 MiB). Prevents pathological files from being
	// parsed.
	DocMaxSizeBytes int64 `json:"doc_max_size_bytes,omitempty" yaml:"doc_max_size_bytes,omitempty"`

	// ExclusivePredicates lists predicates whose object is single-valued
	// per subject ("state", "status", "version", ...). Claims with these
	// predicates and DIFFERENT objects on the same subject are treated as
	// contradictions and resolved by source reliability. All other
	// predicates are multi-valued ("uses_technology", "was_modified_by_pr",
	// "decided_to", ...) — different objects merely coexist.
	ExclusivePredicates []string `json:"exclusive_predicates,omitempty" yaml:"exclusive_predicates,omitempty"`
}

// DefaultFusionConfig returns the built-in defaults. ADR globs cover the
// common ADR locations (docs/adr, docs/decisions, NNNN-name.md and
// adr-NNNN.md conventions); the lexicon covers the most common
// infrastructure technologies.
func DefaultFusionConfig() *FusionConfig {
	gitSources := true
	return &FusionConfig{
		ADRGlobs: []string{
			"docs/adr/**/*.md",
			"docs/adr/*.md",
			"docs/decisions/**/*.md",
			"docs/decisions/*.md",
			"docs/**/adr/**/*.md",
			"**/adr-*.md",
		},
		ReadmeFiles: []string{
			"README.md",
			"README.MD",
			"docs/README.md",
		},
		IncludeGitSources:  &gitSources,
		MaxCommits:         500,
		DocMaxSizeBytes:    1 << 20,
		ExclusivePredicates: []string{"state", "status", "version", "deployed_on"},
	}
}

// ApplyDefaults fills every unset field with the default value. It is the
// single place config merging happens, so a partially-populated "fusion:"
// section in config.yaml still behaves sensibly.
func (c *FusionConfig) ApplyDefaults() {
	if c == nil {
		return
	}
	d := DefaultFusionConfig()
	if len(c.ADRGlobs) == 0 {
		c.ADRGlobs = d.ADRGlobs
	}
	if len(c.ReadmeFiles) == 0 {
		c.ReadmeFiles = d.ReadmeFiles
	}
	if len(c.ExclusivePredicates) == 0 {
		c.ExclusivePredicates = d.ExclusivePredicates
	}
	if c.IncludeGitSources == nil {
		c.IncludeGitSources = d.IncludeGitSources
	}
	if c.MaxCommits == 0 {
		c.MaxCommits = d.MaxCommits
	}
	if c.DocMaxSizeBytes == 0 {
		c.DocMaxSizeBytes = d.DocMaxSizeBytes
	}
}

// GitSourcesEnabled reports whether PR/issue extraction from git history is
// enabled, honoring the tri-state IncludeGitSources (nil = default true).
func (c *FusionConfig) GitSourcesEnabled() bool {
	if c == nil || c.IncludeGitSources == nil {
		return true
	}
	return *c.IncludeGitSources
}

// builtinLexicon is the built-in technology name list. Matching is
// case-insensitive and word-boundary based, so "redis" matches "Redis"
// and "REDIS" but not "rediscount".
var builtinLexicon = []string{
	"Redis", "Memcached", "Hazelcast", "Caffeine", "Ehcache",
	"PostgreSQL", "Postgres", "MySQL", "MariaDB", "MongoDB", "Cassandra",
	"DynamoDB", "Couchbase", "Elasticsearch", "OpenSearch", "InfluxDB",
	"ClickHouse", "SQLite", "Oracle", "SQL Server", "SQLServer",
	"Kafka", "RabbitMQ", "ActiveMQ", "NATS", "Pulsar", "SQS", "SNS",
	"gRPC", "GraphQL", "REST", "WebSocket", "SSE",
	"Docker", "Kubernetes", "K8s", "Terraform", "Helm", "Prometheus",
	"Grafana", "Jaeger", "OpenTelemetry", "Zipkin", "OAuth", "OAuth2",
	"JWT", "SSO", "SAML", "OpenID", "LDAP", "Keycloak", "Vault",
	"S3", "Azure", "AWS", "GCP", "NGINX", "Apache", "Caddy", "Envoy",
	"Spark", "Flink", "Airflow", "Celery", "Sidekiq",
}

// Lexicon returns the effective technology lexicon (built-in + config
// additions), lowercased and deduplicated, in deterministic order. The
// caller may match against it with word boundaries. A nil receiver yields
// the built-in lexicon.
func (c *FusionConfig) Lexicon() []string {
	var extra []string
	if c != nil {
		extra = c.TechLexicon
	}
	seen := make(map[string]bool, len(builtinLexicon)+len(extra))
	var out []string
	add := func(name string) {
		key := strings.ToLower(name)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, key)
	}
	for _, name := range builtinLexicon {
		add(name)
	}
	for _, name := range extra {
		add(name)
	}
	return out
}
