# Release Pipeline Parallelization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce custom release wall-clock time by parallelizing independent CI and artifact jobs while preserving every release gate.

**Architecture:** The reusable backend CI workflow will expose independent unit and integration jobs. The release workflow will build immutable-tag artifacts in parallel with CI/security, then require all verification and artifact jobs before publication. Native amd64 simple releases will skip QEMU.

**Tech Stack:** GitHub Actions reusable workflows, YAML, actionlint, GitHub Actions REST API.

---

### Task 1: Split Backend Tests Into Parallel Jobs

**Files:**
- Modify: `.github/workflows/backend-ci.yml`

- [ ] **Step 1: Verify the structural assertion fails before editing**

Run:

```powershell
$yaml = Get-Content -Raw '.github/workflows/backend-ci.yml'
if ($yaml -match '(?m)^  unit-test:' -and $yaml -match '(?m)^  integration-test:') { exit 0 }
exit 1
```

Expected: exit code `1` because the workflow still has one combined `test` job.

- [ ] **Step 2: Replace the combined test job**

Replace `jobs.test` with two jobs that each check out the requested ref, configure Go 1.26.5 with the existing cache settings, verify the Go version, and run exactly one command:

```yaml
  unit-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with:
          ref: ${{ inputs.ref || github.ref }}
      - uses: actions/setup-go@v6
        with:
          go-version-file: backend/go.mod
          check-latest: false
          cache: true
          cache-dependency-path: backend/go.sum
      - name: Verify Go version
        run: |
          go version | grep -q 'go1.26.5'
      - name: Unit tests
        working-directory: backend
        run: make test-unit

  integration-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with:
          ref: ${{ inputs.ref || github.ref }}
      - uses: actions/setup-go@v6
        with:
          go-version-file: backend/go.mod
          check-latest: false
          cache: true
          cache-dependency-path: backend/go.sum
      - name: Verify Go version
        run: |
          go version | grep -q 'go1.26.5'
      - name: Integration tests
        working-directory: backend
        run: make test-integration
```

- [ ] **Step 3: Verify the structural assertion passes**

Run the Step 1 command again.

Expected: exit code `0`.

### Task 2: Parallelize Release Artifact Jobs

**Files:**
- Modify: `.github/workflows/release.yml`

- [ ] **Step 1: Verify the desired dependency structure is absent**

Run:

```powershell
$yaml = Get-Content -Raw '.github/workflows/release.yml'
$ok = $yaml -match '(?ms)^  update-version:\s+needs: validate-tag' -and
      $yaml -match '(?ms)^  build-frontend:\s+needs: validate-tag' -and
      $yaml -match 'needs: \[verify-ci, verify-security, update-version, build-frontend\]'
if ($ok) { exit 0 }
exit 1
```

Expected: exit code `1` because artifact jobs still wait for verification and release does not list all gates directly.

- [ ] **Step 2: Rewire artifact and publication dependencies**

Apply these exact dependency changes:

```yaml
  update-version:
    needs: validate-tag

  build-frontend:
    needs: validate-tag

  release:
    needs: [verify-ci, verify-security, update-version, build-frontend]
```

This starts artifact production after tag validation while keeping publication blocked on all gates.

- [ ] **Step 3: Skip QEMU for simple native releases**

Add this condition to the existing QEMU step:

```yaml
      - name: Set up QEMU
        if: ${{ env.SIMPLE_RELEASE != 'true' }}
        uses: docker/setup-qemu-action@v3
```

- [ ] **Step 4: Verify the dependency assertion passes**

Run the Step 1 command again.

Expected: exit code `0`.

### Task 3: Validate and Publish the Workflow Change

**Files:**
- Verify: `.github/workflows/backend-ci.yml`
- Verify: `.github/workflows/release.yml`

- [ ] **Step 1: Run actionlint**

Run:

```powershell
docker run --rm -v "${PWD}:/repo" -w /repo rhysd/actionlint:latest
```

Expected: exit code `0` with no findings.

- [ ] **Step 2: Check the staged diff**

Run:

```powershell
git diff --check
git diff -- .github/workflows/backend-ci.yml .github/workflows/release.yml
```

Expected: no whitespace errors; only the approved job split, dependency changes, and QEMU condition appear.

- [ ] **Step 3: Commit the implementation**

Run:

```powershell
git add .github/workflows/backend-ci.yml .github/workflows/release.yml
git commit -m "ci: parallelize custom release verification"
```

- [ ] **Step 4: Push custom**

Run:

```powershell
git push origin custom
```

- [ ] **Step 5: Verify branch CI structure on GitHub**

Use the GitHub Actions REST API to find the pushed `CI` run and confirm separate `unit-test` and `integration-test` jobs start independently. Both jobs, frontend, shell, and golangci-lint must complete successfully.

- [ ] **Step 6: Record expected release measurement**

Do not create another synthetic release tag solely for timing. Measure the next real custom release against the `10m53s` baseline and require a comparable run to complete within `7m30s`, excluding external queue delay.
