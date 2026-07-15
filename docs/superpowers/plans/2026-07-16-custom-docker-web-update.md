# Custom Docker Web Update Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Web update button preserve author release notifications while updating custom Docker deployments to `ghcr.io/xiao-dan-1/sub2api:latest` through an internal Watchtower sidecar.

**Architecture:** The update service continues reading the author's latest GitHub Release and adds a GHCR client to resolve the matching highest `-xd.N` package plus exact/`latest` manifest digests. Custom updates call a bearer-authenticated Watchtower client synchronously, while the frontend recovers from replacement interruptions through the public version endpoint. The tag dispatcher serializes missing package builds, Release gates on exact-ref CI/Security, and `latest` is repaired to the highest published custom tag.

**Tech Stack:** Go, Gin, Wire, Docker Registry HTTP API V2, Watchtower HTTP API, Vue 3, Pinia, TypeScript, Vitest, Docker Compose.

---

### Task 1: Define custom package discovery with failing service tests

**Files:**
- Modify: `backend/internal/service/update_service_test.go`
- Modify: `backend/internal/service/update_service.go`

- [ ] **Step 1: Add failing tests for exact custom tag selection**

Add `ContainerTagClient` and `ContainerUpdater` stubs. Prove that `0.1.156-xd.10` wins over `0.1.156-xd.9`, while `latest`, `0.1.156-xd.10-amd64`, tags from another version core, and malformed tags are ignored.

- [ ] **Step 2: Run the focused test and confirm RED**

```bash
cd backend
go test -tags=unit ./internal/service -run 'TestUpdateService.*Custom' -count=1
```

Expected: FAIL because custom package fields and tag-client dependencies do not exist.

- [ ] **Step 3: Add failing state tests**

Cover these cases:

```text
0.1.156-xd.4 + upstream 0.1.156 + tag 0.1.156-xd.5 => ready
0.1.156-xd.4 + upstream 0.1.157 + no 0.1.157-xd.N => waiting
0.1.156-xd.4 + upstream 0.1.157 + tag 0.1.157-xd.2 => ready
```

- [ ] **Step 4: Implement the minimal resolver**

Add `UpdateServiceOptions`, custom metadata on `UpdateInfo`, exact-tag parsing, numeric `xd.N` comparison, and cache persistence for the new fields.

- [ ] **Step 5: Run tests and confirm GREEN**

Run the focused service command again and expect PASS.

### Task 2: Implement and test the GHCR tag client

**Files:**
- Create: `backend/internal/repository/container_registry_tags.go`
- Create: `backend/internal/repository/container_registry_tags_test.go`
- Modify: `backend/internal/repository/wire.go`

- [ ] **Step 1: Write failing HTTP tests**

Assert the anonymous flow requests a token with scope `repository:xiao-dan-1/sub2api:pull`, then requests `/v2/xiao-dan-1/sub2api/tags/list?n=100` with `Authorization: Bearer <token>`. Cover unsupported hosts and non-success responses.

- [ ] **Step 2: Run tests and confirm RED**

```bash
cd backend
go test ./internal/repository -run 'TestContainerRegistryTagClient' -count=1
```

Expected: FAIL because the client does not exist.

- [ ] **Step 3: Implement tags and manifest digest lookup, then confirm GREEN**

Use the existing proxy-aware HTTP client factory, restrict discovery to `ghcr.io`, paginate Registry API tags, and resolve `Docker-Content-Digest` with an authenticated manifest `HEAD`. Rerun the focused test.

### Task 3: Implement and test the Watchtower client

**Files:**
- Create: `backend/internal/repository/watchtower_client.go`
- Create: `backend/internal/repository/watchtower_client_test.go`
- Modify: `backend/internal/repository/wire.go`

- [ ] **Step 1: Write failing HTTP tests**

Assert `GET /v1/update` and `Authorization: Bearer test-token`. Cover missing configuration, a successful 2xx response, and a bounded error for non-2xx status.

- [ ] **Step 2: Run tests and confirm RED**

```bash
cd backend
go test ./internal/repository -run 'TestWatchtowerClient' -count=1
```

Expected: FAIL because the client does not exist.

- [ ] **Step 3: Implement the client and confirm GREEN**

Use an internal HTTP client with a finite timeout and no environment proxy. Validate the configured absolute HTTP(S) URL without logging the bearer token.

### Task 4: Schedule custom updates and return restart metadata

**Files:**
- Modify: `backend/internal/service/update_service_test.go`
- Modify: `backend/internal/service/update_service.go`
- Modify: `backend/internal/handler/admin/system_handler_test.go`
- Modify: `backend/internal/handler/admin/system_handler.go`

- [ ] **Step 1: Write failing scheduling tests**

Use a blocking channel-backed updater stub to prove a ready custom update does not return before Watchtower accepts or rejects the request:

```go
UpdateExecutionResult{
    NeedRestart:      false,
    AutomaticRestart: true,
    TargetVersion:    "0.1.156-xd.5",
    TargetImage:      "ghcr.io/xiao-dan-1/sub2api:latest",
}
```

Add conflicts for package-not-ready and updater-not-configured states.

- [ ] **Step 2: Run service tests and confirm RED**

Expected: FAIL because `PerformUpdate` still returns only an error and rejects custom builds.

- [ ] **Step 3: Implement custom scheduling**

Keep the official binary updater synchronous. For custom builds, validate a forced check and call `TriggerUpdate` synchronously with `context.WithoutCancel` plus a finite timeout. Return target metadata only after success and propagate Watchtower failures.

- [ ] **Step 4: Update failing handler tests and implementation**

Change the handler stub to return `*UpdateExecutionResult`. Assert manual updates return `need_restart: true`; custom updates return `automatic_restart: true`, `target_version`, and `target_image`. Preserve already-up-to-date behavior.

### Task 5: Add configuration and regenerate Wire

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/internal/repository/wire.go`
- Modify: `backend/cmd/server/wire_gen.go`
- Modify: `deploy/config.example.yaml`

- [ ] **Step 1: Add update fields**

```go
CustomRepo      string `mapstructure:"custom_repo"`
CustomImage     string `mapstructure:"custom_image"`
WatchtowerURL   string `mapstructure:"watchtower_url"`
WatchtowerToken string `mapstructure:"watchtower_token"`
```

- [ ] **Step 2: Wire both adapters into `ProvideUpdateService`**

Pass `UpdateServiceOptions` from `*config.Config`, then run:

```bash
cd backend
go generate ./cmd/server
go test -tags=unit ./internal/service ./internal/handler/admin ./internal/repository -count=1
```

Expected: generated Wire code compiles and focused tests pass.

### Task 6: Define frontend state with failing tests

**Files:**
- Modify: `frontend/src/api/admin/system.ts`
- Modify: `frontend/src/stores/app.ts`
- Modify: `frontend/src/stores/__tests__/app.spec.ts`
- Create: `frontend/src/components/common/__tests__/VersionBadge.custom-update.spec.ts`

- [ ] **Step 1: Add failing store tests**

Mock `checkUpdates` with custom fields and assert `fetchVersion` stores and returns them from cache. Assert `clearVersionCache` clears custom readiness.

- [ ] **Step 2: Confirm store RED**

```bash
cd frontend
pnpm vitest run src/stores/__tests__/app.spec.ts
```

- [ ] **Step 3: Add and run failing component tests**

Assert that ready state preserves the author version/link and shows the exact custom target/button; waiting state preserves the author link without an update button; automatic update enters a reconnecting state.

```bash
cd frontend
pnpm vitest run src/components/common/__tests__/VersionBadge.custom-update.spec.ts
```

Expected: FAIL because custom states do not exist.

### Task 7: Implement the custom Web update experience

**Files:**
- Modify: `frontend/src/api/admin/system.ts`
- Modify: `frontend/src/stores/app.ts`
- Modify: `frontend/src/components/common/VersionBadge.vue`
- Modify: `frontend/src/i18n/locales/zh/misc.ts`
- Modify: `frontend/src/i18n/locales/en/misc.ts`

- [ ] **Step 1: Extend API and store types**

Add custom fields to `VersionInfo` and automatic restart fields to `UpdateResult`. Mirror the custom state in Pinia and its cached return object.

- [ ] **Step 2: Render distinct custom states**

Add `isCustomBuild`, keep the author's Release link visible, show the exact custom target when present, and leave true source builds on the existing `git pull` path.

- [ ] **Step 3: Poll for the target version**

After `automatic_restart: true` or an expected replacement connection interruption, wait briefly and call the public settings API until `version === target_version`. Ignore temporary network errors during replacement, reload on success, and show a timeout error otherwise.

- [ ] **Step 4: Add Chinese and English labels, then confirm GREEN**

Run both focused Vitest commands and expect PASS.

### Task 8: Configure the local Compose deployment

**Files:**
- Modify: `deploy/docker-compose.local.yml`
- Modify: `deploy/.env.example`
- Modify locally (ignored): `deploy/.env`

- [ ] **Step 1: Point Sub2API at the custom image**

Set `ghcr.io/xiao-dan-1/sub2api:latest`, add the Watchtower enable label, and pass custom repo/image plus the internal endpoint/token to the app.

Use the same `${UPDATE_CUSTOM_IMAGE}:latest` expression for the running container so Registry readiness and the image Watchtower updates cannot diverge.

- [ ] **Step 2: Add the Watchtower sidecar**

Use `containrrr/watchtower:1.7.1` with `--http-api-update`, `--label-enable`, and `--cleanup`. Mount `/var/run/docker.sock` only in Watchtower, join only `sub2api-network`, and publish no host port.

Require a unique `WATCHTOWER_SCOPE`, pass it to `--scope`, and apply the matching scope label to both Sub2API and Watchtower.

Set `DOCKER_API_VERSION=${WATCHTOWER_DOCKER_API_VERSION:-1.44}` for all Docker Engine 29 minors. Document `1.40` as the override for Engine 24 or older.

- [ ] **Step 3: Document and set the token**

Keep `WATCHTOWER_HTTP_API_TOKEN` and `WATCHTOWER_SCOPE` empty in `.env.example`; generate a random token and unique scope in the ignored local `.env`.

- [ ] **Step 4: Validate Compose**

```bash
cd deploy
docker compose -f docker-compose.local.yml config
```

Expected: one Docker Socket mount owned by Watchtower, no Watchtower host port, matching API tokens, the custom image, and the app update label.

### Task 9: Harden custom tag release automation

**Files:**
- Modify: `.github/workflows/auto-build-custom-tags.yml`
- Modify: `.github/workflows/sync-upstream-custom-pack.yml`
- Modify: `.github/workflows/release.yml`
- Modify: `.github/workflows/backend-ci.yml`
- Modify: `.github/workflows/security-scan.yml`

- [ ] **Step 1: Make CI and Security reusable against an exact ref**

Add `workflow_call.inputs.ref`, use that ref in every checkout, and exclude tag pushes from the ordinary push trigger so a Release does not duplicate the same gates.

- [ ] **Step 2: Gate Release and use one source revision**

Validate `vX.Y.Z-xd.N`, invoke both reusable workflows with the tag, make all build checkouts use that tag, narrow write permissions to publishing jobs, and serialize Release publication.

- [ ] **Step 3: Turn the auto-builder into the common queue**

For a create/manual event inspect that tag; for schedule-without-input scan every custom tag in version order. Dispatch one missing Release, identify it by `run-name`, wait for success, then continue. Afterward use `crane tag` to point `latest` at the highest published exact image and verify equal digests.

- [ ] **Step 4: Delegate sync-generated tags to the queue**

Remove direct Release dispatch and active-run polling from the sync workflow. After creating a tag, dispatch `auto-build-custom-tags.yml` with that exact tag. Accept a custom-suffixed VERSION when calculating the next base version.

- [ ] **Step 5: Validate workflow syntax**

```bash
docker run --rm -v "${PWD}:/repo" -w /repo rhysd/actionlint:latest
```

Expected: exit 0 with no findings.

### Task 10: Full verification and rendered QA

**Files:**
- No additional production files expected.

- [ ] **Step 1: Run backend verification**

```bash
cd backend
go test -tags=unit ./internal/service ./internal/handler/admin ./internal/repository -count=1
go test ./cmd/server ./internal/config -count=1
```

- [ ] **Step 2: Run frontend verification**

```bash
cd frontend
pnpm vitest run src/stores/__tests__/app.spec.ts src/components/common/__tests__/VersionBadge.custom-update.spec.ts
pnpm typecheck
pnpm build
```

Record the known pre-existing `@intlify/message-compiler` failure separately if a broader suite still encounters it.

- [ ] **Step 3: Start and inspect the local deployment**

```bash
cd deploy
docker compose -f docker-compose.local.yml up -d
docker compose -f docker-compose.local.yml ps
```

Expected: Sub2API, Watchtower, PostgreSQL, and Redis are running.

- [ ] **Step 4: Verify the admin version dropdown in the browser**

Exercise desktop and mobile widths, ready/waiting states, interaction behavior, console output, and layout overlap.

### Task 11: Publish the fixed custom package

**Files:**
- Commit tracked implementation and documentation files; never add `deploy/.env` or `deploy/recovery_backups/`.

- [ ] **Step 1: Review the final diff**

```bash
git status --short
git diff --check
git diff --stat
```

- [ ] **Step 2: Commit and push `custom`**

```bash
git add .github/workflows/auto-build-custom-tags.yml .github/workflows/backend-ci.yml .github/workflows/release.yml .github/workflows/security-scan.yml .github/workflows/sync-upstream-custom-pack.yml docs/superpowers/specs/2026-07-16-custom-docker-web-update-design.md docs/superpowers/plans/2026-07-16-custom-docker-web-update.md backend/internal/config/config.go backend/internal/config/config_test.go backend/internal/service/update_service.go backend/internal/service/update_service_test.go backend/internal/service/wire.go backend/internal/repository/container_registry_tags.go backend/internal/repository/container_registry_tags_test.go backend/internal/repository/watchtower_client.go backend/internal/repository/watchtower_client_test.go backend/internal/repository/wire.go backend/internal/handler/admin/system_handler.go backend/internal/handler/admin/system_handler_test.go backend/cmd/server/wire_gen.go frontend/src/api/admin/system.ts frontend/src/stores/app.ts frontend/src/stores/__tests__/app.spec.ts frontend/src/components/common/VersionBadge.vue frontend/src/components/common/__tests__/VersionBadge.custom-update.spec.ts frontend/src/i18n/locales/zh/misc.ts frontend/src/i18n/locales/en/misc.ts deploy/config.example.yaml deploy/docker-compose.local.yml deploy/.env.example
git commit -m "feat: update custom Docker image from web"
git push origin custom
```

- [ ] **Step 3: Push the next custom tag**

Create the next available `v0.1.156-xd.N` tag on the feature commit. The repaired tag dispatcher must trigger `release.yml`.

- [ ] **Step 4: Verify Actions and GHCR**

Confirm the dispatcher and Release workflow succeed, then verify the exact tag and `latest` manifests exist in `ghcr.io/xiao-dan-1/sub2api`.
