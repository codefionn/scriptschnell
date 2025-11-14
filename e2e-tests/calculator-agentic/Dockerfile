FROM tinygo/tinygo:0.39.0 AS tinygo

FROM golang:1.25-bookworm AS builder

# Install build dependencies
RUN apt-get update && apt-get install -y git gcc libc6-dev && rm -rf /var/lib/apt/lists/*

# Bring in tinygo toolchain
COPY --from=tinygo /usr/local/tinygo /usr/local/tinygo
ENV PATH="/usr/local/tinygo/bin:${PATH}"

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY ./internal ./internal
COPY ./cmd ./cmd

# Build statcode-ai
RUN go build -o statcode-ai ./cmd/statcode-ai

# Final stage - use Go image since statcode-ai will build Go programs
FROM golang:1.25-bookworm

# Install necessary tools
RUN apt-get update && apt-get install -y bash ca-certificates && rm -rf /var/lib/apt/lists/*

# Copy tinygo from stage to final image
COPY --from=tinygo /usr/local/tinygo /usr/local/tinygo
ENV PATH="/usr/local/tinygo/bin:${PATH}"

# Copy statcode-ai binary from builder
COPY --from=builder /app/statcode-ai /usr/local/bin/statcode-ai

# Create config directory
RUN mkdir -p /root/.config/statcode-ai

WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/statcode-ai"]
