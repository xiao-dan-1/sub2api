# Isolate Custom Image Update and Stabilize Upstream Sync — Implementation Plan

> **Execution rule:** Follow this plan task-by-task with red/green/refactor checkpoints. Do not modify the user's original dirty workspace. Do not push `custom` or create a release tag until the isolated branch passes the stated local checks and GitHub CI is available as the full Linux gate.

**Goal:** Preserve `ghcr.io/xiao-dan-1/sub2api` and Watchtower-based `v*-xd.*` updates while restoring upstream's native binary update path and preventing identical unresolved sync conflicts from failing every scheduled run.

**Architecture:** Restore upstream-owned update files to upstream-compatible contracts, then add a separate custom-image service, handler, API client, state, UI call path, and tests. Keep registry discovery and Watchtower clients as low-level adapters. Change the sync workflow to track one conflict incident by the sorted conflict-path signature and fail only once per new signature.

**Technology:** Go, Gin, Wire, Vue 3, TypeScript, Pinia, Vitest, GitHub Actions, GitHub CLI/API, GHCR, Watchtower.

---

## Task 1: Extract the custom image update domain from `UpdateService`

**Files:**

- Create: `backend/internal/service/custom_image_update_service.go`
- Create: `backend/internal/service/custom_image_update_service_test.go`
- Modify: `backend/internal/service/update_service.go`
- Modify: `backend/internal/service/update_service_test.go`

### Step 1: Write failing custom service tests

Move the fork-specific test scenarios out of `update_service_test.go` and target a new `CustomImageUpdateService`:

- discovers the greatest compatible `v*-xd.*` tag;
- verifies exact-tag digest and latest-alias readiness;
- reports no update when the current version is newest;
- rejects invalid or incomplete custom configuration;
- re-checks readiness immediately before triggering;
- waits for Watchtower to return and returns target metadata;
- propagates Watchtower authentication, timeout, and other trigger errors;
- uses a detached, bounded trigger context so request cancellation does not abort replacement.

Run:

```powershell
Set-Location backend
go test -tags=unit ./internal/service -run '^TestCustomImageUpdateService' -count=1
```

Expected: FAIL because the new service does not exist.

### Step 2: Implement the minimal custom service

Move these custom-only types and responsibilities from `update_service.go` into `custom_image_update_service.go`:

- `ContainerTagClient` and `ContainerUpdater` adapter contracts;
- custom image options/configuration;
- custom image readiness/check result;
- custom trigger result;
- custom version/tag selection, exact manifest verification, alias verification;
- detached 10-minute Watchtower trigger behavior.

Keep repository adapters compatible with the extracted interfaces.

### Step 3: Restore the upstream update service contract

Remove custom fields and branches from `UpdateService`, `UpdateInfo`, and `PerformUpdate`. Restore:

```go
func (s *UpdateService) PerformUpdate(ctx context.Context) error
```

Keep upstream binary update, rollback, and 15-minute semantics intact. Retain only upstream system-update tests in `update_service_test.go`.

### Step 4: Run focused service tests

```powershell
Set-Location backend
go test -tags=unit ./internal/service -run '^(TestCustomImageUpdateService|TestUpdateService)' -count=1
```

Expected: PASS.

### Step 5: Commit the service extraction

```powershell
git add backend/internal/service/custom_image_update_service.go backend/internal/service/custom_image_update_service_test.go backend/internal/service/update_service.go backend/internal/service/update_service_test.go
git commit -m "refactor: isolate custom image update service"
```

---

## Task 2: Add a separate custom image admin handler and routes

**Files:**

- Create: `backend/internal/handler/admin/custom_image_update_handler.go`
- Create: `backend/internal/handler/admin/custom_image_update_handler_test.go`
- Modify: `backend/internal/handler/admin/system_handler.go`
- Modify: `backend/internal/handler/admin/system_handler_test.go`
- Modify: `backend/internal/handler/admin/operation_lock.go` or the existing operation-lock owner if required
- Modify: `backend/internal/handler/handler.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/internal/server/routes/admin.go`

### Step 1: Write failing handler tests

Cover the new endpoints:

- `GET /api/v1/admin/system/custom-image/check` returns typed readiness metadata;
- `POST /api/v1/admin/system/custom-image/update` returns operation ID, target version/image/digest, and `automatic_restart: true`;
- the update endpoint acquires the existing system operation lock;
- concurrent operations are rejected consistently;
- browser/request cancellation does not cancel the detached Watchtower trigger;
- service errors and trigger timeouts map to existing API error conventions;
- no Watchtower token or other secret is serialized.

Run:

```powershell
Set-Location backend
go test -tags=unit ./internal/handler/admin -run '^TestCustomImageUpdateHandler' -count=1
```

Expected: FAIL because the handler and routes do not exist.

### Step 2: Implement the handler and register routes

Add an admin-only handler with:

- `Check` method;
- `PerformUpdate` method;
- detached context plus 10-minute service trigger bound;
- existing operation-lock/idempotency behavior;
- no request body capable of overriding image/repository/tag/digest/Watchtower settings.

Register:

- `GET /api/v1/admin/system/custom-image/check`
- `POST /api/v1/admin/system/custom-image/update`

Add the handler to `AdminHandlers` and its provider set.

### Step 3: Restore the upstream system handler

Restore the upstream interface and response behavior around `UpdateService.PerformUpdate(ctx) error`, including its detached binary update execution and 15-minute limit. Remove custom automatic-restart response handling from this handler and move custom tests into the new test file.

### Step 4: Run focused handler tests

```powershell
Set-Location backend
go test -tags=unit ./internal/handler/admin -run '^(TestCustomImageUpdateHandler|TestSystemHandlerPerformUpdate)' -count=1
```

Expected: PASS.

### Step 5: Commit handler separation

```powershell
git add backend/internal/handler backend/internal/server/routes/admin.go
git commit -m "refactor: isolate custom image update handler"
```

---

## Task 3: Separate dependency injection and custom configuration tests

**Files:**

- Create: `backend/internal/config/config_custom_update_test.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/internal/repository/wire.go`
- Modify: `backend/internal/handler/wire.go`
- Regenerate: `backend/cmd/server/wire_gen.go`

### Step 1: Move fork-specific configuration tests

Move the `UPDATE_CUSTOM_IMAGE`, latest alias, Watchtower URL, and Watchtower token assertions out of upstream-owned `config_test.go` into `config_custom_update_test.go`. Keep production configuration fields unchanged unless the extracted service requires a clearer typed options conversion.

Run:

```powershell
Set-Location backend
go test -tags=unit ./internal/config -run 'Custom|Update' -count=1
```

Expected: PASS after the move, with upstream tests unchanged.

### Step 2: Split providers by domain

- `ProvideUpdateService` supplies only upstream binary-update dependencies.
- Add `ProvideCustomImageUpdateService` with tag client, updater, build version/type, and custom update configuration.
- Keep repository adapter providers reusable and secret values server-side.
- Wire the new handler independently from `SystemHandler`.

### Step 3: Regenerate Wire

Use the repository's generation command rather than hand-editing the generated graph:

```powershell
Set-Location backend
go generate ./cmd/server
```

### Step 4: Verify DI compilation and focused tests

```powershell
Set-Location backend
go test -tags=unit ./internal/config ./internal/service ./internal/handler/admin -run 'Custom|UpdateService|SystemHandlerPerformUpdate' -count=1
go test ./cmd/server -run '^$'
```

Expected: PASS.

### Step 5: Commit DI/test isolation

```powershell
git add backend/internal/config backend/internal/service/wire.go backend/internal/repository/wire.go backend/internal/handler/wire.go backend/cmd/server/wire_gen.go
git commit -m "refactor: wire custom image updates separately"
```

---

## Task 4: Separate the frontend API and application state

**Files:**

- Create: `frontend/src/api/admin/custom-image-update.ts`
- Create: `frontend/src/api/admin/__tests__/custom-image-update.spec.ts` or follow the repository's existing API-test directory convention
- Modify: `frontend/src/api/admin/system.ts`
- Modify: `frontend/src/stores/app.ts`
- Modify: `frontend/src/components/common/VersionBadge.vue`
- Modify: affected locale files only where custom-image labels already exist

### Step 1: Write failing frontend API tests

Cover:

- check request and response mapping;
- trigger request with an 11-minute browser timeout;
- no client-controlled image/tag/digest payload;
- 1.5-second initial polling delay;
- public version polling every 2 seconds;
- success when the reported version matches the target;
- connection loss during replacement transitions into polling;
- a 10-minute polling deadline returns the actionable Watchtower timeout message;
- non-transition API errors remain visible.

Run the exact new spec:

```powershell
Set-Location frontend
pnpm exec vitest run src/api/admin/__tests__/custom-image-update.spec.ts
```

Expected: FAIL because the custom module does not exist.

### Step 2: Implement the custom API module

Move all custom-only interfaces, endpoints, timeout constants, sleep/poll logic, and restart detection out of `system.ts`. Restore `system.ts` to upstream binary update/rollback behavior and its 15-minute timeout.

### Step 3: Separate application state

Stop extending upstream `VersionInfo` and `UpdateResult` with custom-image fields. Store custom readiness/update state independently in `app.ts`, or keep it local to `VersionBadge.vue` if it has no other consumer. Make `VersionBadge.vue` call the custom-image module only for custom image builds and the upstream system module only for official binary updates.

### Step 4: Run frontend tests and checks

```powershell
Set-Location frontend
pnpm exec vitest run src/api/admin/__tests__/custom-image-update.spec.ts
pnpm run typecheck
pnpm run lint:check
```

Expected: PASS.

### Step 5: Commit frontend separation

```powershell
git add frontend/src/api/admin frontend/src/stores/app.ts frontend/src/components/common/VersionBadge.vue frontend/src/i18n/locales
git commit -m "refactor: isolate custom image update frontend"
```

---

## Task 5: Merge the current upstream `main` safely

**Files:** Resolve only files reported by Git after the merge.

### Step 1: Refresh refs and record the candidate state

```powershell
git fetch origin main custom --tags
git status --short --branch
git rev-parse HEAD
git rev-parse origin/main
git merge-base HEAD origin/main
```

### Step 2: Merge upstream without publishing

```powershell
git merge --no-ff origin/main
```

Resolution policy:

- retain all upstream production changes;
- `backend/cmd/server/VERSION`: use upstream `0.1.162` (or the fetched newer upstream value if it changed before execution);
- upstream-owned update service/handler/frontend modules keep their restored upstream contracts;
- custom behavior remains only in the isolated files from Tasks 1–4;
- never choose `ours` or `theirs` for an entire unknown source file without reviewing the semantic diff.

### Step 3: Re-run focused regression checks before completing the merge

```powershell
Set-Location backend
go test -tags=unit ./internal/config ./internal/service ./internal/handler/admin -run 'Custom|UpdateService|SystemHandlerPerformUpdate' -count=1
Set-Location ../frontend
pnpm exec vitest run src/api/admin/__tests__/custom-image-update.spec.ts
pnpm run typecheck
```

Expected: PASS.

### Step 4: Complete the merge commit

```powershell
Set-Location ..
git add -A
git commit
```

### Step 5: Prove the previous conflict set no longer blocks future merging

Use `git merge-tree` or a throwaway merge against the current `origin/main`; confirm no unresolved paths remain. Record the command and output for the final report.

---

## Task 6: Deduplicate scheduled sync conflict incidents

**Files:**

- Modify: `.github/workflows/sync-upstream-custom-pack.yml`
- Create/modify: workflow test or script fixture if the repository has a workflow-test convention

### Step 1: Define testable conflict signatures

Implement the signature as the newline-joined, sorted set of unresolved paths. Exercise at least these cases using a local script step or extracted shell helper:

- identical paths in a different order produce the same signature;
- a newer upstream SHA with the same paths remains the same incident;
- adding/removing a path produces a new incident;
- no conflict closes the open incident.

### Step 2: Update permissions and cadence

- add `issues: write`;
- reduce the cron schedule to hourly;
- retain existing branch/content/package permissions required by the workflow.

### Step 3: Implement one-open-issue incident tracking

Use GitHub CLI/API to maintain one open issue labeled `sync-conflict`:

- first unseen path signature: create or replace the incident body and fail once;
- repeated identical signature: update latest upstream SHA/workflow run details, write an Actions summary, and exit successfully;
- changed signature: update the issue as a new incident and fail once;
- successful merge: close the issue with merged SHA/tag information;
- never include tokens or secrets;
- never auto-resolve source conflicts.

Persist a machine-readable signature marker in the issue body so comparisons do not rely on prose formatting.

### Step 4: Validate workflow syntax and behavior

Run available repository workflow validation. If no dedicated validator exists, parse the YAML and execute extracted signature logic against fixtures. Review all `if:` expressions and exit codes manually.

### Step 5: Commit workflow stabilization

```powershell
git add .github/workflows/sync-upstream-custom-pack.yml
git commit -m "ci: deduplicate upstream sync conflicts"
```

---

## Task 7: Full local verification and branch review

### Step 1: Backend targeted and broad checks

```powershell
Set-Location backend
go test -tags=unit ./internal/config ./internal/repository ./internal/service ./internal/handler/admin -count=1
go test ./cmd/server -run '^$'
go test ./...
```

The final command may be slow on Windows; capture whether it passes, fails, or exceeds the bounded local execution window. Do not describe a timeout as a pass.

### Step 2: Frontend checks

```powershell
Set-Location ../frontend
pnpm exec vitest run src/api/admin/__tests__/custom-image-update.spec.ts
pnpm run lint:check
pnpm run typecheck
pnpm run build
```

Also run the repository's critical Vitest target from the repository root.

### Step 3: Inspect the complete change

```powershell
Set-Location ..
git status --short
git diff --check
git log --oneline --decorate origin/custom..HEAD
git diff --stat origin/custom...HEAD
git diff origin/custom...HEAD -- . ':!frontend/pnpm-lock.yaml'
```

Confirm:

- no edits came from the user's original dirty workspace;
- no secret values appear in source, tests, workflow summaries, or issue bodies;
- upstream system update files contain no custom image contract extensions;
- custom endpoints cannot accept arbitrary image inputs;
- generated Wire output matches providers;
- the working tree is clean.

### Step 4: Obtain a review pass

Apply the repository's code-review checklist to the complete diff, address any findings, and re-run affected checks.

---

## Task 8: Publish the repaired `custom` candidate and release

This task changes remote state and is authorized by the user's approval, but only after Task 7 is green.

### Step 1: Determine the next unused custom tag

Read `backend/cmd/server/VERSION`, list existing tags matching `v<base>-xd.*`, and select the smallest unused next `N`. Do not overwrite an existing tag.

### Step 2: Push the candidate branch to `custom`

Push with an explicit source and lease protection based on the fetched remote SHA:

```powershell
git push --force-with-lease=refs/heads/custom:<recorded-origin-custom-sha> origin HEAD:custom
```

Use a normal non-force push if it is a fast-forward. Never use an unqualified force push.

### Step 3: Monitor authoritative GitHub checks

Wait for backend Linux tests, frontend checks, security scanning, and any required branch workflows. If a check fails, diagnose and fix on the candidate branch before tagging.

### Step 4: Create and push the release tag

After required checks pass:

```powershell
git tag -a v<base>-xd.<N> -m "v<base>-xd.<N>"
git push origin v<base>-xd.<N>
```

### Step 5: Monitor release and image publication

Verify:

- release workflow succeeds;
- `ghcr.io/xiao-dan-1/sub2api:v<base>-xd.<N>` exists;
- configured latest alias resolves to the expected digest;
- custom update check reports the published target as ready;
- the next scheduled upstream sync does not recreate the old conflict failure;
- any pre-existing `sync-conflict` issue is closed after a successful merge.

If external deployment access is unavailable, report the exact remaining verification for the user rather than claiming it completed.

---

## Completion criteria

- Upstream binary update behavior and signatures are restored.
- Custom Watchtower/GHCR behavior is isolated behind separate backend and frontend modules.
- Fork-specific tests no longer modify the five upstream conflict hot spots except `VERSION` during an actual upstream merge.
- The current upstream state merges without unresolved source conflicts.
- Identical conflict incidents no longer fail every hourly run.
- Local focused tests, frontend checks, and generated DI pass.
- GitHub Linux CI passes before the next `v*-xd.*` tag is published.
- The release image and alias are verified, and the user receives the exact tag and workflow outcomes.
