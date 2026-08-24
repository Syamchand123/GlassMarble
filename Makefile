# ─────────────────────────────────────────────────────────────────────────────
# GlassMarble Makefile
# ─────────────────────────────────────────────────────────────────────────────

BINARY_NAME := gmb
ALIAS_NAME  := glassmarble
MODULE      := github.com/Syamchand123/GlassMarble

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v1.0.0-dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo "unknown")

LDFLAGS := -s -w \
	-X $(MODULE)/internal/product.Version=$(VERSION) \
	-X $(MODULE)/internal/product.Commit=$(COMMIT) \
	-X $(MODULE)/internal/product.Date=$(DATE) \
	-X $(MODULE)/internal/product.BuiltBy=make

GO ?= go

.PHONY: all build cross snapshot release install test vet lint clean completions man help

all: build

## build: Build the gmb binary with stamped ldflags
build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) .

## install: Install gmb to $$GOPATH/bin
install:
	CGO_ENABLED=0 $(GO) install -trimpath -ldflags "$(LDFLAGS)" .

## cross: Build snapshot binaries across all platforms via GoReleaser
cross:
	goreleaser build --snapshot --clean

## snapshot: Produce full release distribution archives locally without publishing
snapshot:
	goreleaser release --snapshot --clean

## release: Publish a tagged release via GoReleaser (guarded by git tag check)
release:
	@if [ -z "$$(git tag -l --points-at HEAD)" ]; then \
		echo "Error: HEAD must be tagged (e.g. v1.0.0) to cut a release."; \
		exit 1; \
	fi
	goreleaser release --clean

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
	$(GO) run . completion bash > completions/$(BINARY_NAME).bash
	$(GO) run . completion zsh > completions/$(BINARY_NAME).zsh
	$(GO) run . completion fish > completions/$(BINARY_NAME).fish
	$(GO) run . completion powershell > completions/$(BINARY_NAME).ps1

## man: Generate man pages for gmb and subcommands
man:
	@mkdir -p man/man1
	$(GO) run ./cmd/man -o man/man1/ || true

## docker: Build local Docker image
docker:
	docker build -t ghcr.io/syamchand123/glassmarble:local .

## clean: Remove build artifacts and dist/ directory
clean:
	rm -rf $(BINARY_NAME) $(BINARY_NAME).exe $(ALIAS_NAME) $(ALIAS_NAME).exe dist/ completions/ man/

## help: Display this help message
help:
	@echo "GlassMarble Build Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed -e 's/## //' | awk 'BEGIN {FS = ": "}; {printf "  %-14s %s\n", $$1, $$2}'
