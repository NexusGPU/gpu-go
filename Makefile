.PHONY: help build install clean test test-unit test-e2e test-coverage test-verbose test-race fmt vet lint deps tidy

# Variables
BINARY_NAME=ggo
MAIN_PACKAGE=./cmd/ggo
BUILD_DIR=./bin
COVERAGE_DIR=./coverage
GO_VERSION=1.25.0

# Build flags
LDFLAGS=-s -w
BUILD_FLAGS=-trimpath

# Default target
.DEFAULT_GOAL := help

help: ## Display this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# Build targets
build: ## Build the binary (default: release build)
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@go build $(BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "Binary built: $(BUILD_DIR)/$(BINARY_NAME)"

build-debug: ## Build the binary with debug symbols
	@echo "Building $(BINARY_NAME) (debug)..."
	@mkdir -p $(BUILD_DIR)
	@go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "Debug binary built: $(BUILD_DIR)/$(BINARY_NAME)"

build-race: ## Build the binary with race detector
	@echo "Building $(BINARY_NAME) (race detector)..."
	@mkdir -p $(BUILD_DIR)
	@go build $(BUILD_FLAGS) -race -o $(BUILD_DIR)/$(BINARY_NAME)-race $(MAIN_PACKAGE)
	@echo "Race detector binary built: $(BUILD_DIR)/$(BINARY_NAME)-race"

install: ## Install the binary to GOPATH/bin
	@echo "Installing $(BINARY_NAME)..."
	@go install $(BUILD_FLAGS) -ldflags "$(LDFLAGS)" $(MAIN_PACKAGE)
	@echo "Installed to $$(go env GOPATH)/bin/$(BINARY_NAME)"

# Test targets
test: ## Run all tests
	@echo "Running all tests..."
	@go test -v ./...

test-unit: ## Run unit tests only (excludes E2E tests)
	@echo "Running unit tests..."
	@go test -v -short ./...

test-e2e: ## Run E2E tests only
	@echo "Running E2E tests..."
	@go test -v -run "E2E" ./...

test-verbose: ## Run all tests with verbose output
	@echo "Running all tests (verbose)..."
	@go test -v -count=1 ./...

test-race: ## Run tests with race detector
	@echo "Running tests with race detector..."
	@go test -race -v ./...

test-coverage: ## Run tests and generate coverage report
	@echo "Running tests with coverage..."
	@mkdir -p $(COVERAGE_DIR)
	@go test -v -coverprofile=$(COVERAGE_DIR)/coverage.out ./...
	@go tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@go tool cover -func=$(COVERAGE_DIR)/coverage.out
	@echo "Coverage report generated: $(COVERAGE_DIR)/coverage.html"

test-coverage-unit: ## Run unit tests with coverage
	@echo "Running unit tests with coverage..."
	@mkdir -p $(COVERAGE_DIR)
	@go test -v -short -coverprofile=$(COVERAGE_DIR)/coverage-unit.out ./...
	@go tool cover -html=$(COVERAGE_DIR)/coverage-unit.out -o $(COVERAGE_DIR)/coverage-unit.html
	@go tool cover -func=$(COVERAGE_DIR)/coverage-unit.out
	@echo "Unit test coverage report: $(COVERAGE_DIR)/coverage-unit.html"

test-coverage-e2e: ## Run E2E tests with coverage
	@echo "Running E2E tests with coverage..."
	@mkdir -p $(COVERAGE_DIR)
	@go test -v -run "E2E" -coverprofile=$(COVERAGE_DIR)/coverage-e2e.out ./...
	@go tool cover -html=$(COVERAGE_DIR)/coverage-e2e.out -o $(COVERAGE_DIR)/coverage-e2e.html
	@go tool cover -func=$(COVERAGE_DIR)/coverage-e2e.out
	@echo "E2E test coverage report: $(COVERAGE_DIR)/coverage-e2e.html"

test-bench: ## Run benchmark tests
	@echo "Running benchmark tests..."
	@go test -bench=. -benchmem ./...

test-bench-cpu: ## Run benchmark tests with CPU profile
	@echo "Running benchmark tests with CPU profile..."
	@go test -bench=. -benchmem -cpuprofile=$(COVERAGE_DIR)/cpu.prof ./...
	@echo "CPU profile generated: $(COVERAGE_DIR)/cpu.prof"

test-bench-mem: ## Run benchmark tests with memory profile
	@echo "Running benchmark tests with memory profile..."
	@go test -bench=. -benchmem -memprofile=$(COVERAGE_DIR)/mem.prof ./...
	@echo "Memory profile generated: $(COVERAGE_DIR)/mem.prof"

# Code quality targets
fmt: ## Format Go code
	@echo "Formatting Go code..."
	@go fmt ./...
	@echo "Code formatted"

vet: ## Run go vet
	@echo "Running go vet..."
	@go vet ./...
	@echo "Vet checks passed"

lint: ## Run golangci-lint (if installed)
	@echo "Running golangci-lint..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

check: fmt vet ## Format code and run vet checks

# Dependency management
deps: ## Download dependencies
	@echo "Downloading dependencies..."
	@go mod download
	@echo "Dependencies downloaded"

tidy: ## Tidy go.mod and go.sum
	@echo "Tidying go.mod..."
	@go mod tidy
	@echo "go.mod tidied"

# Clean targets
clean: ## Remove build artifacts
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@rm -rf $(COVERAGE_DIR)
	@go clean -cache
	@echo "Clean complete"

clean-coverage: ## Remove coverage reports only
	@echo "Cleaning coverage reports..."
	@rm -rf $(COVERAGE_DIR)
	@echo "Coverage reports cleaned"

clean-build: ## Remove build binaries only
	@echo "Cleaning build binaries..."
	@rm -rf $(BUILD_DIR)
	@echo "Build binaries cleaned"

# Development workflow
dev: clean deps build test ## Full development workflow: clean, deps, build, test

ci: tidy check test-coverage ## CI workflow: tidy, check, test with coverage

# Cross-platform builds (examples)
build-linux: ## Build for Linux
	@echo "Building for Linux..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=linux GOARCH=amd64 go build $(BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PACKAGE)
	@echo "Linux binary: $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64"

build-darwin: ## Build for macOS
	@echo "Building for macOS..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=darwin GOARCH=amd64 go build $(BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 $(MAIN_PACKAGE)
	@GOOS=darwin GOARCH=arm64 go build $(BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 $(MAIN_PACKAGE)
	@echo "macOS binaries built"

build-windows: ## Build for Windows
	@echo "Building for Windows..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=windows GOARCH=amd64 go build $(BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PACKAGE)
	@echo "Windows binary: $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe"

build-all: build-linux build-darwin build-windows ## Build for all platforms
