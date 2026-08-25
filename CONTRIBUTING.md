# Contributing to GlassMarble

Thank you for your interest in contributing to **GlassMarble**! GlassMarble is an open-source AI Architecture Intelligence platform that constructs, queries, and visualizes living Architecture Knowledge Graphs (AKG) from multi-language source code repositories.

---

## 🛠️ Development Setup

### Prerequisites
- **Go 1.25+** (see `go.mod` — CI uses `go-version-file: go.mod`)
- **Git**
- Optional: **Make**

### Quick Clone & Build
```bash
git clone https://github.com/Syamchand123/GlassMarble.git
cd GlassMarble

# Build the gmb binary
make build

# Run the test suite
make test

# Run static analysis
make vet
```

---

## 📦 Useful Make Targets

| Make Target | Command | Description |
|---|---|---|
| `make build` | `go build -trimpath -ldflags "..." -o gmb .` | Builds the static binary with stamped version metadata. |
| `make test` | `go test -v -count=1 ./...` | Executes the complete test suite including AKG determinism tests. |
| `make vet` | `go vet ./...` | Runs Go static analysis across all packages. |
| `make completions` | `go run ./cmd/completions -o completions` | Pre-generates shell completions for Bash, Zsh, Fish, and PowerShell. |
| `make man` | `go run ./cmd/man -o man/man1` | Regenerates the 32 UNIX roff manual pages in `man/man1/`. |
| `make clean` | `rm -rf gmb dist/ ...` | Removes all local build artifacts. |

---

## 🏗️ Architecture Overview

The codebase is organized into modular packages under `internal/`:

```text
internal/
├── akg/                       # AKG GraphJSON database, MVCC transaction manager, lockfile
├── app/                       # Application bootstrapping and DI container
├── code_analysis_engine/      # 4-stage ingestion and semantic linking pipeline
│   ├── ingest/                # Native Tree-sitter parsers for 14 languages
│   ├── normalize/             # Generic AST (GAST) normalization & I/O primitives
│   ├── aggregate/             # Package clustering and visibility resolution
│   └── link/                  # Semantic call-graph and interface linking
├── visualization_engine/      # 31 diagram synthesis layouts (UML, C4, Specialized)
│   ├── extract/               # Virtual subgraph extraction algorithms
│   ├── layout/                # Node positioning and edge routing
│   └── render/                # Multi-format serializers (Mermaid, PlantUML, DOT)
├── ai_engine/                 # Grounded AI architect agent with tool calling and BYOK
├── developer_memory/          # Longitudinal architectural memory and event WAL
├── learning/                  # Convention learning and human correction overlay
├── product/                   # Product metadata, version triple, and unified pipeline
└── tui/                       # Charm-based terminal UI (Lip Gloss, Bubble Tea, Huh, Fang)
```

---

## 🧪 Testing & Golden Parity

GlassMarble enforces strict determinism and golden fixture parity:

1. **AKG Determinism Gate**:
   The generated `akg.json` must be byte-identical across runs on the same codebase.
   ```bash
   go test ./internal/akg/... -run Deterministic -v
   ```

2. **Golden Diagram Tests**:
   Diagram outputs are verified against checked-in golden fixtures. If you intentionally improve a layout or diagram generator, rebase the goldens:
   ```bash
   go run . dev rebase-goldens
   ```

---

## 🏷️ Release Process

Releases are fully automated via GitHub Actions and GoReleaser:
1. Ensure the `refactor/packaging` or `main` branch passes all CI checks across Linux, macOS, and Windows.
2. Create and push a semantic version tag:
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```
3. GitHub Actions builds the multi-platform binary archives, signs them with Cosign, generates SBOMs, and publishes the release.
