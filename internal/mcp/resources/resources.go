package resources

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// URI constants for glassmarble:// resources (Master Plan §7).
const (
	URIStatus       = "glassmarble://status"
	URIIntelligence = "glassmarble://intelligence"
	URIMemory       = "glassmarble://memory"
	URITimeline     = "glassmarble://timeline"
	URIConventions  = "glassmarble://conventions"
	URITelemetry    = "glassmarble://telemetry"
	URIAKG          = "glassmarble://akg"
	URIConfig       = "glassmarble://config"
	URIRules        = "glassmarble://rules"
)

// Descriptor describes a resource that can be listed via resources/list.
type Descriptor struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// List returns the canonical GlassMarble resource descriptors.
func List() []Descriptor {
	return []Descriptor{
		{URI: URIStatus, Name: "AKG Status", Description: "Real-time metadata of the active AKG", MimeType: "application/json"},
		{URI: URIIntelligence, Name: "Architecture Intelligence", Description: "Latest architecture intelligence report (.glassmarble/intelligence/latest.json)", MimeType: "application/json"},
		{URI: URIMemory, Name: "Developer Memory Overview", Description: "Developer memory summary", MimeType: "application/json"},
		{URI: URITimeline, Name: "Architecture Timeline", Description: "Architecture timeline file (.glassmarble/memory/timeline.json)", MimeType: "application/json"},
		{URI: URIConventions, Name: "Learned Project Conventions", Description: "Learned architecture conventions (.glassmarble/memory/conventions.json)", MimeType: "application/json"},
		{URI: URITelemetry, Name: "Pipeline Telemetry", Description: "GlassMarble pipeline performance telemetry (.glassmarble/telemetry.json)", MimeType: "application/json"},
		{URI: URIAKG, Name: "AKG Summary", Description: "Architecture Knowledge Graph summary", MimeType: "application/json"},
		{URI: URIConfig, Name: "GlassMarble Configuration", Description: "Current project configuration (.glassmarble/config.yaml)", MimeType: "text/yaml"},
		{URI: URIRules, Name: "Architecture Rules", Description: "Declarative architecture rules (.glassmarble/rules.yaml)", MimeType: "text/yaml"},
	}
}

// ValidateURI reports whether uri is a known GlassMarble resource.
func ValidateURI(uri string) error {
	for _, d := range List() {
		if d.URI == uri {
			return nil
		}
	}
	// Also allow gmb:// aliases.
	aliases := map[string]string{
		"gmb://status":          URIStatus,
		"gmb://memory/summary":  URIMemory,
		"gmb://timeline/latest": URITimeline,
		"gmb://akg":             URIAKG,
		"gmb://config":          URIConfig,
		"gmb://rules":           URIRules,
	}
	if _, ok := aliases[uri]; ok {
		return nil
	}
	return fmt.Errorf("unknown resource %q", uri)
}

// ReadFile reads the file backing a resource URI, returning JSON/YAML bytes.
// It returns a short JSON error object if the file does not exist.
func ReadFile(storageDir, uri string) ([]byte, string, error) {
	var relPath string
	var mimeType = "application/json"
	switch uri {
	case URIStatus, "gmb://status":
		// Dynamic: caller should synthesize; we return not-found for file mode.
		return nil, mimeType, fmt.Errorf("glassmarble://status is dynamic — use synthesized status")
	case URIIntelligence:
		relPath = filepath.Join("intelligence", "latest.json")
	case URITimeline, "gmb://timeline/latest":
		relPath = filepath.Join("memory", "timeline.json")
	case URIConventions:
		relPath = filepath.Join("memory", "conventions.json")
	case URITelemetry:
		relPath = "telemetry.json"
	case URIMemory, "gmb://memory/summary":
		relPath = filepath.Join("memory", "memory.json")
	case URIConfig, "gmb://config":
		relPath = "config.yaml"
		mimeType = "text/yaml"
	case URIRules, "gmb://rules":
		relPath = "rules.yaml"
		mimeType = "text/yaml"
	case URIAKG, "gmb://akg":
		relPath = "akg.json"
	default:
		return nil, mimeType, fmt.Errorf("unknown resource %q", uri)
	}
	full := filepath.Join(storageDir, relPath)
	data, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			empty := map[string]any{"error": fmt.Sprintf("resource file not found: %s", relPath)}
			b, _ := json.Marshal(empty)
			return b, mimeType, nil
		}
		return nil, mimeType, err
	}
	return data, mimeType, nil
}
