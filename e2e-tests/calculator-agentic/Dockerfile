FROM golang:1.25-bookworm AS builder

# Install dependencies including CGO requirements for wasmtime
RUN apt-get update && apt-get install -y git gcc libc6-dev && rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY ./internal ./internal
COPY ./cmd ./cmd

# Build statcode-ai with CGO enabled (required for wasmtime)
ENV CGO_ENABLED=1
RUN go build -o statcode-ai ./cmd/statcode-ai

# Final stage - use Go image since statcode-ai will build Go programs
FROM golang:1.25-bookworm

# Install necessary tools
RUN apt-get update && apt-get install -y bash ca-certificates && rm -rf /var/lib/apt/lists/*

# Copy statcode-ai binary from builder
COPY --from=builder /app/statcode-ai /usr/local/bin/statcode-ai

# Create config directory
RUN mkdir -p /root/.config/statcode-ai

WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/statcode-ai"]
