# Custom Docker Web Update Design

## Goal

Keep update notifications tied to the author's releases at `Wei-Shaw/sub2api`, while allowing this fork's Web update button to install the matching custom image from `ghcr.io/xiao-dan-1/sub2api:latest` and recreate the Docker container automatically.

The supported deployment is:

```bash
docker compose -f docker-compose.local.yml up -d
```

## Confirmed Root Cause

- `UpdateService.PerformUpdate` rejects every custom build with `CUSTOM_BUILD_ONLINE_UPDATE_DISABLED`.
- `VersionBadge.vue` treats every non-release build as a source checkout and only links to the upstream Release page.
- `docker-compose.local.yml` still runs `weishaw/sub2api:latest`, so recreating the container cannot install this fork's package.
- Custom releases publish container images rather than binary archives, so the official in-place binary replacement updater cannot install them.

## Architecture

### Author Version Notification

Continue using the existing GitHub Release API for `Wei-Shaw/sub2api`. The API response keeps `latest_version`, release notes, publication time, and the author's Release URL unchanged.

Version comparison continues to compare the numeric upstream core (`major.minor.patch`) so a suffix such as `-xd.4` does not incorrectly make the current build older than the same author version.

### Custom Package Discovery

For custom builds, query the Docker Registry HTTP API for tags on `ghcr.io/xiao-dan-1/sub2api`. This package currently permits anonymous pulls and exposes tags such as:

```text
0.1.156-xd.3
0.1.156-xd.4
latest
```

Only exact custom tags matching `v?MAJOR.MINOR.PATCH-xd.N` are candidates. Architecture helper tags such as `-amd64` are ignored.

The resolver chooses the highest `xd.N` tag whose upstream core matches the author's latest release. This supports both cases:

- the author published a newer core version and the matching custom package is ready;
- the author core is unchanged, but a newer custom revision was built.

If the author is newer but no matching custom tag exists yet, the Web UI keeps the author's update notification and shows a waiting-for-custom-package state instead of installing the official image.

### Container Update

Add a Watchtower sidecar to `docker-compose.local.yml` and enable its HTTP update API. The Sub2API container calls Watchtower over the internal Compose network; Watchtower alone receives the Docker Socket mount.

Security boundaries:

- Sub2API never receives `/var/run/docker.sock`.
- Watchtower publishes no host port.
- The API requires a shared random bearer token from `WATCHTOWER_HTTP_API_TOKEN`.
- `--label-enable` limits updates to the explicitly labeled Sub2API container.
- `--cleanup` removes the replaced image after a successful update.

The application image changes to `ghcr.io/xiao-dan-1/sub2api:latest`. A successful custom update schedules the Watchtower request shortly after the Web API response, allowing the browser to receive target-version metadata before the container is stopped.

### API Contract

`check-updates` adds custom-build metadata while preserving all existing fields:

- `custom_version`: exact custom package version selected from GHCR;
- `custom_image`: configured custom image name;
- `custom_release_url`: derived private-fork Release URL;
- `custom_update_available`: the selected custom tag is newer than the running custom build;
- `custom_update_ready`: a newer package exists and Watchtower is configured;
- `custom_update_warning`: package-discovery, waiting, or configuration detail.

`POST /admin/system/update` returns:

- `need_restart: true` and `automatic_restart: false` for the existing binary updater;
- `need_restart: false`, `automatic_restart: true`, `target_version`, and `target_image` for a scheduled Watchtower update.

## Web Experience

Custom builds get their own state instead of the source-build hint:

- Continue showing the author's latest version and Release link.
- When the matching custom package is ready, show its exact `-xd.N` target and an update button.
- While the custom package is still being built, show a waiting state and retain the refresh action.
- After scheduling an update, poll the authenticated version endpoint until the running version equals the target, then reload the page.
- If the target does not appear before the timeout, show a useful error and leave the update notification available for retry.

Source checkouts keep the existing `git pull` guidance. Official release builds keep the existing binary update and manual restart flow.

## Configuration

Add these update settings with fork-specific defaults in the local Compose file:

```text
UPDATE_CUSTOM_REPO=xiao-dan-1/sub2api
UPDATE_CUSTOM_IMAGE=ghcr.io/xiao-dan-1/sub2api
UPDATE_WATCHTOWER_URL=http://watchtower:8080/v1/update
UPDATE_WATCHTOWER_TOKEN=<same value as WATCHTOWER_HTTP_API_TOKEN>
```

`WATCHTOWER_HTTP_API_TOKEN` is required in `deploy/.env`. The GHCR package is currently public, so no registry password is required.

## Error Handling

- An upstream GitHub failure keeps the existing cached fallback behavior.
- A GHCR discovery failure does not erase the author's update notification; it sets `custom_update_warning` and disables the custom update button.
- A missing matching custom tag returns a conflict if the update endpoint is called directly.
- Missing Watchtower URL/token disables the button and returns a configuration error if called directly.
- Background Watchtower request failures are logged. The Web polling timeout tells the administrator to inspect Watchtower logs.

## Testing

- Unit-test GHCR token/tag requests, bearer authentication, tag decoding, and non-success responses.
- Unit-test exact custom-tag filtering, numeric `xd.N` ordering, waiting state, same-core revision upgrades, and custom update scheduling.
- Unit-test Watchtower request method, bearer token, endpoint, and status handling.
- Unit-test handler response metadata for manual and automatic restart paths.
- Unit-test frontend API/store mapping and custom ready/waiting/reconnecting states.
- Validate `docker compose config`, targeted Go tests, frontend tests, typecheck, and production build.
- Render and exercise the version dropdown in the browser at desktop and mobile widths.

## Scope

- Keep upstream synchronization and custom-tag build workflows unchanged; they were repaired and verified separately.
- Do not add online rollback for custom images in this change.
- Do not expose Watchtower publicly or mount the Docker Socket into Sub2API.
- Do not require access to the private source repository at runtime.
