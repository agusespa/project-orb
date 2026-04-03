PROJECT_NAME := project-orb
MAIN_PACKAGE := .
BUILD_DIR := bin
RELEASE_DIR := release
BINARY_NAME := $(BUILD_DIR)/$(PROJECT_NAME)

.PHONY: all setup tidy fmt lint test test-coverage build run devrun clean build-release

all: fmt lint test build

setup:
	@echo "Setting up Go modules..."
	go mod tidy
	@echo "Go modules are set up."

tidy:
	@echo "Tidying go.mod and go.sum..."
	go mod tidy
	@echo "go.mod and go.sum tidied."

fmt:
	@echo "Formatting Go files..."
	gofmt -w *.go
	@echo "Formatting complete."

lint:
	@echo "Running lint checks..."
	go vet ./...
	@echo "Lint checks complete."

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

devrun:
	@echo "Running $(MAIN_PACKAGE) directly..."
	go run $(MAIN_PACKAGE)

clean:
	@echo "Cleaning up..."
	@rm -rf $(BUILD_DIR) $(RELEASE_DIR) coverage.out coverage.html
	@echo "Clean complete."

build-release:
	@echo "Building release binaries..."
	@mkdir -p $(RELEASE_DIR)
	@echo "Building for Linux amd64..."
	GOOS=linux GOARCH=amd64 go build -o $(RELEASE_DIR)/$(PROJECT_NAME)-linux-amd64 $(MAIN_PACKAGE)
	@echo "Building for macOS arm64..."
	GOOS=darwin GOARCH=arm64 go build -o $(RELEASE_DIR)/$(PROJECT_NAME)-darwin-arm64 $(MAIN_PACKAGE)
	@echo "Release binaries built in $(RELEASE_DIR)"
