# AimHarder Sync 🏋️➡️🏃

Export your CrossFit workouts from [AimHarder](https://aimharder.com) and sync them to **Strava**.

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat&logo=docker)](https://www.docker.com/)

## Features

✅ **Fetch workouts** from AimHarder including WOD details and results  
✅ **Upload to Strava** as CrossFit/Weight Training activities  
✅ **TCX export** for manual upload to any fitness platform  
✅ **Historical sync** - sync all your past workouts  
✅ **Incremental sync** - only syncs new workouts  
✅ **Duplicate detection** - won't create duplicate activities  
✅ **Docker support** - clean, portable deployment  
✅ **Home Assistant Add-on** - easy deployment on HAOS  
✅ **Webhook API** - trigger syncs from phone widgets  
✅ **CLI interface** - easy to script and schedule  

## Prerequisites

### 1. AimHarder Account
You need your AimHarder login credentials and your box information:
- **Email** and **Password** for your AimHarder account
- **Box Name**: The subdomain of your box (e.g., `valhallatrainingcamp` from `https://valhallatrainingcamp.aimharder.com`)
- **Box ID**: Found by inspecting network requests in your browser (see [Finding Your Box ID](#finding-your-box-id))
- **User ID**: Found in API requests as `userID` parameter

### 2. Strava API Application
Create a Strava API application to enable syncing:

1. Go to [https://www.strava.com/settings/api](https://www.strava.com/settings/api)
2. Click "Create Application"
3. Fill in the details:
   - **Application Name**: AimHarder Sync (or any name)
   - **Category**: Training
   - **Website**: http://localhost
   - **Authorization Callback Domain**: localhost
4. Note your **Client ID** and **Client Secret**

## Installation

### Option 1: Docker (Recommended) 🐳

```bash
# Clone the repository
git clone https://github.com/yourusername/aimharder-sync.git
cd aimharder-sync

# Copy and edit environment file
cp .env.example .env
nano .env  # or your favorite editor

# Build the image
docker build -t aimharder-sync .

# Run a command
docker run --rm -it \
  --env-file .env \
  -v aimharder-data:/data \
  -p 8080:8080 \
  aimharder-sync status
```

### Option 2: Docker Compose 🐙

```bash
# Copy environment file
cp .env.example .env
nano .env

# Run interactively
docker-compose run --rm aimharder-sync status

# Run sync
docker-compose run --rm aimharder-sync sync --days 30
```

### Option 3: Build from Source 🛠️

```bash
# Requires Go 1.22+
git clone https://github.com/yourusername/aimharder-sync.git
cd aimharder-sync

# Build
go build -o aimharder-sync ./cmd/main.go

# Set environment variables
export AIMHARDER_EMAIL="your.email@example.com"
export AIMHARDER_PASSWORD="your_password"
export AIMHARDER_BOX_NAME="valhallatrainingcamp"
export AIMHARDER_BOX_ID="9818"
export STRAVA_CLIENT_ID="your_client_id"
export STRAVA_CLIENT_SECRET="your_client_secret"

# Run
./aimharder-sync status
```

## Usage

### Initial Setup

1. **Configure credentials** (see [Configuration](#configuration))

2. **Authenticate with Strava**:
```bash
# Docker
docker run --rm -it \
  --env-file .env \
  -v aimharder-data:/data \
  -p 8080:8080 \
  aimharder-sync auth

# Or with docker-compose
docker-compose run --rm --service-ports aimharder-sync auth

# Or native
./aimharder-sync auth
```

This will open a browser URL. Log in to Strava and authorize the application.

### Syncing Workouts

```bash
# Sync last 30 days (default)
aimharder-sync sync

# Sync last 7 days
aimharder-sync sync --days 7

# Sync specific date range
aimharder-sync sync --start 2024-01-01 --end 2024-12-31

# Sync all historical data (be patient!)
aimharder-sync sync --start 2020-01-01

# Force re-sync already synced workouts
aimharder-sync sync --days 30 --force

# Dry run (show what would be synced)
aimharder-sync sync --dry-run
```

### Viewing/Exporting Workouts

```bash
# Fetch and display recent workouts
aimharder-sync fetch --days 7

# Save workouts to JSON file
aimharder-sync fetch --days 30 --output workouts.json

# Export as TCX files (for manual upload)
aimharder-sync export --days 30

# Export to specific directory
aimharder-sync export --days 30 --output ~/my-tcx-files
```

### Checking Status

```bash
aimharder-sync status
```

## Configuration

Configuration can be provided via:
1. **Environment variables** (recommended for credentials)
2. **Config file** (`config.yaml`)
3. **Command line flags**

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `AIMHARDER_EMAIL` | ✅ | Your AimHarder login email |
| `AIMHARDER_PASSWORD` | ✅ | Your AimHarder password |
| `AIMHARDER_BOX_NAME` | ✅ | Box subdomain (e.g., `valhallatrainingcamp`) |
| `AIMHARDER_BOX_ID` | ✅ | Box ID (e.g., `1234`) |
| `AIMHARDER_USER_ID` | ✅ | Your User ID (e.g., `123456`) |
| `AIMHARDER_FAMILY_ID` | ❌ | Family ID (if multiple members) |
| `STRAVA_CLIENT_ID` | ✅* | Strava API Client ID |
| `STRAVA_CLIENT_SECRET` | ✅* | Strava API Client Secret |
| `WEBHOOK_PORT` | ❌ | Webhook server port (default: 8080) |
| `WEBHOOK_TOKEN` | ❌ | Auth token for webhook requests |

*Required for Strava sync

### Config File

Create `~/.aimharder-sync/config.yaml` or use `--config` flag:

```yaml
aimharder:
  box_name: valhallatrainingcamp
  box_id: "9818"

strava:
  redirect_uri: "http://localhost:8080/callback"

sync:
  default_days: 30
  activity_type: crossfit
  include_no_score: true
```

## Finding Your Box ID and User ID

1. Open your browser's Developer Tools (F12)
2. Go to the Network tab
3. Navigate to your box's schedule page on AimHarder
4. Book or view a class, or go to your profile/activity page
5. Look for API requests containing `box=` and `userID=`

Example URLs:
```
# Box ID in booking requests:
https://test.aimharder.com/api/bookings?day=20260108&familyId=&box=1234
                                                                               ^^^^
                                                                           Box ID: 1234

# User ID in activity requests:
https://aimharder.com/api/activity?timeLineFormat=0&timeLineContent=2&userID=123456
                                                                      ^^^^^^
                                                                   User ID: 123456
```

## Scheduled Syncing

### Using Cron (Native)

```bash
# Edit crontab
crontab -e

# Add daily sync at 6 AM
0 6 * * * /path/to/aimharder-sync sync --days 7 >> /var/log/aimharder-sync.log 2>&1
```

### Using Docker Compose

```bash
# Start with scheduled profile (runs at 6 AM daily)
docker-compose --profile scheduled up -d

# View logs
docker-compose logs -f aimharder-sync-cron
```

### Using Systemd Timer

```bash
# Create /etc/systemd/system/aimharder-sync.service
[Unit]
Description=AimHarder to Strava Sync
After=network.target

[Service]
Type=oneshot
EnvironmentFile=/etc/aimharder-sync/.env
ExecStart=/usr/local/bin/aimharder-sync sync --days 7

[Install]
WantedBy=multi-user.target
```

```bash
# Create /etc/systemd/system/aimharder-sync.timer
[Unit]
Description=Run AimHarder Sync Daily

[Timer]
OnCalendar=*-*-* 06:00:00
Persistent=true

[Install]
WantedBy=timers.target
```

```bash
# Enable and start
sudo systemctl enable --now aimharder-sync.timer
```

### Home Assistant OS 🏠

Deploy as a Home Assistant Add-on for automatic syncing and webhook triggers:

1. **Copy the add-on to your HA** (via Samba or SSH):
   ```bash
   # Copy the addons/aimharder-sync folder to /addons/ on your HAOS
   scp -r addons/aimharder-sync root@homeassistant.local:/addons/
   ```

2. **Install the add-on**:
   - Go to **Settings → Add-ons → Add-on Store**
   - Click ⋮ → **Check for updates**
   - Find "AimHarder Sync" under "Local add-ons"
   - Click **Install**

3. **Configure** in the add-on settings with your credentials

4. **Add webhook integration** to `configuration.yaml`:
   ```yaml
   rest_command:
     aimharder_sync:
       url: "http://localhost:8080/sync"
       method: POST
       headers:
         X-Auth-Token: !secret aimharder_webhook_token
       timeout: 120
   ```

5. **Create a script** for the HA Companion App widget:
   ```yaml
   script:
     sync_aimharder:
       alias: "Sync AimHarder"
       icon: mdi:weight-lifter
       sequence:
         - service: rest_command.aimharder_sync
   ```

See [addons/README.md](addons/README.md) for full documentation.

## How It Works

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│                 │     │                 │     │                 │
│   AimHarder     │────▶│  aimharder-sync │────▶│     Strava      │
│   (Your Box)    │     │                 │     │                 │
│                 │     │                 │     │                 │
└─────────────────┘     └─────────────────┘     └─────────────────┘
        │                       │                       │
        │                       │                       │
        ▼                       ▼                       ▼
   📅 Bookings              📄 TCX Files           🏋️ Activities
   🏋️ WOD Details          📊 Sync History        with:
   🎯 Your Results                                 - Name
                                                   - Description
                                                   - Duration
                                                   - Score/Results
```

1. **Login** to AimHarder with your credentials
2. **Fetch** your class bookings and WOD details
3. **Extract** workout information and your results
4. **Generate** TCX files (industry standard workout format)
5. **Upload** to Strava via their API
6. **Track** sync history to avoid duplicates

## Workout Data Captured

The sync captures and transfers:

| Data | Source | Strava Field |
|------|--------|--------------|
| Workout Name | WOD name | Activity Name |
| Description | WOD movements/exercises | Description |
| Date/Time | Class booking time | Start Time |
| Duration | Time cap or result time | Elapsed Time |
| Score | Your recorded result | Notes |
| Scaling (Rx/Scaled) | Your selection | Notes |
| Box Name | Your gym | Notes |

## Troubleshooting

### "Login failed: invalid credentials"
- Double-check your email and password
- Try logging into AimHarder website to verify credentials
- Check for special characters that might need escaping

### "403 Forbidden" from AimHarder
- AimHarder blocks some IP ranges (especially US cloud providers)
- If running from outside Spain, you may need a Spanish proxy
- Docker containers should work fine from Spain

### "Not authenticated with Strava"
- Run `aimharder-sync auth strava` again
- Ensure port 8080 is accessible
- Check that OAuth tokens are saved in the data directory

### "Upload failed: duplicate"
- The workout already exists in Strava
- Use `--force` to re-upload if needed

### No workouts found
- Verify the date range includes days you attended classes
- Check that your bookings show as "attended" in AimHarder
- Try fetching a wider date range

## Development

### Project Structure

```
aimharder-sync/
├── cmd/
│   └── main.go           # CLI entry point
├── internal/
│   ├── aimharder/        # AimHarder client
│   ├── strava/           # Strava client + OAuth
│   ├── tcx/              # TCX file generator
│   ├── config/           # Configuration
│   └── models/           # Data structures
├── configs/              # Example configs
├── Dockerfile
├── docker-compose.yaml
└── README.md
```

## License

MIT License - see LICENSE file

## Contributing

Contributions welcome! Please:
1. Fork the repository
2. Create a feature branch
3. Submit a pull request

## Acknowledgments

- [AimHarder](https://aimharder.com) - For the great CrossFit management platform
- [Strava](https://strava.com) - For the excellent fitness tracking platform
- [pablobuenaposada/fitbot](https://github.com/pablobuenaposada/fitbot) - Inspiration for AimHarder API research

---

Made with 💪 for the CrossFit community
