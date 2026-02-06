# Manual Release Flow Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Gate tagging and R2 uploads to manual workflow_dispatch runs while keeping build/lint/test on all branches, and add a gh CLI trigger script.

**Architecture:** Add lightweight bash tests to assert the build workflow’s triggers and release gating. Refactor `.github/workflows/build.yml` to remove branch filters and to run semantic-release/release only on workflow_dispatch. Add a CLI helper script that wraps `gh workflow run` for manual dispatch.

**Tech Stack:** GitHub Actions YAML, Bash, gh CLI

---

### Task 1: Assert build workflow gating and refactor triggers

**Files:**
- Create: `scripts/build-workflow.test.sh`
- Modify: `.github/workflows/build.yml`

**Step 1: Write the failing test**

```bash
#!/usr/bin/env bash
set -euo pipefail

workflow=".github/workflows/build.yml"

if [[ ! -f "$workflow" ]]; then
  echo "Missing $workflow" >&2
  exit 1
fi

if grep -qE '^[[:space:]]+branches:' "$workflow"; then
  echo "Expected no branch filters in build workflow" >&2
  exit 1
fi

semantic_block=$(awk '
  $1=="semantic-release:" {in=1; next}
  in && /^[^[:space:]]/ {in=0}
  in {print}
' "$workflow")

if ! grep -q "if: github.event_name == 'workflow_dispatch'" <<<"$semantic_block"; then
  echo "Expected semantic-release gated by workflow_dispatch" >&2
  exit 1
fi

release_block=$(awk '
  $1=="release:" {in=1; next}
  in && /^[^[:space:]]/ {in=0}
  in {print}
' "$workflow")

if ! grep -q "needs.semantic-release.outputs.published == 'true'" <<<"$release_block"; then
  echo "Expected release job to depend on semantic-release published output" >&2
  exit 1
fi

if ! grep -q "github.event_name == 'workflow_dispatch'" <<<"$release_block"; then
  echo "Expected release job gated by workflow_dispatch" >&2
  exit 1
fi

echo "OK"
```

**Step 2: Run test to verify it fails**

Run: `bash scripts/build-workflow.test.sh`

Expected: FAIL because branch filters exist and semantic-release is not gated by workflow_dispatch.

**Step 3: Write minimal implementation**

Update `.github/workflows/build.yml`:
- Remove `branches:` filters under `push` and `pull_request`.
- Set `semantic-release` job to `if: github.event_name == 'workflow_dispatch'`.
- Gate `release` job with `if: needs.semantic-release.outputs.published == 'true' && github.event_name == 'workflow_dispatch'`.

**Step 4: Run test to verify it passes**

Run: `bash scripts/build-workflow.test.sh`

Expected: PASS

**Step 5: Commit**

```bash
git add scripts/build-workflow.test.sh .github/workflows/build.yml
git commit -m "ci: gate releases behind manual workflow_dispatch"
```

---

### Task 2: Add gh CLI trigger script (TDD)

**Files:**
- Create: `scripts/trigger-release.test.sh`
- Create: `scripts/trigger-release.sh`

**Step 1: Write the failing test**

```bash
#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="$root/scripts/trigger-release.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

stub="$tmp/gh"
cat > "$stub" <<'STUB'
#!/usr/bin/env bash
echo "$@" > "$CAPTURE_FILE"
STUB
chmod +x "$stub"

export GH_BIN="$stub"

CAPTURE_FILE="$tmp/args1" "$script" --ref feature/test --workflow build.yml
expected="workflow run build.yml --ref feature/test"
got="$(cat "$tmp/args1")"
if [[ "$got" != "$expected" ]]; then
  echo "expected: $expected" >&2
  echo "got: $got" >&2
  exit 1
fi

CAPTURE_FILE="$tmp/args2" "$script" --workflow build.yml
expected="workflow run build.yml --ref main"
got="$(cat "$tmp/args2")"
if [[ "$got" != "$expected" ]]; then
  echo "expected: $expected" >&2
  echo "got: $got" >&2
  exit 1
fi

echo "OK"
```

**Step 2: Run test to verify it fails**

Run: `bash scripts/trigger-release.test.sh`

Expected: FAIL because `scripts/trigger-release.sh` does not exist.

**Step 3: Write minimal implementation**

Create `scripts/trigger-release.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/trigger-release.sh [--ref <git-ref>] [--workflow <workflow-file>]

Triggers the manual release workflow via gh CLI.

Options:
  --ref, -r        Git ref to run the workflow on (default: main)
  --workflow, -w   Workflow file name (default: build.yml)
  --help, -h       Show help
USAGE
}

ref="main"
workflow="build.yml"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -r|--ref)
      ref="$2"
      shift 2
      ;;
    -w|--workflow)
      workflow="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

gh_bin="${GH_BIN:-gh}"
if command -v "$gh_bin" >/dev/null 2>&1; then
  gh_cmd="$gh_bin"
elif [[ -x "$gh_bin" ]]; then
  gh_cmd="$gh_bin"
else
  echo "gh CLI not found. Install from https://cli.github.com" >&2
  exit 1
fi

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || true)
if [[ -z "$repo_root" ]]; then
  echo "Not inside a git repository." >&2
  exit 1
fi

cd "$repo_root"
"$gh_cmd" workflow run "$workflow" --ref "$ref"

echo "Triggered $workflow on ref $ref"
```

Make it executable: `chmod +x scripts/trigger-release.sh`

**Step 4: Run test to verify it passes**

Run: `bash scripts/trigger-release.test.sh`

Expected: PASS

**Step 5: Commit**

```bash
git add scripts/trigger-release.test.sh scripts/trigger-release.sh
git commit -m "scripts: add gh workflow dispatch helper"
```
