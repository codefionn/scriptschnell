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

# Target for building the main application
target "build" {
  context = "."
  dockerfile = "Dockerfile"
  target = "final-build"
  tags = ["${IMAGE_NAME}:build"]
  output = ["type=local,dest=./bin"]
  cache-from = ["type=local,src=.cache/build"]
  cache-to = ["type=local,dest=.cache/build"]
  platforms = ["linux/amd64", "linux/arm64"]
}

# CI pipeline target (lint -> test -> build)
target "ci" {
  context = "."
  dockerfile = "Dockerfile"
  target = "final-build"
  tags = ["${IMAGE_NAME}:ci"]
  output = ["type=local,dest=./bin"]
  cache-from = ["type=local,src=.cache/build"]
  cache-to = ["type=local,dest=.cache/build"]
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

# Default target
target "default" {
  context = "."
  dockerfile = "Dockerfile"
  target = "final-build"
  tags = ["${IMAGE_NAME}:latest"]
  output = ["type=local,dest=./bin"]
  cache-from = ["type=local,src=.cache/build"]
  cache-to = ["type=local,dest=.cache/build"]
  platforms = ["linux/amd64", "linux/arm64"]
}

# Group for CI pipeline (lint -> test -> build)
group "ci" {
  targets = ["lint", "test", "build"]
}

# Group for all targets
group "all" {
  targets = ["lint", "test", "build", "build-debug"]
}
