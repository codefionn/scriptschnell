# Multi-stage Dockerfile for scriptschnell
# Supports testing, linting, and building the Go application

# TinoGo dependency
FROM tinygo/tinygo:0.39.0 AS tinygo

# Base stage with Go and common dependencies
FROM golang:1.25-alpine AS base
WORKDIR /app

# Install git and other build tools
RUN apk add --no-cache git ca-certificates tzdata \
    && update-ca-certificates

COPY --from=tinygo /usr/local/tinygo /usr/local/tinygo
ENV PATH="/usr/local/tinygo/bin:${PATH}"

RUN mkdir /go
ENV GOPATH=/go

# Copy go mod files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Linting stage (minimal)
FROM base AS lint
RUN go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Copy source code
COPY . .

# Run linters
RUN echo "Running golangci-lint..." && \
    golangci-lint run --timeout=5m

# Testing stage
FROM base AS test
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Run tests
ARG CI=1
RUN go test -short ./...

# Build stage for main binary
FROM base AS build
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Build the main application
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -a -installsuffix cgo \
    -o scriptschnell \
    ./cmd/scriptschnell

# Build debug HTML tool
FROM base AS build-debug
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -a -installsuffix cgo \
    -o debug_html \
    ./cmd/debug_html

# Development stage with all tools
FROM base AS development
RUN go install github.com/cosmtrek/air@latest && \
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

COPY go.mod go.sum ./
RUN go mod download
COPY . .

CMD ["air", "-c", ".air.toml"]