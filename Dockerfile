# Build stage — pinned to go.mod toolchain (1.25.x)
FROM golang:1.25-alpine AS builder

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

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w \
      -X github.com/Syamchand123/GlassMarble/internal/product.Version=${VERSION} \
      -X github.com/Syamchand123/GlassMarble/internal/product.Commit=${COMMIT} \
      -X github.com/Syamchand123/GlassMarble/internal/product.Date=${DATE} \
      -X github.com/Syamchand123/GlassMarble/internal/product.BuiltBy=docker" \
    -o /build/gmb . && \
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
