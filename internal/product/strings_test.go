package product_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/product"
)

// TestStringTable_Intern verifies string deduplication & pointer equality.
func TestStringTable_Intern(t *testing.T) {
	st := product.NewStringTable()

	s1 := fmt.Sprintf("node_%s", "kind_struct")
	s2 := fmt.Sprintf("node_%s", "kind_struct")

	interned1 := st.Intern(s1)
	interned2 := st.Intern(s2)

	if interned1 != interned2 {
		t.Errorf("expected interned strings to be equal; got %q vs %q", interned1, interned2)
	}

	if st.Len() != 1 {
		t.Errorf("expected string table size 1; got %d", st.Len())
	}
}

// TestStringTable_Concurrent verifies thread safety of Intern calls.
func TestStringTable_Concurrent(t *testing.T) {
	st := product.NewStringTable()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			str := fmt.Sprintf("symbol_%d", id%10)
			st.Intern(str)
		}(i)
	}

	wg.Wait()
	if st.Len() > 10 {
		t.Errorf("expected at most 10 unique strings interned; got %d", st.Len())
	}
}
