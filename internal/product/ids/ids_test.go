package ids

import (
	"errors"
	"math/rand"
	"testing"

	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
)

// legacyGoldenTable mirrors the concrete mapping table in the plan §4.1:
// legacy form -> canonical form. The legacy-ID normalization shim must map
// exactly these (W0-02).
func TestNormalizeLegacyIDGoldenTable(t *testing.T) {
	cases := []struct{ legacy, canonical string }{
		{"internal/tui/programs/analyze/program.go::Options", "type:internal/tui/programs/analyze/program.go:Options"},
		{"cmd/ai.go::aiStreamSink::empty", "method:cmd/ai.go:aiStreamSink:empty"},
		{"file:cmd/ai.go", "file:cmd/ai.go"},
		{"module:internal/tui", "module:internal/tui"},
		{`ext:akgerrs "github.com/Syamchand123/GlassMarble/internal/errors"`, "ext:internal/errors"},
		{"QUEUE::", "virt:QUEUE"},
	}
	for _, c := range cases {
		if got := NormalizeLegacyID(c.legacy); got != c.canonical {
			t.Errorf("NormalizeLegacyID(%q) = %q, want %q", c.legacy, got, c.canonical)
		}
	}
}

// TestNormalizeLegacyIDIdempotent: the shim must be idempotent — a canonical
// ID (or the shim's own output) passes through unchanged.
func TestNormalizeLegacyIDIdempotent(t *testing.T) {
	for _, c := range []string{
		"internal/tui/programs/analyze/program.go::Options",
		"cmd/ai.go::aiStreamSink::empty",
		"file:cmd/ai.go",
		"module:internal/tui",
		`ext:akgerrs "github.com/Syamchand123/GlassMarble/internal/errors"`,
		"QUEUE::",
		"type:a%20b:Opt%3A%3Aion",
	} {
		once := NormalizeLegacyID(c)
		twice := NormalizeLegacyID(once)
		if twice != once {
			t.Errorf("NormalizeLegacyID not idempotent: %q -> %q -> %q", c, once, twice)
		}
	}
}

func TestBuildCanonicalIDGolden(t *testing.T) {
	cases := []struct {
		kind, pkg, name, file string
		want                  string
	}{
		// The six §4.1 examples expressed through the constructor.
		{"type", "", "Options", "internal/tui/programs/analyze/program.go", "type:internal/tui/programs/analyze/program.go:Options"},
		{"method", "aiStreamSink", "empty", "cmd/ai.go", "method:cmd/ai.go:aiStreamSink:empty"},
		{"file", "", "", "cmd/ai.go", "file:cmd/ai.go"},
		{"module", "internal/tui", "", "", "module:internal/tui"},
		{"ext", "internal/errors", "", "", "ext:internal/errors"},
		{"virt", "QUEUE", "", "", "virt:QUEUE"},
		// Windows path cases: backslashes AND the drive colon are
		// percent-encoded (':' is reserved in the grammar) and survive the
		// round trip unchanged (stability across OS paths).
		{"type", "", "User", `G:\repo\src\user.go`, "type:G%3A%5Crepo%5Csrc%5Cuser.go:User"},
		{"method", "User", "Save", `C:\src\store.go`, "method:C%3A%5Csrc%5Cstore.go:User:Save"},
		// Reserved characters inside segments: ':', '%', spaces, quotes.
		{"type", "", "Opt::ion%s", "a b/c\"d.go", "type:a%20b/c%22d.go:Opt%3A%3Aion%25s"},
		// Go package paths with colons-like separators stay in the path
		// segment only; the module path never enters a symbol segment.
		{"function", "", "Handle", "internal/api/handler.go", "function:internal/api/handler.go:Handle"},
	}
	for _, c := range cases {
		if got := BuildCanonicalID(c.kind, c.pkg, c.name, c.file, ""); got != c.want {
			t.Errorf("BuildCanonicalID(%q,%q,%q,%q) = %q, want %q", c.kind, c.pkg, c.name, c.file, got, c.want)
		}
	}
}

func TestBuildCanonicalIDUnknownKind(t *testing.T) {
	if got := BuildCanonicalID("bogus", "p", "n", "f.go", ""); got != "" {
		t.Errorf("unknown kind produced %q, want empty", got)
	}
	if got := BuildCanonicalID("type", "", "", "", ""); got != "" {
		t.Errorf("no path produced %q, want empty", got)
	}
}

func TestParseCanonicalIDGolden(t *testing.T) {
	cases := []struct {
		id                    string
		wantKind, wantPath, wantOwner, wantSymbol string
	}{
		{"type:internal/tui/programs/analyze/program.go:Options", "type", "internal/tui/programs/analyze/program.go", "", "Options"},
		{"method:cmd/ai.go:aiStreamSink:empty", "method", "cmd/ai.go", "aiStreamSink", "empty"},
		{"file:cmd/ai.go", "file", "cmd/ai.go", "", ""},
		{"module:internal/tui", "module", "internal/tui", "", ""},
		{"ext:internal/errors", "ext", "internal/errors", "", ""},
		{"virt:QUEUE", "virt", "QUEUE", "", ""},
		{"type:G%3A%5Crepo%5Csrc%5Cuser.go:User", "type", `G:\repo\src\user.go`, "", "User"},
		{"type:a%20b/c%22d.go:Opt%3A%3Aion%25s", "type", "a b/c\"d.go", "", "Opt::ion%s"},
	}
	for _, c := range cases {
		got, err := ParseCanonicalID(c.id)
		if err != nil {
			t.Errorf("ParseCanonicalID(%q) error: %v", c.id, err)
			continue
		}
		if string(got.Kind) != c.wantKind || got.Path != c.wantPath || got.Owner != c.wantOwner || got.Symbol != c.wantSymbol {
			t.Errorf("ParseCanonicalID(%q) = %+v, want kind=%q path=%q owner=%q symbol=%q", c.id, got, c.wantKind, c.wantPath, c.wantOwner, c.wantSymbol)
		}
	}
}

func TestParseCanonicalIDRoundTrip(t *testing.T) {
	for _, id := range []string{
		"type:internal/tui/programs/analyze/program.go:Options",
		"method:cmd/ai.go:aiStreamSink:empty",
		"file:cmd/ai.go",
		"module:internal/tui",
		"ext:internal/errors",
		"virt:QUEUE",
		"type:G%3A%5Crepo%5Csrc%5Cuser.go:User",
	} {
		c, err := ParseCanonicalID(id)
		if err != nil {
			t.Fatalf("ParseCanonicalID(%q): %v", id, err)
		}
		if got := c.String(); got != id {
			t.Errorf("Parse(%q).String() = %q", id, got)
		}
		if got := BuildCanonicalID(string(c.Kind), c.Owner, c.Symbol, c.Path, ""); got != id {
			t.Errorf("rebuild of %q = %q", id, got)
		}
	}
}

func TestParseCanonicalIDErrors(t *testing.T) {
	for _, id := range []string{
		"",
		"no-colon-here",
		":type:x",
		"bogus:a:b",
		"type:",
		"type::",
		"type:a:b:c:d",
	} {
		_, err := ParseCanonicalID(id)
		if err == nil {
			t.Errorf("ParseCanonicalID(%q) succeeded, want error", id)
			continue
		}
		if !errors.Is(err, producterrs.ErrValidation) {
			t.Errorf("ParseCanonicalID(%q) error %v does not classify as ErrValidation", id, err)
		}
	}
}

func TestNormalizeKind(t *testing.T) {
	cases := []struct{ in string; want Kind }{
		{"STRUCT", KindType}, {"CLASS", KindType}, {"INTERFACE", KindType}, {"TYPE_DECL", KindType},
		{"METHOD", KindMethod}, {"FUNCTION", KindFunction}, {"EXECUTABLE", KindFunction},
		{"FIELD", KindField}, {"PARAMETER", KindParam}, {"VARIABLE", KindVar}, {"DFG_VAR", KindVar},
		{"NAMESPACE", KindNamespace}, {"PACKAGE", KindModule}, {"MODULE", KindModule}, {"FILE", KindFile},
		{"EXTERNAL_SDK", KindExternal}, {"EXTERNAL", KindExternal},
		{"VIRTUAL_QUEUE", KindVirtual}, {"QUEUE", KindVirtual}, {"DATABASE", KindVirtual},
		{"CONSTRAINT", KindConstraint},
		{"CFG_SUMMARY", KindCFG}, {"IF_BRANCH", KindCFG}, {"BLOCK", KindCFG},
		{"method", KindMethod}, {"type", KindType}, {"virt", KindVirtual},
	}
	for _, c := range cases {
		if got := NormalizeKind(c.in); got != c.want {
			t.Errorf("NormalizeKind(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for _, in := range []string{"", "bogus", "XYZ"} {
		if got := NormalizeKind(in); got != "" {
			t.Errorf("NormalizeKind(%q) = %q, want empty", in, got)
		}
	}
}

// randomSegChars is the byte set used to stress the encoder: it includes
// every reserved grammar character, whitespace, quotes, percent, unicode
// (multi-byte), and ASCII punctuation.
const randomSegChars = "abcXYZ012_-~/:\\ \"'%<>{}|^`\t\n\r#!@&?+=$()*üéπ\u00a0"

func randomSeg(r *rand.Rand, maxLen int) string {
	n := r.Intn(maxLen)
	b := make([]byte, n)
	for i := range b {
		b[i] = randomSegChars[r.Intn(len(randomSegChars))]
	}
	return string(b)
}

// TestRandomRoundTripProperty: round-trip property test over 10k random
// symbols (plan §4.5 acceptance): BuildCanonicalID -> ParseCanonicalID ->
// String() must be the identity for every valid input, and the parsed parts
// must reconstruct the same ID.
func TestRandomRoundTripProperty(t *testing.T) {
	r := rand.New(rand.NewSource(20260806))
	for i := 0; i < 10_000; i++ {
		kind := CanonicalKinds[r.Intn(len(CanonicalKinds))]
		file := randomSeg(r, 40)
		pkg := randomSeg(r, 24)
		name := randomSeg(r, 24)

		id := BuildCanonicalID(kind.String(), pkg, name, file, "1234")
		if id == "" {
			if file == "" && pkg == "" {
				continue // degenerate input (no path) is rejected by design
			}
			t.Fatalf("iter %d: BuildCanonicalID returned empty for %q/%q/%q/%q", i, kind, pkg, name, file)
		}
		c, err := ParseCanonicalID(id)
		if err != nil {
			t.Fatalf("iter %d: ParseCanonicalID(%q): %v", i, id, err)
		}
		if c.String() != id {
			t.Fatalf("iter %d: parse/string mismatch: %q -> %+v -> %q", i, id, c, c.String())
		}
		rebuilt := BuildCanonicalID(c.Kind.String(), c.Owner, c.Symbol, c.Path, "")
		if rebuilt != id {
			t.Fatalf("iter %d: rebuild mismatch: %q -> %+v -> %q", i, id, c, rebuilt)
		}
		if NormalizeLegacyID(id) != id {
			t.Fatalf("iter %d: NormalizeLegacyID changed canonical ID %q -> %q", i, id, NormalizeLegacyID(id))
		}
	}
}

// randomLegacySeg generates a segment for legacy `path::name` forms: the
// legacy separator is "::", so segments must not contain ':' (that is exactly
// why ':' is reserved in the canonical grammar).
const legacySegChars = "abcXYZ012_-~\\/ \"'%<>{}|^`\t\n\r#!@&?+=$()*üéπ\u00a0"

func randomLegacySeg(r *rand.Rand, maxLen int) string {
	n := r.Intn(maxLen)
	b := make([]byte, n)
	for i := range b {
		b[i] = legacySegChars[r.Intn(len(legacySegChars))]
	}
	return string(b)
}

// TestRandomNormalizeRoundTrip: every legacy-shaped random ID must normalize
// to a parseable canonical ID (or pass through unchanged).
func TestRandomNormalizeRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	for i := 0; i < 10_000; i++ {
		path := randomLegacySeg(r, 30)
		if path == "" {
			continue
		}
		owner := randomLegacySeg(r, 16)
		sym := randomLegacySeg(r, 16)
		legacy := path + "::" + sym
		if r.Intn(2) == 0 {
			legacy = path + "::" + owner + "::" + sym
		}
		norm := NormalizeLegacyID(legacy)
		if _, err := ParseCanonicalID(norm); err != nil {
			t.Fatalf("iter %d: normalized legacy %q -> %q is not canonical: %v", i, legacy, norm, err)
		}
	}
}
