# ─────────────────────────────────────────────────────────────────────────────
# GlassMarble Makefile
# ─────────────────────────────────────────────────────────────────────────────

BINARY_NAME := gmb
ALIAS_NAME  := glassmarble
MODULE      := github.com/Syamchand123/GlassMarble

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v1.2.0-dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo "unknown")

LDFLAGS := -s -w \
	-X $(MODULE)/internal/product.Version=$(VERSION) \
	-X $(MODULE)/internal/product.Commit=$(COMMIT) \
	-X $(MODULE)/internal/product.Date=$(DATE) \
	-X $(MODULE)/internal/product.BuiltBy=make

GO ?= go

.PHONY: all build install test vet lint clean completions man help

all: build

## build: Build the gmb binary with stamped ldflags (CGO required for tree-sitter)
build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) .

## install: Install gmb to $$GOPATH/bin (CGO required for tree-sitter)
install:
	$(GO) install -trimpath -ldflags "$(LDFLAGS)" .

## cross: Build for the host platform (releases use the native CI matrix)
# Releases are cut by .github/workflows/release.yml; the tree-sitter cgo
# bindings cannot be cross-compiled from a single runner.
cross: build

## snapshot: Alias for a host build (see cross)
snapshot: build

## release: Guarded no-op that points at the GitHub Actions release workflow
release:
	@echo "Releases are published by .github/workflows/release.yml."
	@echo "Tag the commit and push the tag:"
	@echo "  git tag -a v1.2.0 -m 'Release v1.2.0' && git push origin v1.2.0"
	@if [ -z "$$(git tag -l --points-at HEAD)" ]; then \
		echo "Error: HEAD is not tagged — nothing to release."; \
		exit 1; \
	fi

## test: Run unit, integration and regression test suites
test:
	$(GO) test -v -count=1 ./...

## vet: Run go vet on the entire codebase
vet:
	$(GO) vet ./...

## lint: Run golangci-lint
lint:
	golangci-lint run ./...

## completions: Generate shell completions for bash, zsh, fish, powershell
completions:
	@mkdir -p completions
	$(GO) run ./cmd/completions -o completions

## man: Regenerate the canonical man pages in docs/man (CI-gated)
man:
	$(GO) run ./cmd/man -o docs/man


## clean: Remove local build artifacts (tracked completions/ and docs/man are kept)
clean:
	rm -rf $(BINARY_NAME) $(BINARY_NAME).exe $(ALIAS_NAME) $(ALIAS_NAME).exe dist/

## help: Display this help message
help:
	@echo "GlassMarble Build Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed -e 's/## //' | awk 'BEGIN {FS = ": "}; {printf "  %-14s %s\n", $$1, $$2}'
