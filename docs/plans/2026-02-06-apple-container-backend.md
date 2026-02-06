# Apple Container Backend Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Apple Container (`container` CLI) support for `ggo studio create` on macOS 26+, with correct auto-selection, install/upgrade messaging, and updated CLI/docs/UI strings.

**Architecture:** Introduce platform helpers for macOS version and Docker socket detection, then update backend selection to prefer Apple Container only when no Docker socket is present on macOS 26+. Rebuild the Apple backend around the `container` CLI with JSON parsing for list/inspect, and ensure install/runtime hints are precise.

**Tech Stack:** Go, Cobra, Ginkgo/Gomega, Apple `container` CLI.

---

### Task 1: Add Ginkgo suite + failing tests for macOS selection rules

**Files:**
- Create: `internal/studio/studio_suite_test.go`
- Create: `internal/studio/manager_apple_ginkgo_test.go`

**Step 1: Write the failing test**
```go
var _ = Describe("Apple container selection", func() {
    It("prefers apple-container on macOS 26+ when no Docker socket is present", func() {
        // stub platform helpers to darwin/26/no-socket
        // register apple + docker backends (both available)
        // expect ModeAppleContainer for ModeAuto
    })

    It("prefers docker/colima when Docker socket exists on macOS 26+", func() {
        // stub platform helpers to darwin/26/has-socket
        // register apple + docker backends (both available)
        // expect ModeDocker for ModeAuto
    })

    It("rejects apple-container on macOS < 26 when explicitly requested", func() {
        // stub platform helpers to darwin/25
        // expect error mentioning macOS 26 upgrade
    })
})
```

**Step 2: Run test to verify it fails**
Run: `go test ./internal/studio -run Apple`
Expected: FAIL with missing helper stubs / selection logic not implemented

**Step 3: Write minimal implementation**
Implement platform helper stubs and selection logic hooks (function vars) to satisfy the tests.

**Step 4: Run test to verify it passes**
Run: `go test ./internal/studio -run Apple`
Expected: PASS

**Step 5: Commit**
```bash
git add internal/studio/studio_suite_test.go internal/studio/manager_apple_ginkgo_test.go
GIT_OPTIONAL_LOCKS=0 git -c core.hooksPath=/dev/null commit -m "test: add apple-container selection tests"
```

### Task 2: Implement macOS version + Docker socket helpers and update Manager selection/hints

**Files:**
- Create: `internal/platform/macos_version_darwin.go`
- Create: `internal/platform/macos_version_other.go`
- Create: `internal/platform/docker_socket.go`
- Modify: `internal/studio/manager.go`

**Step 1: Write the failing test**
Extend existing Ginkgo tests (Task 1) to assert explicit error messaging for unsupported macOS version and missing Apple Container install guidance.

**Step 2: Run test to verify it fails**
Run: `go test ./internal/studio -run Apple`
Expected: FAIL with error text mismatch or missing helpers

**Step 3: Write minimal implementation**
- Add `platform.MacOSMajorVersion()` using `syscall.Sysctl("kern.osproductversion")` on darwin; return `0` elsewhere.
- Add `platform.HasDockerSocket()` to scan `DOCKER_HOST`, `/var/run/docker.sock`, `~/.colima/*/docker.sock`, `~/.orbstack/run/docker.sock`.
- Update `Manager.detectBestBackend` to:
  - On darwin 26+: prefer apple-container only when no Docker socket is found.
  - On darwin < 26: exclude apple-container from auto preference list.
- Update `platformBackendHint` to include install guidance:
  - Apple Container: download pkg from GitHub releases.
  - Colima/OrbStack: `brew install`.
  - Docker: link to Docker Desktop.
- When `--mode apple-container` is requested on macOS < 26, return a clear upgrade error.

**Step 4: Run test to verify it passes**
Run: `go test ./internal/studio -run Apple`
Expected: PASS

**Step 5: Commit**
```bash
git add internal/platform/macos_version_darwin.go internal/platform/macos_version_other.go internal/platform/docker_socket.go internal/studio/manager.go
GIT_OPTIONAL_LOCKS=0 git -c core.hooksPath=/dev/null commit -m "feat: add macOS version/socket helpers and selection rules"
```

### Task 3: Rebuild Apple backend to use `container` CLI + parsing helpers

**Files:**
- Modify: `internal/studio/backend_apple.go`
- Create: `internal/studio/apple_container_parse_test.go`

**Step 1: Write the failing test**
```go
var _ = Describe("Apple container parsing", func() {
    It("maps container list JSON to Environment and SSH port", func() {
        // Provide sample JSON from `container list --format json`
        // Expect label filtering, SSH port extraction, GPU_WORKER_URL parsing
    })
})
```

**Step 2: Run test to verify it fails**
Run: `go test ./internal/studio -run Apple`
Expected: FAIL (parsing helpers not implemented)

**Step 3: Write minimal implementation**
- Replace docker CLI usage with `container` CLI subcommands:
  - `container system status` for availability
  - `container system start` in `EnsureRunning`
  - `container run --detach` for create
  - `container list --format json` for list
  - `container inspect` for get
  - `container exec`, `container logs`, `container start`, `container stop`, `container delete --force`
- Add JSON parsing helpers for list/inspect output.
- Normalize memory suffixes for `container` CLI (`Gi`->`G`, `Mi`->`M`).
- Use labels `ggo.managed=true`, `ggo.name`, `ggo.mode=apple-container`.

**Step 4: Run test to verify it passes**
Run: `go test ./internal/studio -run Apple`
Expected: PASS

**Step 5: Commit**
```bash
git add internal/studio/backend_apple.go internal/studio/apple_container_parse_test.go
GIT_OPTIONAL_LOCKS=0 git -c core.hooksPath=/dev/null commit -m "feat: implement apple-container backend via container CLI"
```

### Task 4: Update CLI/Docs/UI strings for `apple-container`

**Files:**
- Modify: `internal/studio/types.go`
- Modify: `cmd/ggo/studio/studio.go`
- Modify: `docs/studio-guide.md`
- Modify: `vscode-extension/src/views/createStudioPanel.ts`

**Step 1: Write the failing test**
(Documentation/UI change; no automated test required)

**Step 2: Run test to verify it fails**
Skip

**Step 3: Write minimal implementation**
- Replace `apple` mode string with `apple-container`.
- Update CLI help, examples, and backend listing output to include install guidance.
- Update Studio guide table and VS Code UI mode labels.

**Step 4: Run test to verify it passes**
Run: `go test ./cmd/ggo/studio ./internal/studio`
Expected: PASS

**Step 5: Commit**
```bash
git add internal/studio/types.go cmd/ggo/studio/studio.go docs/studio-guide.md vscode-extension/src/views/createStudioPanel.ts
GIT_OPTIONAL_LOCKS=0 git -c core.hooksPath=/dev/null commit -m "docs: update apple-container mode strings"
```

### Task 5: Full functional Apple Container CLI test (manual)

**Files:**
- None (manual verification)

**Step 1: Run container services**
Run: `container system start`
Expected: services start successfully

**Step 2: Run a common arm64 image**
Run: `container run --name ggo-apple-test --detach --rm -p 18022:22/tcp alpine:latest sleep 600`
Expected: container ID printed, `container list` shows it running

**Step 3: Exec/logs/stop/delete**
Run:
- `container exec ggo-apple-test sh -c "echo ok"`
- `container logs ggo-apple-test`
- `container stop ggo-apple-test`
Expected: commands succeed

**Step 4: Record results**
Note any failures or deviations.

---

**Global verification after each Go change:**
- Run: `golangci-lint run --fix`
- Run: `go test ./...` (at least once before finalization)

