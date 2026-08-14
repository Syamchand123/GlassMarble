package akg

import (
	"sync/atomic"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// storeCodeFlag tracks whether source code content storage (--store-code) is enabled globally.
var storeCodeFlag int32 = 0

// SetStoreCode enables or disables storing source code snippets in AKG nodes (default off, K-05).
func SetStoreCode(enabled bool) {
	if enabled {
		atomic.StoreInt32(&storeCodeFlag, 1)
	} else {
		atomic.StoreInt32(&storeCodeFlag, 0)
	}
}

// GetStoreCode returns whether storing source code snippets is enabled.
func GetStoreCode() bool {
	return atomic.LoadInt32(&storeCodeFlag) == 1
}

// MaxContentLength is the maximum allowed byte length for source content snippets when --store-code is enabled (512B, K-05).
const MaxContentLength = 512

// ApplyContentPolicy applies the K-05 content policy to a ResolvedNode.
// When storeCode is false (default):
//   - Properties["content"] is removed.
//   - Properties["hasContent"] is set to "false".
//
// When storeCode is true:
//   - Content is kept only for structural nodes (STRUCT, CLASS, INTERFACE, FUNCTION, METHOD).
//   - Content is capped at MaxContentLength (512 bytes).
//   - Properties["hasContent"] is set to "true".
func ApplyContentPolicy(node *link.ResolvedNode, storeCode bool) {
	if node == nil {
		return
	}
	if node.Properties == nil {
		node.Properties = make(map[string]string)
	}

	if !storeCode {
		delete(node.Properties, "content")
		delete(node.Properties, "code")
		node.Properties["hasContent"] = "false"
		return
	}

	// Store code enabled: check structural kinds
	isStructural := false
	switch node.Kind {
	case "STRUCT", "CLASS", "INTERFACE", "TYPE_DECL", "FUNCTION", "METHOD":
		isStructural = true
	}

	contentVal, hasContent := node.Properties["content"]
	if !hasContent {
		contentVal, hasContent = node.Properties["code"]
	}

	if isStructural && hasContent && len(contentVal) > 0 {
		if len(contentVal) > MaxContentLength {
			contentVal = contentVal[:MaxContentLength]
		}
		node.Properties["content"] = contentVal
		delete(node.Properties, "code")
		node.Properties["hasContent"] = "true"
	} else {
		delete(node.Properties, "content")
		delete(node.Properties, "code")
		node.Properties["hasContent"] = "false"
	}
}
