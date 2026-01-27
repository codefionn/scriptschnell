# Multi-stage Dockerfile for scriptschnell
# Supports testing, linting, and cross-compiling for multiple architectures
# Build stages use Debian, runtime uses Alpine

# TinyGo dependency
FROM tinygo/tinygo:0.39.0 AS tinygo

# Base stage with Go (Debian) and common dependencies
FROM golang:1.25-bookworm AS base
WORKDIR /app

# Install git, ca-certificates, tzdata, and cross-compilation toolchains
RUN apt-get update && apt-get install -y \
    git \
    ca-certificates \
    tzdata \
    gcc \
    g++ \
    libc6-dev \
    gcc-x86-64-linux-gnu \
    gcc-aarch64-linux-gnu \
    libc6-dev-amd64-cross \
    libc6-dev-arm64-cross \
    && rm -rf /var/lib/apt/lists/* \
    && update-ca-certificates

COPY --from=tinygo /usr/local/tinygo /usr/local/tinygo
ENV PATH="/usr/local/tinygo/bin:${PATH}"

ENV GOPATH=/go

# Copy go mod files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Linting stage (minimal)
FROM base AS lint
RUN go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest && \
    go install github.com/a-h/templ/cmd/templ@latest

# Copy source code
COPY . .

# Generate templ templates before linting
RUN echo "Generating templ templates..." && \
    templ generate

# Run linters with CGO enabled for tree-sitter
RUN echo "Running golangci-lint..." && \
    CGO_ENABLED=1 golangci-lint run --timeout=5m

# Testing stage
FROM base AS test
# Install templ
RUN go install github.com/a-h/templ/cmd/templ@latest

COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Generate templ templates before testing
RUN echo "Generating templ templates..." && \
    templ generate

# Run tests with CGO enabled for tree-sitter
ARG CI=1
RUN CGO_ENABLED=1 go test -short ./...

# Build stage for linux/amd64
FROM base AS build-amd64
# Install templ
RUN go install github.com/a-h/templ/cmd/templ@latest

COPY go.mod go.sum ./
RUN go mod download

# Download and create embedded TinyGo archive for linux/amd64 (cached layer - only runs when go.mod changes)
RUN echo "Preparing embedded TinyGo for linux/amd64..." && \
    mkdir -p internal/tools/embedded && \
    cd internal/tools/embedded && \
    curl -fsSL "https://github.com/tinygo-org/tinygo/releases/download/v0.39.0/tinygo0.39.0.linux-amd64.tar.gz" -o tinygo_linux_amd64.tar.gz && \
    echo "Re-compressing TinyGo archive with maximum compression..." && \
    gunzip -c tinygo_linux_amd64.tar.gz | gzip -9 > tinygo_linux_amd64.tar.gz.tmp && \
    mv tinygo_linux_amd64.tar.gz.tmp tinygo_linux_amd64.tar.gz

COPY . .

# Generate templ templates before building
RUN echo "Generating templ templates..." && \
    templ generate

# Generate embedded TinyGo file for linux/amd64
RUN echo "Generating embedded TinyGo for linux/amd64..." && \
    GOOS=linux GOARCH=amd64 go run internal/tools/embed_tinygo_gen.go -goos=linux -goarch=amd64 internal/tools/embedded/tinygo_linux_amd64.tar.gz

# Build for linux/amd64 with static linking and embedded TinyGo
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    CC=x86_64-linux-gnu-gcc \
    CXX=x86_64-linux-gnu-g++ \
    go build \
    -ldflags="-w -s -linkmode external -extldflags '-static'" \
    -tags="tinygo_embed,tinygo_has_embed_data" \
    -o scriptschnell-amd64 \
    ./cmd/scriptschnell

# Build stage for linux/arm64
FROM base AS build-arm64
# Install templ
RUN go install github.com/a-h/templ/cmd/templ@latest

COPY go.mod go.sum ./
RUN go mod download

# Download and create embedded TinyGo archive for linux/arm64 (cached layer - only runs when go.mod changes)
RUN echo "Preparing embedded TinyGo for linux/arm64..." && \
    mkdir -p internal/tools/embedded && \
    cd internal/tools/embedded && \
    curl -fsSL "https://github.com/tinygo-org/tinygo/releases/download/v0.39.0/tinygo0.39.0.linux-arm64.tar.gz" -o tinygo_linux_arm64.tar.gz && \
    echo "Re-compressing TinyGo archive with maximum compression..." && \
    gunzip -c tinygo_linux_arm64.tar.gz | gzip -9 > tinygo_linux_arm64.tar.gz.tmp && \
    mv tinygo_linux_arm64.tar.gz.tmp tinygo_linux_arm64.tar.gz

COPY . .

# Generate templ templates before building
RUN echo "Generating templ templates..." && \
    templ generate

# Generate embedded TinyGo file for linux/arm64
RUN echo "Generating embedded TinyGo for linux/arm64..." && \
    GOOS=linux GOARCH=arm64 go run internal/tools/embed_tinygo_gen.go -goos=linux -goarch=arm64 internal/tools/embedded/tinygo_linux_arm64.tar.gz

# Build for linux/arm64 with static linking and embedded TinyGo
RUN CGO_ENABLED=1 GOOS=linux GOARCH=arm64 \
    CC=aarch64-linux-gnu-gcc \
    CXX=aarch64-linux-gnu-g++ \
    go build \
    -ldflags="-w -s -linkmode external -extldflags '-static'" \
    -tags="tinygo_embed,tinygo_has_embed_data" \
    -o scriptschnell-arm64 \
    ./cmd/scriptschnell

# Default build stage (amd64 for backward compatibility)
FROM build-amd64 AS build

# Build debug HTML tool (amd64)
FROM base AS build-debug
# Install templ
RUN go install github.com/a-h/templ/cmd/templ@latest

COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Generate templ templates before building
RUN echo "Generating templ templates..." && \
    templ generate

# Build for linux/amd64
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    CC=x86_64-linux-gnu-gcc \
    CXX=x86_64-linux-gnu-g++ \
    go build \
    -ldflags="-w -s -linkmode external -extldflags '-static'" \
    -o debug_html \
    ./cmd/debug_html

# Development stage with all tools
FROM base AS development
RUN go install github.com/cosmtrek/air@latest && \
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest && \
    go install github.com/a-h/templ/cmd/templ@latest

COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Generate templ templates
RUN echo "Generating templ templates..." && \
    templ generate

CMD ["air", "-c", ".air.toml"]

# Scratch-based final build (amd64)
FROM scratch AS final-build-amd64
COPY --from=build-amd64 /app/scriptschnell-amd64 /scriptschnell
ENTRYPOINT ["/scriptschnell"]

# Scratch-based final build (arm64)
FROM scratch AS final-build-arm64
COPY --from=build-arm64 /app/scriptschnell-arm64 /scriptschnell
ENTRYPOINT ["/scriptschnell"]

# Default scratch final build (amd64)
FROM final-build-amd64 AS final-build
