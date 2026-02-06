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
  $1=="semantic-release:" {inside=1; next}
  inside && /^[^[:space:]]/ {inside=0}
  inside {print}
' "$workflow")

if ! grep -q "if: github.event_name == 'workflow_dispatch'" <<<"$semantic_block"; then
  echo "Expected semantic-release gated by workflow_dispatch" >&2
  exit 1
fi

release_block=$(awk '
  $1=="release:" {inside=1; next}
  inside && /^[^[:space:]]/ {inside=0}
  inside {print}
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
