# AimHarder Sync - Home Assistant Add-ons

Home Assistant add-on repository for AimHarder workout sync.

## Add-ons

### [AimHarder Sync](./aimharder-sync)

![Supports amd64 Architecture][amd64-shield]
![Supports aarch64 Architecture][aarch64-shield]
![Supports armv7 Architecture][armv7-shield]

Automatically sync your AimHarder workouts to Strava.

## Installation

### Option 1: Local Add-on (Recommended for Development)

1. Copy the `addons/aimharder-sync` folder to your Home Assistant's `/addons/` directory
2. Go to **Settings → Add-ons → Add-on Store**
3. Click the **⋮** menu (top right) → **Check for updates**
4. Find "AimHarder Sync" under "Local add-ons"
5. Click **Install**

### Option 2: GitHub Repository

1. Go to **Settings → Add-ons → Add-on Store**
2. Click the **⋮** menu → **Repositories**
3. Add: `https://github.com/Matbe34/aimharder-sync`
4. Find "AimHarder Sync" and install

## Quick Setup

1. Install the add-on
2. Configure with your Strava and AimHarder credentials
3. Start the add-on
4. Add the `rest_command` to your `configuration.yaml`:

```yaml
rest_command:
  aimharder_sync:
    url: "http://localhost:8080/sync"
    method: POST
    headers:
      X-Auth-Token: !secret aimharder_webhook_token
    timeout: 120
```

5. Add to `secrets.yaml`:

```yaml
aimharder_webhook_token: "your-webhook-token-here"
```

6. Create a script for phone widgets:

```yaml
script:
  sync_aimharder:
    alias: "Sync AimHarder"
    icon: mdi:weight-lifter
    sequence:
      - service: rest_command.aimharder_sync
```

7. Add the script to a Home Assistant Companion App widget!

## Deployment

To deploy to Home Assistant OS:

```bash
# Prepare the add-on (copies source files)
bash addons/aimharder-sync/prepare.sh

# Copy to Home Assistant
scp -r addons/aimharder-sync user@homeassistant.local:/addons/
```

Then in HA: Settings → Add-ons → Add-on Store → ⋮ → Check for updates

[amd64-shield]: https://img.shields.io/badge/amd64-yes-green.svg
[aarch64-shield]: https://img.shields.io/badge/aarch64-yes-green.svg
[armv7-shield]: https://img.shields.io/badge/armv7-yes-green.svg
