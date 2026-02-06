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
