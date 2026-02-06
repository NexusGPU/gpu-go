# VSCode Guest Mode + Share Links Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add guest mode and share-link workflows to the VSCode extension with a share-aware Worker Detail view and simple onboarding.

**Architecture:** Keep logic lightweight in the extension. Use CLI (`ggo share list -o json`, `ggo use`) as source of truth. Persist guest mode in global state, gate view visibility via context keys, and update UI copy to use vGPU/Machine terminology.

**Tech Stack:** TypeScript (VSCode extension), VSCode Webviews, Mocha tests, ggo CLI.

---

### Task 1: Add share grouping helpers + tests (TDD)

**Files:**
- Create: `vscode-extension/src/utils/shareUtils.ts`
- Create: `vscode-extension/src/test/suite/shareUtils.test.ts`

**Step 1: Write the failing test**

```ts
import * as assert from 'assert';
import { groupSharesByWorker } from '../../utils/shareUtils';

suite('shareUtils', () => {
    test('groups shares by worker and keeps top 10 newest first', () => {
        const shares = Array.from({ length: 12 }).map((_, i) => ({
            shareId: `s${i}`,
            shortCode: `c${i}`,
            shortLink: `https://go.gpu.tf/s/c${i}`,
            workerId: 'w1',
            hardwareVendor: 'nvidia',
            connectionUrl: `https://1.2.3.4:${9000 + i}`,
            usedCount: 0,
            createdAt: new Date(2026, 0, i + 1).toISOString()
        }));

        const grouped = groupSharesByWorker(shares, 10);
        assert.strictEqual(grouped.get('w1')?.length, 10);
        assert.strictEqual(grouped.get('w1')?.[0].shortCode, 'c11');
    });
});
```

**Step 2: Run test to verify it fails**

Run: `cd vscode-extension && npm run compile && npm test`
Expected: FAIL with “Cannot find module '../../utils/shareUtils'”

**Step 3: Write minimal implementation**

```ts
import { Share } from '../cli/cli';

export function groupSharesByWorker(shares: Share[], limit: number): Map<string, Share[]> {
    const grouped = new Map<string, Share[]>();
    for (const share of shares) {
        const list = grouped.get(share.workerId) ?? [];
        list.push(share);
        grouped.set(share.workerId, list);
    }
    for (const [workerId, list] of grouped.entries()) {
        list.sort((a, b) => new Date(b.createdAt || 0).getTime() - new Date(a.createdAt || 0).getTime());
        grouped.set(workerId, list.slice(0, limit));
    }
    return grouped;
}
```

**Step 4: Run test to verify it passes**

Run: `cd vscode-extension && npm run compile && npm test`
Expected: PASS

**Step 5: Commit**

```bash
git add vscode-extension/src/utils/shareUtils.ts vscode-extension/src/test/suite/shareUtils.test.ts
git commit -m "test: add share grouping helper"
```

---

### Task 2: Guest mode state + onboarding flow (TDD)

**Files:**
- Modify: `vscode-extension/src/auth/authManager.ts`
- Modify: `vscode-extension/src/extension.ts`
- Modify: `vscode-extension/package.json`
- Create: `vscode-extension/src/utils/authState.ts`
- Create: `vscode-extension/src/test/suite/authState.test.ts`

**Step 1: Write the failing test**

```ts
import * as assert from 'assert';
import { resolveAuthMode } from '../../utils/authState';

suite('authState', () => {
    test('guest mode wins when not logged in', () => {
        const mode = resolveAuthMode({ loggedIn: false, guestMode: true });
        assert.strictEqual(mode, 'guest');
    });

    test('login wins over guest', () => {
        const mode = resolveAuthMode({ loggedIn: true, guestMode: true });
        assert.strictEqual(mode, 'full');
    });
});
```

**Step 2: Run test to verify it fails**

Run: `cd vscode-extension && npm run compile && npm test`
Expected: FAIL with “Cannot find module '../../utils/authState'”

**Step 3: Write minimal implementation**

```ts
export type AuthMode = 'guest' | 'full' | 'none';

export function resolveAuthMode(input: { loggedIn: boolean; guestMode: boolean }): AuthMode {
    if (input.loggedIn) {
        return 'full';
    }
    if (input.guestMode) {
        return 'guest';
    }
    return 'none';
}
```

**Step 4: Implement guest mode persistence + onboarding**

- Add `guestMode` state to `AuthManager` (load/save via `ExtensionContext.globalState`).
- Add `setGuestMode` method (updates global state, fires auth change).
- Update `login()` to clear guest mode on success.
- Update `logout()` to set guest mode true (and clear PAT).
- Update `checkAndPromptLogin()` to show onboarding when `resolveAuthMode(...) === 'none'`.
- Add “Continue as Guest” action to onboarding; call `authManager.setGuestMode(true)`.
- Set VSCode context keys (`gpugo.guestMode`, `gpugo.loggedIn`) after auth changes.

**Step 5: Run tests**

Run: `cd vscode-extension && npm run compile && npm test`
Expected: PASS

**Step 6: Commit**

```bash
git add vscode-extension/src/utils/authState.ts vscode-extension/src/test/suite/authState.test.ts vscode-extension/src/auth/authManager.ts vscode-extension/src/extension.ts vscode-extension/package.json
git commit -m "feat: add guest mode state and onboarding"
```

---

### Task 3: View visibility + terminology updates

**Files:**
- Modify: `vscode-extension/package.json`
- Modify: `vscode-extension/src/providers/workersTreeProvider.ts`
- Modify: `vscode-extension/src/providers/devicesTreeProvider.ts`
- Modify: `vscode-extension/src/views/workerDetailPanel.ts`
- Modify: `vscode-extension/src/views/createWorkerPanel.ts`

**Step 1: Update view names and visibility**
- Rename view titles to “vGPU Workers” and “Machine Hosts”.
- Add `when` clauses to hide these views when `gpugo.guestMode == true`.

**Step 2: Update user-facing labels**
- Replace “Worker/Agent/Devices” terms with “vGPU worker / Machine Agent / Machine Host”.
- Update tooltips and helper text where shown.

**Step 3: Manual check**
- Run extension and verify labels are updated.

**Step 4: Commit**

```bash
git add vscode-extension/package.json vscode-extension/src/providers/workersTreeProvider.ts vscode-extension/src/providers/devicesTreeProvider.ts vscode-extension/src/views/workerDetailPanel.ts vscode-extension/src/views/createWorkerPanel.ts
git commit -m "chore: update views and terminology"
```

---

### Task 4: Share list in Worker Detail (UI + actions)

**Files:**
- Modify: `vscode-extension/src/views/workerDetailPanel.ts`
- Modify: `vscode-extension/src/extension.ts`
- Modify: `vscode-extension/src/cli/cli.ts`
- Modify: `vscode-extension/src/views/createStudioPanel.ts`

**Step 1: Add share fetch and grouping**
- In `WorkerDetailPanel.update`, call `cli.shareList()` and group by worker ID.
- Select first share by default; pass to webview as JSON.

**Step 2: Update webview UI**
- Add single-select list of share codes.
- On change, update detail card (connection URL, vendor, short link, created).
- Add buttons:
  - “Use remote vGPU” -> postMessage command `useShare` with short code.
  - “Create Studio from this share” -> postMessage command `createStudio` with share link.

**Step 3: Handle messages in extension**
- Register commands to open terminal and auto-run `ggo use <code> -y` (guard macOS).
- Add a command to open Create Studio with prefilled share link.

**Step 4: Update Create Studio input**
- Rename field label to “Share link or code”.
- Wire Create Studio panel to accept prefilled share link from Worker Detail.
- Map share link to CLI flag `-s` (current `gpuUrl` option).

**Step 5: Manual test**
- Open Worker Detail; verify top 10 shares and actions work.
- Verify terminal auto-runs `ggo use`.
- Verify Create Studio uses the share link.

**Step 6: Commit**

```bash
git add vscode-extension/src/views/workerDetailPanel.ts vscode-extension/src/extension.ts vscode-extension/src/cli/cli.ts vscode-extension/src/views/createStudioPanel.ts
git commit -m "feat: add share links to worker detail"
```

---

### Task 5: Guest-only Studio action (connect with share link)

**Files:**
- Modify: `vscode-extension/src/providers/studioTreeProvider.ts`
- Modify: `vscode-extension/src/extension.ts`
- Modify: `vscode-extension/package.json`

**Step 1: Add command**
- Add `gpugo.connectWithShareLink` command.
- Prompt for share link/code, open terminal, run `ggo use <code> -y`.
- Guard macOS with a clear message.

**Step 2: Add Studio action**
- Add a top-level action item in Studio view when in guest mode.
- Tooltip: “Paste a share link or short code to connect.”

**Step 3: Manual test**
- In guest mode, Studio view shows the action and opens terminal with `ggo use`.

**Step 4: Commit**

```bash
git add vscode-extension/src/providers/studioTreeProvider.ts vscode-extension/src/extension.ts vscode-extension/package.json
git commit -m "feat: add guest share connect action"
```

---

### Task 6: Verify & document

**Files:**
- Modify: `vscode-extension/README.md`
- Modify: `vscode-extension/CHANGELOG.md`

**Step 1: Update docs**
- Add a short “Guest Mode” section.
- Document share link flow and terminology updates.

**Step 2: Run tests**

Run: `cd vscode-extension && npm run compile && npm test`
Expected: PASS

**Step 3: Commit**

```bash
git add vscode-extension/README.md vscode-extension/CHANGELOG.md
git commit -m "docs: add guest mode and share links"
```
