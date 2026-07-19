# Resolve Sync Upstream Custom Pack Conflict

## Context

The scheduled `Sync Upstream Custom Pack` workflow fails while merging the latest upstream `main` into the fork's `custom` branch. Checkout, remote fetch, and upstream mirroring all succeed. The failure is isolated to the merge step.

The failing merge combines:

- fork `custom`: `8f44064d1ce98b0f3ee3fd772c8ccacf038b4b33`
- upstream `main`: `d4b9797ff72024960a035cf22fdd8f213e149169`
- merge base: `b1a6b8026764dedf6a5f76d02bdc292468d22753`

Git reports conflicts in:

- `backend/cmd/server/VERSION`
- `backend/internal/config/config_test.go`

The workflow intentionally auto-resolves only a sole `VERSION` conflict. Because `config_test.go` also conflicts, it correctly stops instead of publishing an unverified merge.

## Goal

Restore the upstream synchronization and custom release pipeline without discarding either upstream security coverage or fork-specific update behavior.

Success means:

1. The latest upstream `main` is merged into remote `custom`.
2. Both independent configuration test additions remain present.
3. The merged backend passes targeted and repository CI-equivalent verification.
4. A correctly numbered custom tag is published and its image/release workflow completes.
5. The existing fail-safe for unknown future source conflicts remains intact.

## Considered Approaches

### Disable or pause the scheduled workflow

This would stop repeated notifications but leave `custom` behind upstream and prevent new custom releases. It does not address the cause and is rejected.

### Make the workflow automatically accept arbitrary source conflicts

This could suppress future failures but risks silently choosing incorrect code and publishing broken or insecure artifacts. General source conflicts require semantic review, so this approach is rejected.

### Resolve this merge precisely and preserve the workflow guard

This is the selected approach. It fixes the current branch state while retaining the workflow's protection against unsafe automatic merges.

## Merge Design

Work is performed in an isolated worktree based on the exact remote `custom` SHA so existing local changes are not included.

The upstream SHA is merged with an explicit merge commit. Conflict resolution is:

- `backend/cmd/server/VERSION`: take the upstream base version, expected to be `0.1.161`. The custom suffix belongs to the release tag and the later release synchronization commit, not the upstream merge result.
- `backend/internal/config/config_test.go`: retain both `TestLoadCustomUpdateConfigFromEnv` from the fork and `TestLoadHTTPIngressSafetyDefaults` from upstream. They validate independent behavior and can coexist as separate test functions.

No unrelated refactoring or workflow behavior change is included.

## Verification Design

Before pushing:

1. Confirm there are no conflict markers or unmerged index entries.
2. Run `gofmt` verification on the resolved Go test file.
3. Run `go test ./internal/config -count=1`.
4. Run the full backend test suite with sufficient time to finish.
5. Run the repository's relevant CI-equivalent checks where locally available.
6. Review the merge diff against both parents to ensure it contains the upstream changes plus only the intended conflict resolution.

The pre-merge `internal/config` baseline already passes. The initial full backend baseline exceeded a 120-second local command limit without reporting a test failure; the final run will use a longer limit and its actual exit status will be recorded.

## Push and Release Safety

Immediately before pushing, query the remote `custom` SHA again. If it differs from the expected `8f44064d1ce98b0f3ee3fd772c8ccacf038b4b33`, stop and integrate the new remote state rather than overwriting it.

Push the verified branch to `refs/heads/custom` with a normal fast-forward push. Never force-push.

After the branch push, create the next available `v0.1.161-xd.N` annotated tag on the verified merge commit. Query remote tags first and choose the same next-number rule used by the workflow. Pushing the tag triggers `Auto Build Missing Custom Tags` through its `create` event.

Monitor the synchronization and custom build/release workflows. If a new failure appears, use its failing step and logs as new evidence; do not bypass checks or publish a replacement tag blindly.

## Rollback and Failure Handling

- If verification fails, do not push the branch or tag.
- If remote `custom` moves, stop before pushing and redo the merge from the new remote head.
- If the branch push succeeds but tag creation fails, leave the valid merge in place and retry only tag creation after confirming no tag collision.
- If the tag exists but the build fails, diagnose and rerun the existing tag build workflow rather than rewriting or force-moving the tag.

## Out of Scope

- Automatically resolving arbitrary future source conflicts.
- Disabling the scheduled synchronization workflow.
- Refactoring unrelated backend or frontend code.
- Changing release numbering or image publication conventions.
