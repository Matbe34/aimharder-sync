# Home Assistant Add-on Dockerfile
# Supports: amd64, aarch64, armv7
# Source files must be copied to src/ before building

# Global ARG for HA base image (must be before any FROM)
ARG BUILD_FROM=ghcr.io/home-assistant/amd64-base:3.19

# First stage: Build with Go
FROM golang:alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy source code from add-on folder
COPY src/ .

# Build the binary with CGO disabled for static linking
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /aimharder-sync ./cmd/

# Second stage: Use HA base image
FROM ${BUILD_FROM}

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    bash \
    jq

# Create app directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /aimharder-sync /app/aimharder-sync

# Copy run script from build context (add-on folder)
COPY run.sh /run.sh
RUN chmod +x /run.sh

# Create data directory
RUN mkdir -p /data

# Labels for Home Assistant
LABEL \
    io.hass.name="AimHarder Sync" \
    io.hass.description="Sync AimHarder workouts to Strava" \
    io.hass.type="addon" \
    io.hass.version="1.0.0"

CMD ["/run.sh"]
