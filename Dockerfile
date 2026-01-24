# Multi-stage Dockerfile for scriptschnell
# Supports testing, linting, and building the Go application

# TinoGo dependency
FROM tinygo/tinygo:0.39.0 AS tinygo

# Base stage with Go and common dependencies
FROM golang:1.25-alpine AS base
WORKDIR /app

# Install git, C development tools, and other build tools
RUN apk add --no-cache git ca-certificates tzdata gcc musl-dev \
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

# Build stage for main binary
FROM base AS build
# Install templ
RUN go install github.com/a-h/templ/cmd/templ@latest

COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Generate templ templates before building
RUN echo "Generating templ templates..." && \
    templ generate

# Build the main application
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -a -installsuffix cgo \
    -o scriptschnell \
    ./cmd/scriptschnell

# Build debug HTML tool
FROM base AS build-debug
# Install templ
RUN go install github.com/a-h/templ/cmd/templ@latest

COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Generate templ templates before building
RUN echo "Generating templ templates..." && \
    templ generate

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -a -installsuffix cgo \
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