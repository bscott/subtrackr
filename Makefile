# Variables
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_TAG := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X 'subtrackr/internal/version.GitCommit=$(GIT_COMMIT)' -X 'subtrackr/internal/version.Version=$(GIT_TAG)'

# Default target
.PHONY: all
all: build

# Build the application
.PHONY: build
build:
	go build -ldflags "$(LDFLAGS)" -o subtrackr cmd/server/main.go

# Run the application
.PHONY: run
run: build
	./subtrackr

# Clean build artifacts
.PHONY: clean
clean:
	rm -f subtrackr

# Development mode with live reload (requires air)
.PHONY: dev
dev:
	air

# Run tests
.PHONY: test
test:
	go test ./...

# Run go vet
.PHONY: vet
vet:
	go vet ./...

# Run go fmt
.PHONY: fmt
fmt:
	go fmt ./...

# Build for multiple platforms
.PHONY: build-all
build-all:
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/subtrackr-darwin-amd64 cmd/server/main.go
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/subtrackr-darwin-arm64 cmd/server/main.go
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/subtrackr-linux-amd64 cmd/server/main.go
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/subtrackr-linux-arm64 cmd/server/main.go
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/subtrackr-windows-amd64.exe cmd/server/main.go

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  make build    - Build the application with git commit SHA"
	@echo "  make run      - Build and run the application"
	@echo "  make clean    - Remove build artifacts"
	@echo "  make test     - Run tests"
	@echo "  make vet      - Run go vet"
	@echo "  make fmt      - Format code"
	@echo "  make build-all - Build for multiple platforms"
	@echo "  make help     - Show this help message"