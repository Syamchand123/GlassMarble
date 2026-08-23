package extract

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// NodeArchetype categorizes a graph node into a semantic architectural role.
type NodeArchetype int

const (
	ArchService NodeArchetype = iota
	ArchEntrypoint
	ArchDataStore
	ArchEventBus
	ArchExternalAPI
	ArchInterface
	ArchModel
	ArchParser
	ArchRenderer
	ArchGateway
	ArchHotspot
	ArchGodObject
)

// ClassifyNodeArchetype determines the architectural archetype of a node.
func ClassifyNodeArchetype(node *types.LayoutNode) NodeArchetype {
	if node == nil {
		return ArchService
	}

	if node.IsGodObject {
		return ArchGodObject
	}
	if node.IsHotspot {
		return ArchHotspot
	}

	// Entrypoints / CLI / User
	if node.Kind == ont.PredUser || node.IsEntrypoint ||
		strings.HasSuffix(node.Name, "Command") || strings.HasSuffix(node.Name, "Cmd") ||
		node.Name == "main" || node.Name == "Execute" || node.Name == "ExecuteContext" {
		return ArchEntrypoint
	}

	// Data Stores / Databases / Caches
	if node.Kind == ont.PredDatabase || node.Kind == ont.PredVirtualDatabase ||
		strings.Contains(node.PrimitiveType, "DATABASE") ||
		strings.Contains(node.PrimitiveType, "CACHE") ||
		strings.Contains(node.PrimitiveType, "STORE") {
		return ArchDataStore
	}

	// Event Brokers / Queues
	if strings.Contains(node.PrimitiveType, "MESSAGE_QUEUE") ||
		strings.Contains(node.PrimitiveType, "EVENT_BUS") ||
		strings.Contains(node.PrimitiveType, "CHANNEL") {
		return ArchEventBus
	}

	// External APIs / Cloud
	if node.Kind == ont.PredExternal || node.Kind == ont.PredExternalAPI ||
		node.Kind == ont.PredExternalSDK || node.Kind == ont.PredExternalFFI {
		return ArchExternalAPI
	}

	// Interfaces / Contracts
	if node.Kind == ont.PredInterface {
		return ArchInterface
	}

	// Data Models / Structs
	if node.Kind == ont.PredStruct || node.Kind == ont.PredClass || node.Kind == ont.PredTypeDecl {
		return ArchModel
	}

	lower := strings.ToLower(node.Name)
	if strings.Contains(lower, "parse") || strings.Contains(lower, "ingest") || strings.Contains(lower, "normaliz") {
		return ArchParser
	}
	if strings.Contains(lower, "render") || strings.Contains(lower, "format") || strings.Contains(lower, "project") {
		return ArchRenderer
	}
	if strings.Contains(lower, "route") || strings.Contains(lower, "gateway") || strings.Contains(lower, "dispatch") || strings.Contains(lower, "filter") {
		return ArchGateway
	}

	return ArchService
}
