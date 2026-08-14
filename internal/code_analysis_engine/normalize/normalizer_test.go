package normalize

import (
	"reflect"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
)

func TestDispatcherReturnsAllLanguages(t *testing.T) {
	langs := []ingest.SupportedLang{
		ingest.LangGo, ingest.LangPython, ingest.LangJava,
		ingest.LangJS, ingest.LangTS, ingest.LangC,
		ingest.LangCpp, ingest.LangCSharp, ingest.LangRuby,
		ingest.LangPHP, ingest.LangRust, ingest.LangCSS,
		ingest.LangHTML, ingest.LangJSON,
	}
	for _, lang := range langs {
		tr := Dispatcher(lang)
		if tr == nil {
			t.Errorf("Dispatcher(%s) returned nil", lang)
		}
	}
}

func TestDispatcherGenericFallback(t *testing.T) {
	tr := Dispatcher(ingest.LangUnknown)
	if tr == nil {
		t.Fatal("Dispatcher(unknown) returned nil")
	}
	gt, ok := tr.(*GenericTranslator)
	if !ok {
		t.Fatalf("Dispatcher(unknown) = %T, want *GenericTranslator", tr)
	}
	if gt.Lang != ingest.LangUnknown {
		t.Errorf("GenericTranslator.Lang = %s, want unknown", gt.Lang)
	}
}

func TestModuleFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"internal/auth/service.go", "internal.auth.service"},
		{"main.go", "main"},
		{"/absolute/path/file.py", "absolute.path.file"},
		{"a/b/c/d.ts", "a.b.c.d"},
		{"single_file.js", "single_file"},
		{"", "main"},
		{".", "main"},
		{"with-hyphen/foo.go", "with_hyphen.foo"},
	}
	for _, tt := range tests {
		got := moduleFromPath(tt.path)
		if got != tt.want {
			t.Errorf("moduleFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestCleanImportPath(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{`import "fmt"`, "fmt"},
		{`import "net/http"`, "net/http"},
		{"import (", "("},
		{"using System.Collections.Generic;", "System.Collections.Generic"},
		{"#include <stdio.h>", "stdio.h"},
		{"require 'active_record'", "active_record"},
		{"from datetime import date", "datetime import date"},
		{`include "mylib.h"`, "mylib.h"},
		{"extern crate serde;", "serde"},
		{"", ""},
		{"   fmt  ", "fmt"},
	}
	for _, tt := range tests {
		got := cleanImportPath(tt.raw)
		if got != tt.want {
			t.Errorf("cleanImportPath(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestBaseNode(t *testing.T) {
	tok := ingest.RichToken{
		Kind:      ingest.TokenDeclaration,
		Type:      "function_declaration",
		Name:      "myFunc",
		Content:   "func myFunc() int { return 0 }",
		StartLine: 10,
		EndLine:   15,
		StartByte: 100,
		EndByte:   200,
	}
	node := baseNode(tok, "src/main.go")
	if node == nil {
		t.Fatal("baseNode returned nil")
	}
	if node.Name != "myFunc" {
		t.Errorf("Name = %q, want %q", node.Name, "myFunc")
	}
	// baseNode doesn't set Type â€” that's the translator's job
	if node.Kind != "function_declaration" {
		t.Errorf("Kind = %q, want %q", node.Kind, "function_declaration")
	}
	if node.StartLine != 10 {
		t.Errorf("StartLine = %d, want 10", node.StartLine)
	}
	if node.Properties["file_path"] != "src/main.go" {
		t.Errorf("file_path = %q, want %q", node.Properties["file_path"], "src/main.go")
	}
	if node.Properties["module_name"] != "src.main" {
		t.Errorf("module_name = %q, want %q", node.Properties["module_name"], "src.main")
	}
}

func TestSetDeclarationFQN(t *testing.T) {
	node := &GASTNode{
		Name:       "MyStruct",
		Properties: map[string]string{},
	}
	setDeclarationFQN(node, "src/models/entity.go", "MyStruct")
	wantFQN := "src.models.entity.MyStruct"
	if node.ID != wantFQN {
		t.Errorf("ID = %q, want %q", node.ID, wantFQN)
	}
	if node.Name != wantFQN {
		t.Errorf("Name = %q, want %q", node.Name, wantFQN)
	}
	if node.Properties["fully_qualified_name"] != wantFQN {
		t.Errorf("FQN = %q, want %q", node.Properties["fully_qualified_name"], wantFQN)
	}
}

func TestParseReceiverAndMethod(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantRecv   string
		wantMethod string
	}{
		{"foo.Bar", "", "foo", "Bar"},
		{"some.pkg.Func", "", "some", "Func"},
		{"", "client.GetName(ctx)", "client", "GetName"},
		{"", `fmt.Sprintf("hello")`, "fmt", "Sprintf"},
		{"", "strings.TrimSpace(s)", "strings", "TrimSpace"},
		{"simpleCall", "", "", "simpleCall"},
		{"", "", "", ""},
	}
	for _, tt := range tests {
		recv, method := parseReceiverAndMethod(tt.name, tt.content)
		if recv != tt.wantRecv || method != tt.wantMethod {
			t.Errorf("parseReceiverAndMethod(%q, %q) = (%q, %q), want (%q, %q)",
				tt.name, tt.content, recv, method, tt.wantRecv, tt.wantMethod)
		}
	}
}

func TestDetectBehavioralPrimitives(t *testing.T) {
	tests := []struct {
		content string
		name    string
		want    []BehavioralPrimitive
	}{
		{"db.Query(\"SELECT * FROM users\")", "queryUsers", []BehavioralPrimitive{PrimDatabaseSQL}},
		{"redis.Get(\"key\")", "getCache", []BehavioralPrimitive{PrimCache}},
		{"http.Get(\"https://api.example.com\")", "fetchData", []BehavioralPrimitive{PrimNetworkIO}},
		{"log.Printf(\"hello\")", "logHello", []BehavioralPrimitive{PrimLogging}},
		{"jwt.Sign(token)", "authUser", []BehavioralPrimitive{PrimSecurityAuth}},
		{"bcrypt.GenerateFromPassword(pw, 10)", "hashPass", []BehavioralPrimitive{PrimCrypto}},
		{"// just a comment", "doNothing", nil},
		{"x + y", "add", nil},
		{"go doWork()", "startWorker", nil},
		{"kafka.Produce(msg)", "sendEvent", []BehavioralPrimitive{PrimMessageQueue}},
		{"aws_s3.PutObject(bucket, key, data)", "uploadFile", []BehavioralPrimitive{PrimCloudSDK}},
		{"lock.Lock()", "acquireLock", []BehavioralPrimitive{PrimSynchronization}},
	}
	for _, tt := range tests {
		got := DetectBehavioralPrimitives(tt.content, tt.name)
		if !primitiveSetsMatch(got, tt.want) {
			t.Errorf("DetectBehavioralPrimitives(%q, %q) = %v, want %v", tt.content, tt.name, got, tt.want)
		}
	}
}

func TestNormalizeDataType(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"", "void"},
		{"int", "integer"},
		{"int32", "integer"},
		{"i64", "integer"},
		{"float64", "number"},
		{"double", "number"},
		{"string", "string"},
		{"std::string", "string"},
		{"bool", "boolean"},
		{"[]byte", "array"},
		{"map[string]int", "map"},
		{"void", "void"},
		{"Promise<string>", "async_handle"},
		{"chan int", "async_handle"},
		{"nil", "void"},
		{"MyCustomType", "MyCustomType"},
	}
	for _, tt := range tests {
		got := NormalizeDataType(tt.raw)
		if got != tt.want {
			t.Errorf("NormalizeDataType(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestPropagatePrimitives(t *testing.T) {
	root := &GASTNode{
		Type: GASTFileRoot,
		Name: "file.go",
		Children: []*GASTNode{
			{
				Type:       GASTFunction,
				Name:       "doWork",
				Primitives: []BehavioralPrimitive{PrimNetworkIO},
				Children: []*GASTNode{
					{
						Type:       GASTCallExpression,
						Name:       "http.Get",
						Primitives: []BehavioralPrimitive{PrimNetworkIO, PrimDiskIO},
					},
				},
			},
		},
	}

	PropagatePrimitives(root)

	// Root should have nil primitives (GASTFileRoot is excluded)
	if root.Primitives != nil {
		t.Errorf("GASTFileRoot should have nil primitives, got %v", root.Primitives)
	}

	// Function should have aggregated primitives
	fn := root.Children[0]
	if len(fn.Primitives) == 0 {
		t.Error("GASTFunction should have aggregated primitives")
	}
	if fn.Properties["has_behavioral_primitives"] != "true" {
		t.Error("GASTFunction should have has_behavioral_primitives=true")
	}
}

func TestDetermineArchitectureTier(t *testing.T) {
	tests := []struct {
		prims []BehavioralPrimitive
		want  string
	}{
		{[]BehavioralPrimitive{PrimDatabaseSQL}, "DataLayer"},
		{[]BehavioralPrimitive{PrimNetworkIO}, "InterfaceLayer"},
		{[]BehavioralPrimitive{PrimUIEvent}, "InterfaceLayer"},
		{[]BehavioralPrimitive{PrimCache}, "InfrastructureLayer"},
		{[]BehavioralPrimitive{PrimMessageQueue}, "InfrastructureLayer"},
		{[]BehavioralPrimitive{PrimComputeMath}, "DomainLayer"},
		{nil, "DomainLayer"},
	}
	for _, tt := range tests {
		got := determineArchitectureTier(tt.prims)
		if got != tt.want {
			t.Errorf("determineArchitectureTier(%v) = %q, want %q", tt.prims, got, tt.want)
		}
	}
}

func TestDetectAntiPatterns(t *testing.T) {
	uiAndDB := detectAntiPatterns([]BehavioralPrimitive{PrimUIEvent, PrimDatabaseSQL})
	if len(uiAndDB) == 0 {
		t.Error("UIEvent + DatabaseSQL should produce anti-pattern warning")
	}

	diskAndNet := detectAntiPatterns([]BehavioralPrimitive{PrimDiskIO, PrimNetworkIO})
	if len(diskAndNet) == 0 {
		t.Error("DiskIO + NetworkIO should produce anti-pattern warning")
	}

	cryptNoAuth := detectAntiPatterns([]BehavioralPrimitive{PrimCrypto})
	if len(cryptNoAuth) == 0 {
		t.Error("Crypto without Auth should produce anti-pattern warning")
	}

	noPattern := detectAntiPatterns([]BehavioralPrimitive{PrimLogging})
	if len(noPattern) > 0 {
		t.Errorf("Logging alone should not produce anti-patterns, got %v", noPattern)
	}
}

func TestExtractGenericTypesAndDecorators(t *testing.T) {
	node := &GASTNode{Properties: make(map[string]string)}
	extractGenericTypesAndDecorators(node, "func Foo[T any](x T) T")
	if node.Properties["type_params"] == "" {
		t.Error("type_params should be extracted from generic function")
	}

	node2 := &GASTNode{Properties: make(map[string]string)}
	extractGenericTypesAndDecorators(node2, "@RestController")
	if len(node2.Annotations) == 0 {
		t.Error("annotations should be extracted")
	}

	node3 := &GASTNode{Properties: make(map[string]string)}
	extractGenericTypesAndDecorators(node3, "async Task<int> DoWork()")
	if node3.Properties["is_async"] != "true" {
		t.Error("is_async should be true for async method")
	}
}

func TestCalculateRiskScore(t *testing.T) {
	if score := calculateRiskScore([]BehavioralPrimitive{PrimCrypto}); score != 30 {
		t.Errorf("Crypto risk score = %d, want 30", score)
	}
	if score := calculateRiskScore([]BehavioralPrimitive{PrimLogging}); score != 5 {
		t.Errorf("Logging risk score = %d, want 5", score)
	}
	if score := calculateRiskScore([]BehavioralPrimitive{PrimDatabaseSQL, PrimCrypto}); score != 50 {
		t.Errorf("DB+Crypto risk score = %d, want 50", score)
	}
	score := calculateRiskScore(nil)
	if score != 0 {
		t.Errorf("empty risk score = %d, want 0", score)
	}
}

func TestGetRiskLevel(t *testing.T) {
	tests := []struct {
		score int
		want  string
	}{
		{0, "LOW"},
		{19, "LOW"},
		{20, "MEDIUM"},
		{39, "MEDIUM"},
		{40, "HIGH"},
		{59, "HIGH"},
		{60, "CRITICAL"},
		{100, "CRITICAL"},
	}
	for _, tt := range tests {
		got := getRiskLevel(tt.score)
		if got != tt.want {
			t.Errorf("getRiskLevel(%d) = %q, want %q", tt.score, got, tt.want)
		}
	}
}

func primitiveSetsMatch(a, b []BehavioralPrimitive) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[BehavioralPrimitive]int)
	for _, p := range a {
		seen[p]++
	}
	for _, p := range b {
		seen[p]--
		if seen[p] < 0 {
			return false
		}
	}
	return true
}

func TestNormalizeOutputStructure(t *testing.T) {
	payload := &NormalizeOutput{
		CommitHash:        "abc123",
		UpsertedTrees:     make(map[string]*GASTNode),
		LocalSymbolTables: make(map[string]*FileSymbolTable),
	}
	payload.UpsertedTrees["main.go"] = &GASTNode{
		Type: GASTFileRoot,
		Name: "main.go",
	}
	payload.LocalSymbolTables["main.go"] = &FileSymbolTable{
		FilePath: "/root/main.go",
		RelPath:  "main.go",
		Language: ingest.LangGo,
	}

	if payload.CommitHash != "abc123" {
		t.Errorf("CommitHash = %q, want abc123", payload.CommitHash)
	}
	if len(payload.UpsertedTrees) != 1 {
		t.Errorf("len(UpsertedTrees) = %d, want 1", len(payload.UpsertedTrees))
	}
	ft := payload.UpsertedTrees["main.go"]
	if ft.Type != GASTFileRoot {
		t.Errorf("GASTNode.Type = %s, want FILE_ROOT", ft.Type)
	}
}

func TestFileSymbolTable(t *testing.T) {
	st := &FileSymbolTable{
		FilePath:    "/root/main.go",
		RelPath:     "main.go",
		Language:    ingest.LangGo,
		PackageName: "main",
		Imports:     []string{"fmt", "net/http"},
		Definitions: []SymbolMeta{
			{Name: "main", Kind: "function_declaration", Visibility: "exported"},
		},
		LocalCalls: []CallSite{
			{CallerNodeID: "main.go::declaration::main::L1", MethodName: "Println"},
		},
	}
	if len(st.Imports) != 2 {
		t.Errorf("len(Imports) = %d, want 2", len(st.Imports))
	}
	if st.PackageName != "main" {
		t.Errorf("PackageName = %q, want main", st.PackageName)
	}
	// Verify all optional fields are appendable (nil slices are valid in Go)
	if st.Endpoints == nil {
		// nil slice is fine, just verify append works
		_ = append(st.Endpoints, EndpointMeta{})
	}
	if st.SecuritySinks == nil {
		_ = append(st.SecuritySinks, SecuritySinkMeta{})
	}
}

// TestDetectInheritanceMultiParent (GAP-M-06 / §5.2.2): a class with
// several comma-separated bases must record one InheritanceMeta per parent,
// and C++ visibility keywords must not leak into the parent names.
func TestDetectInheritanceMultiParent(t *testing.T) {
	st := &FileSymbolTable{}

	detectInheritance("public class A extends B, C, D {", "A", 1, st)
	detectInheritance("interface I extends J, K {", "I", 2, st)
	detectInheritance("class X implements P, Q {", "X", 3, st)
	detectInheritance("class Y : public Base1, protected Base2 {", "Y", 4, st)

	var got []string
	for _, inh := range st.Inheritances {
		got = append(got, inh.ChildName+">"+inh.ParentName)
		if inh.ChildName == "Y" && inh.ParentName == "Base2" && !inh.IsInterface {
			// C++ colon inheritance is class inheritance unless the
			// keyword is interface-like.
		}
	}
	want := []string{
		"A>B", "A>C", "A>D",
		"I>J", "I>K",
		"X>P", "X>Q",
		"Y>Base1", "Y>Base2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("detectInheritance parents = %v, want %v", got, want)
	}
}

// TestDetectInheritanceSkipsTypeAnnotations (GAP-M-06): Python-style type
// hints (`field: Type`) must not be misread as inheritance.
func TestDetectInheritanceSkipsTypeAnnotations(t *testing.T) {
	st := &FileSymbolTable{}
	detectInheritance("var: Dict[str, int] = {}", "var", 1, st)
	detectInheritance("def f(x: int) -> str:", "f", 2, st)

	if len(st.Inheritances) != 0 {
		t.Errorf("type annotations must not create inheritances, got %v", st.Inheritances)
	}
}
