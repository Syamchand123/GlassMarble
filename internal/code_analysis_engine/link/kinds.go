package link

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// KindToClass maps a canonical node kind (e.g. "TYPE_DECL", "FUNCTION") to
// the visualization class string consumed by the extraction configs
// (internal/visualization_engine/extract). The mapping is the shared
// kind-vocabulary contract: kinds produced by the analysis engine map 1:1 to
// classes, and extraction filters consume these exact strings. Fallback
// classes (gm:TypeDecl, gm:Executable, gm:ControlStructure, ...) remain for
// legacy engine kinds that still emit them.
func KindToClass(kind string) string {
	switch kind {
	// Core structural kinds
	case "MODULE":
		return ont.PredModule
	case "NAMESPACE":
		return ont.PredNamespace
	case "FILE":
		return ont.PredFile
	case "STRUCT":
		return ont.PredStruct
	case "CLASS":
		return ont.PredClass
	case "INTERFACE":
		return ont.PredInterface
	case "FUNCTION":
		return ont.PredFunction
	case "METHOD":
		return ont.PredMethod
	case "FIELD":
		return ont.PredMember
	case "PARAMETER":
		return ont.PredParameter
	case "VARIABLE", "DFG_VAR":
		return ont.PredVariable
	case "PACKAGE":
		return ont.PredPackage
	case "META_DATA":
		return ont.PredMetaData
	// Fallback classes for legacy engine kinds mapped to standard classes (K-06)
	case "TYPE_DECL", "TYPE":
		return ont.PredStruct
	case "EXECUTABLE":
		return ont.PredFunction
	case "IF_BRANCH", "LOOP_BRANCH", "SWITCH_BRANCH":
		return ont.PredControlStructure
	case "CFG_SUMMARY":
		return ont.PredCFGSummary
	case "DFG_SUMMARY":
		return ont.PredDFGSummary
	case "EVENT_TOPIC":
		return ont.PredEventTopic
	case "VIRTUAL_DATABASE":
		return ont.PredVirtualDatabase
	case "VIRTUAL_ENDPOINT":
		return ont.PredVirtualEndpoint
	case "BLOCK":
		return ont.PredBlock
	case "ANNOTATION", "DECORATOR":
		return ont.PredAnnotation
	// Virtual / synthetic classes fabricated by the linker passes
	case "VIRTUAL_CONTEXT":
		return ont.PredVirtualContext
	case "VIRTUAL_QUEUE":
		return ont.PredVirtualQueue
	case "VIRTUAL_TAINT_SOURCE":
		return ont.PredVirtualTaintSource
	case "VIRTUAL_GLOBAL_STATE":
		return ont.PredVirtualGlobalState
	case "VIRTUAL_SECURITY_SINK":
		return ont.PredVirtualSecuritySink
	case "VIRTUAL_RESOURCE":
		return ont.PredVirtualResource
	case "VIRTUAL_CLOUD_API":
		return ont.PredVirtualCloudAPI
	case "EXTERNAL_SDK":
		return ont.PredExternalSDK
	case "EXTERNAL_API":
		return ont.PredExternalAPI
	case "EXTERNAL_FFI":
		return ont.PredExternalFFI
	case "HEAP_ALLOCATION":
		return ont.PredHeapAllocation
	case "ABSTRACT_CONSTRAINT":
		return ont.PredAbstractConstraint
	case "CFG_FLOW":
		return ont.PredCFGFlow
	case "EXCEPTIONAL_BRANCH":
		return ont.PredExceptionalBranch
	case "DELETED":
		return ont.PredDeleted
	default:
		return "rdfs:Class"
	}
}

// ClassToKind is the inverse of KindToClass. It reconstructs an internal
// node kind from a serialized class string so that graphs restored from
// persisted state keep their kinds across save/restore cycles.
func ClassToKind(class string) string {
	switch class {
	case ont.PredModule:
		return "MODULE"
	case ont.PredNamespace:
		return "NAMESPACE"
	case ont.PredFile:
		return "FILE"
	case ont.PredStruct:
		return "STRUCT"
	case ont.PredClass:
		return "CLASS"
	case ont.PredInterface:
		return "INTERFACE"
	case ont.PredFunction:
		return "FUNCTION"
	case ont.PredMethod:
		return "METHOD"
	case ont.PredMember:
		return "FIELD"
	case ont.PredVariable:
		return "VARIABLE"
	case ont.PredParameter:
		return "PARAMETER"
	case ont.PredPackage:
		return "PACKAGE"
	case ont.PredTypeDecl:
		return "STRUCT"
	case ont.PredExecutable:
		return "FUNCTION"
	case ont.PredControlStructure:
		return "IF_BRANCH"
	case ont.PredCFGSummary:
		return "CFG_SUMMARY"
	case ont.PredDFGSummary:
		return "DFG_SUMMARY"
	case ont.PredEventTopic:
		return "EVENT_TOPIC"
	case ont.PredVirtualDatabase:
		return "VIRTUAL_DATABASE"
	case ont.PredVirtualEndpoint:
		return "VIRTUAL_ENDPOINT"
	case ont.PredBlock:
		return "BLOCK"
	case ont.PredAnnotation:
		return "ANNOTATION"
	case ont.PredMetaData:
		return "META_DATA"
	case ont.PredVirtualContext:
		return "VIRTUAL_CONTEXT"
	case ont.PredVirtualQueue:
		return "VIRTUAL_QUEUE"
	case ont.PredVirtualTaintSource:
		return "VIRTUAL_TAINT_SOURCE"
	case ont.PredVirtualGlobalState:
		return "VIRTUAL_GLOBAL_STATE"
	case ont.PredVirtualSecuritySink:
		return "VIRTUAL_SECURITY_SINK"
	case ont.PredVirtualResource:
		return "VIRTUAL_RESOURCE"
	case ont.PredVirtualCloudAPI:
		return "VIRTUAL_CLOUD_API"
	case ont.PredExternalSDK:
		return "EXTERNAL_SDK"
	case ont.PredExternalAPI:
		return "EXTERNAL_API"
	case ont.PredExternalFFI:
		return "EXTERNAL_FFI"
	case ont.PredHeapAllocation:
		return "HEAP_ALLOCATION"
	case ont.PredAbstractConstraint:
		return "ABSTRACT_CONSTRAINT"
	case ont.PredCFGFlow:
		return "CFG_FLOW"
	case ont.PredExceptionalBranch:
		return "EXCEPTIONAL_BRANCH"
	case ont.PredDeleted:
		return "DELETED"
	default:
		return strings.TrimPrefix(class, ont.PrefixGM)
	}
}
