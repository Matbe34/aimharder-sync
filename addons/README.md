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

5. Create a script or button to trigger syncs!

[amd64-shield]: https://img.shields.io/badge/amd64-yes-green.svg
[aarch64-shield]: https://img.shields.io/badge/aarch64-yes-green.svg
[armv7-shield]: https://img.shields.io/badge/armv7-yes-green.svg
