package stage4

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// LinkEventSourcing maps Message Broker Publishers and Subscribers (Kafka, RabbitMQ, SNS).
func LinkEventSourcing(stage3Out *stage3.Stage3Output, cpg *Stage4Output) {
	if stage3Out == nil || stage3Out.RootNode == nil || cpg == nil {
		return
	}
	traverseForEvents(stage3Out.RootNode, cpg)
}

func traverseForEvents(dir *stage3.DirectoryNode, cpg *Stage4Output) {
	if dir == nil {
		return
	}
	for _, file := range dir.Files {
		if file == nil || file.GASTRoot == nil {
			continue
		}
		if len(cpg.ModifiedFiles) > 0 && !cpg.ModifiedFiles[stage3.NormalizeRelativePath(file.RelativePath)] {
			continue
		}
		extractEventsFromGAST(file.GASTRoot, file.RelativePath, "", cpg)
	}
	for _, subDir := range dir.SubFolders {
		traverseForEvents(subDir, cpg)
	}
}

func extractEventsFromGAST(node *stage2.GASTNode, relPath, currentFuncID string, cpg *Stage4Output) {
	if node == nil {
		return
	}
	funcID := currentFuncID
	if node.Type == stage2.GASTFunction {
		funcID = universalFuncID(relPath, node.ReceiverType, node.Name)
	}

	if funcID != "" && node.Type == stage2.GASTCallExpression {
		lName := strings.ToLower(node.Name)
		if strings.Contains(lName, "publish") || strings.Contains(lName, "produce") || strings.Contains(lName, "sendmessage") {
			topic := extractTopic(node)
			if topic != "" {
				topicID := "topic::" + topic
				if _, exists := cpg.GetNode(topicID); !exists {
					cpg.GraphNodes[topicID] = &ResolvedNode{
						ID:   topicID,
						Kind: "EVENT_TOPIC",
						Name: topic,
					}
				}
				cpg.AddEdge(funcID, topicID, EdgePublishes, int(node.StartLine))
			}
		}

		if strings.Contains(lName, "subscribe") || strings.Contains(lName, "consume") || strings.Contains(lName, "listen") {
			topic := extractTopic(node)
			if topic != "" {
				topicID := "topic::" + topic
				if _, exists := cpg.GetNode(topicID); !exists {
					cpg.GraphNodes[topicID] = &ResolvedNode{
						ID:   topicID,
						Kind: "EVENT_TOPIC",
						Name: topic,
					}
				}
				cpg.AddEdge(topicID, funcID, EdgeSubscribes, int(node.StartLine))
			}
		}
	}

	for _, child := range node.Children {
		extractEventsFromGAST(child, relPath, funcID, cpg)
	}
}

func extractTopic(node *stage2.GASTNode) string {
	content := node.Properties["content"]
	if content == "" {
		return ""
	}
	// Always extract the exact first argument expression
	idxOpen := strings.Index(content, "(")
	if idxOpen != -1 {
		// We need to carefully find the first comma that is NOT inside nested parenthesis/quotes
		argStart := idxOpen + 1
		argEnd := -1

		nesting := 0
		inQuote := false
		var quoteChar rune

		for i, char := range content[argStart:] {
			if inQuote {
				if char == quoteChar && content[argStart+i-1] != '\\' {
					inQuote = false
				}
				continue
			}

			if char == '"' || char == '\'' || char == '`' {
				inQuote = true
				quoteChar = char
				continue
			}

			if char == '(' {
				nesting++
			} else if char == ')' {
				if nesting == 0 {
					argEnd = i
					break
				}
				nesting--
			} else if char == ',' {
				if nesting == 0 {
					argEnd = i
					break
				}
			}
		}

		if argEnd != -1 {
			arg := strings.TrimSpace(content[argStart : argStart+argEnd])
			// Strip outer quotes if it's a pure string literal
			if (strings.HasPrefix(arg, "\"") && strings.HasSuffix(arg, "\"")) ||
				(strings.HasPrefix(arg, "'") && strings.HasSuffix(arg, "'")) ||
				(strings.HasPrefix(arg, "`") && strings.HasSuffix(arg, "`")) {
				if len(arg) >= 2 {
					arg = arg[1 : len(arg)-1]
				}
			}
			return arg
		}
	}
	return ""
}
