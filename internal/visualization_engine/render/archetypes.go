package aggregate

import (
	"fmt"
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

// Stereotype returns the formal architectural stereotype tag (clean textual badge).
func (a NodeArchetype) Stereotype() string {
	switch a {
	case ArchEntrypoint:
		return "«ENTRYPOINT»"
	case ArchDataStore:
		return "«DATASTORE»"
	case ArchEventBus:
		return "«EVENT_BUS»"
	case ArchExternalAPI:
		return "«EXTERNAL_API»"
	case ArchInterface:
		return "«INTERFACE»"
	case ArchModel:
		return "«MODEL»"
	case ArchParser:
		return "«PARSER»"
	case ArchRenderer:
		return "«RENDERER»"
	case ArchGateway:
		return "«GATEWAY»"
	case ArchHotspot:
		return "«HOTSPOT»"
	case ArchGodObject:
		return "«GOD_OBJECT»"
	default:
		return "«SERVICE»"
	}
}

// ClassName returns the CSS class name mapped in Theme.
func (a NodeArchetype) ClassName() string {
	switch a {
	case ArchEntrypoint:
		return "entrypoint"
	case ArchDataStore:
		return "datastore"
	case ArchEventBus:
		return "eventbus"
	case ArchExternalAPI:
		return "external"
	case ArchInterface:
		return "interface"
	case ArchModel:
		return "model"
	case ArchParser:
		return "parser"
	case ArchRenderer:
		return "renderer"
	case ArchGateway:
		return "gateway"
	case ArchHotspot:
		return "hotspot"
	case ArchGodObject:
		return "godobject"
	default:
		return "service"
	}
}

// FormatMermaidNode formats a node statement using diverse geometry and architectural stereotypes.
func FormatMermaidNode(a NodeArchetype, alias, name, subtext string) string {
	if name == "" {
		name = alias
	}

	var label string
	if subtext != "" {
		label = fmt.Sprintf("<small>%s</small><br/><b>%s</b><br/><small>%s</small>", a.Stereotype(), name, subtext)
	} else {
		label = fmt.Sprintf("<small>%s</small><br/><b>%s</b>", a.Stereotype(), name)
	}

	className := a.ClassName()

	switch a {
	case ArchEntrypoint:
		// Stadium / Capsule
		return fmt.Sprintf("%s([\"%s\"]):::%s", alias, label, className)
	case ArchDataStore:
		// Cylinder
		return fmt.Sprintf("%s[(\"%s\")]:::%s", alias, label, className)
	case ArchEventBus:
		// Hexagon
		return fmt.Sprintf("%s{{\"%s\"}}:::%s", alias, label, className)
	case ArchExternalAPI:
		// Subroutine (Double border)
		return fmt.Sprintf("%s[[\"%s\"]]:::%s", alias, label, className)
	case ArchInterface:
		// Stadium capsule
		return fmt.Sprintf("%s([\"%s\"]):::%s", alias, label, className)
	case ArchParser:
		// Trapezoid
		return fmt.Sprintf("%s[/\"%s\"/]:::%s", alias, label, className)
	case ArchRenderer:
		// Inverted Trapezoid
		return fmt.Sprintf("%s[\\\"%s\"/]:::%s", alias, label, className)
	case ArchGateway:
		// Rhombus / Diamond
		return fmt.Sprintf("%s{\"%s\"}:::%s", alias, label, className)
	case ArchHotspot:
		// Double Hexagon
		return fmt.Sprintf("%s{{\"%s\"}}:::%s", alias, label, className)
	case ArchGodObject:
		// Asymmetric flag
		return fmt.Sprintf("%s>\"%s\"]:::%s", alias, label, className)
	default:
		// Rounded rectangular card
		return fmt.Sprintf("%s[\"%s\"]:::%s", alias, label, className)
	}
}
