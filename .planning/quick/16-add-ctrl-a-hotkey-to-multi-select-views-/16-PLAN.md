---
phase: 16-add-ctrl-a-hotkey-to-multi-select-views-
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - frontend/src/utils/selection-controller.ts
  - frontend/src/services/keyboard-shortcut-service.ts
  - frontend/src/components/track-list/track-list.ts
  - frontend/src/components/queue-panel/queue-panel.ts
  - frontend/src/components/playlist-view/playlist-view.ts
autonomous: true
requirements: [QUICK-16]

must_haves:
  truths:
    - "Pressing Ctrl+A in the track list selects all visible tracks"
    - "Pressing Ctrl+A in the queue panel selects all queue items"
    - "Pressing Ctrl+A in the playlist view selects all tracks in the expanded playlist"
    - "Ctrl+A does NOT fire when a text input is focused"
    - "Selection count updates immediately after Ctrl+A"
  artifacts:
    - path: "frontend/src/utils/selection-controller.ts"
      provides: "selectAll() method on SelectionController"
      contains: "selectAll"
    - path: "frontend/src/services/keyboard-shortcut-service.ts"
      provides: "app.selectAll dispatches custom event instead of document.execCommand"
      contains: "shortcut:select-all"
  key_links:
    - from: "frontend/src/services/keyboard-shortcut-service.ts"
      to: "track-list, queue-panel, playlist-view"
      via: "CustomEvent('shortcut:select-all') on document"
      pattern: "shortcut:select-all"
---

<objective>
Wire Ctrl+A to select all items in multi-select views (track-list, queue-panel, playlist-view).

Purpose: Currently `app.selectAll` calls `document.execCommand('selectAll')` which is the browser's text selection — useless for the app's track/queue selection. This needs to trigger the SelectionController's select-all in whichever panel is focused.

Output: Ctrl+A selects all items in the active multi-select view using the existing shortcut infrastructure and SelectionController.
</objective>

<execution_context>
@/home/caleb/.config/Claude/get-shit-done/workflows/execute-plan.md
@/home/caleb/.config/Claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@frontend/src/utils/selection-controller.ts
@frontend/src/services/keyboard-shortcut-service.ts
@frontend/src/components/track-list/track-list.ts
@frontend/src/components/queue-panel/queue-panel.ts
@frontend/src/components/playlist-view/playlist-view.ts
@backend/shortcuts/config.go

<interfaces>
<!-- SelectionController already has getItemKey/getItemCount via the SelectionHost interface.
     Components implement these to provide the item list. selectAll() will iterate all items
     using these existing methods. -->

From frontend/src/utils/selection-controller.ts:
```typescript
export interface SelectionHost extends ReactiveControllerHost {
    getItemKey(index: number): string | undefined;
    getItemCount(): number;
    onSelectionChanged?(): void;
}

export class SelectionController implements ReactiveController {
    get selectedItems(): ReadonlySet<string>;
    get hasSelection(): boolean;
    get selectionCount(): number;
    isSelected(key: string): boolean;
    handleItemClick(e: MouseEvent, key: string, index: number): void;
    handleContextMenu(key: string): void;
    clear(): void;
    getSelectedKeysOrdered(): string[];
    getSelectedIndices(): number[];
}
```

From frontend/src/services/keyboard-shortcut-service.ts (dispatch function):
```typescript
// Existing pattern for panel-specific shortcuts:
case 'tracklist.play':
    document.dispatchEvent(new CustomEvent('shortcut:tracklist-play'));
    break;
case 'tracklist.delete':
    document.dispatchEvent(new CustomEvent('shortcut:tracklist-delete'));
    break;
```

From backend/shortcuts/config.go (existing binding):
```go
"app.selectAll": "Ctrl+A",  // Already bound — just need to change the dispatch action
```
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Add selectAll() to SelectionController and change app.selectAll dispatch</name>
  <files>
    frontend/src/utils/selection-controller.ts
    frontend/src/services/keyboard-shortcut-service.ts
  </files>
  <action>
    **SelectionController** (`frontend/src/utils/selection-controller.ts`):
    Add a `selectAll()` method in the ACTIONS section (after `clear()`). It should:
    1. Get the total count via `this.host.getItemCount()`
    2. Build a new Set of all item keys by iterating `0..count-1` calling `this.host.getItemKey(i)`
    3. Skip any `undefined` keys (same pattern as `getSelectedKeysOrdered`)
    4. If the resulting set has the same size as `_selectedItems`, return early (already all selected)
    5. Set `this._selectedItems = newSet`
    6. Set `this.lastSelectedIndex = count - 1` (anchor at end for subsequent shift-click)
    7. Call `this.host.requestUpdate()` and `this.host.onSelectionChanged?.()`

    ```typescript
    /** Select all items. */
    selectAll(): void {
        const count = this.host.getItemCount();
        const next = new Set<string>();

        for (let i = 0; i < count; i++) {
            const key = this.host.getItemKey(i);
            if (key !== undefined) next.add(key);
        }

        if (next.size === this._selectedItems.size) return;

        this._selectedItems = next;
        this.lastSelectedIndex = count > 0 ? count - 1 : null;
        this.host.requestUpdate();
        this.host.onSelectionChanged?.();
    }
    ```

    **keyboard-shortcut-service.ts** (`frontend/src/services/keyboard-shortcut-service.ts`):
    In the `dispatch()` function, change the `app.selectAll` case from:
    ```typescript
    case 'app.selectAll':
        document.execCommand('selectAll');
        break;
    ```
    To:
    ```typescript
    case 'app.selectAll':
        document.dispatchEvent(
            new CustomEvent('shortcut:select-all'),
        );
        break;
    ```

    This follows the exact same pattern used by `tracklist.play` and `tracklist.delete`.
  </action>
  <verify>
    <automated>cd frontend && npx tsc --noEmit 2>&1 | head -30</automated>
  </verify>
  <done>SelectionController has a selectAll() method, and Ctrl+A dispatches a 'shortcut:select-all' CustomEvent instead of document.execCommand('selectAll')</done>
</task>

<task type="auto">
  <name>Task 2: Wire select-all event listener in track-list, queue-panel, and playlist-view</name>
  <files>
    frontend/src/components/track-list/track-list.ts
    frontend/src/components/queue-panel/queue-panel.ts
    frontend/src/components/playlist-view/playlist-view.ts
  </files>
  <action>
    Add a `shortcut:select-all` event listener to each of the three components that use SelectionController. Use the same lifecycle pattern — add in `connectedCallback`, remove in `disconnectedCallback` with a bound handler.

    **For each component (track-list, queue-panel, playlist-view):**

    1. Add a bound handler method that calls `this.selection.selectAll()`:
       ```typescript
       private handleSelectAll = (): void => {
           this.selection.selectAll();
       };
       ```

    2. In `connectedCallback()` (create one if it doesn't exist, calling `super.connectedCallback()`):
       ```typescript
       document.addEventListener('shortcut:select-all', this.handleSelectAll);
       ```

    3. In `disconnectedCallback()` (create one if it doesn't exist, calling `super.disconnectedCallback()`):
       ```typescript
       document.removeEventListener('shortcut:select-all', this.handleSelectAll);
       ```

    **IMPORTANT consideration for playlist-view**: The playlist-view has a dual-mode UI (playlist list vs expanded playlist tracks). `getItemCount()` and `getItemKey()` already handle this — when a playlist is expanded, they return the tracks for that playlist; when no playlist is expanded, they return playlist entries. So `selectAll()` will naturally select all items in whatever mode is active.

    **IMPORTANT consideration for multiple listeners**: All three components may be connected simultaneously (track-list in the main panel, queue-panel as a sidebar, playlist-view as a panel). This is correct behavior — `selectAll()` on a component that has 0 items (e.g., queue-panel when it's closed or has no items) is harmless since `getItemCount()` returns 0 and the early-return guard in `selectAll()` triggers (0 === 0). The user's focused panel receives the visual feedback. This matches how `tracklist.play` and `tracklist.delete` already broadcast to all listeners.

    **Check if connectedCallback/disconnectedCallback already exist** in each component before adding. If they exist, add the addEventListener/removeEventListener lines to the existing methods. If they don't exist, create them.
  </action>
  <verify>
    <automated>cd frontend && npx tsc --noEmit 2>&1 | head -30 && echo "--- lint ---" && cd .. && make lint 2>&1 | tail -20</automated>
  </verify>
  <done>All three multi-select components (track-list, queue-panel, playlist-view) listen for 'shortcut:select-all' and call selection.selectAll(). Pressing Ctrl+A selects all items in all connected views.</done>
</task>

</tasks>

<verification>
1. `cd frontend && npx tsc --noEmit` — TypeScript compiles without errors
2. `make lint` — passes golangci-lint and any frontend linting
3. `make build` or `wails build` — full build succeeds
4. Manual: Open app → navigate to track list → press Ctrl+A → all tracks highlight as selected
5. Manual: Open queue panel with items → press Ctrl+A → all queue items highlight as selected
6. Manual: Open playlist view, expand a playlist → press Ctrl+A → all playlist tracks highlight as selected
7. Manual: Focus a text input (search bar) → press Ctrl+A → text is selected (browser default), NOT track selection
</verification>

<success_criteria>
- Ctrl+A selects all items in track-list, queue-panel, and playlist-view
- Selection count badge/indicator updates to show total count
- Text input focus is not affected (shortcut service's text-input scope suppression handles this)
- No TypeScript compilation errors
- Linting passes
</success_criteria>

<output>
After completion, create `.planning/quick/16-add-ctrl-a-hotkey-to-multi-select-views-/16-SUMMARY.md`
</output>
