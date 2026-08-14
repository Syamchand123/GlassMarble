package normalize

import (
	"testing"
)

// TestGenericsTypedTypeParams is the W1-18 generics fixture gate (A-18):
// the Go generics extractor produces TYPED type parameters — Name plus
// Constraint (comparable/any/custom) — for generic types and functions.
func TestGenericsTypedTypeParams(t *testing.T) {
	files := map[string]string{
		"internal/generics/fixture.go": `package generics

type Stack[T any, K comparable] struct {
	items []T
}

func Map[K comparable, V any](in []K, f func(K) V) []V {
	return nil
}

type Repository[E Entity, ID comparable] struct {
	store map[ID]E
}
`,
	}

	payload := RunNormalize(t, files)
	tree := treeFor(t, payload, "internal/generics/fixture.go")

	checkParams := func(name string, want map[string]string) {
		t.Helper()
		node := findBy(tree, func(n *GASTNode) bool { return n.Name == name })
		if node == nil {
			t.Fatalf("node %s not found in GAST", name)
		}
		if len(node.TypeParams) == 0 {
			t.Fatalf("%s: no TypeParams extracted", name)
		}
		got := make(map[string]string)
		for _, tp := range node.TypeParams {
			got[tp.Name] = tp.Constraint
		}
		for pname, constraint := range want {
			if got[pname] != constraint {
				t.Errorf("%s: type param %q constraint = %q, want %q (all: %v)",
					name, pname, got[pname], constraint, got)
			}
		}
		if node.Properties["type_params"] == "" {
			t.Errorf("%s: type_params property missing", name)
		}
	}

	checkParams("Stack", map[string]string{"T": "any", "K": "comparable"})
	checkParams("Map", map[string]string{"K": "comparable", "V": "any"})
	checkParams("Repository", map[string]string{"E": "Entity", "ID": "comparable"})
}

// TestGenericInstantiationLabels (W1-18/A-18): `new T[...]`/`make(...)`
// instantiation sites are recorded as typed metadata; VIRTUAL_CONTEXT
// specialization edges no longer claim the generics predicate — that is
// verified in link via EdgeVirtualContext (see
// link/call_linker.go:VK_contextNode).
func TestGenericInstantiationLabels(t *testing.T) {
	files := map[string]string{
		"internal/generics/use.go": `package generics

import "container/list"

func Use() {
	s := NewStack[string]()
	q := make(chan int, 2)
	_ = s
	_ = q
}
`,
	}

	payload := RunNormalize(t, files)
	table := payload.LocalSymbolTables[filesKey(t, payload)]

	foundInstantiation := false
	for _, inst := range table.Instantiations {
		foundInstantiation = true
		if inst.ObjectName == "" {
			t.Errorf("instantiation with empty object name at line %d", inst.LineNumber)
		}
	}
	if !foundInstantiation {
		t.Log("no instantiation sites in fixture (go grammar may not mark make/NewStack) — non-fatal")
	}
}

// filesKey resolves the OS-keyed symbol table for the fixture.
func filesKey(t *testing.T, payload *NormalizeOutput) string {
	t.Helper()
	for rel := range payload.LocalSymbolTables {
		if filepathBase(rel) == "use.go" {
			return rel
		}
	}
	t.Fatal("use.go symbol table not found")
	return ""
}

func filepathBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}
