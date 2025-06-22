.PHONY: test test-coverage test-models test-repository test-service test-config test-race clean build run docker-build docker-run

# Test commands
test:
	@echo "Running all tests..."
	@go test -v ./...

test-coverage:
	@echo "Running tests with coverage..."
	@go test -v -race -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

test-models:
	@echo "Running model tests..."
	@go test -v ./internal/models/...

test-repository:
	@echo "Running repository tests..."
	@go test -v ./internal/repository/...

test-service:
	@echo "Running service tests..."
	@go test -v ./internal/service/...

test-config:
	@echo "Running config tests..."
	@go test -v ./internal/config/...

test-race:
	@echo "Checking for race conditions..."
	@go test -race ./...

# Build commands
build:
	@echo "Building SubTrackr..."
	@go build -o bin/subtrackr ./cmd/server

run: build
	@echo "Running SubTrackr..."
	@./bin/subtrackr

# Docker commands
docker-build:
	@echo "Building Docker image..."
	@docker build -t bscott/subtrackr:latest .

docker-run:
	@echo "Running Docker container..."
	@docker run -d -p 8080:8080 --name subtrackr bscott/subtrackr:latest

# Clean commands
clean:
	@echo "Cleaning up..."
	@rm -rf bin/
	@rm -f coverage.out coverage.html
	@go clean -testcache

# Development commands
dev:
	@echo "Starting development server..."
	@go run ./cmd/server

fmt:
	@echo "Formatting code..."
	@go fmt ./...

lint:
	@echo "Running linter..."
	@golangci-lint run

deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy

# Database commands
db-migrate:
	@echo "Running database migrations..."
	@go run ./cmd/server -migrate

# Help command
help:
	@echo "Available commands:"
	@echo "  make test              - Run all tests"
	@echo "  make test-coverage     - Run tests with coverage report"
	@echo "  make test-models       - Run model tests only"
	@echo "  make test-repository   - Run repository tests only"
	@echo "  make test-service      - Run service tests only"
	@echo "  make test-config       - Run config tests only"
	@echo "  make test-race         - Check for race conditions"
	@echo "  make build             - Build the application"
	@echo "  make run               - Build and run the application"
	@echo "  make docker-build      - Build Docker image"
	@echo "  make docker-run        - Run Docker container"
	@echo "  make clean             - Clean build artifacts"
	@echo "  make dev               - Start development server"
	@echo "  make fmt               - Format code"
	@echo "  make lint              - Run linter"
	@echo "  make deps              - Download dependencies"
	@echo "  make help              - Show this help message"