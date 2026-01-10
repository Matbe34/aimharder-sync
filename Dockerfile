# Build stage
FROM golang:1.24-alpine AS builder

# Install dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -o /aimharder-sync ./cmd/

# Runtime stage
FROM alpine:3.19

# Install ca-certificates for HTTPS, tzdata for timezone support, and bash for scripts
RUN apk add --no-cache ca-certificates tzdata bash

# Set timezone to Madrid/Spain (for Aimharder)
ENV TZ=Europe/Madrid

# Create non-root user
RUN adduser -D -g '' appuser

# Create data directory
RUN mkdir -p /data && chown appuser:appuser /data

# Copy binary from builder
COPY --from=builder /aimharder-sync /app/aimharder-sync

# Copy entrypoint script
COPY docker-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Copy scheduler script
COPY scripts/scheduler.sh /app/scheduler.sh
RUN chmod +x /app/scheduler.sh

# Add /app to PATH
ENV PATH="/app:${PATH}"

# Switch to non-root user
USER appuser

# Set working directory
WORKDIR /data

# Data volume for persistent storage
VOLUME ["/data"]

# Expose port for OAuth callback
EXPOSE 8080

# Default entrypoint
ENTRYPOINT ["docker-entrypoint.sh"]

# Default command (can be overridden)
CMD ["--help"]
