package stage2

import (
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

func Normalize(stage1Out *stage1.StageOutput, commitHash string) (*Stage2Payload, error) {
	payload := &Stage2Payload{
		CommitHash:        commitHash,
		UpsertedTrees:     make(map[string]*GASTNode),
		LocalSymbolTables: make(map[string]*FileSymbolTable),
		DeletedPaths:      nil,
	}

	if stage1Out == nil {
		return payload, nil
	}

	for _, deleted := range stage1Out.Deleted {
		payload.DeletedPaths = append(payload.DeletedPaths, deleted.RelPath)
	}

	// Filter valid results
	var valid []*stage1.IngestionResult
	for _, res := range stage1Out.Updated {
		if res.Error == nil {
			valid = append(valid, res)
		}
	}

	n := len(valid)
	if n == 0 {
		return payload, nil
	}

	// Bounded worker pool: scale dynamically based on CPU cores
	workerCount := runtime.NumCPU() * 2
	if workerCount < 4 {
		workerCount = 4
	}
	if n < workerCount {
		workerCount = n
	}

	type procResult struct {
		relPath  string
		root     *GASTNode
		symTable *FileSymbolTable
	}

	taskCh := make(chan *stage1.IngestionResult, n)
	resultCh := make(chan procResult, n)

	var wg sync.WaitGroup
	wg.Add(workerCount)

	for w := 0; w < workerCount; w++ {
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("stage2: normalizer worker panicked: %v", r)
				}
			}()
			for res := range taskCh {
				root, symTable := processFileResult(res)
				resultCh <- procResult{
					relPath:  res.RelPath,
					root:     root,
					symTable: symTable,
				}
			}
		}()
	}

	for _, res := range valid {
		taskCh <- res
	}
	close(taskCh)

	// Wait in background and close resultCh
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("stage2: result collector panicked: %v", r)
			}
		}()
		wg.Wait()
		close(resultCh)
	}()

	for r := range resultCh {
		payload.UpsertedTrees[r.relPath] = r.root
		payload.LocalSymbolTables[r.relPath] = r.symTable
	}

	return payload, nil
}

// processFileResult builds the GAST tree and FileSymbolTable for a single file.
func processFileResult(res *stage1.IngestionResult) (*GASTNode, *FileSymbolTable) {
	translator := Dispatcher(res.Language)

	root := &GASTNode{
		ID:         res.RelPath,
		Type:       GASTFileRoot,
		Name:       filepath.Base(res.RelPath),
		Kind:       "file",
		StartLine:  1,
		Properties: map[string]string{"language": string(res.Language), "file_path": res.RelPath},
	}

	symTable := &FileSymbolTable{
		FilePath: res.FilePath,
		RelPath:  res.RelPath,
		Language: res.Language,
	}

	nodes := make([]*GASTNode, len(res.RawTokens))
	for i, tok := range res.RawTokens {
		nodes[i] = translator.CoerceToken(tok, res.RelPath)
	}

	pkgName := detectPackageName(res, nodes)
	symTable.PackageName = pkgName

	// Build parent-child tree structure based on ParentIdx
	for i, tok := range res.RawTokens {
		node := nodes[i]
		if node.Properties == nil {
			node.Properties = make(map[string]string)
		}
		node.Properties["file_path"] = res.RelPath

		if node.ReceiverType != "" {
			node.Properties["receiver_type"] = node.ReceiverType
		}

		// Global Fallback Fix: If translator mistakenly left a control flow node as GASTTypeDeclaration
		if node.Type == GASTTypeDeclaration {
			switch strings.ToLower(tok.Type) {
			case "if_statement", "if", "for_statement", "for",
				"switch_statement", "switch", "return_statement", "return",
				"defer", "go", "go_statement", "while_statement", "while",
				"do_statement", "do", "try_statement", "try", "catch_clause",
				"throw", "throw_statement", "raise", "for_each", "foreach",
				"for_in_statement", "for_of_statement":
				node.Type = GASTControlFlow
				node.Visibility = "internal"
			}
		}

		if node.Type == GASTTypeDeclaration || node.Type == GASTFunction {
			ns := pkgName
			if ns == "" {
				ns = filepath.ToSlash(filepath.Dir(res.RelPath))
				if ns == "." {
					ns = "default"
				}
				ns = strings.ReplaceAll(ns, "/", ".")
			}
			node.Namespace = ns

			// Only generate FQN if translator didn't already set it
			if node.Properties["fully_qualified_name"] == "" {
				fqn := ns + "." + node.Name
				if node.ReceiverType != "" {
					fqn = ns + "." + node.ReceiverType + "." + node.Name
				}
				node.Properties["fully_qualified_name"] = fqn
			}

			if node.Visibility == "public" || node.Visibility == "exported" {
				node.Properties["namespace_scope"] = "exported"
			} else {
				node.Properties["namespace_scope"] = "internal"
			}
		}

		if tok.ParentIdx >= 0 && tok.ParentIdx < len(nodes) {
			parent := nodes[tok.ParentIdx]
			parent.Children = append(parent.Children, node)
			// Scope Propagation: children inherit internal/private visibility from their parent
			if parent.Visibility == "private" || parent.Visibility == "internal" || parent.Properties["namespace_scope"] == "internal" {
				node.Visibility = "internal"
				node.Properties["namespace_scope"] = "internal"
			}
		} else {
			root.Children = append(root.Children, node)
		}

		// Symbol table extraction
		if node.Type == GASTImport {
			symTable.Imports = append(symTable.Imports, node.Name)
		} else if node.Type == GASTTypeDeclaration || node.Type == GASTFunction || node.Type == GASTField {
			// Extract all definitions (public, private, internal, fields, methods, classes)
			ns := pkgName
			symTable.Definitions = append(symTable.Definitions, SymbolMeta{
				Name:         node.Name,
				Kind:         node.Kind,
				Namespace:    filepath.ToSlash(ns),
				ReceiverType: node.ReceiverType,
				GASTNodeID:   node.ID,
				Visibility:   node.Visibility,
				DataType:     node.DataType,
				IsAsync:      node.Properties["is_async"] == "true",
				Annotations:  node.Annotations,
			})
		} else if node.Type == GASTCallExpression {
			callerID := findEnclosingFunctionID(nodes, res.RawTokens, tok.ParentIdx, res.RelPath)
			recv, method := parseReceiverAndMethod(node.Name, tok.Content)
			symTable.LocalCalls = append(symTable.LocalCalls, CallSite{
				CallerNodeID: callerID,
				ReceiverName: recv,
				MethodName:   method,
				LineNumber:   int(node.StartLine),
				HasPrimitive: len(node.Primitives) > 0,
				Primitives:   node.Primitives,
				IsAwait:      strings.Contains(tok.Content, "await "),
			})
		}

		// Advanced Semantic Extraction (Step 2.3+)
		extractAdvancedSemantics(node, tok, symTable)
	}

	// Propagate behavioral primitives up the GAST tree
	PropagatePrimitives(root)

	// Apply 8-Pillar Enterprise Intelligence
	ApplyEnterpriseIntelligence(root, symTable, res.RawTokens, nodes)

	return root, symTable
}

func detectPackageName(res *stage1.IngestionResult, nodes []*GASTNode) string {
	for i, tok := range res.RawTokens {
		node := nodes[i]
		if node.Type == GASTNamespace {
			return node.Name
		}
		if tok.Type == "package_clause" || tok.Type == "package_declaration" || tok.Type == "namespace_declaration" {
			parts := strings.Fields(tok.Content)
			if len(parts) >= 2 {
				name := strings.Trim(parts[1], ";\"{}`")
				return name
			}
		}
	}
	dir := filepath.ToSlash(filepath.Dir(res.RelPath))
	if dir == "." || dir == "" {
		return "main"
	}
	return strings.ReplaceAll(dir, "/", ".")
}

func findEnclosingFunctionID(nodes []*GASTNode, rawTokens []stage1.RawToken, parentIdx int, defaultID string) string {
	curr := parentIdx
	for curr >= 0 && curr < len(rawTokens) && curr < len(nodes) {
		if nodes[curr] != nil && nodes[curr].Type == GASTFunction {
			return nodes[curr].ID
		}
		curr = rawTokens[curr].ParentIdx
	}
	return defaultID
}

func parseReceiverAndMethod(name, content string) (string, string) {
	// Handle content-based extraction for complex call expressions
	if content != "" {
		// Extract the called expression before '('
		parenIdx := strings.Index(content, "(")
		if parenIdx != -1 {
			callPart := strings.TrimSpace(content[:parenIdx])
			// Take the rightmost segment (most specific call)
			if lastDot := strings.LastIndex(callPart, "."); lastDot != -1 {
				before := strings.TrimSpace(callPart[:lastDot])
				after := strings.TrimSpace(callPart[lastDot+1:])
				// Extract the rightmost segment as method
				if lastBefore := strings.LastIndex(before, "."); lastBefore != -1 {
					return strings.TrimSpace(before[lastBefore+1:]), after
				}
				return before, after
			}
		}
	}
	// Fall back to name-based splitting
	if strings.Contains(name, ".") {
		parts := strings.Split(name, ".")
		return parts[0], parts[len(parts)-1]
	}
	return "", name
}

func extractAdvancedSemantics(node *GASTNode, tok stage1.RawToken, symTable *FileSymbolTable) {
	content := strings.TrimSpace(tok.Content)
	line := int(node.StartLine)

	// 1. Type Aliases
	if tok.Type == "type_alias" || strings.HasPrefix(content, "typedef ") || (strings.HasPrefix(content, "type ") && strings.Contains(content, "=")) {
		symTable.TypeAliases = append(symTable.TypeAliases, TypeAliasMeta{
			AliasName:  node.Name,
			TargetType: inferTargetType(content),
			LineNumber: line,
		})
	}

	// 2. Inheritances — multi-language detection
	if node.Type == GASTTypeDeclaration {
		detectInheritance(content, node.Name, line, symTable)
	}

	// 3. Instantiations
	if node.Type == GASTCallExpression {
		if strings.HasPrefix(content, "new ") || strings.HasPrefix(content, "make(") {
			symTable.Instantiations = append(symTable.Instantiations, InstantiationMeta{
				ObjectName: node.Name,
				LineNumber: line,
			})
		}
	}

	// 4. Global State
	if node.Type == GASTVariable && tok.ParentIdx == -1 {
		symTable.GlobalState = append(symTable.GlobalState, SymbolMeta{
			Name:       node.Name,
			Kind:       node.Kind,
			DataType:   node.DataType,
			Visibility: node.Visibility,
			GASTNodeID: node.ID,
		})
	}

	// 5. Exceptions
	if strings.HasPrefix(content, "throw ") || strings.HasPrefix(content, "panic(") || strings.HasPrefix(content, "raise ") {
		symTable.Exceptions = append(symTable.Exceptions, ExceptionMeta{
			ExceptionType: inferExceptionType(content),
			Action:        "THROW",
			LineNumber:    line,
		})
	} else if tok.Type == "catch_clause" || strings.HasPrefix(content, "catch ") || strings.HasPrefix(content, "except ") {
		symTable.Exceptions = append(symTable.Exceptions, ExceptionMeta{
			ExceptionType: "Exception",
			Action:        "CATCH",
			LineNumber:    line,
		})
	}

	// 6. Concurrency Spawns
	if isConcurrencySpawn(content) {
		symTable.ConcurrencySpawns = append(symTable.ConcurrencySpawns, SpawnMeta{
			ConcurrencyModel: inferConcurrencyModel(content),
			TargetNodeID:     node.ID,
			LineNumber:       line,
		})
	}

	// 7. Event Hooks
	if node.Type == GASTCallExpression {
		lowerName := strings.ToLower(node.Name)
		if strings.Contains(lowerName, "emit") || strings.Contains(lowerName, "publish") || strings.Contains(lowerName, "dispatch") {
			symTable.EventHooks = append(symTable.EventHooks, EventMeta{
				EventName:  "unknown",
				Action:     "EMIT",
				LineNumber: line,
			})
		} else if strings.Contains(lowerName, "subscribe") || lowerName == "on" || strings.Contains(lowerName, "addeventlistener") {
			symTable.EventHooks = append(symTable.EventHooks, EventMeta{
				EventName:  "unknown",
				Action:     "LISTEN",
				LineNumber: line,
			})
		}
	}

	// 8. Endpoints
	if node.Type == GASTFunction {
		detectEndpointFromAnnotations(node, symTable, line)
	} else if node.Type == GASTCallExpression {
		detectEndpointFromCallPattern(content, symTable, line)
	}

	// 9. Security Sinks
	if strings.Contains(content, "eval(") || strings.Contains(content, "exec(") {
		symTable.SecuritySinks = append(symTable.SecuritySinks, SecuritySinkMeta{
			SinkType:   "Code Injection",
			Severity:   "CRITICAL",
			LineNumber: line,
		})
	} else if strings.Contains(content, "unsafe {") || strings.Contains(content, "unsafe.Pointer") {
		symTable.SecuritySinks = append(symTable.SecuritySinks, SecuritySinkMeta{
			SinkType:   "Memory Unsafe Block",
			Severity:   "HIGH",
			LineNumber: line,
		})
	}

	// 10. Resource Links
	if node.Type == GASTCallExpression && (strings.HasPrefix(content, "require(") || strings.HasPrefix(content, "fs.readFile")) {
		symTable.ResourceLinks = append(symTable.ResourceLinks, ResourceMeta{
			ResourceType: "File Asset",
			ResourcePath: "unknown",
			LineNumber:   line,
		})
	}
}

func detectInheritance(content, childName string, line int, symTable *FileSymbolTable) {
	content = strings.TrimSpace(content)
	// " extends " — Java, TypeScript, PHP
	if idx := strings.Index(content, " extends "); idx != -1 {
		parts := strings.Fields(content[idx+9:])
		if len(parts) > 0 {
			symTable.Inheritances = append(symTable.Inheritances, InheritanceMeta{
				ChildName:   childName,
				ParentName:  strings.Trim(parts[0], "{},;:"),
				IsInterface: false,
				LineNumber:  line,
			})
		}
	}
	// " implements " — Java, TypeScript
	if idx := strings.Index(content, " implements "); idx != -1 {
		parts := strings.Fields(content[idx+12:])
		if len(parts) > 0 {
			symTable.Inheritances = append(symTable.Inheritances, InheritanceMeta{
				ChildName:   childName,
				ParentName:  strings.Trim(parts[0], "{},;:"),
				IsInterface: true,
				LineNumber:  line,
			})
		}
	}
	// Colon-based inheritance " : Base" or " : public Base" — C++, C#
	// But NOT Rust/Python type annotations "var: Type" or "field: Type"
	if colonIdx := strings.Index(content, " : "); colonIdx != -1 {
		afterColon := strings.TrimSpace(content[colonIdx+3:])
		// Skip if it looks like a Python/C# type annotation (no inheritance keywords)
		firstWord := strings.Fields(afterColon)
		if len(firstWord) > 0 {
			candidate := strings.Trim(firstWord[0], "{},;")
			// Only add if the colon pattern looks like inheritance, not a type hint
			if isLikelyInheritance(content, candidate) {
				symTable.Inheritances = append(symTable.Inheritances, InheritanceMeta{
					ChildName:   childName,
					ParentName:  candidate,
					IsInterface: isInterfaceKeyword(content),
					LineNumber:  line,
				})
			}
		}
	}
}

func isLikelyInheritance(content, parentCandidate string) bool {
	// " : public " — C++ inheritance
	if strings.Contains(content, " : public ") || strings.Contains(content, " : private ") || strings.Contains(content, " : protected ") {
		return true
	}
	// Class + ':' followed by uppercase type name (C++/C# convention)
	if parentCandidate != "" && parentCandidate[0] >= 'A' && parentCandidate[0] <= 'Z' {
		// Avoid matching standard type annotations like ": int", ": string", ": bool"
		lower := strings.ToLower(parentCandidate)
		switch lower {
		case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64",
			"float", "float32", "float64", "double", "string", "bool", "boolean", "char",
			"byte", "void", "object", "any", "nil", "null", "none":
			return false
		}
		return true
	}
	// Rust trait bounds like "struct Foo: Bar"
	if strings.Contains(content, "struct ") || strings.Contains(content, "class ") {
		return parentCandidate != ""
	}
	return false
}

func isInterfaceKeyword(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "interface") || strings.Contains(lower, "implements")
}

func inferTargetType(content string) string {
	// Try to extract the target type from type alias definitions
	// "typedef A B" -> A
	if strings.HasPrefix(content, "typedef ") {
		parts := strings.Fields(content)
		if len(parts) >= 2 {
			return parts[1]
		}
	}
	// "type A = B" -> B
	if eqIdx := strings.Index(content, "="); eqIdx != -1 {
		return strings.TrimSpace(content[eqIdx+1:])
	}
	return "unknown"
}

func inferExceptionType(content string) string {
	// Try to extract the exception type from throw/panic/raise expressions
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "throw ") {
		after := strings.TrimSpace(content[6:])
		// throw new ExceptionType() or throw ExceptionType
		after = strings.TrimPrefix(after, "new ")
		if parenIdx := strings.Index(after, "("); parenIdx != -1 {
			after = after[:parenIdx]
		}
		parts := strings.Fields(after)
		if len(parts) > 0 {
			return strings.Trim(parts[0], "{};")
		}
	}
	if strings.HasPrefix(content, "panic(") {
		return "panic"
	}
	if strings.HasPrefix(content, "raise ") {
		parts := strings.Fields(content[6:])
		if len(parts) > 0 {
			return strings.Trim(parts[0], "{};")
		}
	}
	return "Exception"
}

func isConcurrencySpawn(content string) bool {
	lower := strings.ToLower(content)
	switch {
	case strings.HasPrefix(content, "go "):
		return true
	case strings.Contains(lower, "spawn"):
		return true
	case strings.Contains(lower, "thread("):
		return true
	case strings.Contains(lower, "tokio::spawn"):
		return true
	case strings.Contains(lower, "rayon::spawn"):
		return true
	case strings.Contains(lower, "async "):
		return true
	case strings.Contains(lower, ".start("):
		return true
	default:
		return false
	}
}

func inferConcurrencyModel(content string) string {
	if strings.HasPrefix(content, "go ") {
		return "goroutine"
	}
	lower := strings.ToLower(content)
	if strings.Contains(lower, "tokio") || strings.Contains(lower, "async") {
		return "async_task"
	}
	if strings.Contains(lower, "thread") {
		return "thread"
	}
	if strings.Contains(lower, "rayon") {
		return "rayon_parallel"
	}
	return "thread_or_goroutine"
}

func detectEndpointFromAnnotations(node *GASTNode, symTable *FileSymbolTable, line int) {
	httpMethods := map[string]string{
		"get": "GET", "post": "POST", "put": "PUT",
		"delete": "DELETE", "patch": "PATCH", "head": "HEAD",
		"options": "OPTIONS", "route": "HTTP",
	}
	for _, ann := range node.Annotations {
		annLower := strings.ToLower(ann)
		// Check for common HTTP method annotations with exact boundary matching
		for method, httpMethod := range httpMethods {
			if strings.HasPrefix(annLower, "@"+method) ||
				strings.HasPrefix(annLower, "["+method) ||
				annLower == method ||
				strings.Contains(annLower, "."+method) {
				symTable.Endpoints = append(symTable.Endpoints, EndpointMeta{
					Route:      ann,
					Method:     httpMethod,
					LineNumber: line,
				})
				break
			}
		}
	}
}

func detectEndpointFromCallPattern(content string, symTable *FileSymbolTable, line int) {
	contentLower := strings.ToLower(content)
	// Cover all HTTP method routers: Express/Gin/Echo/Fiber/Go mux
	for _, pattern := range []struct{ match, method string }{
		{"app.get(", "GET"}, {"router.get(", "GET"}, {"r.get(", "GET"}, {"mux.get(", "GET"},
		{"app.post(", "POST"}, {"router.post(", "POST"}, {"r.post(", "POST"}, {"mux.post(", "POST"},
		{"app.put(", "PUT"}, {"router.put(", "PUT"}, {"r.put(", "PUT"},
		{"app.delete(", "DELETE"}, {"router.delete(", "DELETE"}, {"r.delete(", "DELETE"},
		{"app.patch(", "PATCH"}, {"router.patch(", "PATCH"}, {"r.patch(", "PATCH"},
		{"mux.handlefunc(", "HTTP"}, {"http.handlefunc(", "HTTP"}, {"http.handle(", "HTTP"},
		{"r.handlefunc(", "HTTP"}, {"r.handle(", "HTTP"},
		{"gin.get(", "GET"}, {"gin.post(", "POST"}, {"gin.put(", "PUT"}, {"gin.delete(", "DELETE"},
		{"echo.get(", "GET"}, {"echo.post(", "POST"},
		{"fiber.get(", "GET"}, {"fiber.post(", "POST"}, {"fiber.put(", "PUT"}, {"fiber.delete(", "DELETE"},
		{"router.add(", "HTTP"}, {"app.use(", "HTTP"},
		{"@get(", "GET"}, {"@post(", "POST"}, {"@put(", "PUT"}, {"@delete(", "DELETE"}, {"@patch(", "PATCH"},
	} {
		if strings.Contains(contentLower, pattern.match) {
			symTable.Endpoints = append(symTable.Endpoints, EndpointMeta{
				Route:      "unknown",
				Method:     pattern.method,
				LineNumber: line,
			})
			break
		}
	}
}
