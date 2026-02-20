# Plan: Clear Queue Button

## Goal
Add a "Clear Queue" button (trash icon) next to the existing "Add queue to playlist" button in the queue panel header. The button clears all tracks from the queue, stops playback, and resets queue state.

## Architecture Overview
The backend already has a `Queue.Clear()` method (`backend/queue/queue.go:1540`) that handles everything — clearing tracks, stopping playback, resetting state, persisting, and emitting `QueueChanged`. The only missing piece is wiring it to the frontend via the event system and adding the UI button.

## Changes Required (5 files)

### 1. `backend/events/events.go` — Add new event constant
Add `RequestClearQueue` to the queue events const block.

```go
	RequestMoveQueueTracks       = "RequestMoveQueueTracks"
	RequestClearQueue            = "RequestClearQueue"
)
```

### 2. `frontend/src/events.ts` — Add matching TypeScript event constant
Add `RequestClearQueue` to the queue events section.

```typescript
    RequestMoveQueueTracks: "RequestMoveQueueTracks",
    RequestClearQueue: "RequestClearQueue",
```

### 3. `backend/queue/queue.go` — Wire event handler in `registerEventHandlers()`
Add a new `runtime.EventsOn` call at the end of `registerEventHandlers()` (after the existing `RequestMoveQueueTracks` handler around line 289):

```go
	runtime.EventsOn(
		q.ctx,
		events.RequestClearQueue,
		func(_ ...any) {
			q.logger.Info("Received RequestClearQueue")
			q.Clear()
		},
	)
```

### 4. `frontend/src/store/queue-store.ts` — Add `clearQueue()` action
Add after the existing `moveTracksInQueue()` method (around line 250):

```typescript
  clearQueue(): void {
    EventsEmit(Events.RequestClearQueue);
  }
```

### 5. `frontend/src/components/queue-panel/queue-panel.ts` — Add UI button, styles, and handler

#### 5a. Add handler method
Add a new handler method near the other handlers (around line 518, near `handleAddToPlaylist`):

```typescript
    private handleClearQueue = () => {
        queueStore.clearQueue();
    };
```

Note: Import `queueStore` — check if it's already imported (it likely is via the controller).

#### 5b. Add CSS styles for shared header button class
Add a `.header-actions` container style and refactor the button styles. Replace the existing `.add-to-playlist-button` styles:

**Replace:**
```css
        .add-to-playlist-button {
            background: none;
            border: none;
            color: inherit;
            cursor: pointer;
            padding: 4px;
            display: flex;
            align-items: center;
        }

        .add-to-playlist-button:hover {
            color: #ffd43b;
        }

        .add-to-playlist-button:disabled {
            color: #555;
            cursor: not-allowed;
        }
```

**With:**
```css
        .header-actions {
            display: flex;
            align-items: center;
            gap: 4px;
        }

        .header-action-button {
            background: none;
            border: none;
            color: inherit;
            cursor: pointer;
            padding: 4px;
            display: flex;
            align-items: center;
        }

        .header-action-button:hover {
            color: #ffd43b;
        }

        .header-action-button:disabled {
            color: #555;
            cursor: not-allowed;
        }
```

#### 5c. Update the header HTML
Replace the header section (around lines 1183-1193):

**Replace:**
```html
                <div class="header">
                    <h3>Queue</h3>
                    <button
                        class="add-to-playlist-button"
                        @click=${this.handleAddToPlaylist}
                        ?disabled=${tracks.length === 0}
                        title="Add queue to playlist"
                    >
                        <wa-icon name="plus"></wa-icon>
                    </button>
                </div>
```

**With:**
```html
                <div class="header">
                    <h3>Queue</h3>
                    <div class="header-actions">
                        <button
                            class="header-action-button"
                            @click=${this.handleClearQueue}
                            ?disabled=${tracks.length === 0}
                            title="Clear queue"
                        >
                            <wa-icon name="trash"></wa-icon>
                        </button>
                        <button
                            class="header-action-button add-to-playlist-button"
                            @click=${this.handleAddToPlaylist}
                            ?disabled=${tracks.length === 0}
                            title="Add queue to playlist"
                        >
                            <wa-icon name="plus"></wa-icon>
                        </button>
                    </div>
                </div>
```

**Important:** The `add-to-playlist-button` class must remain on the playlist button because it's referenced by `@query` selectors and the `closePickerHandler` (lines 84-85, 527). The new class `header-action-button` provides the shared visual style.

#### 5d. Update CSS selector references
Check that the `.add-to-playlist-button` query selector references still work. Since we're keeping `add-to-playlist-button` as a class on the playlist button, the existing `@query('.add-to-playlist-button')` and `querySelector('.add-to-playlist-button')` calls will continue to work unchanged.

### 6. Import check
Verify that `queueStore` is accessible in `queue-panel.ts`. The component uses a `QueueController` which wraps the store, but the `clearQueue()` call needs to go through the store directly. Check if `queueStore` is already imported; if not, add:

```typescript
import { queueStore } from '@store/queue-store';
```

## Testing
- Run `make lint` to verify Go code passes linting
- Run `cd frontend && pnpm exec tsc --noEmit` to verify TypeScript compiles
- Manual testing: click the trash button when queue has tracks → queue should clear, playback should stop, button should become disabled
