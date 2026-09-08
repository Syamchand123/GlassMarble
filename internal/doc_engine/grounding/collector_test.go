package grounding

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildTestGraph() *akg.CodePropertyGraph {
	g := akg.NewCodePropertyGraph("commit-grounding-test")

	// 1. Exported func with signature and doc comment
	g.Nodes = g.Nodes.Set("internal/auth/jwt.go::ValidateToken", &link.ResolvedNode{
		ID:   "internal/auth/jwt.go::ValidateToken",
		Name: "ValidateToken",
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path:      "internal/auth/jwt.go",
			LineStart: 25,
			LineEnd:   50,
		},
		Properties: map[string]string{
			"signature":   "func ValidateToken(token string) (*Claims, error)",
			"doc_comment": "ValidateToken parses and validates a signed JWT.",
		},
	})

	// 2. Unexported helper func
	g.Nodes = g.Nodes.Set("internal/auth/jwt.go::parseRaw", &link.ResolvedNode{
		ID:   "internal/auth/jwt.go::parseRaw",
		Name: "parseRaw",
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path:      "internal/auth/jwt.go",
			LineStart: 55,
			LineEnd:   65,
		},
		Properties: map[string]string{
			"signature": "func parseRaw(raw string) []byte",
		},
	})

	// 3. Sentinel Error
	g.Nodes = g.Nodes.Set("internal/auth/errors.go::ErrTokenExpired", &link.ResolvedNode{
		ID:   "internal/auth/errors.go::ErrTokenExpired",
		Name: "ErrTokenExpired",
		Kind: "VARIABLE",
		FileSpec: link.LocationMeta{
			Path:      "internal/auth/errors.go",
			LineStart: 12,
			LineEnd:   12,
		},
		Properties: map[string]string{
			"doc_comment": "ErrTokenExpired is returned when the token is past its exp.",
		},
	})

	// 4. Concurrency primitive
	g.Nodes = g.Nodes.Set("internal/auth/cache.go::TokenCache", &link.ResolvedNode{
		ID:   "internal/auth/cache.go::TokenCache",
		Name: "TokenCache",
		Kind: "STRUCT",
		FileSpec: link.LocationMeta{
			Path:      "internal/auth/cache.go",
			LineStart: 10,
			LineEnd:   20,
		},
		Properties: map[string]string{
			"signature": "type TokenCache struct { mu sync.RWMutex }",
		},
	})

	// 5. Config Var
	g.Nodes = g.Nodes.Set("internal/auth/config.go::JWTSecretKey", &link.ResolvedNode{
		ID:   "internal/auth/config.go::JWTSecretKey",
		Name: "os.Getenv(\"JWT_SECRET\")",
		Kind: "CALL",
		FileSpec: link.LocationMeta{
			Path:      "internal/auth/config.go",
			LineStart: 15,
			LineEnd:   15,
		},
		Properties: map[string]string{
			"doc_comment": "Secret key for JWT verification",
		},
	})

	// 6. DB Model
	g.Nodes = g.Nodes.Set("internal/auth/models.go::UserSession", &link.ResolvedNode{
		ID:   "internal/auth/models.go::UserSession",
		Name: "UserSession",
		Kind: "STRUCT",
		FileSpec: link.LocationMeta{
			Path:      "internal/auth/models.go",
			LineStart: 5,
			LineEnd:   15,
		},
		Properties: map[string]string{
			"signature": "type UserSession struct { ID string; UserID string }",
		},
	})

	// 7. HTTP Handler
	g.Nodes = g.Nodes.Set("internal/auth/handler.go::LoginHandler", &link.ResolvedNode{
		ID:   "internal/auth/handler.go::LoginHandler",
		Name: "LoginHandler",
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path:      "internal/auth/handler.go",
			LineStart: 18,
			LineEnd:   35,
		},
		Properties: map[string]string{
			"signature": "func LoginHandler(w http.ResponseWriter, r *http.Request)",
		},
	})

	// Edges for callgraph and dependency
	g.OutboundEdges = g.OutboundEdges.Set("internal/auth/jwt.go::ValidateToken", []link.ResolvedEdge{
		{
			SourceID: "internal/auth/jwt.go::ValidateToken",
			TargetID: "internal/auth/jwt.go::parseRaw",
			Type:     link.EdgeCalls,
		},
	})

	return g
}

func TestCollector_AllDirectives(t *testing.T) {
	g := buildTestGraph()
	c := NewCollector(g)
	scope := &config.ScopeRule{
		Paths:       []string{"internal/auth/**"},
		EntryPoints: []string{"internal/auth/jwt.go::ValidateToken"},
	}

	sec := &config.SectionSpec{
		ID: "full-test",
		GroundWith: []string{
			"signatures",
			"exported_symbols",
			"comments",
			"sentinels",
			"error_returns",
			"concurrency_primitives",
			"config_vars",
			"db_schemas",
			"http_handlers",
			"callgraph",
			"arch_intelligence",
			"arch_events",
			"ingress_points",
			"dependencies",
		},
	}

	payload := c.CollectSectionFacts(sec, scope)
	require.NotNil(t, payload)

	// 1. Signatures & exported symbols
	symbolFQNs := make(map[string]bool)
	for _, s := range payload.Symbols {
		symbolFQNs[s.FQN] = true
	}
	assert.True(t, symbolFQNs["internal/auth/jwt.go::ValidateToken"])
	assert.True(t, symbolFQNs["internal/auth/jwt.go::parseRaw"]) // present via signatures

	// 2. Sentinels
	assert.NotEmpty(t, payload.Sentinels)
	sentinelNames := make(map[string]bool)
	for _, sf := range payload.Sentinels {
		sentinelNames[sf.FQN] = true
	}
	assert.True(t, sentinelNames["internal/auth/errors.go::ErrTokenExpired"])

	// 3. Concurrency
	hasConcurrency := false
	for _, s := range payload.Symbols {
		if s.Kind == "concurrency" {
			hasConcurrency = true
		}
	}
	assert.True(t, hasConcurrency)

	// 4. Config Vars
	assert.NotEmpty(t, payload.ConfigVars)
	hasJWTSecret := false
	for _, cv := range payload.ConfigVars {
		if cv.Name == "os.Getenv(\"JWT_SECRET\")" {
			hasJWTSecret = true
		}
	}
	assert.True(t, hasJWTSecret)

	// 5. DB Schemas
	assert.NotEmpty(t, payload.Schemas)
	assert.Equal(t, "UserSession", payload.Schemas[0].Name)

	// 6. HTTP Handlers
	hasHTTP := false
	for _, s := range payload.Symbols {
		if s.Kind == "http_handler" {
			hasHTTP = true
		}
	}
	assert.True(t, hasHTTP)

	// 7. Ingress & Callgraph
	hasCallee := false
	for _, s := range payload.Symbols {
		if s.Kind == "callee" {
			hasCallee = true
		}
	}
	assert.True(t, hasCallee)
}
