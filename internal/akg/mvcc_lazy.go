package akg

import (
	"cmp"
	"sync/atomic"
	"unsafe"
)

type cowNode[K cmp.Ordered, V any] struct {
	key    K
	val    V
	left   *cowNode[K, V]
	right  *cowNode[K, V]
	height int
}

func heightOf[K cmp.Ordered, V any](n *cowNode[K, V]) int {
	if n == nil {
		return 0
	}
	return n.height
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func newCowNode[K cmp.Ordered, V any](key K, val V) *cowNode[K, V] {
	return &cowNode[K, V]{key: key, val: val, height: 1}
}

func balanceFactor[K cmp.Ordered, V any](n *cowNode[K, V]) int {
	if n == nil {
		return 0
	}
	return heightOf(n.left) - heightOf(n.right)
}

// rotateRight and rotateLeft are PURE: they copy every node whose pointers or
// height they change, and never write through a pointer they were given.
//
// This is load-bearing for MVCC. Set and Delete copy only the path to the
// touched key and alias every sibling subtree, so a rotation reaches nodes that
// earlier snapshots still own — Delete in particular rotates *into* the aliased
// side. Mutating in place there silently rewrites the child pointers of a
// snapshot that concurrent readers are traversing, which corrupted it into a
// tree with duplicated and out-of-order keys.
func rotateRight[K cmp.Ordered, V any](y *cowNode[K, V]) *cowNode[K, V] {
	if y == nil || y.left == nil {
		return y
	}
	newX := *y.left // new pivot (copy)
	newY := *y      // demoted root (copy)
	newY.left = newX.right
	newX.right = &newY
	newY.height = maxInt(heightOf(newY.left), heightOf(newY.right)) + 1
	newX.height = maxInt(heightOf(newX.left), heightOf(newX.right)) + 1
	return &newX
}

func rotateLeft[K cmp.Ordered, V any](x *cowNode[K, V]) *cowNode[K, V] {
	if x == nil || x.right == nil {
		return x
	}
	newY := *x.right // new pivot (copy)
	newX := *x       // demoted root (copy)
	newX.right = newY.left
	newY.left = &newX
	newX.height = maxInt(heightOf(newX.left), heightOf(newX.right)) + 1
	newY.height = maxInt(heightOf(newY.left), heightOf(newY.right)) + 1
	return &newY
}

func cowMapSet[K cmp.Ordered, V any](root *cowNode[K, V], key K, val V) *cowNode[K, V] {
	if root == nil {
		return newCowNode(key, val)
	}
	if key == root.key {
		n := *root
		n.val = val
		return &n
	}
	var newRoot cowNode[K, V]
	newRoot.key = root.key
	newRoot.val = root.val
	if key < root.key {
		newRoot.left = cowMapSet(root.left, key, val)
		newRoot.right = root.right
	} else {
		newRoot.right = cowMapSet(root.right, key, val)
		newRoot.left = root.left
	}
	newRoot.height = maxInt(heightOf(newRoot.left), heightOf(newRoot.right)) + 1
	bf := balanceFactor(&newRoot)
	if bf > 1 && key < newRoot.left.key {
		return rotateRight(&newRoot)
	}
	if bf < -1 && key > newRoot.right.key {
		return rotateLeft(&newRoot)
	}
	if bf > 1 && key > newRoot.left.key {
		newRoot.left = rotateLeft(newRoot.left)
		return rotateRight(&newRoot)
	}
	if bf < -1 && key < newRoot.right.key {
		newRoot.right = rotateRight(newRoot.right)
		return rotateLeft(&newRoot)
	}
	return &newRoot
}

func cowMapGet[K cmp.Ordered, V any](root *cowNode[K, V], key K) (V, bool) {
	current := root
	for current != nil {
		if key == current.key {
			return current.val, true
		}
		if key < current.key {
			current = current.left
		} else {
			current = current.right
		}
	}
	var zero V
	return zero, false
}

func minValueNode[K cmp.Ordered, V any](n *cowNode[K, V]) *cowNode[K, V] {
	current := n
	for current != nil && current.left != nil {
		current = current.left
	}
	return current
}

func cowMapDelete[K cmp.Ordered, V any](root *cowNode[K, V], key K) *cowNode[K, V] {
	if root == nil {
		return nil
	}
	if key < root.key {
		n := *root
		n.left = cowMapDelete(root.left, key)
		n.right = root.right
		return cowMapRebalance(&n)
	}
	if key > root.key {
		n := *root
		n.right = cowMapDelete(root.right, key)
		n.left = root.left
		return cowMapRebalance(&n)
	}
	if root.left == nil {
		return root.right
	}
	if root.right == nil {
		return root.left
	}
	successor := minValueNode(root.right)
	n := *successor
	n.left = root.left
	n.right = cowMapDelete(root.right, successor.key)
	return cowMapRebalance(&n)
}

func cowMapRebalance[K cmp.Ordered, V any](n *cowNode[K, V]) *cowNode[K, V] {
	if n == nil {
		return nil
	}
	n.height = maxInt(heightOf(n.left), heightOf(n.right)) + 1
	bf := balanceFactor(n)
	if bf > 1 && balanceFactor(n.left) >= 0 {
		return rotateRight(n)
	}
	if bf < -1 && balanceFactor(n.right) <= 0 {
		return rotateLeft(n)
	}
	if bf > 1 && balanceFactor(n.left) < 0 {
		n.left = rotateLeft(n.left)
		return rotateRight(n)
	}
	if bf < -1 && balanceFactor(n.right) > 0 {
		n.right = rotateRight(n.right)
		return rotateLeft(n)
	}
	return n
}

func cowMapLen[K cmp.Ordered, V any](root *cowNode[K, V]) int {
	if root == nil {
		return 0
	}
	return 1 + cowMapLen(root.left) + cowMapLen(root.right)
}

func cowMapIterate[K cmp.Ordered, V any](root *cowNode[K, V], f func(K, V)) {
	if root == nil {
		return
	}
	cowMapIterate(root.left, f)
	f(root.key, root.val)
	cowMapIterate(root.right, f)
}

func cowMapToMap[K cmp.Ordered, V any](root *cowNode[K, V]) map[K]V {
	result := make(map[K]V)
	cowMapIterate(root, func(k K, v V) {
		result[k] = v
	})
	return result
}

type CowMap[K cmp.Ordered, V any] struct {
	root unsafe.Pointer
}

func NewCowMap[K cmp.Ordered, V any]() *CowMap[K, V] {
	return &CowMap[K, V]{}
}

func (m *CowMap[K, V]) Get(key K) (V, bool) {
	root := (*cowNode[K, V])(atomic.LoadPointer(&m.root))
	return cowMapGet(root, key)
}

func (m *CowMap[K, V]) Set(key K, val V) *CowMap[K, V] {
	root := (*cowNode[K, V])(atomic.LoadPointer(&m.root))
	newRoot := cowMapSet(root, key, val)
	newMap := &CowMap[K, V]{}
	atomic.StorePointer(&newMap.root, unsafe.Pointer(newRoot))
	return newMap
}

func (m *CowMap[K, V]) Delete(key K) *CowMap[K, V] {
	root := (*cowNode[K, V])(atomic.LoadPointer(&m.root))
	newRoot := cowMapDelete(root, key)
	newMap := &CowMap[K, V]{}
	atomic.StorePointer(&newMap.root, unsafe.Pointer(newRoot))
	return newMap
}

func (m *CowMap[K, V]) Len() int {
	root := (*cowNode[K, V])(atomic.LoadPointer(&m.root))
	return cowMapLen(root)
}

func (m *CowMap[K, V]) Iterate(f func(K, V)) {
	root := (*cowNode[K, V])(atomic.LoadPointer(&m.root))
	cowMapIterate(root, f)
}

func (m *CowMap[K, V]) Snapshot() map[K]V {
	root := (*cowNode[K, V])(atomic.LoadPointer(&m.root))
	return cowMapToMap(root)
}

func (m *CowMap[K, V]) Clone() *CowMap[K, V] {
	root := (*cowNode[K, V])(atomic.LoadPointer(&m.root))
	newMap := &CowMap[K, V]{}
	atomic.StorePointer(&newMap.root, unsafe.Pointer(root))
	return newMap
}

func (m *CowMap[K, V]) Keys() []K {
	var keys []K
	root := (*cowNode[K, V])(atomic.LoadPointer(&m.root))
	cowMapIterate(root, func(k K, _ V) {
		keys = append(keys, k)
	})
	return keys
}

func (m *CowMap[K, V]) Values() []V {
	var vals []V
	root := (*cowNode[K, V])(atomic.LoadPointer(&m.root))
	cowMapIterate(root, func(_ K, v V) {
		vals = append(vals, v)
	})
	return vals
}
