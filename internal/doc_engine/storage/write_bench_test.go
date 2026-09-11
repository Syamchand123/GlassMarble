package storage

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// BenchmarkAtomicWriteFile measures AtomicWriteFile for an ~8KB markdown
// document. Each iteration varies a trailer comment so every op takes the
// full tmp → fsync → rename → verify path (byte-identical input would take
// the zero-write fast path instead).
//
// Ballpark vs plan §13.4 budgets (catalog <100ms, grounding <50ms,
// gates <10ms, write <30ms, deterministic <20ms): measured 2026-09-11
// (i7-1255U windows/amd64, -benchtime=100x) ~19.5-30.5ms/op across runs vs
// the write <30ms budget — BORDERLINE/STRADDLES budget on this Windows box
// (single small-file atomic write incl. tmp+fsync+rename+verify on local disk,
// expect variance across OSes/disks).
func BenchmarkAtomicWriteFile(b *testing.B) {
	dir := b.TempDir()
	target := filepath.Join(dir, "bench.md")
	body := "# Benchmark\n\n" + strings.Repeat("Lorem ipsum dolor sit amet. ", 300)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		content := make([]byte, 0, len(body)+32)
		content = append(content, body...)
		content = append(content, fmt.Sprintf("\n<!-- iter %d -->\n", i)...)
		if _, err := AtomicWriteFile(target, content); err != nil {
			b.Fatal(err)
		}
	}
}
