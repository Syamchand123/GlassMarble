package akg

import (
	"fmt"
	"sort"
	"sync/atomic"
	"testing"
	"unsafe"
)

// snapshotKeys returns the in-order key traversal of a CowMap.
func snapshotKeys(m *CowMap[string, int]) []string {
	var keys []string
	m.Iterate(func(k string, _ int) { keys = append(keys, k) })
	return keys
}

// assertSorted verifies the in-order traversal is strictly increasing, which
// is the defining invariant of the search tree. A corrupted (aliased) rotation
// shows up here as a duplicate or out-of-order key.
func assertSorted(t *testing.T, label string, keys []string) {
	t.Helper()
	for i := 1; i < len(keys); i++ {
		if keys[i-1] >= keys[i] {
			t.Errorf("%s: in-order traversal is not strictly increasing at %d: %v", label, i, keys)
			return
		}
	}
}

// TestCowMap_SnapshotSurvivesRebalancingDelete pins the core MVCC guarantee:
// a snapshot taken before a Delete must be completely unaffected by it, even
// when the delete triggers an AVL rotation.
//
// Regression: rotateLeft/rotateRight mutated their input nodes in place. Delete
// copies only the path to the removed key and aliases the sibling subtree, so a
// rebalance rotated *into* nodes still owned by the previous snapshot and
// rewrote their child pointers — silently corrupting a snapshot that concurrent
// readers were traversing.
func TestCowMap_SnapshotSurvivesRebalancingDelete(t *testing.T) {
	base := NewCowMap[string, int]()
	// a,b,c,d,e builds b(a, d(c,e)); deleting "a" forces a left rotation
	// whose pivot ("d") is aliased from the snapshot.
	for i, k := range []string{"a", "b", "c", "d", "e"} {
		base = base.Set(k, i)
	}

	before := snapshotKeys(base)
	beforeLen := base.Len()
	if len(before) != 5 {
		t.Fatalf("setup: expected 5 keys, got %v", before)
	}

	next := base.Delete("a")

	after := snapshotKeys(base)
	assertSorted(t, "snapshot after delete", after)
	if len(after) != len(before) {
		t.Fatalf("snapshot mutated by Delete: had %v, now %v", before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("snapshot mutated by Delete: had %v, now %v", before, after)
		}
	}
	if got := base.Len(); got != beforeLen {
		t.Fatalf("snapshot length changed after Delete: %d -> %d", beforeLen, got)
	}
	// every original key must still resolve in the snapshot
	for _, k := range before {
		if _, ok := base.Get(k); !ok {
			t.Fatalf("snapshot lost key %q after Delete on a derived map", k)
		}
	}

	// and the derived map must be correct too
	derived := snapshotKeys(next)
	assertSorted(t, "derived map", derived)
	if len(derived) != 4 {
		t.Fatalf("derived map should have 4 keys, got %v", derived)
	}
	if _, ok := next.Get("a"); ok {
		t.Fatal("derived map still contains the deleted key")
	}
}

// TestCowMap_SnapshotSurvivesDoubleRotationInsert covers the Set path: the
// left-right / right-left cases rotate a child whose own child may still be
// aliased from the previous version.
func TestCowMap_SnapshotSurvivesDoubleRotationInsert(t *testing.T) {
	cases := [][]string{
		{"m", "c", "t", "a", "e", "d"}, // left-right
		{"m", "c", "t", "p", "z", "q"}, // right-left
	}
	for ci, seq := range cases {
		base := NewCowMap[string, int]()
		for i, k := range seq[:len(seq)-1] {
			base = base.Set(k, i)
		}
		before := snapshotKeys(base)
		next := base.Set(seq[len(seq)-1], 99)

		after := snapshotKeys(base)
		assertSorted(t, fmt.Sprintf("case %d snapshot", ci), after)
		if len(after) != len(before) {
			t.Fatalf("case %d: snapshot mutated by Set: had %v, now %v", ci, before, after)
		}
		for i := range before {
			if before[i] != after[i] {
				t.Fatalf("case %d: snapshot mutated by Set: had %v, now %v", ci, before, after)
			}
		}
		assertSorted(t, fmt.Sprintf("case %d derived", ci), snapshotKeys(next))
	}
}

// TestCowMap_ManySnapshotsRemainIndependent stress-checks isolation across a
// long chain of interleaved inserts and deletes: every retained version must
// keep exactly the contents it had when it was created.
func TestCowMap_ManySnapshotsRemainIndependent(t *testing.T) {
	const n = 200

	type version struct {
		m    *CowMap[string, int]
		want []string
	}

	cur := NewCowMap[string, int]()
	live := map[string]bool{}
	var versions []version

	for i := 0; i < n; i++ {
		k := fmt.Sprintf("k%04d", i*7919%n) // scattered insertion order
		cur = cur.Set(k, i)
		live[k] = true

		if i%3 == 2 {
			// delete an existing key to force rebalancing deletes
			del := fmt.Sprintf("k%04d", (i/3)*7919%n)
			if live[del] {
				cur = cur.Delete(del)
				delete(live, del)
			}
		}

		if i%20 == 0 {
			want := make([]string, 0, len(live))
			for k := range live {
				want = append(want, k)
			}
			sort.Strings(want)
			versions = append(versions, version{m: cur, want: want})
		}
	}

	for vi, v := range versions {
		got := snapshotKeys(v.m)
		assertSorted(t, fmt.Sprintf("version %d", vi), got)
		if len(got) != len(v.want) {
			t.Fatalf("version %d: expected %d keys, got %d", vi, len(v.want), len(got))
		}
		for i := range v.want {
			if got[i] != v.want[i] {
				t.Fatalf("version %d: key %d mismatch: want %q got %q", vi, i, v.want[i], got[i])
			}
		}
		if v.m.Len() != len(v.want) {
			t.Fatalf("version %d: Len()=%d want %d", vi, v.m.Len(), len(v.want))
		}
	}
}

// TestCowMap_LenMatchesTraversal pins the incrementally-maintained counter
// against a real traversal. Len used to walk the whole tree on every call;
// it is now O(1), so the count must stay exactly in step with Set/Delete —
// including no-op deletes and overwriting Sets.
func TestCowMap_LenMatchesTraversal(t *testing.T) {
	m := NewCowMap[string, int]()
	check := func(stage string) {
		t.Helper()
		walked := cowMapLen((*cowNode[string, int])(atomicLoadRoot(m)))
		if m.Len() != walked {
			t.Fatalf("%s: Len()=%d but traversal counted %d", stage, m.Len(), walked)
		}
	}

	for i := 0; i < 50; i++ {
		m = m.Set(fmt.Sprintf("k%02d", i), i)
	}
	check("after inserts")

	// overwriting an existing key must not change the count
	m = m.Set("k10", 999)
	if m.Len() != 50 {
		t.Fatalf("overwrite changed Len: %d", m.Len())
	}
	check("after overwrite")

	for i := 0; i < 20; i++ {
		m = m.Delete(fmt.Sprintf("k%02d", i))
	}
	check("after deletes")
	if m.Len() != 30 {
		t.Fatalf("expected 30 keys after deletes, Len()=%d", m.Len())
	}

	// deleting an absent key must not change the count
	m = m.Delete("does-not-exist")
	if m.Len() != 30 {
		t.Fatalf("no-op delete changed Len: %d", m.Len())
	}
	check("after no-op delete")
}

// atomicLoadRoot exposes the tree root for invariant checks.
func atomicLoadRoot(m *CowMap[string, int]) unsafe.Pointer {
	return atomic.LoadPointer(&m.root)
}
