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
- The tag dispatcher can publish before CI finishes, and `release.yml` can build frontend assets from `custom` while building the backend from the requested tag.
- The scheduled repair path checks only the newest tag, so rapid or token-generated tag events can leave older custom packages permanently unbuilt.

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

Before marking that package ready, resolve the Registry manifest digest for both the exact custom tag and `latest`. The button is enabled only when both tags point to the same digest, preventing a release race from making Watchtower pull an older image.

### Container Update

Add a Watchtower sidecar to `docker-compose.local.yml` and enable its HTTP update API. The Sub2API container calls Watchtower over the internal Compose network; Watchtower alone receives the Docker Socket mount.

Security boundaries:

- Sub2API never receives `/var/run/docker.sock`.
- Watchtower publishes no host port.
- The API requires a shared random bearer token from `WATCHTOWER_HTTP_API_TOKEN`.
- `--label-enable` limits updates to the explicitly labeled Sub2API container.
- A required unique `WATCHTOWER_SCOPE` labels both Sub2API and its Watchtower instance, preventing interference with other stacks on the same Docker daemon.
- `--cleanup` removes the replaced image after a successful update.

The application image comes from the same `UPDATE_CUSTOM_IMAGE` value used for Registry validation and always runs its `latest` tag. The update endpoint calls Watchtower synchronously with a detached, bounded context so DNS, token, and HTTP failures are returned directly. Watchtower's HTTP 200 means the scan completed, not that every Docker action succeeded; final success is therefore confirmed only when the public version endpoint reports the target version.

### API Contract

`check-updates` adds custom-build metadata while preserving all existing fields:

- `custom_version`: exact custom package version selected from GHCR;
- `custom_image`: configured custom image name;
- `custom_release_url`: derived private-fork Release URL;
- `custom_update_available`: the selected custom tag is newer than the running custom build;
- `custom_update_ready`: a newer exact package exists, its manifest digest matches `latest`, and Watchtower is configured;
- `custom_update_warning`: package-discovery, waiting, or configuration detail.

`POST /admin/system/update` returns:

- `need_restart: true` and `automatic_restart: false` for the existing binary updater;
- `need_restart: false`, `automatic_restart: true`, `target_version`, and `target_image` after Watchtower accepts the update request.

## Web Experience

Custom builds get their own state instead of the source-build hint:

- Continue showing the author's latest version and Release link.
- When the matching custom package is ready, show its exact `-xd.N` target and an update button.
- While the custom package is still being built, show a waiting state and retain the refresh action.
- After triggering an update, poll the public settings endpoint until its running version equals the target, then reload the page. This remains valid even if a blank `JWT_SECRET` invalidates the previous administrator token after restart.
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

`WATCHTOWER_HTTP_API_TOKEN` and a unique `WATCHTOWER_SCOPE` are required in `deploy/.env`; their examples remain empty so copied configuration fails closed. The GHCR package is currently public, so no registry password is required.

Watchtower 1.7.1 defaults to Docker API 1.25, which modern daemons reject. Compose defaults it to API `1.44` so every Docker Engine 29 minor is supported; deployments on Engine 24 or older can override `WATCHTOWER_DOCKER_API_VERSION=1.40`.

## Release Automation

- Local matching tag pushes and sync-generated tags both enter `auto-build-custom-tags.yml`.
- The dispatcher serializes work, scans every missing `v*-xd.*` image, dispatches one Release at a time, and waits for its result.
- `release.yml` validates the tag, runs reusable CI and Security workflows against that exact ref, and checks out the same tag in every build job.
- Release publication is serialized. After all missing images are built, the dispatcher points `latest` at the highest published custom tag and verifies that both registry digests match.
- The sync workflow updates the private mirror and custom branch, creates the next custom tag, and delegates package building to the common dispatcher.

## Error Handling

- An upstream GitHub failure keeps the existing cached fallback behavior.
- A GHCR discovery failure does not erase the author's update notification; it sets `custom_update_warning` and disables the custom update button.
- A missing matching custom tag returns a conflict if the update endpoint is called directly.
- Missing Watchtower URL/token disables the button and returns a configuration error if called directly.
- Watchtower connection/authentication failures are returned synchronously. Docker pull/recreation failures are detected by the bounded public-version polling timeout and remain visible in Watchtower logs.

## Testing

- Unit-test GHCR token/tag/manifest requests, bearer authentication, digest headers, pagination, and non-success responses.
- Unit-test exact custom-tag filtering, numeric `xd.N` ordering, digest alias gating, cached readiness, waiting state, same-core revision upgrades, and synchronous custom update triggering.
- Unit-test Watchtower request method, bearer token, endpoint, and status handling.
- Unit-test handler response metadata for manual and automatic restart paths.
- Unit-test frontend API/store mapping and custom ready/waiting/reconnecting states.
- Validate `docker compose config`, targeted Go tests, frontend tests, typecheck, and production build.
- Render and exercise the version dropdown in the browser at desktop and mobile widths.

## Scope

- Keep the workflow changes limited to reliable custom-tag dispatch, exact-ref verification/building, and `latest` repair.
- Do not add online rollback for custom images in this change.
- Do not expose Watchtower publicly or mount the Docker Socket into Sub2API.
- Do not require access to the private source repository at runtime.
