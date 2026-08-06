package product

import (
	"sync"
)

// StringTable represents a thread-safe interned string table (12.3 / W8-03).
// Deduplicates repeated identifiers, kinds, and paths to reduce GAST & AKG memory retention.
type StringTable struct {
	mu    sync.RWMutex
	table map[string]string
}

// GlobalStringTable is the default global string interning instance.
var GlobalStringTable = NewStringTable()

// NewStringTable creates a new initialized StringTable.
func NewStringTable() *StringTable {
	return &StringTable{
		table: make(map[string]string, 4096),
	}
}

// Intern returns a canonical deduplicated instance of the given string.
func (st *StringTable) Intern(s string) string {
	if s == "" {
		return ""
	}
	st.mu.RLock()
	if canonical, ok := st.table[s]; ok {
		st.mu.RUnlock()
		return canonical
	}
	st.mu.RUnlock()

	st.mu.Lock()
	defer st.mu.Unlock()
	if canonical, ok := st.table[s]; ok {
		return canonical
	}
	st.table[s] = s
	return s
}

// Len returns the total number of unique interned strings.
func (st *StringTable) Len() int {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return len(st.table)
}

// Clear flushes all interned strings from the table.
func (st *StringTable) Clear() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.table = make(map[string]string, 4096)
}

// InternString is a package-level helper that deduplicates strings via GlobalStringTable.
func InternString(s string) string {
	return GlobalStringTable.Intern(s)
}
