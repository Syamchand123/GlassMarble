package grounding

import (
	"os"
	"path/filepath"
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
			"http_handlers",
			"callgraph",
			"arch_intelligence",
			"arch_events",
			"ingress_points",
			"dependencies",
		},
	}

	payload, err := c.CollectSectionFacts(sec, scope)
	require.NoError(t, err)
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

	// 5. Symbol permalinks populated from AKG node positions
	for _, s := range payload.Symbols {
		if s.File == "" {
			continue
		}
		assert.NotEmpty(t, s.Permalink, "symbol %s should carry a permalink", s.FQN)
		assert.Contains(t, s.Permalink, s.File)
	}
	assert.Contains(t, symbolFQNs, "internal/auth/jwt.go::ValidateToken")
	for _, s := range payload.Symbols {
		if s.FQN == "internal/auth/jwt.go::ValidateToken" {
			assert.Equal(t, "internal/auth/jwt.go#L25-L50", s.Permalink)
		}
	}

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

func TestCollector_DescopedDBSchemasIgnored(t *testing.T) {
	// P22 (db_schemas) is descoped: requesting it must not fail and must
	// produce no schema facts.
	g := buildTestGraph()
	c := NewCollector(g)
	scope := &config.ScopeRule{Paths: []string{"internal/auth/**"}}
	sec := &config.SectionSpec{ID: "db", GroundWith: []string{"db_schemas"}}

	payload, err := c.CollectSectionFacts(sec, scope)
	require.NoError(t, err)
	require.NotNil(t, payload)
	assert.Empty(t, payload.Symbols)
}

func TestCollector_AliasesResolve(t *testing.T) {
	g := buildTestGraph()
	c := NewCollector(g)
	scope := &config.ScopeRule{Paths: []string{"internal/auth/**"}}
	sec := &config.SectionSpec{ID: "alias", GroundWith: []string{
		"callers", "components", "symbols", "exported_interfaces",
		"timeline", "timelines", "commit_reasoning", "diagrams",
	}}

	payload, err := c.CollectSectionFacts(sec, scope)
	require.NoError(t, err)
	require.NotNil(t, payload)
	assert.NotEmpty(t, payload.Symbols)
}

func TestCollector_UnknownGroundWithErrors(t *testing.T) {
	g := buildTestGraph()
	c := NewCollector(g)
	scope := &config.ScopeRule{Paths: []string{"internal/auth/**"}}
	sec := &config.SectionSpec{ID: "bad", GroundWith: []string{"quantum_entanglement"}}

	payload, err := c.CollectSectionFacts(sec, scope)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown ground_with "quantum_entanglement"`)
	require.NotNil(t, payload)
}

func TestCollector_ExtendedAliasesResolve(t *testing.T) {
	g := buildTestGraph()
	c := NewCollector(g)
	scope := &config.ScopeRule{Paths: []string{"internal/auth/**"}}
	sec := &config.SectionSpec{ID: "alias2", GroundWith: []string{
		"env_getenv", "flag_defs", "symbol_deltas", "config_var_changes",
		"migration_files", "c4container", "egress_calls", "crypto_primitives",
	}}

	payload, err := c.CollectSectionFacts(sec, scope)
	require.NoError(t, err)
	require.NotNil(t, payload)
}

// addEdgeBoth registers an edge in both the outbound and inbound maps,
// mirroring how the AKG persists edges.
func addEdgeBoth(g *akg.CodePropertyGraph, src, dst string, typ link.RelationshipType) {
	e := link.ResolvedEdge{SourceID: src, TargetID: dst, Type: typ}
	out, _ := g.OutboundEdges.Get(src)
	g.OutboundEdges = g.OutboundEdges.Set(src, append(out, e))
	in, _ := g.InboundEdges.Get(dst)
	g.InboundEdges = g.InboundEdges.Set(dst, append(in, e))
}

func TestCollectCallgraphFacts_InboundCallers(t *testing.T) {
	g := akg.NewCodePropertyGraph("inbound-test")
	ep := "a.go::Main"
	callee := "b.go::Work"
	callerOfEP := "c.go::Trigger"
	callerOfCallee := "d.go::Other"
	for _, id := range []string{ep, callee, callerOfEP, callerOfCallee} {
		g.Nodes = g.Nodes.Set(id, &link.ResolvedNode{
			ID:   id,
			Name: id,
			Kind: "FUNCTION",
			FileSpec: link.LocationMeta{
				Path:      "a.go",
				LineStart: 1,
				LineEnd:   3,
			},
		})
	}
	addEdgeBoth(g, ep, callee, link.EdgeCalls)             // outbound: callee
	addEdgeBoth(g, callerOfEP, ep, link.EdgeCalls)         // inbound of entry point
	addEdgeBoth(g, callerOfCallee, callee, link.EdgeCalls) // one-hop inbound via callee
	addEdgeBoth(g, "e.go::Dep", ep, link.EdgeDependsOn)    // non-call edge: ignored

	c := NewCollector(g)
	scope := &config.ScopeRule{Paths: []string{"**"}, EntryPoints: []string{ep}}
	sec := &config.SectionSpec{ID: "cg", GroundWith: []string{"callgraph"}}

	payload, err := c.CollectSectionFacts(sec, scope)
	require.NoError(t, err)
	require.NotNil(t, payload)

	kindByFQN := make(map[string]string)
	for _, s := range payload.Symbols {
		if prev, dup := kindByFQN[s.FQN]; dup {
			t.Fatalf("duplicate symbol fact %q (kinds %q and %q)", s.FQN, prev, s.Kind)
		}
		kindByFQN[s.FQN] = s.Kind
	}
	assert.Equal(t, "callee", kindByFQN[callee], "outbound callee must be present")
	assert.Equal(t, "caller", kindByFQN[callerOfEP], "inbound caller of entry point must be present")
	assert.Equal(t, "caller", kindByFQN[callerOfCallee], "one-hop inbound caller via callee must be present")
	assert.NotContains(t, kindByFQN, "e.go::Dep", "non-CALLS inbound edges must be ignored")
}

// TestCollector_RealisticAKGNode_SignatureAndDocFallback guards against a
// regression where every symbol's Signature/Doc came back empty ("No doc
// comment provided.", signature == bare name) in real usage, even though
// buildTestGraph's synthetic fixture (above) sets "signature"/"doc_comment"
// properties directly and always passed. The real AKG never populates
// those two properties for any node — confirmed via `gmb inspect` against a
// freshly analyzed repo — so a test fixture that pre-populates them masked
// the gap entirely. This test instead sets only "content" (which the real
// AKG DOES populate) and no doc_comment/doc property at all, then relies on
// WithRepoRoot's Go-source fallback for the doc comment, exactly like a real
// `gmb doc` run against real analyzed source.
func TestCollector_RealisticAKGNode_SignatureAndDocFallback(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "pkg", "auth")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	src := `package auth

import "errors"

// ErrInvalidToken is returned when a token fails validation.
var ErrInvalidToken = errors.New("invalid token")

// Session represents an authenticated user session.
type Session struct {
	UserID string
}

// Login authenticates a user and returns a session token.
func Login(username, password string) (string, error) {
	if username == "" || password == "" {
		return "", errors.New("missing credentials")
	}
	return "tok-" + username, nil
}
`
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "auth.go"), []byte(src), 0644))

	g := akg.NewCodePropertyGraph("commit-realistic")
	g.Nodes = g.Nodes.Set("pkg/auth/auth.go::Login", &link.ResolvedNode{
		ID:   "pkg/auth/auth.go::Login",
		Name: "Login",
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path:      "pkg/auth/auth.go",
			LineStart: 14,
			LineEnd:   19,
		},
		// Only "content" — no "signature", no "doc_comment"/"doc" — matches
		// what the real AKG ingestion pipeline actually produces today.
		Properties: map[string]string{
			"content": "func Login(username, password string) (string, error) {\n\tif username == \"\" || password == \"\" {\n\t\treturn \"\", errors.New(\"missing credentials\")\n\t}\n\treturn \"tok-\" + username, nil\n}",
		},
	})
	g.Nodes = g.Nodes.Set("pkg/auth/auth.go::Session", &link.ResolvedNode{
		ID:   "pkg/auth/auth.go::Session",
		Name: "Session",
		Kind: "STRUCT",
		FileSpec: link.LocationMeta{
			Path:      "pkg/auth/auth.go",
			LineStart: 9,
			LineEnd:   11,
		},
		Properties: map[string]string{
			"content": "type Session struct {\n\tUserID string\n}",
		},
	})
	g.Nodes = g.Nodes.Set("pkg/auth/auth.go::ErrInvalidToken", &link.ResolvedNode{
		ID:   "pkg/auth/auth.go::ErrInvalidToken",
		Name: "ErrInvalidToken",
		Kind: "VARIABLE",
		FileSpec: link.LocationMeta{
			Path:      "pkg/auth/auth.go",
			LineStart: 6,
			LineEnd:   6,
		},
		Properties: map[string]string{}, // no content either, real for a simple var
	})

	c := NewCollector(g).WithRepoRoot(root)
	scope := &config.ScopeRule{Paths: []string{"pkg/auth/**"}}
	sec := &config.SectionSpec{ID: "s", GroundWith: []string{"signatures", "sentinels"}}

	payload, err := c.CollectSectionFacts(sec, scope)
	require.NoError(t, err)

	var loginFact, sessionFact *config.SymbolFact
	for i := range payload.Symbols {
		switch payload.Symbols[i].FQN {
		case "pkg/auth/auth.go::Login":
			loginFact = &payload.Symbols[i]
		case "pkg/auth/auth.go::Session":
			sessionFact = &payload.Symbols[i]
		}
	}
	require.NotNil(t, loginFact, "Login symbol fact missing")
	assert.Equal(t, "func Login(username, password string) (string, error)", loginFact.Signature,
		"signature must be reconstructed from content, not just the bare name")
	assert.Equal(t, "Login authenticates a user and returns a session token.", loginFact.Doc,
		"doc comment must come from the Go-source fallback scan")

	require.NotNil(t, sessionFact, "Session symbol fact missing")
	assert.Equal(t, "Session represents an authenticated user session.", sessionFact.Doc)

	require.Len(t, payload.Sentinels, 1)
	assert.Equal(t, "ErrInvalidToken is returned when a token fails validation.", payload.Sentinels[0].Doc)
}

// TestCollectHTTPHandlers_ExtractsRoutesFromSource guards against a
// regression where the "api" archetype's "Endpoints & Route Handlers"
// section never actually showed a route's HTTP method or path — the AKG
// does not index call-argument literals, so http.HandleFunc("/tasks", ...)
// 's path string is otherwise invisible, and a handler function only ever
// appeared in the generic Functions and Methods table like any other
// function. Covers both a net/http-style HandleFunc call (method unknown,
// reported as ANY) and a gin/echo/chi-style chained method call (method
// taken from the selector name).
func TestCollectHTTPHandlers_ExtractsRoutesFromSource(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "pkg", "api")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	src := `package api

import "net/http"

// CreateTaskHandler handles POST /tasks and creates a new task.
func CreateTaskHandler(w http.ResponseWriter, r *http.Request) {}

// ListTasksHandler handles GET /tasks and lists tasks.
func ListTasksHandler(w http.ResponseWriter, r *http.Request) {}

func setupRoutes(router *Router) {
	http.HandleFunc("/tasks", CreateTaskHandler)
	router.GET("/tasks", ListTasksHandler)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "handler.go"), []byte(src), 0644))

	g := akg.NewCodePropertyGraph("commit-http-routes")
	g.Nodes = g.Nodes.Set("pkg/api/handler.go::CreateTaskHandler", &link.ResolvedNode{
		ID:   "pkg/api/handler.go::CreateTaskHandler",
		Name: "CreateTaskHandler",
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path:      "pkg/api/handler.go",
			LineStart: 6,
			LineEnd:   6,
		},
		Properties: map[string]string{"signature": "func CreateTaskHandler(w http.ResponseWriter, r *http.Request)"},
	})
	g.Nodes = g.Nodes.Set("pkg/api/handler.go::ListTasksHandler", &link.ResolvedNode{
		ID:   "pkg/api/handler.go::ListTasksHandler",
		Name: "ListTasksHandler",
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path:      "pkg/api/handler.go",
			LineStart: 9,
			LineEnd:   9,
		},
		Properties: map[string]string{"signature": "func ListTasksHandler(w http.ResponseWriter, r *http.Request)"},
	})

	c := NewCollector(g).WithRepoRoot(root)
	scope := &config.ScopeRule{Paths: []string{"pkg/api/**"}}
	sec := &config.SectionSpec{ID: "endpoints", GroundWith: []string{"http_handlers"}}

	payload, err := c.CollectSectionFacts(sec, scope)
	require.NoError(t, err)
	require.Len(t, payload.Endpoints, 2, "expected both routes: %+v", payload.Endpoints)

	byHandler := make(map[string]config.EndpointFact)
	for _, ep := range payload.Endpoints {
		byHandler[ep.Handler] = ep
	}

	create, ok := byHandler["CreateTaskHandler"]
	require.True(t, ok, "CreateTaskHandler route missing: %+v", payload.Endpoints)
	assert.Equal(t, "ANY", create.Method, "net/http.HandleFunc has no method restriction")
	assert.Equal(t, "/tasks", create.Path)
	assert.Equal(t, "CreateTaskHandler handles POST /tasks and creates a new task.", create.Doc)
	assert.Contains(t, create.Permalink, "pkg/api/handler.go")

	list, ok := byHandler["ListTasksHandler"]
	require.True(t, ok, "ListTasksHandler route missing: %+v", payload.Endpoints)
	assert.Equal(t, "GET", list.Method, "chained .GET(...) call must report method GET")
	assert.Equal(t, "/tasks", list.Path)
}

// TestCollectHTTPHandlers_RegistrationOutsideScopeStillResolves guards
// against a regression in the fix above: route registrations are commonly
// written in main.go or a router-setup file, separate from the handler
// package itself (the doc's scope, e.g. "pkg/api/**") — scoping the source
// scan to the document's own package found nothing at all for that
// entirely normal layout. What must decide whether a route belongs to this
// document is whether its HANDLER resolves to an in-scope symbol, not
// where the registration call happens to be written.
func TestCollectHTTPHandlers_RegistrationOutsideScopeStillResolves(t *testing.T) {
	root := t.TempDir()
	apiDir := filepath.Join(root, "pkg", "api")
	require.NoError(t, os.MkdirAll(apiDir, 0755))
	handlerSrc := `package api

import "net/http"

// CreateTaskHandler handles POST /tasks and creates a new task.
func CreateTaskHandler(w http.ResponseWriter, r *http.Request) {}
`
	require.NoError(t, os.WriteFile(filepath.Join(apiDir, "handler.go"), []byte(handlerSrc), 0644))

	cmdDir := filepath.Join(root, "cmd", "server")
	require.NoError(t, os.MkdirAll(cmdDir, 0755))
	mainSrc := `package main

import (
	"net/http"

	"example.com/taskmgr/pkg/api"
)

func main() {
	http.HandleFunc("/tasks", api.CreateTaskHandler)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(mainSrc), 0644))

	g := akg.NewCodePropertyGraph("commit-cross-file-route")
	g.Nodes = g.Nodes.Set("pkg/api/handler.go::CreateTaskHandler", &link.ResolvedNode{
		ID:   "pkg/api/handler.go::CreateTaskHandler",
		Name: "CreateTaskHandler",
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path:      "pkg/api/handler.go",
			LineStart: 5,
			LineEnd:   5,
		},
		Properties: map[string]string{"signature": "func CreateTaskHandler(w http.ResponseWriter, r *http.Request)"},
	})

	c := NewCollector(g).WithRepoRoot(root)
	scope := &config.ScopeRule{Paths: []string{"pkg/api/**"}} // deliberately excludes cmd/server
	sec := &config.SectionSpec{ID: "endpoints", GroundWith: []string{"http_handlers"}}

	payload, err := c.CollectSectionFacts(sec, scope)
	require.NoError(t, err)
	require.Len(t, payload.Endpoints, 1, "route must resolve even though its registration lives outside scope: %+v", payload.Endpoints)
	assert.Equal(t, "CreateTaskHandler", payload.Endpoints[0].Handler)
	assert.Equal(t, "/tasks", payload.Endpoints[0].Path)
	assert.Contains(t, payload.Endpoints[0].Permalink, "pkg/api/handler.go",
		"permalink must point at the in-scope handler, not the out-of-scope registration site")
}

// TestExtractDoc_NoRepoRootIsNoOpFallback guards the opt-in nature of the
// Go-source fallback: a Collector with no WithRepoRoot call (every existing
// caller before this feature, and every collector_test.go fixture above)
// must behave exactly as before — doc comes back empty when the AKG node
// has no doc_comment/doc property, never attempting any file I/O.
func TestExtractDoc_NoRepoRootIsNoOpFallback(t *testing.T) {
	g := akg.NewCodePropertyGraph("commit-no-reporoot")
	g.Nodes = g.Nodes.Set("pkg/x.go::Foo", &link.ResolvedNode{
		ID:   "pkg/x.go::Foo",
		Name: "Foo",
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path:      "pkg/x.go",
			LineStart: 1,
			LineEnd:   3,
		},
		Properties: map[string]string{"content": "func Foo() {}"},
	})
	c := NewCollector(g) // no WithRepoRoot
	scope := &config.ScopeRule{Paths: []string{"**"}}
	sec := &config.SectionSpec{ID: "s", GroundWith: []string{"signatures"}}

	payload, err := c.CollectSectionFacts(sec, scope)
	require.NoError(t, err)
	require.Len(t, payload.Symbols, 1)
	assert.Equal(t, "", payload.Symbols[0].Doc, "no repoRoot means no source-scan fallback")
	assert.Equal(t, "func Foo()", payload.Symbols[0].Signature, "content-based signature fallback needs no repoRoot")
}
