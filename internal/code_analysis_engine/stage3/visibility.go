package stage3

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
)

// ComputeVisibilityEnclave walks the GAST of a single file and stamps the strict visibility boundary on each export.
func ComputeVisibilityEnclave(node *stage2.GASTNode, fileRelPath string, wc *WorkspaceContext) {
	if node == nil {
		return
	}

	if node.Properties == nil {
		node.Properties = make(map[string]string)
	}

	if node.Type == stage2.GASTTypeDeclaration || node.Type == stage2.GASTFunction || node.Type == stage2.GASTVariable {
		vis := strings.ToLower(node.Visibility)
		localBoundary := NormalizeRelativePath(fileRelPath)
		// Usually the boundary is the folder
		if idx := strings.LastIndex(localBoundary, "/"); idx != -1 {
			localBoundary = localBoundary[:idx]
		}

		moduleBoundary := wc.GetModuleBoundary(fileRelPath)

		switch vis {
		case "public", "exported":
			node.Properties["namespace_scope"] = "Public"
		case "protected":
			node.Properties["namespace_scope"] = "Protected"
			node.Properties["local_boundary"] = localBoundary
		case "packageprivate", "internal":
			node.Properties["namespace_scope"] = "PackagePrivate"
			node.Properties["local_boundary"] = localBoundary
		case "moduleinternal":
			node.Properties["namespace_scope"] = "ModuleInternal"
			node.Properties["local_boundary"] = moduleBoundary
		case "private", "strictprivate":
			node.Properties["namespace_scope"] = "StrictPrivate"
			node.Properties["local_boundary"] = fileRelPath // Only visible within the file itself
		default:
			// Fallback heuristics
			if node.Name != "" && (node.Name[0] >= 'a' && node.Name[0] <= 'z') || strings.HasPrefix(node.Name, "_") {
				node.Properties["namespace_scope"] = "PackagePrivate"
				node.Properties["local_boundary"] = localBoundary
			} else {
				node.Properties["namespace_scope"] = "Public"
			}
		}
	}

	for _, child := range node.Children {
		ComputeVisibilityEnclave(child, fileRelPath, wc)
	}
}
