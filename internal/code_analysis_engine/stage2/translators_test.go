package stage2

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

func TestGoTranslatorImport(t *testing.T) {
	tr := &GoTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenImport,
		Type:    "import_declaration",
		Content: `"fmt"`,
		Name:    "fmt",
	}
	node := tr.CoerceToken(tok, "main.go")
	if node == nil {
		t.Fatal("GoTranslator returned nil")
	}
	if node.Type != GASTImport {
		t.Errorf("Type = %s, want IMPORT", node.Type)
	}
	if node.Name != "fmt" {
		t.Errorf("Name = %q, want fmt", node.Name)
	}
}

func TestGoTranslatorFunction(t *testing.T) {
	tr := &GoTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "function_declaration",
		Content: "func main() { }",
		Name:    "main",
	}
	node := tr.CoerceToken(tok, "main.go")
	if node.Type != GASTFunction {
		t.Errorf("Type = %s, want FUNCTION", node.Type)
	}
	if node.Visibility != "internal" {
		t.Errorf("Visibility = %s, want internal", node.Visibility)
	}
	if !strings.Contains(node.ID, "main") {
		t.Errorf("ID = %q, should contain main", node.ID)
	}
}

func TestGoTranslatorPrivateFunction(t *testing.T) {
	tr := &GoTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "function_declaration",
		Content: "func helper() { }",
		Name:    "helper",
	}
	node := tr.CoerceToken(tok, "main.go")
	if node.Visibility != "internal" {
		t.Errorf("Visibility = %s, want internal", node.Visibility)
	}
}

func TestGoTranslatorTypeDeclaration(t *testing.T) {
	tr := &GoTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "type_spec",
		Content: "type Person struct { Name string }",
		Name:    "Person",
	}
	node := tr.CoerceToken(tok, "models/person.go")
	if node.Type != GASTTypeDeclaration {
		t.Errorf("Type = %s, want TYPE_DECLARATION", node.Type)
	}
	if node.Visibility != "public" {
		t.Errorf("Visibility = %s, want public", node.Visibility)
	}
}

func TestGoTranslatorMethod(t *testing.T) {
	tr := &GoTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "method_declaration",
		Content: "func (p *Person) Greet() string { }",
		Name:    "Greet",
	}
	node := tr.CoerceToken(tok, "models/person.go")
	if node.Type != GASTFunction {
		t.Errorf("Type = %s, want FUNCTION", node.Type)
	}
	if node.ReceiverType != "Person" {
		t.Errorf("ReceiverType = %q, want Person", node.ReceiverType)
	}
	if node.Visibility != "public" {
		t.Errorf("Visibility = %s, want public", node.Visibility)
	}
}

func TestGoTranslatorPackage(t *testing.T) {
	tr := &GoTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "package_clause",
		Content: "package main",
		Name:    "main",
	}
	node := tr.CoerceToken(tok, "main.go")
	if node.Type != GASTNamespace {
		t.Errorf("Type = %s, want NAMESPACE", node.Type)
	}
}

func TestGoTranslatorCall(t *testing.T) {
	tr := &GoTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenCall,
		Type:    "call_expression",
		Content: "fmt.Println(\"hello\")",
		Name:    "fmt.Println",
	}
	node := tr.CoerceToken(tok, "main.go")
	if node.Type != GASTCallExpression {
		t.Errorf("Type = %s, want CALL_EXPRESSION", node.Type)
	}
}

func TestPythonTranslatorImport(t *testing.T) {
	tr := &PythonTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenImport,
		Type:    "import_statement",
		Content: "import os",
		Name:    "os",
	}
	node := tr.CoerceToken(tok, "main.py")
	if node.Type != GASTImport {
		t.Errorf("Type = %s, want IMPORT", node.Type)
	}
}

func TestPythonTranslatorClass(t *testing.T) {
	tr := &PythonTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "class_definition",
		Content: "class UserModel:",
		Name:    "UserModel",
	}
	node := tr.CoerceToken(tok, "models/user.py")
	if node.Type != GASTTypeDeclaration {
		t.Errorf("Type = %s, want TYPE_DECLARATION", node.Type)
	}
	if node.Kind != "class" {
		t.Errorf("Kind = %q, want class", node.Kind)
	}
	if node.Visibility != "public" {
		t.Errorf("Visibility = %s, want public", node.Visibility)
	}
}

func TestPythonTranslatorFunction(t *testing.T) {
	tr := &PythonTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "function_definition",
		Content: "def get_user():",
		Name:    "get_user",
	}
	node := tr.CoerceToken(tok, "services/user_service.py")
	if node.Type != GASTFunction {
		t.Errorf("Type = %s, want FUNCTION", node.Type)
	}
	if node.Visibility != "public" {
		t.Errorf("Visibility = %s, want public (no underscore prefix)", node.Visibility)
	}
}

func TestPythonTranslatorPrivateFunction(t *testing.T) {
	tr := &PythonTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "function_definition",
		Content: "def __helper():",
		Name:    "__helper",
	}
	node := tr.CoerceToken(tok, "utils.py")
	if node.Visibility != "private" {
		t.Errorf("Visibility = %s, want private (dunder convention)", node.Visibility)
	}
}

func TestPythonTranslatorCall(t *testing.T) {
	tr := &PythonTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenCall,
		Type:    "call",
		Content: "os.getenv(\"HOME\")",
		Name:    "os.getenv",
	}
	node := tr.CoerceToken(tok, "main.py")
	if node.Type != GASTCallExpression {
		t.Errorf("Type = %s, want CALL_EXPRESSION", node.Type)
	}
}

func TestPythonPackageFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"myapp/utils/__init__.py", "myapp.utils"},
		{"myapp/service.py", "myapp.service"},
		{"main.py", "main"},
		{"a/b/c.py", "a.b.c"},
	}
	for _, tt := range tests {
		got := pythonPackageFromPath(tt.path)
		if got != tt.want {
			t.Errorf("pythonPackageFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestJavaTranslatorImport(t *testing.T) {
	tr := &JavaTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenImport,
		Type:    "import_declaration",
		Content: "import java.util.List;",
		Name:    "java.util.List",
	}
	node := tr.CoerceToken(tok, "Main.java")
	if node.Type != GASTImport {
		t.Errorf("Type = %s, want IMPORT", node.Type)
	}
}

func TestJavaTranslatorClass(t *testing.T) {
	tr := &JavaTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "class_declaration",
		Content: "public class UserController {",
		Name:    "UserController",
	}
	node := tr.CoerceToken(tok, "controllers/UserController.java")
	if node.Type != GASTTypeDeclaration {
		t.Errorf("Type = %s, want TYPE_DECLARATION", node.Type)
	}
	if node.Kind != "class" {
		t.Errorf("Kind = %q, want class", node.Kind)
	}
	if node.Visibility != "public" {
		t.Errorf("Visibility = %s, want public", node.Visibility)
	}
}

func TestJavaTranslatorMethod(t *testing.T) {
	tr := &JavaTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "method_declaration",
		Content: "public String getName() {",
		Name:    "getName",
	}
	node := tr.CoerceToken(tok, "UserController.java")
	if node.Type != GASTFunction {
		t.Errorf("Type = %s, want FUNCTION", node.Type)
	}
	if node.Kind != "method" {
		t.Errorf("Kind = %q, want method", node.Kind)
	}
	if node.Visibility != "public" {
		t.Errorf("Visibility = %s, want public", node.Visibility)
	}
}

func TestJavaTranslatorInterface(t *testing.T) {
	tr := &JavaTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "interface_declaration",
		Content: "public interface UserRepository {",
		Name:    "UserRepository",
	}
	node := tr.CoerceToken(tok, "repositories/UserRepository.java")
	if node.Type != GASTTypeDeclaration {
		t.Errorf("Type = %s, want TYPE_DECLARATION", node.Type)
	}
	if node.Kind != "interface" {
		t.Errorf("Kind = %q, want interface", node.Kind)
	}
}

func TestJavaTranslatorPackage(t *testing.T) {
	tr := &JavaTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "package_declaration",
		Content: "package com.example.app;",
		Name:    "com.example.app",
	}
	node := tr.CoerceToken(tok, "Main.java")
	if node.Type != GASTNamespace {
		t.Errorf("Type = %s, want NAMESPACE", node.Type)
	}
}

func TestJavaTranslatorConstructor(t *testing.T) {
	tr := &JavaTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "constructor_declaration",
		Content: "public UserController() {",
		Name:    "UserController",
	}
	node := tr.CoerceToken(tok, "UserController.java")
	if node.Type != GASTFunction {
		t.Errorf("Type = %s, want FUNCTION", node.Type)
	}
	if node.Kind != "constructor" {
		t.Errorf("Kind = %q, want constructor", node.Kind)
	}
}

func TestJavaTranslatorCall(t *testing.T) {
	tr := &JavaTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenCall,
		Type:    "method_invocation",
		Content: "user.getName()",
		Name:    "user.getName",
	}
	node := tr.CoerceToken(tok, "UserController.java")
	if node.Type != GASTCallExpression {
		t.Errorf("Type = %s, want CALL_EXPRESSION", node.Type)
	}
}

func TestResolveGoVisibility(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"main", "internal"},
		{"Main", "public"},
		{"helper", "internal"},
		{"ParseJSON", "public"},
		{"", "internal"},
	}
	for _, tt := range tests {
		got := resolveGoVisibility(tt.name)
		if got != tt.want {
			t.Errorf("resolveGoVisibility(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestResolvePythonVisibility(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"public_func", "public"},
		{"_internal_func", "internal"},
		{"__private_func", "private"},
		{"__init__", "public"},
		{"", "public"},
	}
	for _, tt := range tests {
		got := resolvePythonVisibility(tt.name)
		if got != tt.want {
			t.Errorf("resolvePythonVisibility(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestResolveJavaVisibility(t *testing.T) {
	tests := []struct {
		content string
		want    string
	}{
		{"public class Foo", "public"},
		{"private int x", "private"},
		{"protected void method()", "protected"},
		{"void doSomething()", "internal"},
		{"", "internal"},
	}
	for _, tt := range tests {
		got := resolveJavaVisibility(tt.content)
		if got != tt.want {
			t.Errorf("resolveJavaVisibility(%q) = %q, want %q", tt.content, got, tt.want)
		}
	}
}

func TestGoTranslatorControlFlow(t *testing.T) {
	tr := &GoTranslator{}
	tok := stage1.RawToken{
		Kind: stage1.TokenDeclaration,
		Type: "if_statement",
		Name: "if",
	}
	node := tr.CoerceToken(tok, "main.go")
	if node.Type != GASTControlFlow {
		t.Errorf("Type = %s, want CONTROL_FLOW", node.Type)
	}
}

func TestGoTranslatorInterfaceDetection(t *testing.T) {
	tr := &GoTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "type_spec",
		Content: "type Reader interface { Read() }",
		Name:    "Reader",
	}
	node := tr.CoerceToken(tok, "io/reader.go")
	if node.Kind != "interface" {
		t.Errorf("Kind = %q, want interface", node.Kind)
	}
}

func TestGoTranslatorField(t *testing.T) {
	tr := &GoTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "field_declaration",
		Content: "Name string",
		Name:    "Name",
	}
	node := tr.CoerceToken(tok, "models/person.go")
	if node.Type != GASTField {
		t.Errorf("Type = %s, want FIELD", node.Type)
	}
}

func TestGoTranslatorParameter(t *testing.T) {
	tr := &GoTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "parameter_declaration",
		Content: "x int",
		Name:    "x",
	}
	node := tr.CoerceToken(tok, "main.go")
	if node.Type != GASTParameter {
		t.Errorf("Type = %s, want PARAMETER", node.Type)
	}
}

func TestPythonTranslatorField(t *testing.T) {
	tr := &PythonTranslator{}
	tok := stage1.RawToken{
		Kind:    stage1.TokenDeclaration,
		Type:    "field_definition",
		Content: "self.name = name",
		Name:    "name",
	}
	node := tr.CoerceToken(tok, "models/user.py")
	if node.Type != GASTField {
		t.Errorf("Type = %s, want FIELD", node.Type)
	}
}
