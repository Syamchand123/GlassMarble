# Build stage — pinned to go.mod toolchain (1.25.x)
# --platform=$BUILDPLATFORM keeps the COMPILER on the runner's native arch;
# cross-compile to TARGETARCH via plain env vars (CGO_ENABLED=0 = pure static).
# This avoids QEMU-emulated arm64 builds, which die with bare "exit code: 1"
# on large packages (tree-sitter grammar tables).
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

WORKDIR /build

# Copy module files first for layer caching
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build static binaries
ARG VERSION=v1.0.0-dev
ARG COMMIT=none
ARG DATE=unknown
## Provided automatically by docker buildx for each target platform:
ARG TARGETOS
ARG TARGETARCH

# -buildvcs=false : .dockerignore excludes .git, and Go >=1.18 can fail with
#   "error obtaining VCS status" when a repo is partially visible.
# GOTOOLCHAIN=local : fail fast with a clear message instead of silently
#   downloading a different toolchain mid-build.
# set -eux : surface the real compiler error instead of a bare exit-code-1.
RUN set -eux; \
    export CGO_ENABLED=0 GOOS="${TARGETOS:-linux}" GOARCH="${TARGETARCH:-amd64}"; \
    export GOTOOLCHAIN=local GOFLAGS=-buildvcs=false; \
    LDFLAGS="-s -w \
      -X github.com/Syamchand123/GlassMarble/internal/product.Version=${VERSION} \
      -X github.com/Syamchand123/GlassMarble/internal/product.Commit=${COMMIT} \
      -X github.com/Syamchand123/GlassMarble/internal/product.Date=${DATE} \
      -X github.com/Syamchand123/GlassMarble/internal/product.BuiltBy=docker"; \
    go build -trimpath -ldflags "${LDFLAGS}" -o /build/gmb . ; \
    cp /build/gmb /build/glassmarble

# Runtime stage: minimal non-root distroless
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="GlassMarble"
LABEL org.opencontainers.image.description="AI Architecture Intelligence Platform"
LABEL org.opencontainers.image.url="https://github.com/Syamchand123/GlassMarble"
LABEL org.opencontainers.image.source="https://github.com/Syamchand123/GlassMarble"
LABEL org.opencontainers.image.licenses="MIT"

COPY --from=builder /build/gmb /usr/local/bin/gmb
COPY --from=builder /build/glassmarble /usr/local/bin/glassmarble

USER nonroot:nonroot
WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/gmb"]
CMD ["--help"]
