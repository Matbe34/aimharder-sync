#!/bin/bash
# Build script for Home Assistant Add-on
# Run from the project root directory

set -e

ADDON_DIR="addons/aimharder-sync"
ADDON_NAME="aimharder-sync"

echo "========================================"
echo "Building AimHarder Sync Add-on"
echo "========================================"

# Check if running from project root
if [ ! -f "go.mod" ]; then
    echo "ERROR: Run this script from the project root directory"
    exit 1
fi

# Detect architecture
ARCH=$(uname -m)
case $ARCH in
    x86_64)
        BUILD_ARCH="amd64"
        ;;
    aarch64)
        BUILD_ARCH="aarch64"
        ;;
    armv7l)
        BUILD_ARCH="armv7"
        ;;
    *)
        echo "Unknown architecture: $ARCH"
        BUILD_ARCH="amd64"
        ;;
esac

echo "Detected architecture: $BUILD_ARCH"

# Build the Docker image
echo "Building Docker image..."
docker build \
    --build-arg BUILD_FROM="golang:alpine" \
    -t "local/$ADDON_NAME:latest" \
    -f "$ADDON_DIR/Dockerfile" \
    .

echo ""
echo "Build complete!"
echo "Image: local/$ADDON_NAME:latest"
echo ""
echo "To test locally:"
echo "  docker run --rm -it -p 8080:8080 local/$ADDON_NAME:latest"
