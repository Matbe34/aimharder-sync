#!/bin/sh
set -e

# Set data directory
export AIMHARDER_STORAGE_DATA_DIR="${AIMHARDER_STORAGE_DATA_DIR:-/data}"
export AIMHARDER_STORAGE_TOKENS_FILE="${AIMHARDER_STORAGE_TOKENS_FILE:-/data/tokens.json}"
export AIMHARDER_STORAGE_HISTORY_FILE="${AIMHARDER_STORAGE_HISTORY_FILE:-/data/sync_history.json}"
export AIMHARDER_STORAGE_TCX_DIR="${AIMHARDER_STORAGE_TCX_DIR:-/data/tcx}"

# Check required environment variables for sync operations
check_aimharder_config() {
    if [ -z "$AIMHARDER_EMAIL" ]; then
        echo "⚠️  Warning: AIMHARDER_EMAIL not set"
    fi
    if [ -z "$AIMHARDER_PASSWORD" ]; then
        echo "⚠️  Warning: AIMHARDER_PASSWORD not set"
    fi
}

check_strava_config() {
    if [ -z "$STRAVA_CLIENT_ID" ]; then
        echo "⚠️  Warning: STRAVA_CLIENT_ID not set"
    fi
    if [ -z "$STRAVA_CLIENT_SECRET" ]; then
        echo "⚠️  Warning: STRAVA_CLIENT_SECRET not set"
    fi
}

# Display configuration on verbose mode
if [ "$VERBOSE" = "true" ] || [ "$VERBOSE" = "1" ]; then
    echo "📋 Configuration:"
    echo "   AIMHARDER_BOX_NAME: ${AIMHARDER_BOX_NAME:-valhallatrainingcamp}"
    echo "   AIMHARDER_BOX_ID: ${AIMHARDER_BOX_ID:-9818}"
    echo "   Data directory: $AIMHARDER_STORAGE_DATA_DIR"
    echo ""
fi

# Check configs based on command
case "$1" in
    sync|fetch|export)
        check_aimharder_config
        if [ "$1" = "sync" ]; then
            check_strava_config
        fi
        ;;
    auth)
        if [ "$2" = "strava" ]; then
            check_strava_config
        fi
        ;;
esac

# Execute the command
exec aimharder-sync "$@"
