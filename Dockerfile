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
COPY . .

# Generate templ templates before building
RUN echo "Generating templ templates..." && \
    templ generate

# Build for linux/amd64 with static linking using cross-compiler
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    CC=x86_64-linux-gnu-gcc \
    CXX=x86_64-linux-gnu-g++ \
    go build \
    -ldflags="-w -s -linkmode external -extldflags '-static'" \
    -o scriptschnell-amd64 \
    ./cmd/scriptschnell

# Build stage for linux/arm64
FROM base AS build-arm64
# Install templ
RUN go install github.com/a-h/templ/cmd/templ@latest

COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Generate templ templates before building
RUN echo "Generating templ templates..." && \
    templ generate

# Build for linux/arm64 with static linking using cross-compiler
RUN CGO_ENABLED=1 GOOS=linux GOARCH=arm64 \
    CC=aarch64-linux-gnu-gcc \
    CXX=aarch64-linux-gnu-g++ \
    go build \
    -ldflags="-w -s -linkmode external -extldflags '-static'" \
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

# Final stage for amd64 - minimal Alpine image with just the binary
FROM alpine:3.18 AS final-amd64
WORKDIR /app

# Copy only the built binary from the build stage
COPY --from=build-amd64 /app/scriptschnell-amd64 /app/scriptschnell

# Set entrypoint to run the binary
ENTRYPOINT ["./scriptschnell"]

# Final stage for arm64 - minimal Alpine image with just the binary
FROM alpine:3.18 AS final-arm64
WORKDIR /app

# Copy only the built binary from the build stage
COPY --from=build-arm64 /app/scriptschnell-arm64 /app/scriptschnell

# Set entrypoint to run the binary
ENTRYPOINT ["./scriptschnell"]

# Default final stage (amd64 for backward compatibility)
FROM final-amd64 AS final

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