# Changelog

## [1.0.0] - 2026-01-10

### Added
- Initial release
- Automatic workout sync from AimHarder to Strava
- Webhook API for manual sync triggers (`/sync`, `/status`, `/health`)
- Configurable sync interval with quiet hours
- Multi-architecture support (amd64, aarch64, armv7)
- Duplicate prevention using Strava API and local history
- Home Assistant integration with rest_command
- Phone widget support via HA Companion App scripts

### Configuration Options
- `aimharder_email`, `aimharder_password`, `aimharder_box_id`, `aimharder_user_id`
- `strava_client_id`, `strava_client_secret`, `strava_refresh_token`
- `webhook_token`, `webhook_port`
- `sync_days`, `check_interval`, `quiet_hours_start`, `quiet_hours_end`
- `enable_scheduler`, `dry_run`
