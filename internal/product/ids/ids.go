// Package ids implements the canonical GlassMarble node ID scheme
// (master_overhaul_plan.md §4.1). It replaces the ad-hoc `path::name`,
// `module:x`, `file:x` and `ext:alias "uri"` schemes with a single grammar:
//
//	canonical := type + ":" + urlencoded(path) + ":" + urlencoded(owner) + ":" + urlencoded(symbol)
//
// Every segment except the leading kind token is URL-encoded, so `::`, `:`,
// spaces, quotes and Windows backslashes can never collide with the grammar.
// A path never contains the module path (github.com/...) — that stays in the
// gm:module_name property only.
//
// The legacy forms are normalized onto this scheme by NormalizeLegacyID so
// the visualization engine has exactly one ID grammar to parse.
package ids

import (
	"fmt"
	"strings"

	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// Kind is the leading token of a canonical ID (§4.1).
type Kind string

// Canonical kind tokens. Legacy engine kinds (STRUCT, METHOD, VIRTUAL_QUEUE,
// ...) are normalized onto these tokens by NormalizeKind.
const (
	KindModule     Kind = "module"
	KindFile       Kind = "file"
	KindType       Kind = "type"
	KindMethod     Kind = "method"
	KindFunction   Kind = "function"
	KindField      Kind = "field"
	KindParam      Kind = "param"
	KindVar        Kind = "var"
	KindNamespace  Kind = "ns"
	KindExternal   Kind = "ext"
	KindVirtual    Kind = "virt"
	KindConstraint Kind = "constraint"
	KindCFG        Kind = "cfg"
)

// CanonicalKinds lists every valid kind token in declaration order.
var CanonicalKinds = []Kind{
	KindModule, KindFile, KindType, KindMethod, KindFunction, KindField,
	KindParam, KindVar, KindNamespace, KindExternal, KindVirtual,
	KindConstraint, KindCFG,
}

// String returns the canonical token itself (satisfies fmt.Stringer).
func (k Kind) String() string { return string(k) }

// CanonicalID is the structured form of a canonical node ID.
type CanonicalID struct {
	Kind   Kind
	Path   string
	Owner  string
	Symbol string
}

// String rebuilds the canonical ID from its parts (inverse of ParseCanonicalID).
// Empty trailing segments are dropped, so an unowned symbol yields
// `type:path:symbol` (two segments) rather than `type:path::symbol`.
func (c CanonicalID) String() string {
	segs := make([]string, 0, 3)
	if c.Path != "" {
		segs = append(segs, c.Path)
	}
	if c.Owner != "" {
		segs = append(segs, c.Owner)
	}
	if c.Symbol != "" {
		segs = append(segs, c.Symbol)
	}
	return string(c.Kind) + ":" + strings.Join(encodeSegs(segs), ":")
}

// NormalizeKind maps a legacy engine kind onto the canonical kind token.
// Unknown kinds return the empty string (callers should treat that as an
// error). Accepts both the canonical lowercase tokens and the legacy
// uppercase engine kinds.
func NormalizeKind(kind string) Kind {
	switch kind {
	case "module", "MODULE", "PACKAGE":
		return KindModule
	case "file", "FILE":
		return KindFile
	case "type", "TYPE", "STRUCT", "CLASS", "INTERFACE", "TYPE_DECL":
		return KindType
	case "method", "METHOD":
		return KindMethod
	case "function", "FUNCTION", "EXECUTABLE":
		return KindFunction
	case "field", "FIELD":
		return KindField
	case "param", "PARAMETER":
		return KindParam
	case "var", "VARIABLE", "DFG_VAR":
		return KindVar
	case "ns", "NAMESPACE":
		return KindNamespace
	case "ext", "EXTERNAL", "EXTERNAL_SDK", "EXTERNAL_API", "EXTERNAL_FFI":
		return KindExternal
	case "virt", "VIRTUAL", "VIRTUAL_CONTEXT", "VIRTUAL_QUEUE", "VIRTUAL_DATABASE",
		"VIRTUAL_ENDPOINT", "VIRTUAL_TAINT_SOURCE", "VIRTUAL_GLOBAL_STATE",
		"VIRTUAL_SECURITY_SINK", "VIRTUAL_RESOURCE", "VIRTUAL_CLOUD_API",
		"QUEUE", "DATABASE", "CLOUD_API", "TAINT", "TOPIC", "EVENT_TOPIC",
		"SINK", "FFI", "MEMORY", "ALLOC", "GLOBAL":
		return KindVirtual
	case "constraint", "CONSTRAINT":
		return KindConstraint
	case "cfg", "CFG", "CFG_SUMMARY", "DFG_SUMMARY", "IF_BRANCH", "LOOP_BRANCH",
		"SWITCH_BRANCH", "BLOCK", "CONTROL_STRUCTURE", "CFG_FLOW", "EXCEPTIONAL_BRANCH":
		return KindCFG
	}
	return ""
}

// BuildCanonicalID constructs the canonical node ID from its semantic parts
// (W0-01). The ID grammar is type:path:owner:symbol where the path segment is
// the owning file (or the module/namespace path when there is no file) and
// the owner is the direct owner symbol (receiver type for methods, type for
// fields/params).
//
// line is provenance metadata only: it never appears in the ID, because IDs
// must be stable across source edits (§4.1 grammar has no line segment).
// BuildCanonicalID returns "" for an unknown kind.
func BuildCanonicalID(kind, pkg, name, file, line string) string {
	k := NormalizeKind(kind)
	if k == "" {
		return ""
	}
	path, owner := file, pkg
	if path == "" {
		path = pkg
		owner = ""
	}
	if path == "" {
		return ""
	}
	return (CanonicalID{Kind: k, Path: path, Owner: owner, Symbol: name}).String()
}

// ParseCanonicalID splits a canonical ID into its structured parts. It
// returns an error (classified as ErrValidation) for unknown kind tokens or
// malformed segment counts.
func ParseCanonicalID(id string) (CanonicalID, error) {
	var c CanonicalID
	colon := strings.IndexByte(id, ':')
	if colon <= 0 {
		return c, fmt.Errorf("%w: %q is not a canonical node ID (missing kind token)", producterrs.ErrValidation, id)
	}
	k := NormalizeKind(id[:colon])
	if k == "" {
		return c, fmt.Errorf("%w: %q has unknown kind token %q", producterrs.ErrValidation, id, id[:colon])
	}
	rest := id[colon+1:]
	if rest == "" {
		return c, fmt.Errorf("%w: %q has no path segment", producterrs.ErrValidation, id)
	}
	segs := strings.Split(rest, ":")
	if len(segs) > 3 {
		return c, fmt.Errorf("%w: %q has %d segments (max 3 after kind)", producterrs.ErrValidation, id, len(segs))
	}
	decoded := make([]string, len(segs))
	for i, s := range segs {
		decoded[i] = percentDecode(s)
	}
	c.Kind = k
	c.Path = decoded[0]
	if c.Path == "" {
		return c, fmt.Errorf("%w: %q has an empty path segment", producterrs.ErrValidation, id)
	}
	if len(decoded) >= 3 {
		c.Owner, c.Symbol = decoded[1], decoded[2]
	} else if len(decoded) == 2 {
		c.Symbol = decoded[1]
	}
	return c, nil
}

// NormalizeLegacyID maps a legacy node ID onto the canonical scheme (§4.1
// migration rule). It is idempotent: canonical IDs pass through unchanged.
//
//	legacy `path::Symbol`            -> `type:path:Symbol`
//	legacy `path::Receiver::Method`  -> `method:path:Receiver:Method`
//	legacy `Name::` (virtual)        -> `virt:Name`
//	legacy `file:x` / `module:x`     -> canonical segment encoding of x
//	legacy `ext:alias "module/path"` -> `ext:module/path` (alias dropped)
func NormalizeLegacyID(id string) string {
	if id == "" {
		return ""
	}
	// Canonical IDs (kind token in the set, raw-canonical encoding) pass
	// through unchanged.
	colon := strings.IndexByte(id, ':')
	if colon > 0 && NormalizeKind(id[:colon]) != "" && looksCanonical(id) {
		if _, err := ParseCanonicalID(id); err == nil {
			return id
		}
	}

	switch {
	case strings.HasPrefix(id, ont.PrefixExt):
		// `ext:alias "module/path"` -> `ext:module/path`. The alias is a
		// display hint only; the module path is the stable identifier.
		rest := id[len(ont.PrefixExt):]
		if q := strings.IndexByte(rest, '"'); q != -1 {
			rest = rest[q+1:]
			if end := strings.IndexByte(rest, '"'); end != -1 {
				rest = rest[:end]
			}
		}
		rest = strings.TrimSpace(rest)
		if rest == "" {
			return ""
		}
		// A module path never appears in a symbol segment: strip the
		// repo-qualifying prefix (host/owner/repo) when present.
		rest = stripModuleRoot(rest)
		return KindExternal.String() + ":" + encodeSeg(rest)
	case strings.HasPrefix(id, "file:"):
		return KindFile.String() + ":" + encodeSeg(id[len("file:"):])
	case strings.HasPrefix(id, "module:"):
		return KindModule.String() + ":" + encodeSeg(id[len("module:"):])
	}

	if !strings.Contains(id, "::") {
		return id
	}
	parts := strings.Split(id, "::")
	switch {
	case len(parts) == 2 && parts[1] == "":
		// `QUEUE::` -> virt:QUEUE
		if parts[0] == "" {
			return ""
		}
		return KindVirtual.String() + ":" + encodeSeg(parts[0])
	case len(parts) == 2:
		if parts[0] == "" {
			// `::name` is degenerate (no path); leave it unchanged rather
			// than fabricating an empty path segment.
			return id
		}
		return KindType.String() + ":" + encodeSeg(parts[0]) + ":" + encodeSeg(parts[1])
	case len(parts) == 3:
		return KindMethod.String() + ":" + encodeSeg(parts[0]) + ":" + encodeSeg(parts[1]) + ":" + encodeSeg(parts[2])
	default:
		return id
	}
}

// moduleRoot is the repo-qualifying prefix stripped from external module
// paths so no github.com/... path ever appears in a symbol segment (§4.1).
// It must match the go.mod module path.
const moduleRoot = "github.com/Syamchand123/GlassMarble/"

func stripModuleRoot(path string) string {
	if strings.HasPrefix(path, moduleRoot) {
		return strings.TrimPrefix(path, moduleRoot)
	}
	return path
}

// looksCanonical reports whether id uses raw-canonical encoding: no
// whitespace, quotes, backslashes or URI-unsafe characters that a canonical
// encoder would have percent-escaped. Legacy forms (e.g. `ext:alias "uri"`)
// always contain such characters and are routed to the legacy shim.
func looksCanonical(id string) bool {
	return !strings.ContainsAny(id, " \t\r\n\"\\<>{}|^`")
}

// encodeSegs URL-encodes every non-empty segment.
func encodeSegs(segs []string) []string {
	out := make([]string, len(segs))
	for i, s := range segs {
		if s == "" {
			out[i] = ""
			continue
		}
		out[i] = encodeSeg(s)
	}
	return out
}

// encodeSeg escapes every byte that is not unreserved ([A-Za-z0-9-._~]) or a
// path separator, using uppercase %XX escapes. This keeps file paths readable
// while reserving `:` and `::` and every other grammar character.
func encodeSeg(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == '_' || c == '~' || c == '/' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

// percentDecode decodes %XX escapes; invalid sequences are left verbatim.
func percentDecode(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if c, ok := decodeHex(s[i+1], s[i+2]); ok {
				b.WriteByte(c)
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func decodeHex(hi, lo byte) (byte, bool) {
	h, ok1 := hexVal(hi)
	l, ok2 := hexVal(lo)
	if !ok1 || !ok2 {
		return 0, false
	}
	return h<<4 | l, true
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
