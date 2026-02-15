# Plan: Queue Click-to-Play

## Goal
When a track in the queue panel is clicked, that track should start playing.

## Architecture Overview
The app uses a unidirectional event system: Frontend emits request events -> Backend processes them -> Backend emits state-changed events -> Frontend stores update -> Lit components re-render. The queue backend (`backend/queue/queue.go`) drives playback via `playCurrentTrack()` which calls `player.LoadFile()` then `player.Play()`.

## Changes Required (6 files)

### 1. `backend/events/events.go` — Add new event constant
Add `RequestPlayQueueIndex = "RequestPlayQueueIndex"` to the queue events const block.

```go
	RequestAddTracksToQueue  = "RequestAddTracksToQueue"
	RequestPlayTracksNext   = "RequestPlayTracksNext"
	RequestPlayQueueIndex   = "RequestPlayQueueIndex"
```

### 2. `frontend/src/events.ts` — Add matching TypeScript event constant
Add `RequestPlayQueueIndex: "RequestPlayQueueIndex"` to the Events object.

```typescript
    RequestAddTracksToQueue: "RequestAddTracksToQueue",
    RequestPlayTracksNext: "RequestPlayTracksNext",
    RequestPlayQueueIndex: "RequestPlayQueueIndex",
```

### 3. `backend/queue/queue.go` — Add PlayIndex method + event handler

**a) Add event handler registration** in `registerEventHandlers()`, after the `RequestPlayTracksNext` handler (around line 184):

```go
	runtime.EventsOn(q.ctx, events.RequestPlayQueueIndex, func(data ...any) {
		q.logger.Info("Received RequestPlayQueueIndex")
		q.handlePlayQueueIndex(data...)
	})
```

**b) Add handler function** (after `handlePlayTracksNext`, around line 329):

```go
// handlePlayQueueIndex processes the RequestPlayQueueIndex event payload.
// Expects data[0] = float64 index.
func (q *Queue) handlePlayQueueIndex(data ...any) {
	if len(data) < 1 {
		q.logger.Error("RequestPlayQueueIndex: missing data")

		return
	}

	index, ok := data[0].(float64)
	if !ok {
		q.logger.Error("RequestPlayQueueIndex: invalid index type", "got", data[0])

		return
	}

	q.PlayIndex(int(index))
}
```

**c) Add `PlayIndex` method** (after `Previous()`, around line 679):

```go
// PlayIndex jumps to and plays the track at the given index.
func (q *Queue) PlayIndex(index int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.tracks) == 0 {
		return
	}

	if index < 0 || index >= len(q.tracks) {
		q.logger.Warn("PlayIndex: index out of range", "index", index, "trackCount", len(q.tracks))

		return
	}

	q.currentIndex = index
	q.playCurrentTrack()
	q.emitQueueChanged()
}
```

This is simple and consistent with how `SetQueue` works — it sets `currentIndex` directly and calls `playCurrentTrack()`. When shuffle is on, the current track changes but the shuffle order stays intact. Subsequent Next/Previous calls will navigate relative to the new position in the shuffle order.

### 4. `frontend/src/store/queue-store.ts` — Add `playAtIndex` + fix QueueTrack type

**a) Fix QueueTrack interface** (add title and artist fields that the backend sends):

```typescript
export interface QueueTrack {
  id: number;
  audioFileId: number;
  filePath: string;
  position: number;
  title: string;
  artist: string;
}
```

**b) Add `playAtIndex` action** (after `cycleRepeat()`, around line 108):

```typescript
  playAtIndex(index: number): void {
    EventsEmit(Events.RequestPlayQueueIndex, index);
  }
```

### 5. `frontend/src/store/controllers/queue-controller.ts` — Expose `playAtIndex`

Add after `cycleRepeat()` (around line 112):

```typescript
  playAtIndex(index: number): void {
    queueStore.playAtIndex(index);
  }
```

### 6. `frontend/src/components/queue-panel/queue-panel.ts` — Add click handler

**a) Add click handler method** (after `handleRemoveTrack`, around line 170):

```typescript
  private handleTrackClick(index: number) {
    this.queue.playAtIndex(index);
  }
```

**b) Update the `<li>` element** to add a click handler and change cursor style. Update the `track-item` CSS from `cursor: default` to `cursor: pointer`:

```css
    .track-item {
      display: flex;
      align-items: center;
      padding: 8px 16px;
      gap: 12px;
      border-bottom: 1px solid rgba(255, 255, 255, 0.05);
      cursor: pointer;
    }
```

**c) Add `@click` handler to the `<li>`** and **stop propagation on the remove button** so clicking remove doesn't also trigger playback:

```html
<li class="track-item ${index === currentIndex ? 'active' : ''}"
    @click=${() => this.handleTrackClick(index)}>
  <span class="track-position">${index + 1}</span>
  <div class="track-details">
    <span class="track-title">${this.getDisplayTitle(track)}</span>
    <span class="track-artist">${track.artist || 'Unknown Artist'}</span>
  </div>
  <button
    class="remove-button"
    @click=${(e: Event) => { e.stopPropagation(); this.handleRemoveTrack(index); }}
    title="Remove from queue"
  >
    <wa-icon name="xmark"></wa-icon>
  </button>
</li>
```

## Verification
After making changes:
1. `make lint` — Go linting passes
2. `make test` — Go tests pass
3. `cd frontend && pnpm exec tsc --noEmit` — TypeScript type checking passes
