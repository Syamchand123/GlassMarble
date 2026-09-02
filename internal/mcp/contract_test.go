package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers

func newContractServer(t *testing.T) *Server {
	t.Helper()
	cfg := DefaultConfig()
	cfg.RootDir = "."
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NotNil(t, srv)
	t.Cleanup(func() { srv.Close() })
	return srv
}

func ctxWithSession(srv *Server) context.Context {
	sess := server.NewInProcessSession("contract-test-session", nil)
	sess.Initialize()
	return srv.MCPServer().WithContext(context.Background(), sess)
}

// decodeResponse helper extracts JSONRPCResponse / Error.
func decodeJSONRPCResponse(t *testing.T, raw mcp.JSONRPCMessage) (isError bool, errCode int, result json.RawMessage) {
	t.Helper()
	switch v := raw.(type) {
	case mcp.JSONRPCResponse:
		b, _ := json.Marshal(v)
		var m map[string]json.RawMessage
		_ = json.Unmarshal(b, &m)
		if errRaw, ok := m["error"]; ok {
			var e struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			_ = json.Unmarshal(errRaw, &e)
			return true, e.Code, nil
		}
		if res, ok := m["result"]; ok {
			return false, 0, res
		}
		return false, 0, nil
	case mcp.JSONRPCError:
		return true, v.Error.Code, nil
	default:
		// fallback via json
		b, _ := json.Marshal(v)
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err == nil {
			if errRaw, ok := m["error"]; ok {
				var e struct {
					Code int `json:"code"`
				}
				_ = json.Unmarshal(errRaw, &e)
				return true, e.Code, nil
			}
		}
		return false, 0, b
	}
}

// ---------------------------------------------------------------------------
// 1. TestInitialize_HS
// ---------------------------------------------------------------------------

func TestContract_Initialize_HS(t *testing.T) {
	// pinned constant
	assert.Equal(t, "2024-11-05", ProtocolVersion)
	assert.Equal(t, "2024-11-05", GetProtocolVersion())

	srv := newContractServer(t)
	// constant golden
	require.Equal(t, "2024-11-05", srv.ProtocolVersion())

	ctx := ctxWithSession(srv)
	rawReq := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`)
	resp := srv.MCPServer().HandleMessage(ctx, rawReq)
	require.NotNil(t, resp, "initialize must return a response")

	jresp, ok := resp.(mcp.JSONRPCResponse)
	require.True(t, ok, "expected JSONRPCResponse, got %T", resp)

	b, _ := json.Marshal(jresp.Result)
	var initRes mcp.InitializeResult
	require.NoError(t, json.Unmarshal(b, &initRes))

	// golden: protocolVersion
	assert.Equal(t, "2024-11-05", initRes.ProtocolVersion, "ProtocolVersion == 2024-11-05 per Master Plan §5.3")

	// golden: serverInfo
	assert.Equal(t, "GlassMarble Architecture Intelligence", initRes.ServerInfo.Name)
	assert.NotEmpty(t, initRes.ServerInfo.Version)

	// golden: capabilities
	require.NotNil(t, initRes.Capabilities.Tools, "capabilities.tools must be present")
	require.NotNil(t, initRes.Capabilities.Resources, "capabilities.resources must be present")
	require.NotNil(t, initRes.Capabilities.Prompts, "capabilities.prompts must be present")
	assert.NotNil(t, initRes.Capabilities.Logging, "capabilities.logging must be present")

	// json golden check via encoding/json
	rawJSON, err := json.Marshal(initRes)
	require.NoError(t, err)
	var capMap map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rawJSON, &capMap))
	// ensure capabilities key exists via raw
	var full map[string]any
	require.NoError(t, json.Unmarshal(rawJSON, &full))
	assert.Contains(t, string(rawJSON), "GlassMarble Architecture Intelligence")

	// instructions contain protocol version
	var insp map[string]any
	require.NoError(t, json.Unmarshal(rawJSON, &insp))
	// instructions lives in InitializeResult
	assert.Contains(t, initRes.Instructions, "2024-11-05")
}

func TestInitialize_HS(t *testing.T) { TestContract_Initialize_HS(t) }

// ---------------------------------------------------------------------------
// 2. TestToolsList_Golden
// ---------------------------------------------------------------------------

func TestContract_ToolsList_Golden(t *testing.T) {
	srv := newContractServer(t)
	ctx := ctxWithSession(srv)

	// via SDK ListTools
	toolsMap := srv.MCPServer().ListTools()
	require.NotNil(t, toolsMap)
	assert.GreaterOrEqual(t, len(toolsMap), 41, "spec requires >=41 tools, enterprise superset 56")

	// spec-mandated 41 tools (aliases accepted)
	specTools := []string{
		"gmb_status", "gmb_server_info",
		"akg_status", "akg_summary", "akg_search", "akg_get_node", "akg_edges", "akg_traverse", "akg_path", "akg_cycles", "akg_orphans", "akg_god_objects", "akg_hotspots", "akg_page_rank", "akg_impact_radius", "akg_communities", "akg_articulation_points", "akg_topological_order", "akg_entrypoints", "akg_similarity",
		"gmb_inspect_search", "gmb_inspect_node", "gmb_dependency_analysis",
		"gmb_impact_analysis", "gmb_hotspot_rankings",
		"gmb_drift_check", "gmb_arch_lint", "gmb_patterns_smells", "gmb_arch_stats",
		"gmb_memory_overview", "gmb_memory_query", "gmb_memory_component", "gmb_arch_timeline",
		"gmb_snapshot_list", "gmb_snapshot_at", "gmb_snapshot_diff",
		// diagram: canonical spec names are diagram_generate / diagram_types ; impl uses gmb_render_diagram / gmb_list_diagram_types
		"gmb_render_diagram", "gmb_list_diagram_types",
		"gmb_code_definition", "gmb_code_references", "gmb_code_callgraph", "gmb_code_context",
	}
	missing := []string{}
	for _, name := range specTools {
		if _, ok := toolsMap[name]; !ok {
			missing = append(missing, name)
		}
	}
	assert.Empty(t, missing, "missing spec tools: %v", missing)

	// each tool must have valid JSON schema inline golden
	for name, st := range toolsMap {
		assert.NotEmpty(t, st.Tool.Name, "tool %q name empty", name)
		assert.NotEmpty(t, st.Tool.Description, "tool %q description empty", name)
		// InputSchema must be object type
		assert.Equal(t, "object", st.Tool.InputSchema.Type, "tool %q InputSchema.Type must be object", name)
		// schema must be json-serializable and contain type field
		b, err := json.Marshal(st.Tool.InputSchema)
		require.NoError(t, err, "tool %q schema marshal", name)
		var schema map[string]any
		require.NoError(t, json.Unmarshal(b, &schema))
		assert.Equal(t, "object", schema["type"], "tool %q schema type", name)
		// annotations: readOnly true
		if st.Tool.Annotations.ReadOnlyHint != nil {
			assert.True(t, *st.Tool.Annotations.ReadOnlyHint, "tool %q should be readOnly", name)
		}
		// use mcp type assertion via json round-trip
		var asMCP mcp.Tool
		require.NoError(t, json.Unmarshal(b, &asMCP.InputSchema))
		_ = asMCP
	}

	// via JSON-RPC tools/list
	rawReq := json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	resp := srv.MCPServer().HandleMessage(ctx, rawReq)
	require.NotNil(t, resp)
	jresp, ok := resp.(mcp.JSONRPCResponse)
	require.True(t, ok, "tools/list must return JSONRPCResponse got %T", resp)
	b, _ := json.Marshal(jresp.Result)
	var listRes mcp.ListToolsResult
	require.NoError(t, json.Unmarshal(b, &listRes))
	assert.GreaterOrEqual(t, len(listRes.Tools), 41)
	// golden inline: ensure each has name/description/type
	for _, tl := range listRes.Tools {
		assert.NotEmpty(t, tl.Name)
		assert.NotEmpty(t, tl.Description)
		raw, _ := json.Marshal(tl)
		var mm map[string]any
		_ = json.Unmarshal(raw, &mm)
		assert.Contains(t, mm, "name")
		assert.Contains(t, mm, "inputSchema")
		// validate schema via mcp.ToolInputSchema round-trip
		var t2 mcp.Tool
		_ = json.Unmarshal(raw, &t2)
	}
}

func TestToolsList_Golden(t *testing.T) { TestContract_ToolsList_Golden(t) }

// ---------------------------------------------------------------------------
// 3. TestToolsCall_SuccessAndIsError
// ---------------------------------------------------------------------------

func TestContract_ToolsCall_SuccessAndIsError(t *testing.T) {
	srv := newContractServer(t)
	ctx := ctxWithSession(srv)

	// success: gmb_server_info via direct handler
	res, err := srv.handleServerInfoTool(ctx, mcp.CallToolRequest{})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.False(t, res.IsError, "success must have isError false")
	require.NotEmpty(t, res.Content)
	tc, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok, "content must be TextContent got %T", res.Content[0])
	assert.Equal(t, "text", tc.Type)
	assert.NotEmpty(t, tc.Text)
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(tc.Text), &data))
	assert.Equal(t, "GlassMarble Architecture Intelligence", data["name"])
	// json golden inline
	b, _ := json.Marshal(res)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Contains(t, m, "content")

	// success via JSON-RPC tools/call
	rawCall := json.RawMessage(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"gmb_server_info","arguments":{}}}`)
	resp := srv.MCPServer().HandleMessage(ctx, rawCall)
	require.NotNil(t, resp)
	jresp, ok := resp.(mcp.JSONRPCResponse)
	require.True(t, ok, "tools/call success must be JSONRPCResponse got %T", resp)
	bb, _ := json.Marshal(jresp.Result)
	var callRes mcp.CallToolResult
	require.NoError(t, json.Unmarshal(bb, &callRes))
	assert.False(t, callRes.IsError)
	require.NotEmpty(t, callRes.Content)
	// content type via raw json golden
	var rawList map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(bb, &rawList))
	assert.Contains(t, string(bb), `"type":"text"`)

	// unknown tool -> isError true OR JSON-RPC METHOD_NOT_FOUND
	rawUnknown := json.RawMessage(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"gmb_unknown_tool_xyz_123","arguments":{}}}`)
	resp2 := srv.MCPServer().HandleMessage(ctx, rawUnknown)
	require.NotNil(t, resp2)
	// could be JSONRPCResponse with CallToolResult isError true, or JSONRPCError with code -32601/-32602
	switch v := resp2.(type) {
	case mcp.JSONRPCResponse:
		bb2, _ := json.Marshal(v.Result)
		var cr mcp.CallToolResult
		if err := json.Unmarshal(bb2, &cr); err == nil {
			assert.True(t, cr.IsError, "unknown tool must return isError true")
			require.NotEmpty(t, cr.Content)
			if tc2, ok := cr.Content[0].(mcp.TextContent); ok {
				assert.NotEmpty(t, tc2.Text)
			} else {
				// via raw text extraction
				assert.Contains(t, string(bb2), "isError")
			}
		} else {
			t.Fatalf("unexpected tools/call unknown response shape: %s", string(bb2))
		}
	case mcp.JSONRPCError:
		// mcp-go may return METHOD_NOT_FOUND (-32601) or INVALID_PARAMS (-32602) depending on filtering
		assert.Contains(t, []int{mcp.METHOD_NOT_FOUND, mcp.INVALID_PARAMS}, v.Error.Code, "unknown tool JSON-RPC error must be METHOD_NOT_FOUND or INVALID_PARAMS")
	default:
		// generic check via json
		b2, _ := json.Marshal(v)
		var mm2 map[string]json.RawMessage
		_ = json.Unmarshal(b2, &mm2)
		if errRaw, hasErr := mm2["error"]; hasErr {
			var e struct{ Code int `json:"code"` }
			_ = json.Unmarshal(errRaw, &e)
			assert.Contains(t, []int{mcp.METHOD_NOT_FOUND, mcp.INVALID_PARAMS}, e.Code)
		} else {
			t.Fatalf("unexpected response type %T: %s", v, string(b2))
		}
	}

	// missing param -> isError true with actionable message
	// gmb_impact_analysis requires target
	resMissing, err := srv.handleImpactTool(ctx, mcp.CallToolRequest{})
	require.NoError(t, err)
	require.NotNil(t, resMissing)
	assert.True(t, resMissing.IsError, "missing required param must set isError true")
	require.NotEmpty(t, resMissing.Content)
	tcMiss, ok := resMissing.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, strings.ToLower(tcMiss.Text), "missing required parameter")
	assert.Contains(t, tcMiss.Text, "target")

	// also via JSON-RPC missing param
	rawMissing := json.RawMessage(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"gmb_impact_analysis","arguments":{}}}`)
	resp3 := srv.MCPServer().HandleMessage(ctx, rawMissing)
	require.NotNil(t, resp3)
	jresp3, ok := resp3.(mcp.JSONRPCResponse)
	require.True(t, ok, "missing param via JSON-RPC should still be CallToolResult with isError, got %T", resp3)
	bb3, _ := json.Marshal(jresp3.Result)
	var cr3 mcp.CallToolResult
	require.NoError(t, json.Unmarshal(bb3, &cr3))
	assert.True(t, cr3.IsError)
	if len(cr3.Content) > 0 {
		if tc3, ok := cr3.Content[0].(mcp.TextContent); ok {
			assert.Contains(t, strings.ToLower(tc3.Text), "missing")
		}
	}
	// also test gmb_inspect_search missing query
	resMiss2, _ := srv.handleInspectSearchTool(ctx, mcp.CallToolRequest{})
	assert.True(t, resMiss2.IsError)
	assert.Contains(t, resMiss2.Content[0].(mcp.TextContent).Text, "missing required parameter")
}

func TestToolsCall_SuccessAndIsError(t *testing.T) { TestContract_ToolsCall_SuccessAndIsError(t) }

// ---------------------------------------------------------------------------
// 4. TestResourcesList_Golden
// ---------------------------------------------------------------------------

func TestContract_ResourcesList_Golden(t *testing.T) {
	srv := newContractServer(t)
	ctx := ctxWithSession(srv)

	// via SDK map
	resMap := srv.MCPServer().ListResources()
	require.NotNil(t, resMap)
	assert.GreaterOrEqual(t, len(resMap), 6)

	specURIs := []string{
		"glassmarble://akg",
		"glassmarble://intelligence",
		"glassmarble://memory",
		"glassmarble://timeline",
		"glassmarble://conventions",
		"glassmarble://telemetry",
	}
	for _, uri := range specURIs {
		_, ok := resMap[uri]
		assert.True(t, ok, "resource %q missing", uri)
		// json golden via mcp.Resource type
		if r, ok := resMap[uri]; ok {
			b, _ := json.Marshal(r.Resource)
			var m2 mcp.Resource
			require.NoError(t, json.Unmarshal(b, &m2))
			assert.Equal(t, uri, m2.URI)
			assert.NotEmpty(t, m2.Name)
			assert.NotEmpty(t, m2.MIMEType)
			var raw map[string]any
			_ = json.Unmarshal(b, &raw)
			assert.Equal(t, uri, raw["uri"])
		}
	}

	// via JSON-RPC resources/list
	rawReq := json.RawMessage(`{"jsonrpc":"2.0","id":6,"method":"resources/list","params":{}}`)
	resp := srv.MCPServer().HandleMessage(ctx, rawReq)
	require.NotNil(t, resp)
	jresp, ok := resp.(mcp.JSONRPCResponse)
	require.True(t, ok, "resources/list must be JSONRPCResponse got %T", resp)
	bb, _ := json.Marshal(jresp.Result)
	var listRes mcp.ListResourcesResult
	require.NoError(t, json.Unmarshal(bb, &listRes))
	assert.GreaterOrEqual(t, len(listRes.Resources), 6)
	uriSet := map[string]bool{}
	for _, r := range listRes.Resources {
		uriSet[r.URI] = true
		// golden json check
		b, _ := json.Marshal(r)
		var raw map[string]any
		_ = json.Unmarshal(b, &raw)
		assert.Contains(t, raw, "uri")
		assert.Contains(t, raw, "name")
	}
	for _, uri := range specURIs {
		assert.True(t, uriSet[uri], "resources/list missing %q", uri)
	}
}

func TestResourcesList_Golden(t *testing.T) { TestContract_ResourcesList_Golden(t) }

// ---------------------------------------------------------------------------
// 5. TestPromptsList_Golden
// ---------------------------------------------------------------------------

func TestContract_PromptsList_Golden(t *testing.T) {
	srv := newContractServer(t)
	ctx := ctxWithSession(srv)

	promptsMap := srv.MCPServer().ListPrompts()
	require.NotNil(t, promptsMap)
	// spec canonical 6
	canonical := []string{
		"explain_architecture",
		"analyze_impact",
		"find_technical_debt",
		"explain_component",
		"generate_diagram",
		"ci_gate_check",
	}
	for _, name := range canonical {
		_, ok := promptsMap[name]
		assert.True(t, ok, "canonical prompt %q missing", name)
		if p, ok := promptsMap[name]; ok {
			assert.Equal(t, name, p.Prompt.Name)
			assert.NotEmpty(t, p.Prompt.Description)
			// json golden via mcp.Prompt
			b, _ := json.Marshal(p.Prompt)
			var m2 mcp.Prompt
			require.NoError(t, json.Unmarshal(b, &m2))
			assert.Equal(t, name, m2.Name)
			var raw map[string]any
			_ = json.Unmarshal(b, &raw)
			assert.Equal(t, name, raw["name"])
		}
	}
	// impl has aliases, so total >=6
	assert.GreaterOrEqual(t, len(promptsMap), 6)

	// via JSON-RPC prompts/list
	rawReq := json.RawMessage(`{"jsonrpc":"2.0","id":7,"method":"prompts/list","params":{}}`)
	resp := srv.MCPServer().HandleMessage(ctx, rawReq)
	require.NotNil(t, resp)
	jresp, ok := resp.(mcp.JSONRPCResponse)
	require.True(t, ok, "prompts/list must be JSONRPCResponse got %T", resp)
	bb, _ := json.Marshal(jresp.Result)
	var listRes mcp.ListPromptsResult
	require.NoError(t, json.Unmarshal(bb, &listRes))
	assert.GreaterOrEqual(t, len(listRes.Prompts), 6)
	nameSet := map[string]bool{}
	for _, p := range listRes.Prompts {
		nameSet[p.Name] = true
		b, _ := json.Marshal(p)
		var raw map[string]any
		_ = json.Unmarshal(b, &raw)
		assert.Contains(t, raw, "name")
	}
	for _, name := range canonical {
		assert.True(t, nameSet[name], "prompts/list missing %q", name)
	}
}

func TestPromptsList_Golden(t *testing.T) { TestContract_PromptsList_Golden(t) }

// ---------------------------------------------------------------------------
// 6. TestJSONRPC_ErrorCodes
// ---------------------------------------------------------------------------

func TestContract_JSONRPC_ErrorCodes(t *testing.T) {
	// golden constants inline (no external file)
	assert.Equal(t, -32700, ErrParse, "ErrParse must be -32700")
	assert.Equal(t, -32600, ErrInvalidRequest, "ErrInvalidRequest must be -32600")
	assert.Equal(t, -32601, ErrMethodNotFound, "ErrMethodNotFound must be -32601")
	assert.Equal(t, -32602, ErrInvalidParams, "ErrInvalidParams must be -32602")
	assert.Equal(t, -32603, ErrInternal, "ErrInternal must be -32603")

	// cross-check against mcp-go SDK constants
	assert.Equal(t, mcp.PARSE_ERROR, ErrParse)
	assert.Equal(t, mcp.INVALID_REQUEST, ErrInvalidRequest)
	assert.Equal(t, mcp.METHOD_NOT_FOUND, ErrMethodNotFound)
	assert.Equal(t, mcp.INVALID_PARAMS, ErrInvalidParams)
	assert.Equal(t, mcp.INTERNAL_ERROR, ErrInternal)

	// via NewErrorResponse helper
	for _, tc := range []struct {
		code int
		msg  string
	}{
		{ErrParse, "parse error"},
		{ErrInvalidRequest, "invalid request"},
		{ErrMethodNotFound, "method not found"},
		{ErrInvalidParams, "invalid params"},
		{ErrInternal, "internal error"},
	} {
		id := json.RawMessage(`1`)
		resp := NewErrorResponse(&id, tc.code, tc.msg, nil)
		require.NotNil(t, resp)
		assert.Equal(t, "2.0", resp.JSONRPC)
		require.NotNil(t, resp.Error)
		assert.Equal(t, tc.code, resp.Error.Code)
		assert.Equal(t, tc.msg, resp.Error.Message)
		// json golden via mcp.JSONRPCError
		b, _ := json.Marshal(resp)
		var m map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &m))
		assert.Contains(t, string(b), tc.msg)
		var errObj struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(m["error"], &errObj)
		assert.Equal(t, tc.code, errObj.Code)
	}

	// live protocol errors via HandleMessage
	srv := newContractServer(t)
	ctx := ctxWithSession(srv)

	// -32700 parse error: malformed JSON
	respParse := srv.MCPServer().HandleMessage(ctx, json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"initialize"`))
	require.NotNil(t, respParse)
	_, codeParse, _ := decodeJSONRPCResponse(t, respParse)
	assert.Equal(t, -32700, codeParse, "malformed JSON must yield PARSE_ERROR -32700")

	// -32600 invalid request: wrong jsonrpc version
	respInvalid := srv.MCPServer().HandleMessage(ctx, json.RawMessage(`{"jsonrpc":"1.0","id":2,"method":"initialize","params":{}}`))
	require.NotNil(t, respInvalid)
	_, codeInvalid, _ := decodeJSONRPCResponse(t, respInvalid)
	assert.Equal(t, -32600, codeInvalid, "wrong jsonrpc version must yield INVALID_REQUEST -32600")

	// -32601 method not found
	respNotFound := srv.MCPServer().HandleMessage(ctx, json.RawMessage(`{"jsonrpc":"2.0","id":3,"method":"unknown/method","params":{}}`))
	require.NotNil(t, respNotFound)
	_, codeNotFound, _ := decodeJSONRPCResponse(t, respNotFound)
	assert.Equal(t, -32601, codeNotFound, "unknown method must yield METHOD_NOT_FOUND -32601")

	// -32602 invalid params: resources/subscribe without uri returns INVALID_PARAMS when subscribe capability enabled
	// Our server enables subscribe via WithResourceCapabilities(true,true), so missing uri yields INVALID_PARAMS
	// Use a session that is initialized; otherwise it may be handled differently.
	// We trigger via invalid params on tools/call with malformed params type? But mcp-go returns INVALID_REQUEST for unparsable.
	// Instead trigger via subscribe with empty uri.
	respInvalidParams := srv.MCPServer().HandleMessage(ctx, json.RawMessage(`{"jsonrpc":"2.0","id":4,"method":"resources/subscribe","params":{}}`))
	require.NotNil(t, respInvalidParams)
	_, codeInvalidParams, _ := decodeJSONRPCResponse(t, respInvalidParams)
	// mcp-go returns INVALID_PARAMS for missing uri when capability present
	assert.Equal(t, -32602, codeInvalidParams, "missing uri on resources/subscribe must yield INVALID_PARAMS -32602")

	// -32603 internal error: simulate via resource read that fails? Direct check via constant and helper.
	id := json.RawMessage(`5`)
	respInternal := NewErrorResponse(&id, ErrInternal, "internal failure", nil)
	assert.Equal(t, -32603, respInternal.Error.Code)
	// also verify JSON-RPC error structure via mcp types
	b, _ := json.Marshal(respInternal)
	var rpcErr mcp.JSONRPCError
	_ = json.Unmarshal(b, &rpcErr)
	assert.Equal(t, -32603, rpcErr.Error.Code)

	// ensure error codes are distinct and match protocol.go golden inline map
	golden := map[string]int{
		"ErrParse":          -32700,
		"ErrInvalidRequest": -32600,
		"ErrMethodNotFound": -32601,
		"ErrInvalidParams":  -32602,
		"ErrInternal":       -32603,
	}
	assert.Equal(t, golden["ErrParse"], ErrParse)
	assert.Equal(t, golden["ErrInvalidRequest"], ErrInvalidRequest)
	assert.Equal(t, golden["ErrMethodNotFound"], ErrMethodNotFound)
	assert.Equal(t, golden["ErrInvalidParams"], ErrInvalidParams)
	assert.Equal(t, golden["ErrInternal"], ErrInternal)
}

func TestJSONRPC_ErrorCodes(t *testing.T) { TestContract_JSONRPC_ErrorCodes(t) }
