# Apple Container Backend Design

**Date:** 2026-02-06

## Goal
Enable `ggo studio create` to use Apple Container on macOS 26+ as a first-choice fallback when no Docker socket is available, while preserving existing Docker/Colima/OrbStack flows and providing clear install/upgrade guidance.

## Key Behaviors
- Replace the `apple` mode with `apple-container` across CLI flags, help text, and internal mode constants.
- On macOS 26+:
  - If no Docker socket is found, prefer Apple Container for auto mode.
  - If `--mode apple-container` is specified but Apple Container is missing, prompt install.
  - If no Docker socket is found and Apple Container is missing, prompt install.
- On macOS < 26:
  - If `--mode apple-container` is specified, error with OS upgrade requirement.
  - If no runtime is available, recommend installing Colima (plus other options) and note Apple Container needs macOS 26.

## Backend Strategy
- Implement Apple Container backend using the `container` CLI (not Docker).
- Detect availability with `container system status` and presence of the `container` CLI.
- Implement list/get/start/stop/exec/logs/delete via `container` subcommands.
- Parse JSON from `container list --format json` / `container inspect` to build `Environment` objects.

## Runtime Detection
- Add helper for macOS major version detection using `runtime` + `internal/platform` helper.
- Add Docker socket discovery for common paths:
  - `DOCKER_HOST` (unix socket)
  - `/var/run/docker.sock`
  - `~/.colima/*/docker.sock`
  - `~/.orbstack/run/docker.sock`
- Use socket presence to decide preference order on macOS 26+.

## User-Facing Messaging
- Update CLI help strings and examples to include `apple-container`.
- Provide install guidance for:
  - Apple Container: download signed pkg from GitHub releases.
  - Colima/OrbStack: `brew install` suggestions.

## Testing
- Add ginkgo tests covering backend selection rules and macOS version gating.
- Run `container` CLI end-to-end tests on a common arm64 image (e.g., `alpine:latest`), ensuring create/list/exec/logs/stop/delete flows work.

