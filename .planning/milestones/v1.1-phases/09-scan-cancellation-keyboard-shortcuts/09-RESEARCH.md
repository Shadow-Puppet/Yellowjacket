# Phase 9: Scan Cancellation & Keyboard Shortcuts - Research

**Researched:** 2026-03-06
**Domain:** Go context cancellation, frontend keyboard event management, Lit web component architecture
**Confidence:** HIGH

## Summary

This phase adds two independent feature sets to YellowJacket: scan control (cancel/pause/resume) on the Go backend with frontend buttons, and a full keyboard shortcut system on the Lit frontend with configurable bindings persisted via the existing TOML config.

**Scan cancellation** requires threading a cancellable `context.Context` through the existing scan pipeline. The current `Scan()` method already checks `l.ctx.Done()` in several `select` blocks within the directory walker and worker pool. The implementation adds a dedicated `scanCancel context.CancelFunc` field on `Library`, Pause/Resume via a sync-based mechanism (channel or mutex), and new Wails-bound methods (`CancelScan`, `PauseScan`, `ResumeScan`). The cancel confirmation dialog ("Keep X tracks found so far, or discard?") is a frontend concern — the backend simply stops and reports partial results vs rolls back.

**Keyboard shortcuts** are a pure frontend feature. No external libraries are needed — the browser's `KeyboardEvent` API is sufficient for a Wails desktop app. A central `KeyboardShortcutService` singleton listens on `document.keydown`, resolves the active scope (Global, Panel-specific, Text Input), looks up the action, and dispatches it. Bindings are stored in the Go config (new `Shortcuts` TOML section) and exposed via Wails bindings. The settings UI adds a "Keyboard Shortcuts" tab to the existing `config-page` component with record-style key capture.

**Primary recommendation:** Implement scan cancellation via `context.WithCancel` + a pause channel on the backend, and keyboard shortcuts as a frontend-only `KeyboardShortcutService` with Go config persistence. Both are zero-dependency — no new libraries needed on either side.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- Hybrid style: Space/arrows for player controls (no modifier), Ctrl+key for app actions
- Up/Down arrows adjust volume, Left/Right seek within track
- Both `/` and `Ctrl+F` focus the search box
- `Q` toggles the queue panel
- `S` for shuffle, `R` for repeat (single-key player controls)
- `Ctrl+A` for select-all in any multi-select context (track lists, etc.)
- All bindings are configurable — the above are defaults
- Claude fills in remaining defaults (mute, etc.) using common media player conventions
- Record-style key capture: click a shortcut row, press the new key combo, it captures live
- Conflicts show a warning with the conflicting action — user chooses to overwrite (old becomes unbound) or cancel
- Shortcuts grouped by category (Player, Navigation, App) in the settings view
- "Reset to defaults" button resets all shortcuts; individual per-shortcut reset also available
- Lives as a "Keyboard Shortcuts" tab within the existing settings dialog
- Three scopes: Global (always active), Panel-specific (when a panel has focus), Text Input (shortcuts suppressed)
- Global scope: player controls (Space, arrows, S, R, Q, etc.) fire regardless of which panel is focused
- Panel-specific scope: track list gets Enter-to-play and Delete-to-remove when focused
- Text Input scope: only Escape works (blurs the text input) — all other shortcuts suppressed
- No visual scope indicator — relies on natural browser focus behavior; users learn through use
- Pause and Cancel buttons placed next to the existing status label, above the existing progress bar in the scanner UI
- On cancel: prompt the user — "Keep X tracks found so far, or discard?" — gives user control over partial results
- On resume after pause: skip already-processed files and continue with remaining — no duplicate work
- Scan control is buttons-only — no keyboard shortcuts for cancel/pause (scans are infrequent)

### Claude's Discretion
- Remaining default key assignments not explicitly discussed (mute, volume step size, etc.)
- Scan progress detail level and error handling during scan
- Loading/disabled states for scan control buttons
- Visual design of the shortcut settings UI (spacing, grouping headers, etc.)
- How the cancel confirmation dialog looks and behaves

### Deferred Ideas (OUT OF SCOPE)
None — discussion stayed within phase scope
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| SCAN-01 | User can cancel an in-progress library scan via a cancel button | Go context cancellation pattern; new `CancelScan()` Wails binding; frontend cancel button in config-page scan section |
| SCAN-02 | Cancelled scan stops gracefully without corrupting the database | Batch-transactional writes already atomic; cancel skips orphan cleanup (STATE.md warning); partial results either kept or discarded per user choice |
| SCAN-03 | User can pause a library scan and resume it without re-scanning processed files | Pause channel blocks worker pool goroutines; resume unblocks; existingPaths sync.Map already tracks processed files |
| KEY-01 | Default keybindings work out of box | Frontend `KeyboardShortcutService` with hardcoded default map; Go config stores overrides |
| KEY-02 | User can customize all keyboard shortcuts via a visual settings UI | "Keyboard Shortcuts" tab in config-page; record-style key capture component; Wails config bindings for persistence |
| KEY-03 | Shortcut conflicts are detected and warned about when rebinding | Frontend conflict detection during key capture — compare against all bindings in same scope |
| KEY-04 | Shortcuts are scoped — different bindings apply based on focused component | Three-scope system (Global, Panel, TextInput); scope resolved by checking `document.activeElement` shadow DOM chain |
| KEY-05 | Shortcuts are disabled when text input has focus (except Escape to blur) | TextInput scope check: if active element is `<input>`, `<textarea>`, or `contenteditable`, suppress all except Escape |
</phase_requirements>

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go `context` | stdlib | Scan cancellation via `context.WithCancel` | Standard Go cancellation pattern; already used in scan pipeline |
| `sync` | stdlib | Pause/resume via channel or conditional variable | No external dependency needed for goroutine coordination |
| Browser `KeyboardEvent` API | Web standard | Key capture, modifier detection, key identification | Native API, no library needed for desktop Wails app |
| Lit 3.x | 3.2.1 (existing) | Shortcut settings UI components | Already the project's component framework |
| BurntSushi/toml | existing | Config persistence for shortcut bindings | Already the project's config format |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `golang.org/x/sync/errgroup` | existing | Worker pool with context-aware cancellation | Already used in scan worker pool |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Custom key manager | `hotkeys-js` or `tinykeys` | Unnecessary dependency for a Wails app — no global OS hotkeys needed, browser events suffice |
| TOML config for shortcuts | JSON file or SQLite | TOML is the existing config format — consistency wins |
| sync.Cond for pause | Channel-based pause | Channels are simpler and more idiomatic in Go; sync.Cond is error-prone |

## Architecture Patterns

### Recommended Project Structure
```
backend/
├── library/
│   ├── library.go          # Add scanCancel, scanPaused fields; modify Scan()
│   ├── scan_control.go     # New: CancelScan(), PauseScan(), ResumeScan() methods
│   └── metrics.go          # Add Cancelled bool field to ScanMetrics
├── config/
│   └── config.go           # Add Shortcuts *shortcuts.Config section
├── shortcuts/              # New package
│   ├── config.go           # ShortcutConfig struct, defaults, validation
│   └── config_test.go      # Unit tests for config validation
└── events/
    └── events.go           # Add ScanCancelled, ScanPaused, ScanResumed events

frontend/src/
├── services/
│   └── keyboard-shortcut-service.ts  # New: singleton, keydown listener, scope resolution, action dispatch
├── store/
│   └── shortcuts-store.ts            # New: persisted shortcut bindings from config
├── components/
│   └── config-page/
│       ├── config-page.ts            # Add "Keyboard Shortcuts" tab
│       └── shortcut-capture.ts       # New: record-style key capture widget
```

### Pattern 1: Context Cancellation for Scan
**What:** Use `context.WithCancel` to create a per-scan context that propagates cancellation to all goroutines.
**When to use:** Every call to `Scan()` creates a child context from `l.ctx`.

```go
// In library.go — Scan() method modification
func (l *Library) Scan() (*ScanMetrics, error) {
    // Create cancellable context for this scan
    scanCtx, cancel := context.WithCancel(l.ctx)
    
    l.mu.Lock()
    l.scanCancel = cancel
    l.scanActive = true
    l.mu.Unlock()
    
    defer func() {
        l.mu.Lock()
        l.scanCancel = nil
        l.scanActive = false
        l.mu.Unlock()
    }()
    
    // Pass scanCtx instead of l.ctx to all operations
    // Workers check scanCtx.Done() for cancellation
    // ...
}
```

### Pattern 2: Channel-Based Pause/Resume
**What:** Use a channel that workers check before processing each file. When paused, the channel blocks; when resumed, it's replaced with a closed channel (always readable).
**When to use:** Pause/resume scan control.

```go
type Library struct {
    // ...
    scanPauseCh chan struct{} // nil = not paused, non-nil closed = running, non-nil open = paused
}

// Workers call this before processing each file:
func (l *Library) waitIfPaused(ctx context.Context) error {
    l.mu.Lock()
    ch := l.scanPauseCh
    l.mu.Unlock()
    
    if ch == nil {
        return nil
    }
    
    select {
    case <-ch:      // channel closed = unpaused, proceed
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

### Pattern 3: Frontend Keyboard Shortcut Service
**What:** A singleton service that listens on `document.keydown`, resolves scope, looks up binding, and dispatches action.
**When to use:** The service is created once at app startup and never destroyed.

```typescript
// keyboard-shortcut-service.ts
class KeyboardShortcutService {
    private bindings: Map<string, ShortcutBinding>;
    
    constructor() {
        document.addEventListener('keydown', this.handleKeydown);
    }
    
    private handleKeydown = (e: KeyboardEvent) => {
        // 1. Check if text input focused — suppress all except Escape
        if (this.isTextInputFocused()) {
            if (e.key === 'Escape') {
                (document.activeElement as HTMLElement)?.blur();
                e.preventDefault();
            }
            return;
        }
        
        // 2. Build key string: "Ctrl+Shift+K" format
        const keyStr = this.buildKeyString(e);
        
        // 3. Check panel-specific bindings first, then global
        const scope = this.resolveScope();
        const action = this.findAction(keyStr, scope);
        
        if (action) {
            e.preventDefault();
            this.dispatch(action);
        }
    };
    
    private isTextInputFocused(): boolean {
        const el = this.getDeepActiveElement();
        if (!el) return false;
        
        const tag = el.tagName.toLowerCase();
        if (tag === 'input' || tag === 'textarea') return true;
        if ((el as HTMLElement).isContentEditable) return true;
        
        return false;
    }
    
    // Shadow DOM aware active element resolution
    private getDeepActiveElement(): Element | null {
        let el = document.activeElement;
        while (el?.shadowRoot?.activeElement) {
            el = el.shadowRoot.activeElement;
        }
        return el;
    }
}
```

### Pattern 4: Config Extension for Shortcuts
**What:** Add a `Shortcuts` section to the existing TOML config following the same pattern as Theme, TrackList, Favorites.
**When to use:** Persisting user-customized keyboard shortcuts.

```go
// backend/shortcuts/config.go
type Config struct {
    Bindings map[string]string `toml:"Bindings"` // action -> key combo
}

func (c *Config) ApplyDefaults() {
    if c.Bindings == nil {
        c.Bindings = DefaultBindings()
    }
}

// backend/config/config.go — add to Config struct
type Config struct {
    // ... existing fields
    Shortcuts *shortcuts.Config `toml:"Shortcuts"`
}
```

### Anti-Patterns to Avoid
- **Anti-pattern: Global mutable state for pause:** Don't use a global variable. Keep pause state on the Library struct, protected by the existing mutex.
- **Anti-pattern: Keyboard listeners on individual components:** Don't add `keydown` handlers to every component. Use a single document-level listener that delegates based on scope.
- **Anti-pattern: Storing shortcuts in localStorage:** Don't bypass the Go config system. All persistent config flows through the TOML config file via Wails bindings, consistent with existing patterns (theme, tracklist columns, favorites).
- **Anti-pattern: Using `e.keyCode` or `e.which`:** Use `e.key` and `e.code` — they're the modern standard and handle international keyboards correctly.
- **Anti-pattern: Cancelling scan inside a transaction:** The batch commit is already atomic. Cancellation should happen between batches, not mid-transaction.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Key event normalization | Custom key string builder from scratch | `e.key` + modifier booleans (`e.ctrlKey`, `e.shiftKey`, etc.) | The browser API is sufficient; `e.key` returns the logical key value |
| Context cancellation | Custom goroutine signaling | `context.WithCancel` | Standard Go pattern, already partially in use in the scan pipeline |
| Goroutine pause | Manual sync.Mutex lock/unlock cycling | Channel-based blocking | Channels compose naturally with `select` and context cancellation |

**Key insight:** Both features (scan control and keyboard shortcuts) are well-served by standard library/platform capabilities. No external dependencies are needed.

## Common Pitfalls

### Pitfall 1: Orphan Cleanup After Cancelled Scan
**What goes wrong:** The scan's orphan cleanup phase (Phase 5) iterates `existingPaths` and deletes DB entries for files not found on disk. If a scan is cancelled mid-way, `existingPaths` still contains files that weren't visited yet — they'd be incorrectly deleted as "orphans."
**Why it happens:** The scan loads all existing files into `existingPaths` at the start, then removes entries as they're found during the walk. A cancelled walk leaves legitimate files in the map.
**How to avoid:** Skip orphan cleanup entirely when the scan is cancelled. This is already called out as a warning in STATE.md: "Scan cancellation: skip orphan cleanup on cancelled scans."
**Warning signs:** Tracks disappearing from the library after cancelling a scan.

### Pitfall 2: Shadow DOM Active Element Detection
**What goes wrong:** `document.activeElement` returns the host element of a shadow root, not the actual focused element inside. Shortcut suppression during text input would fail because the check sees `<search-bar>` not `<input>`.
**Why it happens:** Lit components use Shadow DOM. The focused `<input>` inside `<search-bar>` shadow root isn't directly visible to `document.activeElement`.
**How to avoid:** Walk the `shadowRoot.activeElement` chain recursively until reaching the leaf focused element (shown in Pattern 3 above).
**Warning signs:** Keyboard shortcuts firing while typing in the search box.

### Pitfall 3: Race Between Cancel and Batch Commit
**What goes wrong:** Calling `CancelScan()` while a batch transaction is in progress could leave the database in an inconsistent state if the context is cancelled during `tx.Commit()`.
**Why it happens:** SQLite `Commit()` with modernc.org/sqlite checks context cancellation.
**How to avoid:** The scan context should be checked between batches, not during a commit. Use a separate check: after each `flushBatch()` call, check if `scanCtx` is done before processing more results. The batch commit itself should use the parent `l.ctx` (not the scan-specific cancellable context) so in-flight transactions always complete.
**Warning signs:** "database is locked" errors or partial batch commits.

### Pitfall 4: Key Combo String Normalization
**What goes wrong:** Different representations of the same key combo: "ctrl+f" vs "Ctrl+F" vs "Control+f" — lookups fail.
**Why it happens:** No consistent normalization of key strings.
**How to avoid:** Define a canonical format: modifiers in fixed order (Ctrl+Alt+Shift+Meta) + lowercase key name. Always normalize both when storing and when matching.
**Warning signs:** Shortcuts not firing after reassignment, or duplicate entries in settings.

### Pitfall 5: Space Key Conflicts with Scrollable Areas
**What goes wrong:** Space is the default browser scroll-down key. If Space is bound to play/pause globally, scrollable panels may stop scrolling.
**Why it happens:** `e.preventDefault()` on Space prevents the browser's native scroll behavior.
**How to avoid:** The scope system handles this — when a scrollable panel has focus and the user intends to scroll, the panel-specific scope should not have Space bound. The Global scope's Space binding calls `preventDefault()` which is acceptable since this is a desktop app (not a web page), and the primary use of Space is play/pause.
**Warning signs:** Users unable to scroll with keyboard in track lists.

### Pitfall 6: Partial Results Handling on Cancel
**What goes wrong:** When user cancels and chooses "discard," the backend has already committed batches to the database. Rolling back multiple committed transactions is complex.
**Why it happens:** Scan writes in batches of 50 that are committed as they go.
**How to avoid:** "Discard" means "delete the tracks added during this scan." Track which audio file IDs were added during the current scan (via the `added` counter mechanism — extend to track IDs). On discard, delete those specific records. Alternatively, simpler: "discard" triggers a FullRescan minus the cancel-interrupted data. Given complexity, the simpler approach is: "Keep" is the default, "Discard" just clears the entire library (same as FullRescan clear phase) since partial state is unreliable.
**Warning signs:** Stale or duplicate entries after cancel-and-discard.

## Code Examples

### Scan Control — Backend Methods

```go
// scan_control.go

// CancelScan cancels an in-progress scan. Returns immediately;
// the scan goroutines will stop at their next check point.
func (l *Library) CancelScan() {
    l.mu.Lock()
    defer l.mu.Unlock()
    
    if l.scanCancel != nil {
        l.scanCancel()
    }
}

// PauseScan pauses an in-progress scan. Workers block at their
// next pause checkpoint until ResumeScan is called.
func (l *Library) PauseScan() {
    l.mu.Lock()
    defer l.mu.Unlock()
    
    if !l.scanActive || l.scanPaused {
        return
    }
    
    l.scanPaused = true
    l.scanPauseCh = make(chan struct{})
    
    runtime.EventsEmit(l.ctx, events.LibraryScanPaused)
}

// ResumeScan unblocks a paused scan.
func (l *Library) ResumeScan() {
    l.mu.Lock()
    defer l.mu.Unlock()
    
    if !l.scanPaused {
        return
    }
    
    l.scanPaused = false
    close(l.scanPauseCh) // unblocks all waiting workers
    
    runtime.EventsEmit(l.ctx, events.LibraryScanResumed)
}

// IsScanActive returns the current scan state for the frontend.
func (l *Library) IsScanActive() bool {
    l.mu.Lock()
    defer l.mu.Unlock()
    return l.scanActive
}

// IsScanPaused returns whether the scan is currently paused.
func (l *Library) IsScanPaused() bool {
    l.mu.Lock()
    defer l.mu.Unlock()
    return l.scanPaused
}
```

### Key String Builder

```typescript
// keyboard-shortcut-service.ts
function buildKeyString(e: KeyboardEvent): string {
    const parts: string[] = [];
    
    if (e.ctrlKey || e.metaKey) parts.push('Ctrl');
    if (e.altKey) parts.push('Alt');
    if (e.shiftKey) parts.push('Shift');
    
    // Normalize key name
    let key = e.key;
    
    // Skip standalone modifier presses
    if (['Control', 'Alt', 'Shift', 'Meta'].includes(key)) {
        return '';
    }
    
    // Normalize common key names
    if (key === ' ') key = 'Space';
    if (key === 'ArrowUp') key = 'Up';
    if (key === 'ArrowDown') key = 'Down';
    if (key === 'ArrowLeft') key = 'Left';
    if (key === 'ArrowRight') key = 'Right';
    
    // Single character keys: uppercase for display
    if (key.length === 1) key = key.toUpperCase();
    
    parts.push(key);
    
    return parts.join('+');
}
```

### Default Bindings Map

```typescript
// Based on user decisions + common media player conventions
const DEFAULT_BINDINGS: Record<string, ShortcutBinding> = {
    // Player controls (Global scope, no modifier)
    'player.playPause':   { key: 'Space',  scope: 'global', category: 'Player' },
    'player.volumeUp':    { key: 'Up',     scope: 'global', category: 'Player' },
    'player.volumeDown':  { key: 'Down',   scope: 'global', category: 'Player' },
    'player.seekForward': { key: 'Right',  scope: 'global', category: 'Player' },
    'player.seekBack':    { key: 'Left',   scope: 'global', category: 'Player' },
    'player.shuffle':     { key: 'S',      scope: 'global', category: 'Player' },
    'player.repeat':      { key: 'R',      scope: 'global', category: 'Player' },
    'player.mute':        { key: 'M',      scope: 'global', category: 'Player' },
    'player.next':        { key: 'N',      scope: 'global', category: 'Player' },
    'player.previous':    { key: 'P',      scope: 'global', category: 'Player' },
    
    // Navigation (Global scope)
    'nav.search':         { key: '/',      scope: 'global', category: 'Navigation' },
    'nav.searchAlt':      { key: 'Ctrl+F', scope: 'global', category: 'Navigation' },
    'nav.queue':          { key: 'Q',      scope: 'global', category: 'Navigation' },
    
    // App actions (Global scope, Ctrl modifier)
    'app.selectAll':      { key: 'Ctrl+A', scope: 'global', category: 'App' },
    
    // Panel-specific (track list focused)
    'tracklist.play':     { key: 'Enter',  scope: 'panel:track-list', category: 'Navigation' },
    'tracklist.delete':   { key: 'Delete', scope: 'panel:track-list', category: 'Navigation' },
};
```

### Shortcut Settings Tab — Key Capture Widget

```typescript
// shortcut-capture.ts — Record-style key capture (VS Code inspired)
@customElement('shortcut-capture')
class ShortcutCapture extends LitElement {
    @property() action = '';
    @property() currentKey = '';
    @state() private recording = false;
    @state() private pendingKey = '';
    
    private handleClick = () => {
        this.recording = true;
        this.pendingKey = '';
    };
    
    private handleKeydown = (e: KeyboardEvent) => {
        if (!this.recording) return;
        
        e.preventDefault();
        e.stopPropagation();
        
        const keyStr = buildKeyString(e);
        if (!keyStr) return; // bare modifier press
        
        if (keyStr === 'Escape') {
            // Cancel recording
            this.recording = false;
            this.pendingKey = '';
            return;
        }
        
        this.pendingKey = keyStr;
        this.recording = false;
        
        // Dispatch event for parent to handle conflict check + save
        this.dispatchEvent(new CustomEvent('shortcut-change', {
            detail: { action: this.action, key: keyStr },
            bubbles: true, composed: true,
        }));
    };
    
    override render() {
        return html`
            <button
                class=${this.recording ? 'recording' : ''}
                @click=${this.handleClick}
                @keydown=${this.handleKeydown}
            >
                ${this.recording
                    ? 'Press a key combo...'
                    : this.pendingKey || this.currentKey || 'Not set'}
            </button>
        `;
    }
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `KeyboardEvent.keyCode` | `KeyboardEvent.key` / `.code` | Deprecated for years | Use `.key` for logical key, `.code` for physical position |
| Manual goroutine cancellation with channels | `context.WithCancel` | Standard since Go 1.7 (2016) | Composes with existing context-aware APIs |
| Global keyboard shortcut libraries (mousetrap, hotkeys.js) | Native KeyboardEvent API | N/A | Desktop Wails app doesn't need library overhead |

**Deprecated/outdated:**
- `KeyboardEvent.keyCode` / `KeyboardEvent.which`: Deprecated. Use `.key` for the logical key value.
- `KeyboardEvent.charCode`: Removed. Not relevant for this use case.

## Open Questions

1. **Volume step size for arrow keys**
   - What we know: Up/Down arrows should adjust volume. Player.SetVolume accepts 0-100 integer.
   - What's unclear: Step size per keypress (5? 10?)
   - Recommendation: Default to 5 units per keypress (matches common media player conventions). This is a Claude's Discretion item.

2. **Seek step size for arrow keys**
   - What we know: Left/Right arrows should seek. Player.Seek accepts seconds.
   - What's unclear: How many seconds per keypress.
   - Recommendation: Default to 5 seconds per keypress. This is a Claude's Discretion item.

3. **"Discard" implementation on scan cancel**
   - What we know: User can choose "Keep X tracks" or "Discard." Keeping is straightforward (do nothing).
   - What's unclear: Precise discard mechanism — delete individual added IDs vs clear-and-rescan approach.
   - Recommendation: Track added audio file IDs during the scan. On discard, batch-delete those IDs within a transaction. This avoids the nuclear option of a full library clear while being precise. If this proves too complex, a simpler fallback is to trigger the library clear tables operation (existing `clearLibraryTables()`) and leave the user with an empty library that they can rescan.

4. **N and P for next/previous vs typing**
   - What we know: Single-key shortcuts (S, R, Q) work in global scope. N/P follow the same pattern.
   - What's unclear: Whether N/P could conflict with other planned features (e.g., future search-as-you-type).
   - Recommendation: Include N/P as defaults but since all bindings are configurable, users can remap if conflicts arise. The text input scope suppression ensures they don't fire during typing.

## Sources

### Primary (HIGH confidence)
- **Codebase analysis** — Direct reading of all scanner, config, events, and frontend component source files
- **Go `context` package** — Standard library documentation for `WithCancel` pattern
- **MDN `KeyboardEvent`** — `e.key`, `e.code`, modifier properties (`ctrlKey`, `altKey`, `shiftKey`, `metaKey`)

### Secondary (MEDIUM confidence)
- **VS Code keybinding UX** — Reference for record-style key capture interaction pattern (widely adopted UX pattern)
- **Wails v2 event system** — `runtime.EventsEmit` / `EventsOn` patterns verified from existing codebase usage

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no new dependencies, all patterns verified from existing codebase and Go/Web standards
- Architecture: HIGH — extends existing patterns (config sections, Wails bindings, Lit components, event system)
- Pitfalls: HIGH — identified from direct codebase analysis (shadow DOM, orphan cleanup, batch commits)

**Research date:** 2026-03-06
**Valid until:** 2026-04-06 (stable domain — no rapidly changing dependencies)
