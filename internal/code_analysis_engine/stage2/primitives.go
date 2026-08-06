package stage2

import (
	"strconv"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

// NormalizeDataType converts language-specific raw compiler types into standardized primitives.
func NormalizeDataType(rawType string) string {
	raw := strings.TrimSpace(rawType)
	if raw == "" {
		return "void"
	}

	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "promise") || strings.HasPrefix(lower, "task") || strings.HasPrefix(lower, "future") || strings.HasPrefix(lower, "completablefuture") || strings.HasPrefix(lower, "chan ") || strings.HasPrefix(lower, "channel"):
		return "async_handle"
	case strings.HasPrefix(lower, "option") || strings.HasPrefix(lower, "optional") || strings.HasSuffix(raw, "?") || strings.HasPrefix(lower, "nullable"):
		return "optional"
	case strings.HasPrefix(raw, "[]") || strings.HasPrefix(lower, "list") || strings.HasPrefix(lower, "array") || strings.HasPrefix(lower, "vector") || strings.HasPrefix(lower, "slice") || strings.HasPrefix(lower, "set") || strings.HasPrefix(lower, "hashset"):
		return "array"
	case strings.HasPrefix(lower, "map") || strings.HasPrefix(lower, "dict") || strings.HasPrefix(lower, "hash") || strings.HasPrefix(lower, "btree") || strings.HasPrefix(lower, "concurrenthashmap"):
		return "map"
	case strings.Contains(lower, "string") || lower == "str" || lower == "char*" || lower == "std::string" || lower == "text" || lower == "stringbuilder":
		return "string"
	case strings.Contains(lower, "int") || lower == "i8" || lower == "i16" || lower == "i32" || lower == "i64" || lower == "u8" || lower == "u16" || lower == "u32" || lower == "u64" || lower == "size_t" || lower == "ssize_t" || lower == "long" || lower == "short" || lower == "byte":
		return "integer"
	case strings.Contains(lower, "float") || strings.Contains(lower, "double") || lower == "f32" || lower == "f64" || lower == "number" || lower == "decimal" || lower == "bigdecimal":
		return "number"
	case lower == "bool" || lower == "boolean":
		return "boolean"
	case lower == "void" || lower == "nil" || lower == "null" || lower == "none" || lower == "()" || lower == "std::nullptr_t":
		return "void"
	default:
		return raw
	}
}

// DetectBehavioralPrimitives inspects content and identifier tokens for system boundary interactions.
func DetectBehavioralPrimitives(content, name string) []BehavioralPrimitive {
	var primitives []BehavioralPrimitive
	cleanContent := stripCommentsAndStrings(content)
	lower := strings.ToLower(cleanContent + " " + name)

	// 1. Disk I/O detection
	if matchAny(lower, "os.writefile", "os.readfile", "ioutil", "fstream", "open(", "writefile", "readfile",
		"filewriter", "filereader", "file.open", "os.create", "bufio", "stderr", "stdout", "fs.", "file.",
		"appendalltext", "fopen", "fwrite", "fread", "std::fs", "file.write", "file_put_contents", "filepath") {
		primitives = append(primitives, PrimDiskIO)
	}

	// 2. Network I/O detection
	if matchAny(lower, "http.get", "http.post", "http.put", "http.delete", "http.patch",
		"fetch(", "axios", "socket", "net.dial", "curl", "urlconnection",
		"httpclient", "express()", "router.", "httprequest", "webclient",
		"requests.", "http:", "https:", "urllib", "httpx", "reqwest", "spring-web", "fastapi", "ktor") {
		primitives = append(primitives, PrimNetworkIO)
	}

	// 3. Database SQL detection
	if matchAny(lower, "db.query", "db.exec", "db.queryrow", "sql.open", "executereader",
		"select ", "insert into", "insert ", "update ", "delete from", "gorm", "prisma", "sqlalchemy",
		"repository", "sqlite3", "pg.", "postgres", "mysql", "oracle", "mssql", "efcore", "dapper", "diesel", "active_record", "pdo") {
		primitives = append(primitives, PrimDatabaseSQL)
	}

	// 4. Database NoSQL detection
	if matchAny(lower, "mongoclient", "cassandra", "dynamodb", "neo4j", "firestore", "couchbase",
		"rethinkdb", "collection.find", "bson", "key-value", "documentstore") {
		primitives = append(primitives, PrimDatabaseNoSQL)
	}

	// 5. Cache detection
	if matchAny(lower, "redis", "memcached", "hazelcast", "guava", "lru-cache", "lru.", "cache.get",
		"cache.set", "valkey", "dragonfly", "inmemorycache", "ristretto") {
		primitives = append(primitives, PrimCache)
	}

	// 6. Message Queue & Streaming detection
	if matchAny(lower, "kafka", "rabbitmq", "nats.", "nats:", "sqs", "sns", "eventhub", "amqp",
		"celery", "sidekiq", "pubsub", "messagebroker", "consumer.subscribe", "producer.send") {
		primitives = append(primitives, PrimMessageQueue)
	}

	// 7. Cloud SDK detection
	if matchAny(lower, "aws-sdk", "boto3", "azure-sdk", "google-cloud", "s3.", "blobstorage",
		"gcs.", "aws_s3", "cloudformation", "google.cloud") {
		primitives = append(primitives, PrimCloudSDK)
	}

	// 8. Container & DevOps APIs detection
	if matchAny(lower, "docker", "k8s.io", "kubernetes", "helm", "terraform", "vault", "consul",
		"containerd", "kubeclient") {
		primitives = append(primitives, PrimContainerDevOps)
	}

	// 9. Concurrency detection
	if matchAny(lower, "go func", "goroutine", "thread", "async", "await", "promise", "task.run",
		"executor", "spawn", "fork", "tokio::spawn", "pthreads", "rayon") {
		primitives = append(primitives, PrimConcurrency)
	}

	// 10. Synchronization detection
	if matchAny(lower, "sync.waitgroup", "sync.mutex", "sync.rwmutex", "atomic.", "chan ", "channel",
		"lock()", "unlock()", "semaphore", "reentrantlock", "conditionvariable") {
		primitives = append(primitives, PrimSynchronization)
	}

	// 11. Memory Allocation detection
	if matchAny(lower, "make([]", "malloc", "calloc", "bytebuffer.allocate", "sync.pool", "mempool",
		"allocator", "arena") {
		primitives = append(primitives, PrimAllocation)
	}

	// 12. High-Performance Math & ML Compute detection
	if matchAny(lower, "numpy", "pytorch", "tensorflow", "linalg", "simd", "cuda", "opencl",
		"matrix", "tensor", "ndarray", "onnx", "tensorrt") {
		primitives = append(primitives, PrimComputeMath)
	}

	// 13. Security Auth detection
	if matchAny(lower, "jwt.", "oauth", "oauth2", "passport.", "spring-security", "saml",
		"oidc", "authenticate", "authorize", "tokenvalidation") {
		primitives = append(primitives, PrimSecurityAuth)
	}

	// 14. Cryptography & TLS detection
	if matchAny(lower, "bcrypt", "argon2", "aes.", "rsa.", "tls.config", "crypto/sha256",
		"sha256", "cipher", "hmac", "secp256k1", "x509") {
		primitives = append(primitives, PrimCrypto)
	}

	// 15. Logging detection
	if matchAny(lower, "zap.logger", "logrus", "winston", "logback", "slf4j", "log4j",
		"zerolog", "tracing::info", "logger.info", "log.printf") {
		primitives = append(primitives, PrimLogging)
	}

	// 16. Telemetry & Observability detection
	if matchAny(lower, "opentelemetry", "prometheus", "datadog", "newrelic", "statsd",
		"jaeger", "zipkin", "otel.", "metrics.counter", "tracer.start") {
		primitives = append(primitives, PrimTelemetry)
	}

	// 17. AI & LLM detection
	if matchAny(lower, "openai", "anthropic", "langchain", "pinecone", "qdrant", "weaviate",
		"ollama", "huggingface", "chromadb", "llm", "embedding") {
		primitives = append(primitives, PrimAI)
	}

	// 18. Inter-Process Communication (IPC) detection
	if matchAny(lower, "shm_open", "named_pipe", "unix_socket", "zeromq", "dbus", "ipc.",
		"mmap", "sharedmemory") {
		primitives = append(primitives, PrimIPC)
	}

	// 19. Remote Procedure Call (RPC) detection
	if matchAny(lower, "grpc", "protobuf", "thrift", "json-rpc", "trpc", "proto.") {
		primitives = append(primitives, PrimRPC)
	}

	// 20. Frontend / UI Event detection
	if matchAny(lower, "onclick", "addeventlistener", "electron", "flutter", "swiftui",
		"qt.", "react-native", "componentdidmount") {
		primitives = append(primitives, PrimUIEvent)
	}

	return deduplicatePrimitives(primitives)
}

// PropagatePrimitives propagates behavioral primitives from child call expressions and blocks
// up to their enclosing GASTFunction / GASTMethod node. It intentionally stops at GASTFunction
// and does NOT pollute GASTFileRoot nodes, preserving architectural granularity for Stage 4 whole-program AKG linking.
func PropagatePrimitives(node *GASTNode) []BehavioralPrimitive {
	if node == nil {
		return nil
	}

	acc := append([]BehavioralPrimitive(nil), node.Primitives...)
	for _, child := range node.Children {
		childPrims := PropagatePrimitives(child)
		acc = append(acc, childPrims...)
	}

	deduped := deduplicatePrimitives(acc)

	// Keep primitives low: tag function and call execution nodes, but do NOT pollute GASTFileRoot.
	if node.Type != GASTFileRoot {
		node.Primitives = deduped
		if len(deduped) > 0 {
			if node.Properties == nil {
				node.Properties = make(map[string]string)
			}
			node.Properties["has_behavioral_primitives"] = "true"
		}
	} else {
		node.Primitives = nil
	}

	return deduped
}

func matchAny(target string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(target, p) {
			return true
		}
	}
	return false
}

func deduplicatePrimitives(slice []BehavioralPrimitive) []BehavioralPrimitive {
	if len(slice) <= 1 {
		return slice
	}
	seen := make(map[BehavioralPrimitive]bool, len(slice))
	var result []BehavioralPrimitive
	for _, p := range slice {
		if !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}
	return result
}

func stripCommentsAndStrings(s string) string {
	var result strings.Builder
	inString := false
	var stringChar rune
	inSingleComment := false
	inBlockComment := false
	runes := []rune(s)

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if inSingleComment {
			if r == '\n' {
				inSingleComment = false
			}
			continue
		}
		if inBlockComment {
			if r == '*' && i+1 < len(runes) && runes[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}
		if inString {
			if r == stringChar {
				inString = false
			}
			continue
		}
		if r == '/' && i+1 < len(runes) && runes[i+1] == '/' {
			inSingleComment = true
			i++
			continue
		}
		if r == '#' {
			inSingleComment = true
			continue
		}
		if r == '/' && i+1 < len(runes) && runes[i+1] == '*' {
			inBlockComment = true
			i++
			continue
		}
		if r == '"' || r == '\'' || r == '`' {
			inString = true
			stringChar = r
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}

func ApplyEnterpriseIntelligence(node *GASTNode, symTable *FileSymbolTable, RichTokens []stage1.RichToken, nodes []*GASTNode) {
	if node == nil {
		return
	}

	if node.Type == GASTFunction || node.Type == GASTTypeDeclaration {
		if len(node.Primitives) > 0 {
			if node.Properties == nil {
				node.Properties = make(map[string]string)
			}

			// 1. Risk Score
			score := calculateRiskScore(node.Primitives)
			node.Properties["primitive_risk_score"] = strconv.Itoa(score)
			node.Properties["primitive_risk_level"] = getRiskLevel(score)

			// 2. Architecture Tier
			node.Properties["architecture_tier"] = determineArchitectureTier(node.Primitives)

			// 3. Anti-Patterns
			antiPatterns := detectAntiPatterns(node.Primitives)
			if len(antiPatterns) > 0 {
				node.Properties["architectural_violations"] = strings.Join(antiPatterns, ";")
			}

			// 4. Async Side-Effects
			if hasAsyncSideEffects(node, symTable) {
				node.Properties["has_async_side_effects"] = "true"
			}

			// 5. Compliance (PII)
			if isPIILeak(node) {
				node.Properties["data_sensitivity_level"] = "PII_WARNING"
				node.Properties["data_privacy_violation"] = "true"
			}

			// 6. Hot-Paths (N+1)
			if hasNPlusOne(node, RichTokens, nodes) {
				node.Properties["n_plus_one_query_warning"] = "true"
			}
			if isPerformanceHotPath(node.Primitives) {
				node.Properties["performance_hot_path"] = "true"
			}

			// 7. Resilience
			if hasPrimitive(node.Primitives, PrimNetworkIO) || hasPrimitive(node.Primitives, PrimRPC) {
				if hasResilience(node, symTable) {
					node.Properties["resilience"] = "Hardened"
				} else {
					node.Properties["resilience"] = "Fragile"
				}
			}

			// 8. Observability Blindspots
			if (hasPrimitive(node.Primitives, PrimNetworkIO) || hasPrimitive(node.Primitives, PrimIPC)) &&
				!(hasPrimitive(node.Primitives, PrimTelemetry) || hasPrimitive(node.Primitives, PrimLogging)) {
				node.Properties["observability_blindspot"] = "true"
			}
		}
	}

	for _, child := range node.Children {
		ApplyEnterpriseIntelligence(child, symTable, RichTokens, nodes)
	}
}

func calculateRiskScore(prims []BehavioralPrimitive) int {
	score := 0
	for _, p := range prims {
		switch p {
		case PrimCrypto, PrimSecurityAuth:
			score += 30
		case PrimDatabaseSQL, PrimDatabaseNoSQL, PrimNetworkIO, PrimRPC, PrimIPC, PrimDiskIO:
			score += 20
		case PrimContainerDevOps, PrimCloudSDK, PrimMessageQueue:
			score += 15
		case PrimConcurrency, PrimSynchronization, PrimAllocation, PrimComputeMath:
			score += 10
		case PrimCache, PrimTelemetry, PrimLogging, PrimUIEvent:
			score += 5
		default:
			score += 5
		}
	}
	return score
}

func getRiskLevel(score int) string {
	if score >= 60 {
		return "CRITICAL"
	} else if score >= 40 {
		return "HIGH"
	} else if score >= 20 {
		return "MEDIUM"
	}
	return "LOW"
}

func determineArchitectureTier(prims []BehavioralPrimitive) string {
	if hasPrimitive(prims, PrimDatabaseSQL) || hasPrimitive(prims, PrimDatabaseNoSQL) {
		return "DataLayer"
	}
	if hasPrimitive(prims, PrimNetworkIO) || hasPrimitive(prims, PrimUIEvent) || hasPrimitive(prims, PrimRPC) {
		return "InterfaceLayer"
	}
	if hasPrimitive(prims, PrimCache) || hasPrimitive(prims, PrimMessageQueue) || hasPrimitive(prims, PrimContainerDevOps) || hasPrimitive(prims, PrimCloudSDK) {
		return "InfrastructureLayer"
	}
	return "DomainLayer"
}

func detectAntiPatterns(prims []BehavioralPrimitive) []string {
	var smells []string
	if hasPrimitive(prims, PrimUIEvent) && (hasPrimitive(prims, PrimDatabaseSQL) || hasPrimitive(prims, PrimDatabaseNoSQL)) {
		smells = append(smells, "Anti-Pattern: UI-to-DB Direct Access")
	}
	if hasPrimitive(prims, PrimDiskIO) && hasPrimitive(prims, PrimNetworkIO) {
		smells = append(smells, "Anti-Pattern: High I/O Coupling")
	}
	if hasPrimitive(prims, PrimCrypto) && !hasPrimitive(prims, PrimSecurityAuth) {
		smells = append(smells, "Anti-Pattern: Crypto without Auth")
	}
	return smells
}

func hasPrimitive(prims []BehavioralPrimitive, target BehavioralPrimitive) bool {
	for _, p := range prims {
		if p == target {
			return true
		}
	}
	return false
}

func hasAsyncSideEffects(node *GASTNode, symTable *FileSymbolTable) bool {
	for _, spawn := range symTable.ConcurrencySpawns {
		if isDescendant(node, spawn.TargetNodeID) {
			return true
		}
	}
	return false
}

func isDescendant(root *GASTNode, targetID string) bool {
	if root.ID == targetID {
		return true
	}
	for _, child := range root.Children {
		if isDescendant(child, targetID) {
			return true
		}
	}
	return false
}

func isPIILeak(node *GASTNode) bool {
	if !hasPrimitive(node.Primitives, PrimLogging) && !hasPrimitive(node.Primitives, PrimNetworkIO) {
		return false
	}
	if hasPrimitive(node.Primitives, PrimCrypto) {
		return false
	}
	lower := strings.ToLower(node.Name)
	if strings.Contains(lower, "password") || strings.Contains(lower, "ssn") || strings.Contains(lower, "credit_card") || strings.Contains(lower, "token") {
		return true
	}
	for _, child := range node.Children {
		childLower := strings.ToLower(child.Name)
		if strings.Contains(childLower, "password") || strings.Contains(childLower, "ssn") || strings.Contains(childLower, "credit_card") || strings.Contains(childLower, "token") {
			return true
		}
	}
	return false
}

func hasNPlusOne(node *GASTNode, RichTokens []stage1.RichToken, nodes []*GASTNode) bool {
	if !hasPrimitive(node.Primitives, PrimDatabaseSQL) && !hasPrimitive(node.Primitives, PrimDiskIO) {
		return false
	}
	// Check if any loop token traces back to this function node
	for i, tok := range RichTokens {
		content := strings.TrimSpace(tok.Content)
		if strings.HasPrefix(content, "for ") || strings.HasPrefix(content, "while ") || strings.HasPrefix(content, "foreach ") {
			// trace up ParentIdx
			curr := i
			for curr >= 0 && curr < len(nodes) {
				if nodes[curr] != nil && nodes[curr].ID == node.ID {
					return true
				}
				curr = RichTokens[curr].ParentIdx
			}
		}
	}
	return false
}

func isPerformanceHotPath(prims []BehavioralPrimitive) bool {
	return hasPrimitive(prims, PrimComputeMath) && hasPrimitive(prims, PrimAllocation)
}

func hasResilience(node *GASTNode, symTable *FileSymbolTable) bool {
	// Simple check: if a CATCH block exists in the same file around or inside this node's line.
	// Actually, just having any CATCH inside the function is a good enough heuristic for resilience.
	for _, exc := range symTable.Exceptions {
		if exc.Action == "CATCH" && int(node.StartLine) <= exc.LineNumber {
			return true
		}
	}

	// Add Go-specific resilience checks (defer/recover) via deep search
	if searchForRecover(node) {
		return true
	}

	return false
}

func searchForRecover(node *GASTNode) bool {
	if node == nil {
		return false
	}
	if node.Properties != nil && strings.Contains(node.Properties["content"], "recover(") {
		return true
	}
	for _, child := range node.Children {
		if searchForRecover(child) {
			return true
		}
	}
	return false
}
