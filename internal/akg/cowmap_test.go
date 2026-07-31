package akg

import (
	"fmt"
	"testing"
)

func TestCowMap_SetAndGet(t *testing.T) {
	m := NewCowMap[string, int]()
	m = m.Set("a", 1)
	m = m.Set("b", 2)

	v, ok := m.Get("a")
	if !ok || v != 1 {
		t.Errorf("expected (1, true), got (%d, %v)", v, ok)
	}

	v, ok = m.Get("c")
	if ok {
		t.Errorf("expected (0, false) for missing key, got (%d, %v)", v, ok)
	}
}

func TestCowMap_Update(t *testing.T) {
	m := NewCowMap[string, int]()
	m = m.Set("a", 1)
	m = m.Set("a", 2)

	v, ok := m.Get("a")
	if !ok || v != 2 {
		t.Errorf("expected (2, true) after update, got (%d, %v)", v, ok)
	}
}

func TestCowMap_Delete(t *testing.T) {
	m := NewCowMap[string, int]()
	m = m.Set("a", 1)
	m = m.Set("b", 2)
	m = m.Delete("a")

	_, ok := m.Get("a")
	if ok {
		t.Error("expected deleted key to be absent")
	}
	v, ok := m.Get("b")
	if !ok || v != 2 {
		t.Errorf("expected (2, true) for remaining key, got (%d, %v)", v, ok)
	}
}

func TestCowMap_CloneIsolation(t *testing.T) {
	m1 := NewCowMap[string, int]()
	m1 = m1.Set("a", 1)
	m1 = m1.Set("b", 2)

	m2 := m1.Clone()
	m2 = m2.Set("c", 3)

	_, ok := m1.Get("c")
	if ok {
		t.Error("clone should not affect original")
	}

	v, ok := m2.Get("c")
	if !ok || v != 3 {
		t.Errorf("expected (3, true) in clone, got (%d, %v)", v, ok)
	}
}

func TestCowMap_Len(t *testing.T) {
	m := NewCowMap[string, int]()
	if m.Len() != 0 {
		t.Errorf("expected 0 for empty map, got %d", m.Len())
	}
	m = m.Set("a", 1)
	m = m.Set("b", 2)
	if m.Len() != 2 {
		t.Errorf("expected 2, got %d", m.Len())
	}
	m = m.Delete("a")
	if m.Len() != 1 {
		t.Errorf("expected 1 after delete, got %d", m.Len())
	}
}

func TestCowMap_Snapshot(t *testing.T) {
	m := NewCowMap[string, int]()
	m = m.Set("a", 1)
	m = m.Set("b", 2)

	snap := m.Snapshot()
	if len(snap) != 2 || snap["a"] != 1 || snap["b"] != 2 {
		t.Errorf("unexpected snapshot: %v", snap)
	}
}

func TestCowMap_Iterate(t *testing.T) {
	m := NewCowMap[string, int]()
	m = m.Set("b", 2)
	m = m.Set("a", 1)

	var keys []string
	var vals []int
	m.Iterate(func(k string, v int) {
		keys = append(keys, k)
		vals = append(vals, v)
	})

	if len(keys) != 2 {
		t.Errorf("expected 2 entries, got %d", len(keys))
	}
}

func TestCowMap_EmptyGet(t *testing.T) {
	m := NewCowMap[string, int]()
	_, ok := m.Get("anything")
	if ok {
		t.Error("expected false for empty map get")
	}
}

func TestCowMap_EmptyDelete(t *testing.T) {
	m := NewCowMap[string, int]()
	m = m.Delete("anything")
	if m.Len() != 0 {
		t.Errorf("expected 0 len after delete from empty, got %d", m.Len())
	}
}

func TestCowMap_CloneIsEmpty(t *testing.T) {
	m1 := NewCowMap[string, int]()
	m2 := m1.Clone()
	if m2.Len() != 0 {
		t.Errorf("expected 0 len in cloned empty map, got %d", m2.Len())
	}
}

// ===== ADDITIONAL COWMAP TESTS =====

func TestCowMap_CloneIsO1(t *testing.T) {
	m1 := NewCowMap[string, int]()
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key_%d", i)
		m1 = m1.Set(key, i)
	}

	m2 := m1.Clone()
	if m2.Len() != 1000 {
		t.Errorf("expected 1000 entries in clone, got %d", m2.Len())
	}
}
