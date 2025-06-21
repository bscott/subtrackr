# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o subtrackr ./cmd/server

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates sqlite tzdata
RUN mkdir /app

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/subtrackr .

# Copy static files and templates
COPY --from=builder /app/web ./web
COPY --from=builder /app/templates ./templates

# Create data directory for SQLite
RUN mkdir -p /app/data

# Expose port
EXPOSE 8080

# Set environment variables
ENV GIN_MODE=release
ENV DATABASE_PATH=/app/data/subtrackr.db

# Run the application
CMD ["./subtrackr"]