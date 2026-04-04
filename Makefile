PROJECT_NAME := project-orb
MAIN_PACKAGE := ./cmd/project-orb
BUILD_DIR := bin
RELEASE_DIR := release
BINARY_NAME := $(BUILD_DIR)/$(PROJECT_NAME)
INSTALL_PATH := $(HOME)/bin

.PHONY: all help tidy fmt lint lint-full test test-coverage build run install clean

all: fmt lint test build

help:
	@echo "Available targets:"
	@echo "  make all           - Format, lint, test, and build"
	@echo "  make tidy          - Tidy go.mod and go.sum"
	@echo "  make fmt           - Format Go files"
	@echo "  make lint          - Run basic lint checks (go vet)"
	@echo "  make lint-full     - Run comprehensive lint checks (golangci-lint)"
	@echo "  make test          - Run tests"
	@echo "  make test-coverage - Run tests with coverage report"
	@echo "  make build         - Build the binary"
	@echo "  make run           - Build and run the binary"
	@echo "  make install       - Install binary to $(INSTALL_PATH)"
	@echo "  make clean         - Remove build artifacts"

tidy:
	@echo "Tidying go.mod and go.sum..."
	go mod tidy
	@echo "go.mod and go.sum tidied."

fmt:
	@echo "Formatting Go files..."
	gofmt -w ./cmd ./internal
	@echo "Formatting complete."

lint:
	@echo "Running basic lint checks..."
	go vet ./...
	@echo "Lint checks complete."

lint-full:
	@echo "Running comprehensive lint checks..."
	golangci-lint run ./...
	@echo "Comprehensive lint checks complete."

test:
	@echo "Running tests..."
	go test ./... -v
	@echo "Tests complete."

test-coverage:
	@echo "Running tests with coverage..."
	go test ./... -v -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

build:
	@echo "Creating build directory if it doesn't exist..."
	@mkdir -p $(BUILD_DIR)
	@echo "Building $(BINARY_NAME)..."
	go build -o $(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "Build complete. Executable: $(BINARY_NAME)"

run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BINARY_NAME)

install: build
	@echo "Installing $(BINARY_NAME) to $(INSTALL_PATH)..."
	@mkdir -p $(INSTALL_PATH)
	@cp $(BINARY_NAME) $(INSTALL_PATH)/$(PROJECT_NAME)
	@echo "Installed to $(INSTALL_PATH)/$(PROJECT_NAME)"

clean:
	@echo "Cleaning up..."
	@rm -rf $(BUILD_DIR) $(RELEASE_DIR) coverage.out coverage.html
	@echo "Clean complete."