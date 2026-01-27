# Docker Bake configuration for scriptschnell
# Defines multiple targets for testing, linting, and building

variable "REGISTRY" {
  default = "ghcr.io"
}

variable "IMAGE_NAME" {
  default = "scriptschnell"
}

variable "TAG" {
  default = "latest"
}

# Target for running linting
target "lint" {
  context = "."
  dockerfile = "Dockerfile"
  target = "lint"
  tags = ["${IMAGE_NAME}:lint"]
  output = ["type=cacheonly"]
  cache-from = ["type=local,src=.cache/lint"]
  cache-to = ["type=local,dest=.cache/lint"]
}

# Target for running tests (simplified)
target "test" {
  context = "."
  dockerfile = "Dockerfile"
  target = "test"
  tags = ["${IMAGE_NAME}:test"]
  output = ["type=cacheonly"]
  cache-from = ["type=local,src=.cache/test"]
  cache-to = ["type=local,dest=.cache/test"]
}

# Platform-specific build targets
target "build-amd64" {
  context = "."
  dockerfile = "Dockerfile"
  target = "final-build-amd64"
  tags = ["${IMAGE_NAME}:build-amd64"]
  output = ["type=local,dest=./bin/linux_amd64"]
  cache-from = ["type=local,src=.cache/build"]
  cache-to = ["type=local,dest=.cache/build"]
}

target "build-arm64" {
  context = "."
  dockerfile = "Dockerfile"
  target = "final-build-arm64"
  tags = ["${IMAGE_NAME}:build-arm64"]
  output = ["type=local,dest=./bin/linux_arm64"]
  cache-from = ["type=local,src=.cache/build"]
  cache-to = ["type=local,dest=.cache/build"]
}

# Combined build target (both platforms)
group "build" {
  targets = ["build-amd64", "build-arm64"]
}

# CI pipeline target (lint -> test -> build both platforms)
group "ci" {
  targets = ["lint", "test", "build-amd64", "build-arm64"]
}

# Target for building debug HTML tool
target "build-debug" {
  context = "."
  dockerfile = "Dockerfile"
  target = "build-debug"
  tags = ["${IMAGE_NAME}:build-debug"]
  output = ["type=local,dest=./bin"]
  cache-from = ["type=local,src=.cache/build-debug"]
  cache-to = ["type=local,dest=.cache/build-debug"]
}

# Default target (amd64 only for backward compatibility)
target "default" {
  context = "."
  dockerfile = "Dockerfile"
  target = "final-build-amd64"
  tags = ["${IMAGE_NAME}:latest"]
  output = ["type=local,dest=./bin/linux_amd64"]
  cache-to = ["type=local,dest=.cache/build"]
}


# Group for all targets
group "all" {
  targets = ["lint", "test", "build", "build-debug"]
}