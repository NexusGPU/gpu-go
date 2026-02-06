# VSCode Guest Mode + Share Links Design

**Date:** 2026-02-06

## Goal
Add guest mode and share link workflows to the VSCode extension so users can connect quickly with a share link, while keeping full management features behind PAT login. Improve the Worker Detail view with share link selection and actions. Keep UI copy simple and user-centric.

## Summary of Decisions
- Guest mode is persisted (global state). No PAT required.
- In guest mode: only Studio view is visible; vGPU Workers and Machine Hosts are hidden.
- Onboarding prompt offers "Continue as Guest" or "Login with PAT".
- Worker Detail panel shows top 10 share links for that vGPU worker, sorted newest-first.
- Selecting a share shows Connection URL (IP:port), vendor, short code/link, created time, and actions.
- "Use remote vGPU" opens a VS Code terminal and auto-runs `ggo use <code> -y`.
- "Create Studio" opens the Create Studio flow with share link prefilled.
- User-facing terms: vGPU worker, Machine Host, Machine Agent.

## Architecture
- CLI remains the source of truth. Extension uses `ggo share list -o json`.
- Share list is grouped by `worker_id` in the extension. Sorting by `created_at` DESC; top 10 per worker.
- Guest mode stored in `ExtensionContext.globalState` (e.g., `gpugo.guestMode`).
- Views visibility controlled by context keys (e.g., `gpugo.guestMode`, `gpugo.loggedIn`).

## Data Flow
1. Extension activation
   - Check auth status via CLI.
   - If not logged in and not guest, show onboarding prompt.
   - Set context keys and refresh views.
2. Worker Detail view
   - Fetch worker details via CLI.
   - Fetch shares via `shareList()` and filter by worker ID.
   - Sort by `createdAt` DESC, select first by default.
   - Render select list and detail card.
3. Share actions
   - Use remote vGPU: open terminal, run `ggo use <shortCode> -y`.
   - Create Studio: open Create Studio panel with share link prefilled.
4. Guest mode
   - Studio view adds "Connect with share link" action.
   - Prompt for share link/code, open terminal and run `ggo use <code> -y`.

## UI / Copy
- Views:
  - Workers -> "vGPU Workers"
  - Devices -> "Machine Hosts"
- Worker Detail labels:
  - "vGPU worker ID", "Machine Agent ID", "Listen Port", "vGPU IDs"
- Share section:
  - Helper text: "Share links let others connect to this vGPU worker."
  - Detail card fields: Connection URL (IP:port), Vendor, Short Link, Created
- Buttons:
  - "Use remote vGPU"
  - "Create Studio from this share"
- Guest onboarding:
  - Message: "Use a share link to connect instantly, or sign in to manage vGPU workers."
  - Actions: "Continue as Guest" (default), "Login with PAT"
- Tooltips:
  - "Share link: a one-time or reusable link that connects to a vGPU worker."
  - "Use remote vGPU opens a terminal and runs ggo use for you."

## Error Handling
- No shares: show "No share links yet" with hint to use `ggo worker share` in CLI.
- Missing fields: show "Not available" and disable actions if needed.
- CLI missing or command failure: show toast and keep terminal open.
- macOS: show message that remote vGPU use is not supported on macOS before attempting.

## Testing Plan (high value)
- Unit: share grouping + sorting + top-10 per worker (pure function).
- Unit: guest mode state transitions (guest -> login, login -> logout -> guest).
- UI: worker detail renders share select and updates detail card on change.
- Manual: guest onboarding, hide/show views, terminal auto-run behavior.

## Open Questions (none)
All requirements resolved with current decisions.
