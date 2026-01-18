# Makefile for Vote Coloré - Real-time voting application
# Usage: make [target]

# Variables
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -s -w"

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod

# Binary names and paths
BINARY_NAME := vote-server
BINARY_PATH := ./backend

# Package name
PACKAGE_NAME := vote
PACKAGE_VERSION := $(VERSION)

# Colors for output
COLOR_RESET := \033[0m
COLOR_BOLD := \033[1m
COLOR_GREEN := \033[32m
COLOR_YELLOW := \033[33m

.PHONY: all
all: build

## build: Build the binary
.PHONY: build
build:
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Building $(BINARY_NAME)...$(COLOR_RESET)"
	cd $(BINARY_PATH) && $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) ./cmd/server
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Build complete: $(BINARY_PATH)/$(BINARY_NAME)$(COLOR_RESET)"

## build-deb: Build Debian package
.PHONY: build-deb
build-deb:
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Building Debian package...$(COLOR_RESET)"
	@VERSION=$(PACKAGE_VERSION) dpkg-buildpackage -us -uc -b
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Package built successfully!$(COLOR_RESET)"
	@ls -lh ../$(PACKAGE_NAME)_*.deb 2>/dev/null || echo "Package not found in parent directory"

## build-deb-clean: Clean and build Debian package
.PHONY: build-deb-clean
build-deb-clean: clean-deb build-deb

## install: Build and install the binary locally
.PHONY: install
install: build
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Installing $(BINARY_NAME)...$(COLOR_RESET)"
	install -D -m 0755 $(BINARY_PATH)/$(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)

## uninstall: Uninstall the binary
.PHONY: uninstall
uninstall:
	@echo "$(COLOR_BOLD)$(COLOR_YELLOW)Uninstalling $(BINARY_NAME)...$(COLOR_RESET)"
	rm -f /usr/local/bin/$(BINARY_NAME)

## test: Run tests
.PHONY: test
test:
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Running tests...$(COLOR_RESET)"
	cd $(BINARY_PATH) && $(GOTEST) -v -race -cover ./...

## test-short: Run short tests only
.PHONY: test-short
test-short:
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Running short tests...$(COLOR_RESET)"
	cd $(BINARY_PATH) && $(GOTEST) -short -v ./...

## test-integration: Run WebSocket integration tests
.PHONY: test-integration
test-integration:
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Running WebSocket integration tests...$(COLOR_RESET)"
	cd $(BINARY_PATH) && $(GOTEST) -v -race -timeout 60s ./integration/...

## test-e2e: Run E2E tests with Playwright (requires backend running)
.PHONY: test-e2e
test-e2e:
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Running E2E tests...$(COLOR_RESET)"
	cd tests/e2e && npm test

## test-e2e-standalone: Run E2E tests with backend auto-started
.PHONY: test-e2e-standalone
test-e2e-standalone:
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Running E2E tests (with backend)...$(COLOR_RESET)"
	cd tests/e2e && npm test

## test-all: Run all tests (unit, integration, E2E)
.PHONY: test-all
test-all: test test-integration test-e2e-standalone
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)All tests complete!$(COLOR_RESET)"

## test-cover: Run tests with coverage
.PHONY: test-cover
test-cover:
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Running tests with coverage...$(COLOR_RESET)"
	cd $(BINARY_PATH) && $(GOTEST) -v -race -coverprofile=coverage.out -covermode=atomic ./...
	cd $(BINARY_PATH) && $(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Coverage report: $(BINARY_PATH)/coverage.html$(COLOR_RESET)"

## lint: Run linter
.PHONY: lint
lint:
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Running linter...$(COLOR_RESET)"
	@if command -v golangci-lint >/dev/null 2>&1; then \
		cd $(BINARY_PATH) && golangci-lint run ./...; \
	else \
		echo "$(COLOR_YELLOW)golangci-lint not installed. Install with:$(COLOR_RESET)"; \
		echo "  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin"; \
	fi

## fmt: Format code
.PHONY: fmt
fmt:
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Formatting code...$(COLOR_RESET)"
	cd $(BINARY_PATH) && $(GOCMD) fmt ./...

## vet: Run go vet
.PHONY: vet
vet:
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Running go vet...$(COLOR_RESET)"
	cd $(BINARY_PATH) && $(GOCMD) vet ./...

## clean: Clean build artifacts
.PHONY: clean
clean:
	@echo "$(COLOR_BOLD)$(COLOR_YELLOW)Cleaning...$(COLOR_RESET)"
	rm -f $(BINARY_PATH)/$(BINARY_NAME)
	rm -f $(BINARY_PATH)/coverage.out $(BINARY_PATH)/coverage.html
	cd $(BINARY_PATH) && $(GOCMD) clean

## clean-deb: Clean Debian build artifacts
.PHONY: clean-deb
clean-deb:
	@echo "$(COLOR_BOLD)$(COLOR_YELLOW)Cleaning Debian artifacts...$(COLOR_RESET)"
	rm -f ../$(PACKAGE_NAME)_*
	rm -f ../$(BINARY_NAME)_*.build
	rm -f ../$(BINARY_NAME)_*.changes
	rm -f ../$(BINARY_NAME)_*.deb
	rm -rf debian/.debhelper/
	rm -f debian/debhelper-build-stamp
	rm -f debian/files
	rm -rf debian/vote/

## clean-all: Clean all build artifacts
.PHONY: clean-all
clean-all: clean clean-deb
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)All clean!$(COLOR_RESET)"

## run: Build and run the server
.PHONY: run
run: build
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Starting server...$(COLOR_RESET)"
	./$(BINARY_PATH)/$(BINARY_NAME)

## dev: Run in development mode with hot reload (requires air)
.PHONY: dev
dev:
	@if command -v air >/dev/null 2>&1; then \
		cd $(BINARY_PATH) && air; \
	else \
		echo "$(COLOR_YELLOW)air not installed. Install with:$(COLOR_RESET)"
		echo "  go install github.com/cosmtrek/air@latest"
		cd $(BINARY_PATH) && $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) . && ./$(BINARY_NAME); \
	fi

## deps: Download dependencies
.PHONY: deps
deps:
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Downloading dependencies...$(COLOR_RESET)"
	cd $(BINARY_PATH) && $(GOMOD) download
	cd $(BINARY_PATH) && $(GOMOD) tidy

## docker: Build Docker image
.PHONY: docker
docker:
	@echo "$(COLOR_BOLD)$(COLOR_GREEN)Building Docker image...$(COLOR_RESET)"
	docker build -t $(PACKAGE_NAME):$(VERSION) .

## help: Show this help message
.PHONY: help
help:
	@echo "$(COLOR_BOLD)Vote Coloré - Real-time voting application$(COLOR_RESET)"
	@echo ""
	@echo "$(COLOR_BOLD)Usage:$(COLOR_RESET)"
	@echo "  make [target]"
	@echo ""
	@echo "$(COLOR_BOLD)Targets:$(COLOR_RESET)"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
	@echo ""
	@echo "$(COLOR_BOLD)Examples:$(COLOR_RESET)"
	@echo "  make build          # Build the binary"
	@echo "  make build-deb      # Build the .deb package"
	@echo "  make test           # Run all tests"
	@echo "  make clean-all      # Clean all artifacts"