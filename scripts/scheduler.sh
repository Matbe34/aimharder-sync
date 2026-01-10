#!/bin/bash
# AimHarder Sync Scheduler
# Checks for new workouts every minute and syncs them to Strava

set -e

# Configuration
CHECK_INTERVAL=${CHECK_INTERVAL:-60}  # seconds between checks (default: 1 minute)
SYNC_DAYS=${SYNC_DAYS:-1}             # days to look back for workouts (default: 1)
QUIET_HOURS_START=${QUIET_HOURS_START:-0}  # hour to start quiet period (0-23, 0=disabled)
QUIET_HOURS_END=${QUIET_HOURS_END:-0}      # hour to end quiet period (0-23, 0=disabled)

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

is_quiet_hours() {
    if [ "$QUIET_HOURS_START" -eq 0 ] && [ "$QUIET_HOURS_END" -eq 0 ]; then
        return 1  # Quiet hours disabled
    fi
    
    current_hour=$(date '+%H')
    current_hour=$((10#$current_hour))  # Remove leading zeros
    
    if [ "$QUIET_HOURS_START" -lt "$QUIET_HOURS_END" ]; then
        # Simple case: e.g., 23:00 to 07:00 doesn't wrap
        if [ "$current_hour" -ge "$QUIET_HOURS_START" ] && [ "$current_hour" -lt "$QUIET_HOURS_END" ]; then
            return 0
        fi
    else
        # Wrapping case: e.g., 23:00 to 07:00
        if [ "$current_hour" -ge "$QUIET_HOURS_START" ] || [ "$current_hour" -lt "$QUIET_HOURS_END" ]; then
            return 0
        fi
    fi
    
    return 1
}

run_sync() {
    log "🔍 Checking for new workouts..."
    
    # Run sync with the specified number of days
    # The sync command already handles:
    # - Checking local history to skip already-synced workouts
    # - Checking Strava for existing activities
    # - Only uploading truly new workouts
    
    if /app/aimharder-sync sync --days "$SYNC_DAYS" 2>&1; then
        log "✅ Sync completed successfully"
    else
        log "⚠️  Sync completed with errors (check output above)"
    fi
}

# Main loop
log "🚀 AimHarder Sync Scheduler started"
log "   Check interval: ${CHECK_INTERVAL}s"
log "   Days to sync: ${SYNC_DAYS}"
if [ "$QUIET_HOURS_START" -ne 0 ] || [ "$QUIET_HOURS_END" -ne 0 ]; then
    log "   Quiet hours: ${QUIET_HOURS_START}:00 - ${QUIET_HOURS_END}:00"
fi

# Initial sync on startup
log "📥 Running initial sync..."
run_sync

while true; do
    sleep "$CHECK_INTERVAL"
    
    if is_quiet_hours; then
        log "😴 Quiet hours - skipping check"
        continue
    fi
    
    run_sync
done
