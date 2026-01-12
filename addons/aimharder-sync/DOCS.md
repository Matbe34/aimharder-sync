# AimHarder Sync - Home Assistant Add-on

Automatically sync your AimHarder workouts to Strava.

## Features

- **Automatic Sync**: Periodically checks for new workouts and uploads to Strava
- **Webhook API**: Trigger syncs manually via HTTP (great for automations/widgets)
- **Duplicate Prevention**: Won't upload workouts that already exist in Strava
- **Quiet Hours**: Pause syncing during specified hours
- **Multi-Architecture**: Works on amd64, aarch64 (Raspberry Pi 4), and armv7

## Prerequisites

### 1. Strava API Application

1. Go to [Strava API Settings](https://www.strava.com/settings/api)
2. Create an application (use any website URL and `localhost` as callback)
3. Note your **Client ID** and **Client Secret**
4. Get a refresh token with `activity:write` scope. You can use:
   - https://www.strava.com/oauth/authorize?client_id=YOUR_CLIENT_ID&response_type=code&redirect_uri=http://localhost&scope=activity:write,activity:read_all
   - Exchange the code for tokens using the Strava API

### 2. AimHarder Credentials

- Your AimHarder login email and password
- Your **Box ID** (found in network requests when browsing your box)
- Your **User ID** (found in network requests - look for `userID` parameter)

To find your Box ID and User ID:
1. Open your browser's Developer Tools (F12)
2. Go to the Network tab
3. Navigate to your box's schedule page on AimHarder
4. Look for API requests containing `box=XXXXX` and `userID=XXXXXX`

## Configuration

| Option | Description | Required |
|--------|-------------|----------|
| `strava_client_id` | Strava API Client ID | Yes |
| `strava_client_secret` | Strava API Client Secret | Yes |
| `strava_refresh_token` | Strava OAuth Refresh Token | Yes |
| `aimharder_email` | AimHarder login email | Yes |
| `aimharder_password` | AimHarder login password | Yes |
| `aimharder_box_id` | Your box ID | Yes |
| `aimharder_user_id` | Your user ID | Yes |
| `webhook_token` | Secret token for webhook API | Yes |
| `webhook_port` | Port for webhook server | No (default: 8080) |
| `sync_days` | How many days back to sync | No (default: 1) |
| `check_interval` | Seconds between sync checks | No (default: 60) |
| `quiet_hours_start` | Hour to start quiet period (0-23) | No (default: 0) |
| `quiet_hours_end` | Hour to end quiet period (0-23) | No (default: 6) |
| `enable_scheduler` | Enable automatic periodic sync | No (default: true) |
| `dry_run` | Test mode - don't actually upload | No (default: false) |

## Webhook API

The add-on exposes an HTTP API for triggering syncs:

### Endpoints

#### POST /sync
Trigger a workout sync.

```bash
curl -X POST http://homeassistant.local:8080/sync \
  -H "X-Auth-Token: YOUR_WEBHOOK_TOKEN"
```

Optional query parameters:
- `?days=N` - Override sync days (default: from config)
- `?dry-run=true` - Test without uploading

Response:
```json
{
  "success": true,
  "message": "Sync completed",
  "uploaded": 1,
  "skipped": 0,
  "errors": 0
}
```

#### GET /status
Check add-on status.

```bash
curl http://homeassistant.local:8080/status \
  -H "X-Auth-Token: YOUR_WEBHOOK_TOKEN"
```

#### GET /health
Health check (no auth required).

```bash
curl http://homeassistant.local:8080/health
```

## Home Assistant Integration

### REST Command

Add to your `configuration.yaml`:

```yaml
rest_command:
  aimharder_sync:
    url: "http://localhost:8080/sync"
    method: POST
    headers:
      X-Auth-Token: !secret aimharder_webhook_token
    timeout: 120
```

Add to `secrets.yaml`:
```yaml
aimharder_webhook_token: "your-token-here"
```

### Automation Example

```yaml
automation:
  - alias: "Sync workout after gym hours"
    trigger:
      - platform: time
        at: "21:00:00"
    action:
      - service: rest_command.aimharder_sync
```

### Dashboard Button

```yaml
type: button
name: Sync Workout
icon: mdi:weight-lifter
tap_action:
  action: call-service
  service: rest_command.aimharder_sync
```

### Script for Companion App Widget

Create a script in Home Assistant:

```yaml
script:
  sync_aimharder:
    alias: "Sync AimHarder"
    icon: mdi:weight-lifter
    sequence:
      - service: rest_command.aimharder_sync
```

Then add this script to a widget using the Home Assistant Companion App.

## Troubleshooting

### Check Add-on Logs

Go to **Settings → Add-ons → AimHarder Sync → Log** tab

### Common Issues

1. **"strava_refresh_token is required"**
   - Make sure you've obtained a valid refresh token with `activity:write` scope

2. **Sync fails with 401 error**
   - Your Strava refresh token may have expired
   - Re-authenticate with Strava and update the token

3. **No workouts found**
   - Check that `aimharder_box_id` and `aimharder_user_id` are correct
   - Verify your AimHarder credentials
   - Try increasing `sync_days`

4. **Webhook returns 401**
   - Verify the `X-Auth-Token` header matches your `webhook_token`

## Data Storage

Workout history is stored in `/share/aimharder-sync/` to persist across add-on restarts and prevent duplicate uploads.
