# Build stage
FROM golang:1.24 AS builder

# Install build dependencies
RUN apt-get update && apt-get install -y \
    gcc \
    libc6-dev \
    libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy only necessary source directories
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# Build arguments for version info (should be provided by CI/CD)
ARG GIT_TAG=dev
ARG GIT_COMMIT=unknown

# Build the application with optimizations and version info
# Use build args directly - no need for .git directory
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-w -s -X 'subtrackr/internal/version.Version=${GIT_TAG}' -X 'subtrackr/internal/version.GitCommit=${GIT_COMMIT}'" \
    -o subtrackr ./cmd/server

# Final stage
FROM debian:bookworm-slim

# Install runtime dependencies in a single layer
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    sqlite3 \
    tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /app/data

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/subtrackr .

# Copy templates and static assets
COPY templates/ ./templates/
COPY web/ ./web/

# Expose port
EXPOSE 8080

# Set environment variables
ENV GIN_MODE=release
ENV DATABASE_PATH=/app/data/subtrackr.db

# Healthcheck to verify the application is running and database is accessible
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/healthz || exit 1

# Run the application
CMD ["./subtrackr"]