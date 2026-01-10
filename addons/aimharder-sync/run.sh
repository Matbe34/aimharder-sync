#!/usr/bin/env bash
set -e

# Home Assistant Add-on stores options at /data/options.json
CONFIG_PATH=/data/options.json

echo "========================================"
echo "AimHarder Sync Add-on Starting..."
echo "========================================"
echo "Version: 1.0.0"
echo ""

# Check if config exists (running as HA add-on)
if [ ! -f "$CONFIG_PATH" ]; then
    echo "No options.json found - using environment variables"
    # For standalone Docker testing, use env vars directly
    if [ -z "$STRAVA_CLIENT_ID" ]; then
        echo "ERROR: Neither options.json nor STRAVA_CLIENT_ID found"
        exit 1
    fi
    
    # Use defaults for optional vars
    WEBHOOK_PORT="${WEBHOOK_PORT:-8080}"
    WEBHOOK_TOKEN="${WEBHOOK_TOKEN:-}"
    SYNC_DAYS="${SYNC_DAYS:-1}"
    CHECK_INTERVAL="${CHECK_INTERVAL:-60}"
    QUIET_HOURS_START="${QUIET_HOURS_START:-0}"
    QUIET_HOURS_END="${QUIET_HOURS_END:-6}"
    ENABLE_SCHEDULER="${ENABLE_SCHEDULER:-true}"
    DRY_RUN="${DRY_RUN:-false}"
    DATA_DIR="${DATA_DIR:-/data}"
else
    # Read configuration from Home Assistant add-on options
    echo "Loading configuration from options.json..."
    
    STRAVA_CLIENT_ID=$(jq -r '.strava_client_id' $CONFIG_PATH)
    STRAVA_CLIENT_SECRET=$(jq -r '.strava_client_secret' $CONFIG_PATH)
    STRAVA_REFRESH_TOKEN=$(jq -r '.strava_refresh_token' $CONFIG_PATH)
    AIMHARDER_EMAIL=$(jq -r '.aimharder_email' $CONFIG_PATH)
    AIMHARDER_PASSWORD=$(jq -r '.aimharder_password' $CONFIG_PATH)
    AIMHARDER_BOX_ID=$(jq -r '.aimharder_box_id' $CONFIG_PATH)
    AIMHARDER_USER_ID=$(jq -r '.aimharder_user_id' $CONFIG_PATH)
    WEBHOOK_TOKEN=$(jq -r '.webhook_token' $CONFIG_PATH)
    WEBHOOK_PORT=$(jq -r '.webhook_port' $CONFIG_PATH)
    SYNC_DAYS=$(jq -r '.sync_days' $CONFIG_PATH)
    CHECK_INTERVAL=$(jq -r '.check_interval' $CONFIG_PATH)
    QUIET_HOURS_START=$(jq -r '.quiet_hours_start' $CONFIG_PATH)
    QUIET_HOURS_END=$(jq -r '.quiet_hours_end' $CONFIG_PATH)
    ENABLE_SCHEDULER=$(jq -r '.enable_scheduler' $CONFIG_PATH)
    DRY_RUN=$(jq -r '.dry_run' $CONFIG_PATH)
    
    # Use /share for persistent storage on HAOS
    DATA_DIR="/share/aimharder-sync"
fi

# Export environment variables for the Go binary
export STRAVA_CLIENT_ID
export STRAVA_CLIENT_SECRET
export STRAVA_REFRESH_TOKEN
export AIMHARDER_EMAIL
export AIMHARDER_PASSWORD
export AIMHARDER_BOX_ID
export AIMHARDER_USER_ID
export DATA_DIR

# Create data directory
mkdir -p "$DATA_DIR"

# Validate required configuration
if [ -z "$STRAVA_CLIENT_ID" ] || [ "$STRAVA_CLIENT_ID" = "null" ] || [ "$STRAVA_CLIENT_ID" = "" ]; then
    echo "ERROR: strava_client_id is required"
    exit 1
fi

if [ -z "$STRAVA_REFRESH_TOKEN" ] || [ "$STRAVA_REFRESH_TOKEN" = "null" ] || [ "$STRAVA_REFRESH_TOKEN" = "" ]; then
    echo "ERROR: strava_refresh_token is required"
    exit 1
fi

if [ -z "$AIMHARDER_EMAIL" ] || [ "$AIMHARDER_EMAIL" = "null" ] || [ "$AIMHARDER_EMAIL" = "" ]; then
    echo "ERROR: aimharder_email is required"
    exit 1
fi

if [ -z "$WEBHOOK_TOKEN" ] || [ "$WEBHOOK_TOKEN" = "null" ] || [ "$WEBHOOK_TOKEN" = "" ]; then
    echo "ERROR: webhook_token is required for security"
    exit 1
fi

echo "Configuration loaded successfully"
echo "  - Webhook Port: $WEBHOOK_PORT"
echo "  - Sync Days: $SYNC_DAYS"
echo "  - Scheduler Enabled: $ENABLE_SCHEDULER"
echo "  - Check Interval: ${CHECK_INTERVAL}s"
echo "  - Quiet Hours: $QUIET_HOURS_START:00 - $QUIET_HOURS_END:00"
echo "  - Dry Run: $DRY_RUN"
echo "  - Data Dir: $DATA_DIR"

# Build dry-run flag
DRY_RUN_FLAG=""
if [ "$DRY_RUN" = "true" ]; then
    DRY_RUN_FLAG="--dry-run"
fi

# Function to run sync
run_sync() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Running sync..."
    /app/aimharder-sync sync --days "$SYNC_DAYS" $DRY_RUN_FLAG 2>&1 || true
}

# Function to check if in quiet hours
in_quiet_hours() {
    if [ "$QUIET_HOURS_START" = "$QUIET_HOURS_END" ]; then
        return 1  # No quiet hours
    fi
    
    current_hour=$(date +%H | sed 's/^0//')
    
    if [ "$QUIET_HOURS_START" -lt "$QUIET_HOURS_END" ]; then
        # Simple range (e.g., 2-6)
        [ "$current_hour" -ge "$QUIET_HOURS_START" ] && [ "$current_hour" -lt "$QUIET_HOURS_END" ]
    else
        # Wrapping range (e.g., 22-6)
        [ "$current_hour" -ge "$QUIET_HOURS_START" ] || [ "$current_hour" -lt "$QUIET_HOURS_END" ]
    fi
}

# Start webhook server in background
echo "Starting webhook server on port $WEBHOOK_PORT..."
/app/aimharder-sync webhook --port "$WEBHOOK_PORT" --token "$WEBHOOK_TOKEN" &
WEBHOOK_PID=$!

# Give webhook time to start
sleep 2

# Check if webhook started successfully
if ! kill -0 $WEBHOOK_PID 2>/dev/null; then
    echo "ERROR: Webhook server failed to start"
    exit 1
fi

echo "Webhook server started (PID: $WEBHOOK_PID)"

# Run scheduler if enabled
if [ "$ENABLE_SCHEDULER" = "true" ]; then
    echo "Scheduler enabled - checking every ${CHECK_INTERVAL}s"
    
    # Initial sync on startup
    echo "Running initial sync..."
    run_sync
    
    # Scheduler loop
    while true; do
        sleep "$CHECK_INTERVAL"
        
        # Check if webhook is still running
        if ! kill -0 $WEBHOOK_PID 2>/dev/null; then
            echo "ERROR: Webhook server died, restarting..."
            /app/aimharder-sync webhook --port "$WEBHOOK_PORT" --token "$WEBHOOK_TOKEN" &
            WEBHOOK_PID=$!
            sleep 2
        fi
        
        # Skip during quiet hours
        if in_quiet_hours; then
            echo "[$(date '+%Y-%m-%d %H:%M:%S')] Quiet hours - skipping sync"
            continue
        fi
        
        run_sync
    done
else
    echo "Scheduler disabled - webhook only mode"
    # Wait for webhook to exit
    wait $WEBHOOK_PID
fi
