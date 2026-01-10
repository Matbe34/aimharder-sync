#!/bin/bash
# Prepare the add-on for deployment to Home Assistant OS
# This copies all required source files into the add-on folder

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ADDON_DIR="$SCRIPT_DIR"
SRC_DIR="$ADDON_DIR/src"

echo "========================================"
echo "Preparing AimHarder Sync Add-on"
echo "========================================"
echo "Project root: $PROJECT_ROOT"
echo "Add-on dir: $ADDON_DIR"
echo ""

# Clean previous source
rm -rf "$SRC_DIR"
mkdir -p "$SRC_DIR"

# Copy Go source files
echo "Copying source files..."
cp "$PROJECT_ROOT/go.mod" "$SRC_DIR/"
cp "$PROJECT_ROOT/go.sum" "$SRC_DIR/" 2>/dev/null || true

# Copy source directories
cp -r "$PROJECT_ROOT/cmd" "$SRC_DIR/"
cp -r "$PROJECT_ROOT/internal" "$SRC_DIR/"

echo "✓ Source files copied to $SRC_DIR"
echo ""

# Show what's included
echo "Add-on contents:"
find "$ADDON_DIR" -type f | sed "s|$ADDON_DIR/||" | sort | head -30
echo ""

echo "========================================"
echo "Add-on is ready!"
echo "========================================"
echo ""
echo "Deploy to Home Assistant with:"
echo "  scp -r $ADDON_DIR hassio@YOUR_HA_IP:/addons/"
echo ""
echo "Or if the add-on already exists:"
echo "  scp -r $ADDON_DIR/* hassio@YOUR_HA_IP:/addons/aimharder-sync/"
