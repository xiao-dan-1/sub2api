# Isolate Custom Image Update and Stabilize Upstream Sync

## Context

The fork must continue publishing and updating from its own image repository, `ghcr.io/xiao-dan-1/sub2api`, with custom `v*-xd.*` releases and Watchtower-driven container replacement.

The current implementation embeds this behavior into the upstream system update path. Custom commit `107362bd9` changes the same handler, handler tests, configuration tests, and frontend API module that upstream later changed for detached long-running updates. Git cannot combine these overlapping edits automatically.

The scheduled sync workflow successfully mirrors `Wei-Shaw/sub2api:main` to the fork's `main`, then fails while merging `main` into `custom`. Runs 76 through 102 failed on the same blocked custom state. The current conflict set is:

- `backend/cmd/server/VERSION`
- `backend/internal/config/config_test.go`
- `backend/internal/handler/admin/system_handler.go`
- `backend/internal/handler/admin/system_handler_test.go`
- `frontend/src/api/admin/system.ts`

Repeated scheduled failures do not add new information and create notification fatigue.

## Goals

1. Preserve the fork's own GHCR image, `xd.N` versioning, web update control, and Watchtower restart behavior.
2. Restore upstream's native binary update implementation without fork-specific method signatures or response fields.
3. Move custom image update behavior into isolated backend and frontend modules.
4. Resolve the current upstream merge and publish the next custom release.
5. Notify once per unresolved conflict incident instead of failing every scheduled run.
6. Keep `custom` at the last fully tested and releasable commit until a candidate merge passes CI.

## Non-Goals

- Allowing users to submit arbitrary image names, registries, or tags.
- Automatically choosing one side of unknown source conflicts.
- Replacing Watchtower with direct Docker socket access.
- Removing the upstream binary update path for source or official release builds.
- Guaranteeing that no future Git conflict can ever occur.

## Architecture

### Separate update domains

The upstream update domain remains responsible for official binary release updates and rollback:

- `SystemHandler.PerformUpdate`
- `UpdateService.PerformUpdate(ctx) error`
- upstream `frontend/src/api/admin/system.ts`
- upstream request-detachment and 15-minute update/rollback timeout behavior

The custom image update domain becomes a separate vertical slice:

- `backend/internal/service/custom_image_update_service.go`
- `backend/internal/handler/admin/custom_image_update_handler.go`
- separate custom-image handler tests
- `frontend/src/api/admin/custom-image-update.ts`
- custom-image frontend tests

Existing low-level clients are reused:

- `ContainerRegistryTagClient` for GHCR tag and manifest digest lookup
- `WatchtowerClient` for authenticated update triggering

The custom service no longer changes the return type of upstream `UpdateService.PerformUpdate`.

### Backend API

The custom handler exposes admin-only endpoints:

- `GET /api/v1/admin/system/custom-image/check`
- `POST /api/v1/admin/system/custom-image/update`

The check response contains:

- current custom version
- target custom version
- configured image repository
- exact target image tag
- target manifest digest
- whether an update exists
- whether the exact tag and latest alias are ready
- a non-secret warning when configuration or publication is incomplete

The update request accepts no image, repository, tag, digest, or Watchtower URL from the browser. All targets are derived from server configuration and verified registry metadata.

When Watchtower returns before container replacement, the update response uses HTTP `200 OK` and contains:

- operation ID
- target version
- target image
- target digest
- `automatic_restart: true`

The handler waits for the existing bounded Watchtower trigger request, using a detached context with the existing 10-minute limit. Watchtower may replace the current container before the HTTP response reaches the browser, so a connection loss after a validated trigger is treated as an expected transition and the frontend starts version polling.

### Update execution

`CustomImageUpdateService.Check`:

1. Reads the current running custom version.
2. Lists compatible `v*-xd.*` tags from the configured GHCR repository.
3. Selects the highest valid custom version.
4. Confirms the exact image manifest exists.
5. Confirms the configured latest alias points at the expected release when required.
6. Returns a typed readiness result.

`CustomImageUpdateService.Trigger`:

1. Re-runs readiness validation to prevent a stale UI decision.
2. Rejects missing configuration, invalid tag formats, missing digests, or unchanged versions.
3. Calls Watchtower with a detached context and the existing 10-minute trigger timeout so a browser disconnect cannot cancel the update cycle.
4. Returns the target metadata if Watchtower responds before replacement.

The custom handler acquires the existing system-operation idempotency lock before calling the service. The frontend preserves the current restart-detection behavior: wait 1.5 seconds, poll the existing public settings/version endpoint every 2 seconds, and stop after 10 minutes. A matching target version is success; a timeout produces an actionable Watchtower status message.

### Frontend separation

`frontend/src/api/admin/system.ts` is restored to the upstream implementation, including the upstream 15-minute timeout for binary update and rollback.

`frontend/src/api/admin/custom-image-update.ts` owns:

- custom image check/result types
- the check endpoint
- the trigger endpoint
- public-version polling
- the 11-minute browser timeout for the 10-minute Watchtower trigger
- the 1.5-second initial delay, 2-second polling interval, and 10-minute restart-detection limit

Custom UI components import the custom module directly. They do not extend upstream `VersionInfo` or `UpdateResult` interfaces.

### Test separation

Fork-specific tests move out of upstream-owned files:

- custom configuration environment tests move to `config_custom_update_test.go`
- custom handler tests move to `custom_image_update_handler_test.go`
- custom frontend API tests move to `custom-image-update.spec.ts`

This prevents additive custom tests from conflicting when upstream adds tests to its primary files.

## Current Conflict Resolution

The current merge adopts upstream `0.1.162` as the base version and combines all upstream changes.

Resolution policy:

- `VERSION`: take upstream `0.1.162`; custom versioning is applied by the release tag.
- `config_test.go`: retain upstream tests; move the custom environment test to its own file.
- `system_handler.go`: retain upstream's detached binary update implementation.
- `system_handler_test.go`: retain upstream tests; move fork-specific image tests to the new custom handler test file.
- `system.ts`: retain upstream's binary update API; move fork fields and requests to the custom frontend module.

After verification, publish the next unused `v0.1.162-xd.N` tag.

## Sync Workflow Conflict Deduplication

The workflow continues to fail safely on unknown semantic conflicts, but it does not repeatedly fail for the same incident.

A conflict incident is identified by the sorted set of conflicted paths stored in the single open `sync-conflict` issue. A newer upstream SHA with the same path set updates the existing incident; a changed path set is a new incident and produces one new failure notification.

On a new incident:

1. Create one GitHub Issue labeled `sync-conflict`.
2. Include custom SHA, latest upstream SHA, merge base, conflict paths, workflow URL, and reproduction commands.
3. Keep the first run failed so the incident is visible.

On subsequent runs with the same unresolved conflict path set:

1. Update the issue with the newest upstream SHA when it changes.
2. Write an Actions summary.
3. Exit successfully without another failure notification.

On a successful merge:

1. Close the open conflict issue with the merge/tag information.
2. Continue normal custom tag creation and release dispatch.

The workflow receives `issues: write` permission. It never force-resolves source conflicts.

The polling schedule is reduced from four times per hour to once per hour because upstream releases do not require 15-minute polling and repeated merge attempts do not improve a blocked state.

## Security

- GHCR repository and Watchtower endpoint are server-side configuration.
- Watchtower tokens are never returned, logged, or included in issue content.
- Only authenticated administrators may check or trigger custom image updates.
- Tag names must match the custom semantic version pattern.
- Manifest digest verification prevents an unverified tag from being triggered.
- The client cannot request arbitrary images or registries.
- Existing operation locking prevents concurrent update triggers.

## Verification

Local and CI verification must include:

- custom image service readiness and trigger unit tests
- Watchtower authentication, timeout, and error tests
- GHCR pagination, tag filtering, and digest tests
- custom handler authorization, idempotency, success-response, expected disconnect, and timeout tests
- frontend check, trigger, polling-success, polling-timeout, and error tests
- upstream system handler tests unchanged and passing
- upstream frontend system API tests unchanged and passing
- full backend unit and integration CI
- frontend lint, typecheck, and critical Vitest suite
- security scan and release workflow
- a merge-tree reproduction that fails before the refactor and succeeds after it

The latest custom-image-specific baseline tests pass. A full Windows `go test ./...` baseline produced no failure event but exceeded the 10-minute local command limit; GitHub Linux CI is the authoritative full-suite gate.

## Rollout

1. Implement the isolated custom image update slice with tests.
2. Resolve the current upstream merge using the separation policy.
3. Add workflow conflict issue deduplication.
4. Run complete local verification.
5. Push the candidate to `custom`.
6. Publish the next custom tag.
7. Monitor CI, security, release, image publication, and the next scheduled sync.
