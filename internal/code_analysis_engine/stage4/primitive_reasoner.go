package stage4

import (
	"strconv"
	"strings"
)

// ReasonWholeProgramPrimitives executes whole-program primitive propagation along CALLS and SPAWNS edges.
func ReasonWholeProgramPrimitives(cpg *Stage4Output) {
	if cpg == nil || len(cpg.GraphNodes) == 0 {
		return
	}

	// 1. Propagate primitives from target callee nodes back to caller nodes along CALLS / SPAWNS edges
	changed := true
	for i := 0; i < 5 && changed; i++ { // max 5 depth passes
		changed = false
		for sourceID, edges := range cpg.OutboundEdges {
			sourceNode, ok := cpg.GetNode(sourceID)
			if !ok || sourceNode == nil {
				continue
			}

			if sourceNode.Properties == nil {
				sourceNode.Properties = make(map[string]string)
			}

			for _, edge := range edges {
				if edge.Type == EdgeCalls || edge.Type == EdgeSpawnsConcurrent || edge.Type == EdgeDispatchesEvent {
					targetNode, targetOk := cpg.GetNode(edge.TargetID)
					if targetOk && targetNode != nil {
						// 1. Merge Primitives with Attenuation Decay (Step 4.11)
						if targetNode.Primitive != "" || len(targetNode.PrimitiveScores) > 0 {
							if sourceNode.PrimitiveScores == nil {
								sourceNode.PrimitiveScores = make(map[string]float64)
							}

							// Initial seeding from strings if they don't have scores yet
							if targetNode.Primitive != "" && len(targetNode.PrimitiveScores) == 0 {
								if targetNode.PrimitiveScores == nil {
									targetNode.PrimitiveScores = make(map[string]float64)
								}
								parts := strings.Split(targetNode.Primitive, ",")
								for _, p := range parts {
									if p != "" {
										targetNode.PrimitiveScores[p] = 1.0 // Base score
									}
								}
							}

							// Decay factor (e.g. 20% loss per jump)
							decayFactor := 0.80

							for prim, targetScore := range targetNode.PrimitiveScores {
								attenuatedScore := targetScore * decayFactor
								if attenuatedScore > 0.1 { // Cutoff threshold to prevent infinite spread
									currentScore := sourceNode.PrimitiveScores[prim]
									if attenuatedScore > currentScore {
										sourceNode.PrimitiveScores[prim] = attenuatedScore
										changed = true
									}
								}
							}

							// Rebuild string representation for easy viewing
							var newPrimStrs []string
							for k := range sourceNode.PrimitiveScores {
								newPrimStrs = append(newPrimStrs, k)
							}

							// Sort for deterministic hashing
							for i := 0; i < len(newPrimStrs); i++ {
								for j := i + 1; j < len(newPrimStrs); j++ {
									if newPrimStrs[i] > newPrimStrs[j] {
										newPrimStrs[i], newPrimStrs[j] = newPrimStrs[j], newPrimStrs[i]
									}
								}
							}

							newPrimStr := strings.Join(newPrimStrs, ",")
							if sourceNode.Primitive != newPrimStr {
								sourceNode.Primitive = newPrimStr
								changed = true
							}
						}

						// 2. Merge Enterprise Intelligence Properties
						if targetNode.Properties != nil {
							// Inherit Async Side-Effects
							if targetNode.Properties["has_async_side_effects"] == "true" && sourceNode.Properties["has_async_side_effects"] != "true" {
								sourceNode.Properties["has_async_side_effects"] = "true"
								changed = true
							}

							// Inherit PII Violations
							if targetNode.Properties["data_privacy_violation"] == "true" && sourceNode.Properties["data_privacy_violation"] != "true" {
								sourceNode.Properties["data_privacy_violation"] = "true"
								sourceNode.Properties["data_sensitivity_level"] = targetNode.Properties["data_sensitivity_level"]
								changed = true
							}

							// Inherit N+1 Warning
							if targetNode.Properties["n_plus_one_query_warning"] == "true" && sourceNode.Properties["n_plus_one_query_warning"] != "true" {
								sourceNode.Properties["n_plus_one_query_warning"] = "true"
								changed = true
							}

							// Inherit Performance Hot Path
							if targetNode.Properties["performance_hot_path"] == "true" && sourceNode.Properties["performance_hot_path"] != "true" {
								sourceNode.Properties["performance_hot_path"] = "true"
								changed = true
							}

							// Inherit Observability Blindspot
							if targetNode.Properties["observability_blindspot"] == "true" && sourceNode.Properties["observability_blindspot"] != "true" {
								sourceNode.Properties["observability_blindspot"] = "true"
								changed = true
							}

							// Maximize Risk Score
							if targetNode.Properties["primitive_risk_score"] != "" {
								targetRisk, _ := strconv.Atoi(targetNode.Properties["primitive_risk_score"])
								sourceRisk := 0
								if sourceNode.Properties["primitive_risk_score"] != "" {
									sourceRisk, _ = strconv.Atoi(sourceNode.Properties["primitive_risk_score"])
								}
								if targetRisk > sourceRisk {
									sourceNode.Properties["primitive_risk_score"] = targetNode.Properties["primitive_risk_score"]
									sourceNode.Properties["primitive_risk_level"] = targetNode.Properties["primitive_risk_level"]
									changed = true
								}
							}

							// Inherit Anti-Patterns
							if targetNode.Properties["architectural_violations"] != "" {
								targetSmells := strings.Split(targetNode.Properties["architectural_violations"], ";")
								for _, smell := range targetSmells {
									if !strings.Contains(sourceNode.Properties["architectural_violations"], smell) {
										if sourceNode.Properties["architectural_violations"] == "" {
											sourceNode.Properties["architectural_violations"] = smell
										} else {
											sourceNode.Properties["architectural_violations"] += ";" + smell
										}
										changed = true
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// 2. Propagate whole-program primitives up to FILE and MODULE nodes
	for _, node := range cpg.GraphNodes {
		if node.Kind == "FUNCTION" || node.Kind == "METHOD" {
			if node.Primitive == "" {
				continue
			}

			fileID := "file:" + node.FileSpec.Path
			if fileNode, ok := cpg.GetNode(fileID); ok && fileNode != nil {
				fileNode.Primitive = mergePrimitives(fileNode.Primitive, node.Primitive)
			}

			modPath := getModulePath(node.FileSpec.Path)
			if modPath != "" {
				modID := "module:" + modPath
				if modNode, ok := cpg.GetNode(modID); ok && modNode != nil {
					modNode.Primitive = mergePrimitives(modNode.Primitive, node.Primitive)
				}
			}
		}
	}
}

func mergePrimitives(existing, newStr string) string {
	if existing == "" {
		return newStr
	}
	if newStr == "" {
		return existing
	}

	set := make(map[string]bool)
	for _, p := range strings.Split(existing, ",") {
		if p != "" {
			set[p] = true
		}
	}
	for _, p := range strings.Split(newStr, ",") {
		if p != "" {
			set[p] = true
		}
	}

	var list []string
	for k := range set {
		list = append(list, k)
	}

	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[i] > list[j] {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	return strings.Join(list, ",")
}

func getModulePath(filePath string) string {
	parts := strings.Split(filePath, "/")
	if len(parts) > 1 {
		return strings.Join(parts[:len(parts)-1], "/")
	}
	return ""
}
