# Plan: Replace `GetCurrentTrackInfo` `map[string]interface{}` with a Typed Struct

**Refactoring catalog item:** #9
**Priority:** P2
**Risk:** Low — the player is not in `FEBindings`, so no Wails binding regeneration is needed. All data flows through the event system.

---

## Problem Statement

`player.getCurrentTrackInfoLocked()` returns `map[string]interface{}` — a stringly-typed map with 10 keys. Then `emitTrackChanged()` mutates this map by bolting on 3 additional keys (`trackLength`, `seekPosition`, `trackChangeId`) before emitting it via `runtime.EventsEmit`. This pattern has several issues:

1. **No compile-time safety** — a typo like `"fileName"` vs `"filename"` is a silent bug.
2. **Split construction** — the 13-field payload is built in two places (`getCurrentTrackInfoLocked` builds 10 fields, `emitTrackChanged` appends 3 more via map mutation). The shape of the data is not visible in any single location.
3. **Inconsistent nil-file fallback** — when `p.currentFile == nil`, the returned map has 7 keys (missing `coverArtSmall`, `coverArtMedium`, `coverArtLarge`). The error fallback in `emitTrackChanged` has only 3 keys. Both cases produce maps with incomplete field sets that differ from each other and from the happy path (13 keys).
4. **Missed opportunity for Wails type generation** — if the player were ever added to `FEBindings`, a struct return type would auto-generate TypeScript bindings. Currently the frontend manually maintains a `TrackInfo` interface that must be kept in sync by hand.
5. **Contrast with rest of codebase** — the queue package already uses proper structs with JSON tags (`queue.Track`, `queue.State`, etc.) for all event payloads. The player is an outlier.

---

## Design Decisions & Reasoning

### Decision 1: Define a single `TrackInfo` struct (not two separate types)

The catalog suggests defining a `TrackInfo` struct. A question arises: should `getCurrentTrackInfoLocked` return a "partial" struct (10 fields) while `emitTrackChanged` extends it with 3 more? No — the whole point is to eliminate the mutation pattern. A single struct with all 13 fields is cleaner. The struct represents "everything the frontend needs to know about the current track for the TrackChanged event."

**Reasoning:** A single struct means one source of truth for the shape of the data. The zero values for `TrackLength`, `SeekPosition`, and `TrackChangeID` are naturally `0` in Go, which is semantically correct for "no track loaded" or "error" fallback cases.

### Decision 2: Use `json` struct tags with camelCase keys

The existing map uses camelCase keys (`"fileName"`, `"coverArtSmall"`, etc.). Wails serializes event payloads as JSON. The struct must use `json:"fileName"` tags to preserve the exact same wire format — otherwise the frontend would break.

**Reasoning:** This is a behavioral requirement, not a style choice. The frontend `TrackInfo` interface expects camelCase keys. Changing them would require coordinated frontend changes for zero benefit.

### Decision 3: Keep `getCurrentTrackInfoLocked` but change its return type

Rather than inlining all logic into `emitTrackChanged`, keep the `getCurrentTrackInfoLocked` helper but have it return `TrackInfo` (with the base 10 fields populated). Then `emitTrackChanged` fills in the remaining 3 fields (`TrackLength`, `SeekPosition`, `TrackChangeID`) on the struct before emitting.

**Reasoning:** This preserves the separation of concerns — "build metadata from file/DB" vs "compute playback position and emit." It also keeps `GetCurrentTrackInfo()` (the public method) useful: it returns the same struct, just without the playback-timing fields (which are zero-valued). If the player is ever added to `FEBindings`, this method's return type would auto-generate a TypeScript class.

### Decision 4: Eliminate `GetCurrentTrackInfo()` public method — or keep it?

`GetCurrentTrackInfo()` has **zero Go callers** and **zero TypeScript callers** (the player is not in `FEBindings`). It exists only as dead code. However, it was likely intended as a Wails binding that hasn't been wired up yet, and it could be useful in the future.

**Decision: Keep it.** The cost of a single unused method is minimal, and it now returns a proper struct which would be useful if the player is added to `FEBindings` later. If desired, it can be removed as part of a separate cleanup (item #13 addresses dead player methods).

### Decision 5: Fix the inconsistent nil-file/error fallbacks

Currently:
- **nil file fallback** (line 840-848): returns 7 keys — missing `coverArtSmall`, `coverArtMedium`, `coverArtLarge`
- **error fallback** in `emitTrackChanged` (line 319-323): returns only 3 keys — missing most fields

With a struct, both fallbacks naturally return a fully-populated struct (all fields present, most set to zero values). The `State` field should still be set explicitly in both cases. This eliminates the inconsistency for free.

### Decision 6: Place the struct in the existing `player.go` file, not a new file

The player package has only 3 files (`player.go`, `volume.go`, `player_test.go`). The struct is tightly coupled to the player — it describes what the player emits. Creating a separate `trackinfo.go` file for a single ~20-line struct definition would be premature file splitting for such a small package.

**Reasoning:** Follow the existing pattern — `State` type and playback constants are already defined in `player.go`. The `TrackInfo` struct logically belongs alongside them.

### Decision 7: Use `State` type (not `string`) in the struct

Currently the map stores `string(p.state)` — explicitly converting the `State` type to `string`. The struct should use the `State` type with `json:"state"` tag. Since `State` is `type State string`, JSON serialization produces the same string value. This gives us type safety in Go without changing the wire format.

**Reasoning:** The whole point of this refactoring is compile-time safety. Using `string` in the struct for the state field would undermine that goal.

### Decision 8: Use `uint64` for `TrackChangeID` (match the field type)

The `Player` struct defines `trackChangeID uint64`. The struct field should be `TrackChangeID uint64`. The frontend `TrackInfo` interface uses `number` which can safely represent integers up to 2^53 — more than sufficient for a monotonic counter that starts at 0 per session.

---

## Implementation Plan

### Step 1: Define the `TrackInfo` struct in `player.go`

Add the struct definition near the existing `State` type (around line 58-65), after the sentinel errors:

```go
// TrackInfo contains metadata and playback state for the currently loaded track.
type TrackInfo struct {
	FileName       string `json:"fileName"`
	FilePath       string `json:"filePath"`
	State          State  `json:"state"`
	Title          string `json:"title"`
	Artist         string `json:"artist"`
	Album          string `json:"album"`
	CoverArt       string `json:"coverArt"`
	CoverArtSmall  string `json:"coverArtSmall"`
	CoverArtMedium string `json:"coverArtMedium"`
	CoverArtLarge  string `json:"coverArtLarge"`
	TrackLength    int    `json:"trackLength"`
	SeekPosition   int    `json:"seekPosition"`
	TrackChangeID  uint64 `json:"trackChangeId"`
}
```

**Note:** `json:"trackChangeId"` (lowercase `d`) matches the existing frontend interface key `trackChangeId`.

### Step 2: Refactor `getCurrentTrackInfoLocked` to return `TrackInfo`

Change the signature from `(map[string]interface{}, error)` to `TrackInfo` (no error needed — see reasoning below).

**Why remove the error return?** The current function never actually returns an error. It handles all error cases internally (DB lookup failure logs and falls back to defaults). The error in the return signature is unused dead weight. With a struct, the zero-value fallback is even cleaner.

Updated implementation:

```go
func (p *Player) getCurrentTrackInfoLocked() TrackInfo {
	info := TrackInfo{
		State: p.state,
	}

	if p.currentFile == nil {
		return info
	}

	info.FileName = filepath.Base(p.currentFile.Name())
	info.FilePath = p.currentFile.Name()
	info.Title = info.FileName // default title

	if p.db != nil {
		meta, err := p.db.Queries.GetTrackMetadataByPath(
			p.ctx, info.FilePath,
		)
		if err == nil {
			if meta.Title != "" {
				info.Title = meta.Title
			}

			info.Artist = meta.Artist
			info.Album = meta.Album

			if meta.CoverArtPath != "" {
				base := filepath.Base(meta.CoverArtPath)
				info.CoverArt = "/covers/" + base
				info.CoverArtSmall = "/covers/" +
					library.SizedFilename(base, "_sm")
				info.CoverArtMedium = "/covers/" +
					library.SizedFilename(base, "_md")
				info.CoverArtLarge = "/covers/" +
					library.SizedFilename(base, "_lg")
			}
		} else {
			p.logger.Debug(
				"Could not get track metadata from database",
				"path", info.FilePath, "err", err,
			)
		}
	}

	return info
}
```

### Step 3: Update `GetCurrentTrackInfo` (public method)

Change return type from `(map[string]interface{}, error)` to `TrackInfo`:

```go
// GetCurrentTrackInfo returns information about the currently loaded track.
func (p *Player) GetCurrentTrackInfo() TrackInfo {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.getCurrentTrackInfoLocked()
}
```

**Note:** Dropping the error return is safe — there are zero callers of this method.

### Step 4: Refactor `emitTrackChanged` to build the struct directly

Replace map mutation with direct struct field assignment:

```go
func (p *Player) emitTrackChanged() {
	if p.ctx == nil {
		p.logger.Error("Context is nil, cannot emit event")

		return
	}

	trackInfo := p.getCurrentTrackInfoLocked()

	trackLengthSecs, err := p.trackLengthLocked()
	if err != nil {
		p.logger.Error("Cannot get track length")
	}

	trackInfo.TrackLength = trackLengthSecs

	// Compute current seek position in seconds.
	if p.seeker != nil {
		speaker.Lock()
		trackInfo.SeekPosition = p.seeker.Position() /
			int(p.format.SampleRate)
		speaker.Unlock()
	}

	// Increment track change ID so the frontend can detect changes
	// even when the same file plays consecutively.
	p.trackChangeID++
	trackInfo.TrackChangeID = p.trackChangeID

	runtime.EventsEmit(p.ctx, events.TrackChanged, trackInfo)

	p.logger.Info(
		"Emitting TrackChangedEvent with track info",
		"trackInfo", trackInfo,
	)
}
```

**Key change:** No more error-fallback map with only 3 keys. If `getCurrentTrackInfoLocked()` returns a zero-valued struct (e.g., when no file is loaded), it still has all 13 fields — the frontend receives a complete, predictable shape every time.

### Step 5: Verify `UnloadTrack` emits `nil` (no change needed)

At `player.go:678`, `UnloadTrack` emits:
```go
runtime.EventsEmit(p.ctx, events.TrackChanged, nil)
```

This is correct and intentional — it signals "no track loaded" to the frontend, which handles `null` in `(trackInfo: TrackInfo | null) => { ... }`. No changes needed here.

### Step 6: Run `make lint` and `make test`

Ensure:
- No linting violations (line length, godot, nlreturn, etc.)
- Tests pass (the existing test is integration-only and skips in CI, but the build itself must succeed with `-tags webkit2_41`)

### Step 7: (Optional) Update the frontend `TrackInfo` interface comments

The frontend `TrackInfo` interface in `frontend/src/store/player-store.ts` already matches the struct fields exactly. No field changes are needed. However, a comment noting that it mirrors `player.TrackInfo` from the backend could be helpful for future maintainers:

```typescript
// TrackInfo mirrors the player.TrackInfo struct in the Go backend.
// Fields are serialized as camelCase JSON via struct tags.
export interface TrackInfo {
  // ... (existing fields, unchanged)
}
```

---

## Files Changed

| File | Change |
|------|--------|
| `backend/player/player.go` | Add `TrackInfo` struct; refactor `getCurrentTrackInfoLocked`, `GetCurrentTrackInfo`, and `emitTrackChanged` |
| `frontend/src/store/player-store.ts` | Add comment noting Go struct mirror (optional) |

**No other files need changes.** The frontend receives the data via events and the JSON wire format is identical (same keys, same types). No Wails binding regeneration is needed since the player is not in `FEBindings`.

---

## Risks & Mitigations

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| JSON key mismatch after refactoring | Low | The `json` struct tags are set to exactly match the current map keys. Verify by running the app and checking the frontend receives correct data. |
| `slog` logging of struct differs from map | Very low | `slog` will log the struct fields. The output format changes but the information is equivalent. No functional impact. |
| Future addition of `player` to `FEBindings` | N/A | This refactoring *enables* that future change — Wails will auto-generate a `player.TrackInfo` TypeScript class from the struct. |

---

## Verification

1. `make lint` passes
2. `make build-dev` succeeds
3. Manual test: play a track, verify `now-playing` component shows correct title/artist/cover art
4. Manual test: verify seek bar shows correct track length and seek position
5. Manual test: unload track (stop playback, clear queue), verify frontend clears the now-playing display
6. Manual test: play the same track twice consecutively, verify the seek bar resets (trackChangeId detection)
