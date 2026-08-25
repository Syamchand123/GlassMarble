package product

// Version is the GlassMarble release version surfaced by `gmb version` and
// stamped into generated diagram headers. It is overridden at build time via:
//
//	go build -ldflags "-X github.com/Syamchand123/GlassMarble/internal/product.Version=v1.0.0"
//
// The Makefile and GoReleaser inject this automatically; the fallback "0.1.0"
// is intentionally a non-v-prefixed dev marker.
var Version = "0.1.0"

// Commit is the short git SHA injected at build time:
//
//	-X github.com/Syamchand123/GlassMarble/internal/product.Commit=abc1234
var Commit = "none"

// Date is the RFC3339 build timestamp injected at build time:
//
//	-X github.com/Syamchand123/GlassMarble/internal/product.Date=2026-08-24T00:00:00Z
var Date = "unknown"

// BuiltBy identifies the build system (goreleaser | make | go-install | dev):
//
//	-X github.com/Syamchand123/GlassMarble/internal/product.BuiltBy=goreleaser
var BuiltBy = "dev"
