package types

// Diagram AST (Abstract Syntax Tree) and Intermediate Representation (DiagramIR).
// Provides a strongly-typed model for software architecture diagrams across all
// 31 diagram types, 3 scope levels, and multiple target formats (Mermaid, PlantUML, DOT).

type BoundaryKind string

const (
	BoundaryEnterprise BoundaryKind = "ENTERPRISE"
	BoundarySystem     BoundaryKind = "SYSTEM"
	BoundaryContainer  BoundaryKind = "CONTAINER"
	BoundaryComponent  BoundaryKind = "COMPONENT"
	BoundaryDevice     BoundaryKind = "DEVICE"
	BoundaryPackage    BoundaryKind = "PACKAGE"
	BoundaryCluster    BoundaryKind = "CLUSTER"
	BoundaryFolder     BoundaryKind = "FOLDER"
	BoundarySubgraph   BoundaryKind = "SUBGRAPH"
)

type ElementKind string

const (
	ElemStruct    ElementKind = "STRUCT"
	ElemInterface ElementKind = "INTERFACE"
	ElemClass     ElementKind = "CLASS"
	ElemFunction  ElementKind = "FUNCTION"
	ElemMethod    ElementKind = "METHOD"
	ElemPackage   ElementKind = "PACKAGE"
	ElemModule    ElementKind = "MODULE"
	ElemFile      ElementKind = "FILE"
	ElemDatabase  ElementKind = "DATABASE"
	ElemQueue     ElementKind = "QUEUE"
	ElemService   ElementKind = "SERVICE"
	ElemActor     ElementKind = "ACTOR"
	ElemGeneric   ElementKind = "GENERIC"
)

type EdgeStyle string

const (
	EdgeSolid  EdgeStyle = "SOLID"
	EdgeDashed EdgeStyle = "DASHED"
	EdgeDotted EdgeStyle = "DOTTED"
	EdgeThick  EdgeStyle = "THICK"
	EdgeCross  EdgeStyle = "CROSS"
)

type ArrowKind string

const (
	ArrowNormal      ArrowKind = "NORMAL"
	ArrowInherit     ArrowKind = "INHERIT"
	ArrowCompose     ArrowKind = "COMPOSE"
	ArrowAggregate   ArrowKind = "AGGREGATE"
	ArrowDependency  ArrowKind = "DEPENDENCY"
	ArrowAsync       ArrowKind = "ASYNC"
	ArrowBidirect    ArrowKind = "BIDIRECT"
	ArrowNone        ArrowKind = "NONE"
)

type MemberVisibility string

const (
	VisibilityPublic    MemberVisibility = "+"
	VisibilityPrivate   MemberVisibility = "-"
	VisibilityProtected MemberVisibility = "#"
	VisibilityPackage   MemberVisibility = "~"
)

type ASTMember struct {
	Name       string           `json:"name"`
	Type       string           `json:"type,omitempty"`
	Visibility MemberVisibility `json:"visibility,omitempty"`
	IsStatic   bool             `json:"is_static,omitempty"`
	IsAbstract bool             `json:"is_abstract,omitempty"`
	Parameters []string         `json:"parameters,omitempty"`
}

type ASTElement struct {
	ID          string            `json:"id"`
	RawID       string            `json:"raw_id"`
	Name        string            `json:"name"`
	Kind        ElementKind       `json:"kind"`
	Tech        string            `json:"tech,omitempty"`
	Description string            `json:"description,omitempty"`
	Fields      []ASTMember       `json:"fields,omitempty"`
	Methods     []ASTMember       `json:"methods,omitempty"`
	Stereotype  string            `json:"stereotype,omitempty"`
	IsExternal  bool              `json:"is_external,omitempty"`
	IsHotspot   bool              `json:"is_hotspot,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
}

type ASTBoundary struct {
	ID          string         `json:"id"`
	RawName     string         `json:"raw_name"`
	Label       string         `json:"label"`
	Kind        BoundaryKind   `json:"kind"`
	Tech        string         `json:"tech,omitempty"`
	Description string         `json:"description,omitempty"`
	Children    []*ASTBoundary `json:"children,omitempty"`
	Elements    []*ASTElement  `json:"elements,omitempty"`
}

type ASTEdge struct {
	SourceID     string    `json:"source_id"`
	TargetID     string    `json:"target_id"`
	Predicate    string    `json:"predicate"`
	Label        string    `json:"label,omitempty"`
	Style        EdgeStyle `json:"style"`
	ArrowKind    ArrowKind `json:"arrow_kind"`
	Weight       int       `json:"weight,omitempty"`
	Multiplicity string    `json:"multiplicity,omitempty"`
	IsCycle      bool      `json:"is_cycle,omitempty"`
	LineNumber   int       `json:"line_number,omitempty"`
}

type DiagramAST struct {
	Title       string        `json:"title"`
	Type        DiagramType   `json:"type"`
	Scope       ScopeLevel    `json:"scope"`
	ScopePath   string        `json:"scope_path,omitempty"`
	Direction   string        `json:"direction"` // TB, LR, etc.
	Root        *ASTBoundary  `json:"root"`
	Edges       []ASTEdge     `json:"edges"`
	Summary     *GraphSummary `json:"summary,omitempty"`
	Format      string        `json:"format,omitempty"`
}

// CollectAllElements returns a flat slice of all elements in the AST.
func (ast *DiagramAST) CollectAllElements() []*ASTElement {
	if ast == nil || ast.Root == nil {
		return nil
	}
	var elements []*ASTElement
	var walk func(b *ASTBoundary)
	walk = func(b *ASTBoundary) {
		if b == nil {
			return
		}
		elements = append(elements, b.Elements...)
		for _, child := range b.Children {
			walk(child)
		}
	}
	walk(ast.Root)
	return elements
}

// HasElements reports whether the boundary or any of its children contains at least one element.
func (b *ASTBoundary) HasElements() bool {
	if b == nil {
		return false
	}
	if len(b.Elements) > 0 {
		return true
	}
	for _, child := range b.Children {
		if child.HasElements() {
			return true
		}
	}
	return false
}
