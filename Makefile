.PHONY: help build server demo test lint fmt clean install docker-build docker-run

# Variables
BINARY_NAME=saltare
BUILD_DIR=./bin
CMD_DIR=./cmd/saltare
GO=go
GOFLAGS=-v

# Help
help:
	@echo "Saltare Makefile Commands:"
	@echo ""
	@echo "Build & Run:"
	@echo "  make build         - Build binary"
	@echo "  make server        - Run server"
	@echo "  make demo          - Run demo (sync/async MCP calls)"
	@echo ""
	@echo "Testing:"
	@echo "  make test          - Run all tests"
	@echo ""
	@echo "Code Quality:"
	@echo "  make lint          - Run linter"
	@echo "  make fmt           - Format code"
	@echo ""
	@echo "Other:"
	@echo "  make clean         - Clean build artifacts"
	@echo "  make install       - Install binary"
	@echo "  make docker-build  - Build Docker image"
	@echo "  make docker-run    - Run in Docker"

# Build binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)/main.go
	@echo "✓ Built: $(BUILD_DIR)/$(BINARY_NAME)"

# Run server
server: build
	@echo "Starting Saltare server..."
	$(BUILD_DIR)/$(BINARY_NAME) server

# Run demo (requires server running in another terminal)
demo:
	@echo "Running Saltare demo..."
	@echo "Make sure server is running: make server"
	@cd demo && ./demo.sh

# Run tests
test:
	@echo "Running tests..."
	$(GO) test -v -race ./...
	@echo "✓ Tests complete"

# Run linter
lint:
	@echo "Running linter..."
	golangci-lint run ./...

# Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...
	@echo "✓ Code formatted"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
	@echo "✓ Cleaned"

# Install binary
install: build
	@echo "Installing to $(GOPATH)/bin..."
	cp $(BUILD_DIR)/$(BINARY_NAME) $(GOPATH)/bin/
	@echo "✓ Installed"

# Docker build
docker-build:
	@echo "Building Docker image..."
	docker build -t saltare:latest -f deployments/docker/Dockerfile .
	@echo "✓ Docker image built"

# Docker run
docker-run:
	docker run -p 8080:8080 -p 8081:8081 saltare:latest
