# Resolve Sync Upstream Custom Pack Conflict Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge upstream `main` into remote `custom`, preserve both independent configuration test additions, publish the next custom tag, and verify the release chain.

**Architecture:** Work from the exact remote `custom` SHA in an isolated worktree. Perform one explicit merge with narrow semantic conflict resolution, keep the workflow fail-safe unchanged, verify locally, then push normally and publish the next tag.

**Tech Stack:** Git 2.51, Go, GitHub Actions, GitHub REST API, PowerShell.

---

### Task 1: Reconfirm remote inputs and release slot

**Files:** Read only: live GitHub refs and workflow files.

- [ ] **Step 1: Query live remote heads**

```powershell
$h=@{'User-Agent'='Codex'}
$custom=(Invoke-RestMethod -Headers $h -Uri 'https://api.github.com/repos/xiao-dan-1/sub2api/branches/custom').commit.sha
$upstream=(Invoke-RestMethod -Headers $h -Uri 'https://api.github.com/repos/Wei-Shaw/sub2api/branches/main').commit.sha
"custom=$custom"; "upstream=$upstream"
```

Expected for this incident: `custom=8f44064d1ce98b0f3ee3fd772c8ccacf038b4b33` and `upstream=d4b9797ff72024960a035cf22fdd8f213e149169`. If either differs, recompute the merge from the new heads.

- [ ] **Step 2: Confirm the next tag slot**

```powershell
git ls-remote --tags https://github.com/xiao-dan-1/sub2api.git 'refs/tags/v0.1.161-xd.*'
```

Expected: no matching refs, making `v0.1.161-xd.1` the first candidate. If a tag exists, use the next numeric suffix and never overwrite it.

### Task 2: Reproduce the failing merge (RED)

**Files:** Read only: Git object database.

- [ ] **Step 1: Run the known failing merge-tree check**

```powershell
git merge-tree --write-tree --name-only --messages 8f44064d1ce98b0f3ee3fd772c8ccacf038b4b33 d4b9797ff72024960a035cf22fdd8f213e149169
"MERGE_TREE_EXIT=$LASTEXITCODE"
```

Expected: exit code `1` with conflicts in `backend/cmd/server/VERSION` and `backend/internal/config/config_test.go`.

### Task 3: Perform the minimal semantic merge

**Files:** Modify `backend/cmd/server/VERSION` and `backend/internal/config/config_test.go`.

- [ ] **Step 1: Start the explicit merge**

```powershell
git merge --no-edit d4b9797ff72024960a035cf22fdd8f213e149169
```

Expected: the two conflicts from Task 2 and an uncommitted merge state.

- [ ] **Step 2: Resolve the version from upstream**

```powershell
git checkout --theirs -- backend/cmd/server/VERSION
git add -- backend/cmd/server/VERSION
```

Expected content: exactly `0.1.161` plus a newline.

- [ ] **Step 3: Keep both independent config tests**

In `backend/internal/config/config_test.go`, after `TestLoadServerTimingConfig`, retain both complete functions: the fork's `TestLoadCustomUpdateConfigFromEnv` (testing `UPDATE_CUSTOM_REPO`, `UPDATE_CUSTOM_IMAGE`, `UPDATE_WATCHTOWER_URL`, and `UPDATE_WATCHTOWER_TOKEN`) and upstream's `TestLoadHTTPIngressSafetyDefaults` (testing the default read-header timeout, max header bytes, text body size, and invalid-auth abuse defaults). Remove all conflict markers, then run:

```powershell
gofmt -w backend/internal/config/config_test.go
git add -- backend/internal/config/config_test.go
git diff --check
```

Expected: exit code `0` and no whitespace errors.

- [ ] **Step 4: Verify and commit the merge**

```powershell
git ls-files -u
Select-String -Path backend/internal/config/config_test.go -Pattern '<<<<<<<|=======|>>>>>>>'
```

Expected: both commands produce no output. Then commit:

```powershell
git commit -m "merge: sync upstream v0.1.161 into custom"
```

### Task 4: Run GREEN verification

**Files:** Read only: merged sources and test output.

- [ ] **Step 1: Check intended content and clean status**

```powershell
Select-String -Path backend/internal/config/config_test.go -Pattern 'TestLoadCustomUpdateConfigFromEnv','TestLoadHTTPIngressSafetyDefaults'
Get-Content backend/cmd/server/VERSION
git status --short --branch
```

Expected: both tests appear, version is `0.1.161`, and status is clean.

- [ ] **Step 2: Run the focused test**

```powershell
Set-Location backend
go test ./internal/config -count=1
```

Expected: exit code `0` and an `ok` result for `github.com/Wei-Shaw/sub2api/internal/config`.

- [ ] **Step 3: Run the full backend suite**

```powershell
go test ./...
```

Run with a 10-minute timeout. Expected: exit code `0` and no package failures. A timeout is inconclusive, not success.

- [ ] **Step 4: Audit the merge**

```powershell
git diff --check HEAD^1 HEAD
git diff --check HEAD^2 HEAD
git show --stat --oneline --decorate HEAD
```

Expected: no whitespace errors and only the upstream changes plus the intended conflict resolution and approved documentation.

### Task 5: Push and tag safely

**Files:** Remote refs only: `origin/custom` and `v0.1.161-xd.N`.

- [ ] **Step 1: Recheck remote custom**

Query the GitHub branch API again. Continue only if it is still `8f44064d1ce98b0f3ee3fd772c8ccacf038b4b33`; otherwise redo Task 3 from the new head.

- [ ] **Step 2: Push normally**

```powershell
git push origin HEAD:refs/heads/custom
```

Expected: a non-force fast-forward update to the verified merge commit.

- [ ] **Step 3: Publish the next annotated tag**

```powershell
git tag -a v0.1.161-xd.1 -m "Custom release v0.1.161-xd.1"
git push origin refs/tags/v0.1.161-xd.1
```

Substitute the next unused suffix found in Task 1. Never move or overwrite an existing tag.

### Task 6: Monitor Actions

**Files:** Read only: GitHub Actions run and job APIs.

- [ ] **Step 1: Confirm custom builder**

Poll `https://api.github.com/repos/xiao-dan-1/sub2api/actions/workflows/auto-build-custom-tags.yml/runs?per_page=5` and confirm a run for the new tag reaches `completed` with `success`.

- [ ] **Step 2: Confirm sync recovery**

Poll `https://api.github.com/repos/xiao-dan-1/sub2api/actions/workflows/sync-upstream-custom-pack.yml/runs?per_page=5` after the next schedule. Confirm a subsequent run reaches `completed` with `success` and does not create a duplicate tag.

- [ ] **Step 3: Preserve evidence on any new failure**

For a failure, set `$runId` to the failed run ID and query `/repos/xiao-dan-1/sub2api/actions/runs/$runId/jobs?per_page=100`, record the failed step and logs, and stop before any force-push, tag deletion, or blind rerun.
