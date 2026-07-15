# Release Pipeline Parallelization Design

## Goal

Reduce the custom release workflow wall-clock duration from about 10 minutes 53 seconds to roughly 6.5-7.5 minutes without removing CI, security, exact-tag checkout, or release publication safeguards.

## Current Bottleneck

The release critical path is currently serialized as follows:

1. Validate the requested tag.
2. Run reusable CI and security workflows.
3. Run unit tests and integration tests sequentially inside one CI job.
4. Build the frontend only after every verification job succeeds.
5. Run GoReleaser and synchronize the branch VERSION file.

Measured timings from `v0.1.156-xd.7`:

- Unit tests: 4 minutes 20 seconds.
- Integration tests: 2 minutes 11 seconds.
- golangci-lint: 2 minutes 35 seconds.
- Frontend build job: 1 minute 41 seconds.
- Release job: 1 minute 59 seconds.

The unit and integration steps account for about 6.5 minutes because they share one job and therefore cannot overlap. The frontend build adds another 1 minute 41 seconds after verification even though it only reads the immutable release tag.

## Proposed Workflow

After tag validation, start these operations in parallel:

- Unit tests.
- Integration tests.
- Shell checks.
- Frontend checks.
- golangci-lint.
- Backend and frontend security scans.
- Frontend production build and artifact upload.
- VERSION artifact generation.

The release publication job will depend on all verification jobs and both build artifacts. Publication therefore remains blocked unless every existing gate succeeds.

The reusable backend CI workflow will replace its combined `test` job with separate `unit-test` and `integration-test` jobs. Each job checks out and configures Go independently, accepting a small increase in total runner minutes in exchange for a shorter critical path.

The QEMU setup step will run only for non-simple releases. The simple custom release targets native `linux/amd64`, so it does not require CPU emulation.

## Failure Behavior

- A failed unit, integration, lint, frontend, shell, or security job prevents release publication.
- A failed frontend build or VERSION artifact job also prevents publication.
- Frontend and VERSION artifacts may be produced before a verification failure is known. This can waste about two runner minutes on a failed run, but does not publish an image.
- Existing release concurrency, exact-tag checkouts, GHCR authentication, digest handling, and VERSION anti-regression behavior remain unchanged.

## Scope

### Included

- Split unit and integration tests into parallel jobs in `.github/workflows/backend-ci.yml`.
- Start `build-frontend` and `update-version` after `validate-tag` in `.github/workflows/release.yml`.
- Make the `release` job explicitly depend on verification and artifact jobs.
- Skip QEMU setup for simple amd64-only releases.

### Excluded

- Changing test implementation, timers, retries, or `t.Parallel()` usage.
- Skipping or weakening any release gate.
- Docker BuildKit cache configuration.
- Build-once/promote-by-digest architecture.
- Larger or self-hosted runners.

## Verification

1. Run `actionlint` across all workflows.
2. Confirm the backend CI workflow exposes separate unit and integration jobs.
3. Confirm release publication lists verification, frontend artifact, and VERSION artifact jobs in `needs`.
4. Push the workflow change and verify branch CI runs unit and integration jobs independently.
5. Measure the next real custom release and compare its critical path with the 10 minute 53 second baseline.

## Success Criteria

- All existing verification and security checks remain required.
- Branch CI passes with separate unit and integration jobs.
- No workflow syntax or expression errors are reported by actionlint.
- The next successful custom release completes in no more than 7.5 minutes under comparable GitHub runner conditions, excluding external queue delays.
