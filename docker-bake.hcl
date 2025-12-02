# Docker Bake configuration for scriptschnell
# Defines multiple targets for testing, linting, and building

variable "REGISTRY" {
  default = "ghcr.io"
}

variable "IMAGE_NAME" {
  default = "statcode-ai/scriptschnell"
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
}

# Target for running tests (simplified)
target "test" {
  context = "."
  dockerfile = "Dockerfile"
  target = "test"
  tags = ["${IMAGE_NAME}:test"]
  output = ["type=cacheonly"]
  cache-from = ["type=gha"]
  cache-to = ["type=gha,mode=max"]
}

# Target for building the main application
target "build" {
  context = "."
  dockerfile = "Dockerfile"
  target = "build"
  tags = ["${IMAGE_NAME}:build"]
  output = ["type=local,dest=./bin"]
}

# CI pipeline target (lint -> test -> build)
target "ci" {
  context = "."
  dockerfile = "Dockerfile"
  target = "build"
  tags = ["${IMAGE_NAME}:ci"]
  output = ["type=local,dest=./bin"]
}

# Target for building debug HTML tool
target "build-debug" {
  context = "."
  dockerfile = "Dockerfile"
  target = "build-debug"
  tags = ["${IMAGE_NAME}:build-debug"]
  output = ["type=local,dest=./bin"]
}

# Default target
target "default" {
  context = "."
  dockerfile = "Dockerfile"
  target = "build"
  tags = ["${IMAGE_NAME}:latest"]
  output = ["type=local,dest=./bin"]
}

# Group for CI pipeline (lint -> test -> build)
group "ci" {
  targets = ["lint", "test", "build"]
}

# Group for all targets
group "all" {
  targets = ["lint", "test", "build", "build-debug"]
}