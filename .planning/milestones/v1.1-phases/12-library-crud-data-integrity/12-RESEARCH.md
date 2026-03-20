# Phase 12: Library CRUD & Data Integrity - Research

**Researched:** 2026-03-12
**Domain:** SQLite data lifecycle management, orphan cleanup, Wails CRUD API, Lit Web Components
**Confidence:** HIGH

## Summary

Phase 12 adds the user-facing library management API and UI — add, rename, and remove libraries — plus the data integrity logic that keeps the database consistent when a library is removed. The schema (Phase 10) and per-library scanning (Phase 11) are complete; this phase wires CRUD operations to the existing infrastructure and builds the orphan cleanup pipeline.

The primary technical challenge is the **remove library** operation: it must atomically delete a library's tracks, cascade-delete queue entries, convert playlist tracks to phantoms, identify and delete orphaned entities (recordings, release groups, artist credits, artists, genres, cover art) that are no longer referenced by any remaining library, and rebuild the FTS5 search index. All of this must happen in a single transaction (except FTS5 rebuild, which cannot run inside a transaction).

**Primary recommendation:** Implement removal as a single Go method on the Library struct that runs the full cleanup pipeline in one transaction, returns a cleanup summary struct, and emits events so the frontend can show a toast and invalidate its caches.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- Integrated library + scan section in the settings page — combine library management and scanning into one unified section
- Remove the libraries tab from the sidebar list entirely
- Each library row displays: name, directory path, track count in the main row; actions (rename, remove, rescan) hidden behind a `...` overflow menu
- Replace the old single-directory config UI (directory path field + rescan button) completely — the migrated library appears in the new list
- Click "Add Library" button in the library management section
- OS folder picker dialog opens
- Library auto-named from the folder name (editable later via rename)
- Scan starts automatically after adding
- Uses the existing per-library scan pipeline from Phase 11
- Warning dialog with impact summary before removal: "Remove 'Jazz Collection'? This will delete 1,234 tracks, affect 2 playlists, and remove 15 queue items."
- If a track from the library being removed is currently playing, stop playback first, then proceed with removal; queue advances to next valid track if one exists
- Blocking operation with spinner on the dialog while cleanup runs (expected < 1 second for most libraries)
- Toast notification on completion: "Removed 'Jazz Collection' (1,234 tracks deleted)"
- Immediate cleanup in the same database transaction — delete tracks, identify orphaned entities, delete orphans, convert playlist phantoms, all atomic
- Reference-counting bottom-up: only delete artists/albums/genres that have zero remaining track references after the library's tracks are removed
- Rebuild the entire FTS5 index from remaining tracks after library removal (handles contentless table limitation cleanly)
- Playlist phantom track conversion in the same transaction: copy track metadata to phantom columns on playlist_tracks, then SET NULL the audio_file_id
- Queue tracks cascade-delete (queue is ephemeral, not user-curated)
- Removal API endpoint returns cleanup summary: {tracks_deleted, artists_removed, albums_removed, genres_removed, playlists_affected, queue_items_removed} — feeds the toast notification
- Library names must be unique — validation error if user tries to use an existing name
- Inline edit on the list row: click name (or rename action from menu) turns it into an editable text field, Enter to save, Escape to cancel
- Name validation: 1-50 characters, non-empty
- Rename changes display name only — changing a library's directory path requires remove + add (no path editing)

### Claude's Discretion
- Exact layout/styling of the library management section within settings
- Loading skeleton design while library list loads
- Error state handling for failed operations
- Exact spinner implementation during removal
- Toast notification library/component choice
- API endpoint URL structure and HTTP methods
- SQL query optimization for orphan detection

### Deferred Ideas (OUT OF SCOPE)
None — discussion stayed within phase scope
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| LIB-01 | User can add a new library directory via a folder picker dialog | DirectoryPicker already exists in `frontendutil.go:27`. Add-library flow: picker → CreateLibrary query → ScanLibrary. Auto-name from `filepath.Base()`. |
| LIB-02 | User can rename a library (display name) | UpdateLibraryName query already exists in `libraries.sql:14`. Need uniqueness validation and frontend inline edit. |
| LIB-03 | User can remove a library — tracks deleted, shared entities cleaned up only if no other library references them | Core orphan cleanup pipeline needed. New hand-crafted SQL for bottom-up reference-counting deletes. Phantom conversion before delete. |
| LIB-06 | Library list displayed in a management UI (settings or sidebar section) | Replace existing library-manager and config-page library section. New unified section using GetAllLibraries + CountAudioFilesByLibrary. |
| DATA-02 | Orphan cleanup after library removal: reference-counting bottom-up deletes | New SQL queries for identifying orphaned recordings, release_groups, artist_credits, artists, genres, cover_art. Single transaction. |
| DATA-03 | FTS5 index entries for removed tracks cleaned up | RebuildSearchIndex already exists in `search.go:159`. Call after removal transaction commits. Contentless FTS5 cannot delete individual rows. |
| PLAY-04 | Queue tracks from a removed library are cascade-deleted | Already handled by schema: `queue_tracks.audio_file_id` has `ON DELETE CASCADE`. Queue state adjustment needed (current_position, shuffle_order). |
</phase_requirements>

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib (`database/sql`) | go1.24 | Transaction management, raw SQL for orphan cleanup | Already used throughout; sqlc queries + hand-crafted SQL for complex operations |
| sqlc | v1.30.0 | Code generation for simple CRUD queries | Existing pattern; generates typed Go from SQL |
| modernc.org/sqlite | current | Pure-Go SQLite driver | Already used; single-writer, WAL mode |
| Lit | 3.x | Frontend web components | Existing UI framework |
| Wails v2 | v2.x | Go↔JS binding, events, runtime dialogs | Existing app framework |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `@runtime/runtime` (Wails JS) | v2.x | EventsOn/EventsEmit for scan events, toast triggers | All frontend event handling |
| `frontendutil.DirectoryPicker` | existing | OS folder selection dialog | Add-library flow |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Full FTS5 rebuild on removal | `contentless_delete=1` migration | Would require migration 7 to recreate FTS5 table; full rebuild is simpler and removal is rare |
| Hand-crafted orphan SQL | Multiple sqlc queries in a loop | Hand-crafted SQL is a single statement per entity type, far more efficient than N+1 queries |
| Custom toast component | Third-party toast library | No dependency needed; a simple `<div>` with CSS animation and auto-dismiss timer suffices |

## Architecture Patterns

### Recommended Project Structure
```
backend/library/
├── library.go          # Existing: scan pipeline, entity processing
├── scan_queue.go       # Existing: per-library scan coordination
├── rescan.go           # Existing: FullRescan, clearLibraryTables
├── query.go            # Existing: GetAllTracks, GetAllAlbums, etc.
├── crud.go             # NEW: AddLibrary, RenameLibrary, RemoveLibrary
└── scan_control.go     # Existing: pause/resume/cancel

backend/database/
├── search.go           # Existing: FTS5 operations (RebuildSearchIndex)
└── sql/queries/
    └── libraries.sql   # EXTEND: add orphan cleanup queries

frontend/src/
├── components/
│   └── config-page/
│       └── config-page.ts  # MODIFY: replace library section with new unified UI
└── store/
    └── library-store.ts    # MODIFY: add library list, invalidation on add/remove
```

### Pattern 1: Transactional Orphan Cleanup
**What:** A single Go method that runs the entire removal pipeline in one transaction, then rebuilds FTS5 outside the transaction.
**When to use:** Library removal.
**Example:**
```go
// Source: Derived from existing clearLibraryTables pattern in rescan.go:100
func (l *Library) RemoveLibrary(id int64) (*RemovalSummary, error) {
    // 1. Pre-removal: count impacts for summary
    // 2. Stop playback if current track belongs to this library
    // 3. Begin transaction
    // 4. Populate phantom metadata on playlist_tracks for this library's tracks
    // 5. Delete audio_files WHERE library_id = ? (CASCADE deletes queue_tracks, SET NULL on playlist_tracks)
    // 6. Delete orphaned recordings (no remaining audio_files reference them)
    // 7. Delete orphaned release_group_recordings, recording_genres
    // 8. Delete orphaned release_groups (no remaining recordings reference them)
    // 9. Delete orphaned artist_credits (no remaining recordings reference them)
    // 10. Delete orphaned artist_credit_artists
    // 11. Delete orphaned artists (no remaining credits reference them)
    // 12. Delete orphaned genres (no remaining recording_genres reference them)
    // 13. Delete orphaned cover_art (no remaining release_groups reference them)
    // 14. Delete the library row itself
    // 15. Commit transaction
    // 16. Rebuild FTS5 search index (outside transaction)
    // 17. Emit events
    // 18. Return summary
}
```

### Pattern 2: Pre-Removal Impact Summary
**What:** A read-only query that returns the counts shown in the removal confirmation dialog, run before the user confirms.
**When to use:** Before showing the removal warning dialog.
**Example:**
```go
// SAFETY: Hand-crafted SQL for impact summary. Read-only, no modifications.
type RemovalImpact struct {
    TrackCount        int64
    PlaylistsAffected int64
    QueueItemCount    int64
}

func (l *Library) GetRemovalImpact(libraryID int64) (*RemovalImpact, error) {
    // Count tracks: SELECT COUNT(*) FROM audio_files WHERE library_id = ?
    // Count affected playlists: SELECT COUNT(DISTINCT playlist_id) FROM playlist_tracks
    //     WHERE audio_file_id IN (SELECT id FROM audio_files WHERE library_id = ?)
    // Count queue items: SELECT COUNT(*) FROM queue_tracks
    //     WHERE audio_file_id IN (SELECT id FROM audio_files WHERE library_id = ?)
}
```

### Pattern 3: Phantom Metadata Population Before DELETE
**What:** Before deleting audio_files, copy live track metadata into the phantom columns on playlist_tracks.
**When to use:** Library removal, inside the transaction before the DELETE.
**Example:**
```sql
-- SAFETY: Hand-crafted SQL for phantom metadata population.
-- Must run BEFORE DELETE FROM audio_files (which triggers SET NULL on audio_file_id).
UPDATE playlist_tracks SET
    phantom_title = sub.title,
    phantom_artist = sub.artist,
    phantom_album = sub.album,
    phantom_duration_ms = sub.duration,
    phantom_genre = sub.genre,
    phantom_cover_art_path = sub.cover_art_path
FROM (
    SELECT
        pt.id AS pt_id,
        COALESCE(r.name, '') AS title,
        COALESCE(ac.text, '') AS artist,
        COALESCE(rg.name, '') AS album,
        af.length_milliseconds AS duration,
        CAST(COALESCE(
            (SELECT GROUP_CONCAT(g.name, '||')
             FROM recording_genres rg_sub
             JOIN genres g ON rg_sub.genre_id = g.id
             WHERE rg_sub.recording_id = r.id),
            ''
        ) AS TEXT) AS genre,
        COALESCE(ca.file_path, '') AS cover_art_path
    FROM playlist_tracks pt
    JOIN audio_files af ON pt.audio_file_id = af.id
    LEFT JOIN recordings r ON af.recording_id = r.id
    LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
    LEFT JOIN (
        SELECT recording_id, MIN(release_group_id) AS release_group_id
        FROM release_group_recordings
        GROUP BY recording_id
    ) rgr ON r.id = rgr.recording_id
    LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
    LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
    WHERE af.library_id = ?
) sub
WHERE playlist_tracks.id = sub.pt_id;
```

### Pattern 4: Bottom-Up Orphan Deletion
**What:** Delete orphaned entities by checking for zero remaining references, in dependency order.
**When to use:** After deleting audio_files for a library.
**Example:**
```sql
-- SAFETY: Hand-crafted orphan cleanup SQL. All parameterized.

-- 1. Delete orphaned recordings (no audio_files reference them)
DELETE FROM recordings WHERE id NOT IN (
    SELECT DISTINCT recording_id FROM audio_files
);

-- 2. Delete orphaned recording_genres (recording no longer exists)
DELETE FROM recording_genres WHERE recording_id NOT IN (
    SELECT id FROM recordings
);

-- 3. Delete orphaned release_group_recordings (recording no longer exists)
DELETE FROM release_group_recordings WHERE recording_id NOT IN (
    SELECT id FROM recordings
);

-- 4. Delete orphaned release_groups (no recordings reference them)
DELETE FROM release_groups WHERE id NOT IN (
    SELECT DISTINCT release_group_id FROM release_group_recordings
);

-- 5. Delete orphaned artist_credits (no recordings reference them)
DELETE FROM artist_credit WHERE id NOT IN (
    SELECT DISTINCT artist_credit_id FROM recordings
) AND id NOT IN (
    SELECT DISTINCT album_artist_credit_id FROM release_groups
    WHERE album_artist_credit_id IS NOT NULL
);

-- 6. Delete orphaned artist_credit_artists (credit no longer exists)
DELETE FROM artist_credit_artist WHERE credit_id NOT IN (
    SELECT id FROM artist_credit
);

-- 7. Delete orphaned artists (no credits reference them)
DELETE FROM artists WHERE id NOT IN (
    SELECT DISTINCT artist_id FROM artist_credit_artist
);

-- 8. Delete orphaned genres (no recording_genres reference them)
DELETE FROM genres WHERE id NOT IN (
    SELECT DISTINCT genre_id FROM recording_genres
);

-- 9. Delete orphaned cover_art (no release_groups reference them)
DELETE FROM cover_art WHERE id NOT IN (
    SELECT DISTINCT cover_art_id FROM release_groups
    WHERE cover_art_id IS NOT NULL
);
```

### Pattern 5: Event-Driven Frontend Invalidation
**What:** Backend emits events after CRUD operations; frontend store invalidates caches and re-fetches.
**When to use:** After library add/rename/remove.
**Example:**
```go
// New events for library CRUD
const (
    LibraryAdded   = "LibraryAdded"
    LibraryRenamed = "LibraryRenamed"
    LibraryRemoved = "LibraryRemoved"
)
```

### Anti-Patterns to Avoid
- **Deleting audio_files before populating phantom metadata:** The SET NULL cascade on playlist_tracks fires immediately when audio_files are deleted. Phantom columns MUST be populated first, in the same transaction.
- **Running orphan cleanup outside a transaction:** If the app crashes mid-cleanup, the database would be in an inconsistent state. All deletes must be in one transaction (except FTS5 rebuild).
- **Using `NOT EXISTS` subqueries instead of `NOT IN`:** For this use case, both work, but `NOT IN (SELECT DISTINCT ...)` is simpler to read and performs well on SQLite's optimizer with indexed columns.
- **Deleting cover art files inside the transaction:** File I/O should happen after the transaction commits. Collect orphaned cover art file paths, commit the DB changes, then delete files.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| FTS5 per-row deletion | Custom contentless_delete migration | `RebuildSearchIndex()` after removal | Rebuild is already implemented, tested, and handles edge cases. Removal is rare enough that full rebuild is acceptable. |
| Folder picker dialog | Custom file browser | `frontendutil.DirectoryPicker()` | Already implemented, uses native OS dialog via Wails runtime |
| Toast notifications | Third-party library | Simple custom element with CSS transition | Two states (show/hide), auto-dismiss timer, no external dependency needed |
| Unique name validation | Frontend-only check | Backend `GetLibraryByName` query + frontend error display | Backend must enforce uniqueness regardless of frontend validation |

**Key insight:** The orphan cleanup SQL is the only truly novel code in this phase. Everything else composes existing infrastructure (scan pipeline, events, sqlc queries, Wails dialogs).

## Common Pitfalls

### Pitfall 1: Phantom Metadata Must Be Populated Before DELETE
**What goes wrong:** If audio_files rows are deleted first, the SET NULL cascade fires on playlist_tracks.audio_file_id, and the JOIN to populate phantom columns finds no matching audio_files — phantom columns stay NULL forever.
**Why it happens:** SQLite fires ON DELETE SET NULL immediately when the parent row is deleted, before any other statements in the transaction run.
**How to avoid:** Always run the phantom population UPDATE before the DELETE FROM audio_files.
**Warning signs:** Playlist tracks showing empty metadata after library removal.

### Pitfall 2: Queue Position/Shuffle Order Desync After CASCADE Delete
**What goes wrong:** Queue tracks are cascade-deleted, but the queue's `current_position` and `shuffle_order` JSON still reference the old positions. The player tries to play a non-existent position.
**Why it happens:** CASCADE only deletes rows; it doesn't update the queue state table.
**How to avoid:** Before removing the library, count queue items that will be deleted. After removal, recalculate queue positions (compact remaining tracks) and reset `current_position` to 0 or the next valid track. Clear `shuffle_order` (will be regenerated on next shuffle toggle).
**Warning signs:** "Track not found" errors after library removal, player crashes.

### Pitfall 3: Artist Credits Referenced by Both Recordings AND Release Groups
**What goes wrong:** An artist_credit is deleted because no recordings reference it, but a release_group still uses it as `album_artist_credit_id`. The release_group now has a dangling FK.
**Why it happens:** artist_credit is referenced from TWO tables: recordings.artist_credit_id and release_groups.album_artist_credit_id.
**How to avoid:** The orphan cleanup for artist_credit must check BOTH tables: `NOT IN (SELECT artist_credit_id FROM recordings) AND NOT IN (SELECT album_artist_credit_id FROM release_groups WHERE ...)`.
**Warning signs:** FK constraint violations during cleanup.

### Pitfall 4: Scan-While-Remove Race Condition
**What goes wrong:** A scan is running for a library while the user tries to remove it. The scan writes new tracks while the removal deletes them, causing unpredictable state.
**Why it happens:** Scan and CRUD operations are not serialized.
**How to avoid:** Before removing a library, cancel any active scan for that library and wait for it to complete. Check `currentScanLibraryID` and also remove the library from the scan queue.
**Warning signs:** Partial data after removal, orphaned tracks.

### Pitfall 5: Cover Art File Deletion Inside Transaction
**What goes wrong:** Cover art files are deleted from disk inside the transaction. If the transaction rolls back, the files are gone but the DB still references them.
**Why it happens:** File I/O is not transactional.
**How to avoid:** Collect orphaned cover art file paths during the transaction, commit, then delete files. If file deletion fails, it's a minor leak (orphaned files), not data corruption.
**Warning signs:** Broken cover art images after a failed removal.

### Pitfall 6: Currently-Playing Track From Removed Library
**What goes wrong:** The player holds a reference to a file path from the removed library. After removal, the player tries to seek or read from a track whose DB entry is gone.
**Why it happens:** The player streams from a file handle, not from the DB. But metadata lookups and queue state depend on the DB.
**How to avoid:** Before the removal transaction, check if the currently-playing track belongs to the target library. If so, stop playback and unload the track.
**Warning signs:** Player errors or crashes after library removal.

## Code Examples

### Adding a Library (Backend)
```go
// Source: Derived from existing CreateLibrary query + ScanLibrary pattern
func (l *Library) AddLibrary(path string) (*sqlcgen.Library, error) {
    // Validate path exists
    if _, err := os.Stat(path); err != nil {
        return nil, fmt.Errorf("directory does not exist: %w", err)
    }

    // Auto-name from folder
    name := filepath.Base(path)

    // Create in DB (path has UNIQUE constraint — handles duplicate paths)
    lib, err := l.db.Queries.CreateLibrary(l.ctx, sqlcgen.CreateLibraryParams{
        Name: name,
        Path: path,
    })
    if err != nil {
        return nil, fmt.Errorf("could not create library: %w", err)
    }

    // Emit event for frontend
    runtime.EventsEmit(l.ctx, events.LibraryAdded, lib)

    // Start scanning (async, via scan queue)
    go func() {
        if err := l.ScanLibrary(lib.ID); err != nil {
            l.logger.Error("auto-scan after add failed", "err", err)
        }
    }()

    return &lib, nil
}
```

### Renaming a Library (Backend)
```go
// Source: Derived from existing UpdateLibraryName query
func (l *Library) RenameLibrary(id int64, newName string) error {
    newName = strings.TrimSpace(newName)
    if newName == "" || len(newName) > 50 {
        return fmt.Errorf("name must be 1-50 characters")
    }

    // Check uniqueness (could also rely on a UNIQUE constraint on name)
    libs, err := l.db.Queries.GetAllLibraries(l.ctx)
    if err != nil {
        return fmt.Errorf("could not check existing names: %w", err)
    }
    for _, lib := range libs {
        if lib.ID != id && lib.Name == newName {
            return fmt.Errorf("a library named %q already exists", newName)
        }
    }

    if err := l.db.Queries.UpdateLibraryName(l.ctx, sqlcgen.UpdateLibraryNameParams{
        Name: newName,
        ID:   id,
    }); err != nil {
        return fmt.Errorf("could not rename library: %w", err)
    }

    runtime.EventsEmit(l.ctx, events.LibraryRenamed, map[string]any{
        "id":   id,
        "name": newName,
    })

    return nil
}
```

### Frontend Toast Component (Simple Approach)
```typescript
// A minimal toast notification — no external dependencies.
// Show via: showToast("Removed 'Jazz Collection' (1,234 tracks deleted)")
let toastEl: HTMLElement | null = null;
let toastTimer: ReturnType<typeof setTimeout> | null = null;

function showToast(message: string, durationMs = 4000): void {
    if (!toastEl) {
        toastEl = document.createElement('div');
        toastEl.className = 'yj-toast';
        document.body.appendChild(toastEl);
    }
    toastEl.textContent = message;
    toastEl.classList.add('visible');

    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(() => {
        toastEl?.classList.remove('visible');
    }, durationMs);
}
```

### Library List Row (Frontend Pattern)
```typescript
// Each row: name | path | track count | overflow menu
private renderLibraryRow(lib: LibraryInfo) {
    return html`
        <div class="library-row">
            <span class="library-name"
                @dblclick=${() => this.startRename(lib.id)}
            >${lib.name}</span>
            <span class="library-path">${lib.path}</span>
            <span class="library-count">${lib.trackCount} tracks</span>
            <button class="overflow-menu" @click=${(e: Event) => this.showMenu(e, lib)}>
                ···
            </button>
        </div>
    `;
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Single directory path in TOML | Multi-library in SQLite | Phase 10 (migration 6) | Library management is now DB-driven, not config-file-driven |
| Single `Scan()` entry point | `ScanLibrary(id)` + scan queue | Phase 11 | Per-library scanning with queue coordination |
| `library-manager` component (standalone) | Library section in settings page | Phase 12 (this phase) | Unified settings experience |
| Old config-page library directory picker | Library list with CRUD | Phase 12 (this phase) | Full multi-library management |

**Deprecated/outdated:**
- `library-manager` component: Will be replaced by the new library section in config-page
- `GetLibraryDirectory()` / `SetLibraryDirectory()` config methods: No longer needed — libraries are managed via DB CRUD
- `Scan()` legacy wrapper: Already deleted in Phase 11 (referenced only by old library-manager)
- Sidebar "Libraries" nav item: Removed per user decision — library management moves to settings

## Open Questions

1. **Cover art file cleanup strategy**
   - What we know: Orphaned cover_art DB rows can be identified. Corresponding files on disk need cleanup.
   - What's unclear: Whether to delete cover art files immediately after the removal transaction, or batch them in a background task.
   - Recommendation: Delete immediately after transaction commit. Collect file paths during the transaction, delete after commit. If deletion fails, log a warning but don't fail the operation. Cover art files are small and few.

2. **Queue state after cascade delete**
   - What we know: `queue_tracks` rows are cascade-deleted. The `queue` table's `current_position` and `shuffle_order` may reference invalid positions.
   - What's unclear: Exact queue compaction logic needed.
   - Recommendation: After removal, call a queue method that re-compacts positions (renumber 0..N-1) and resets `current_position` to 0 if the current track was removed, or adjusts it to the correct new position. Clear `shuffle_order` (it will be regenerated). Emit QueueChanged event.

3. **Library name uniqueness enforcement**
   - What we know: User decision requires unique names. The `libraries` table currently has UNIQUE on `path` but not on `name`.
   - What's unclear: Whether to add a UNIQUE constraint via migration 7 or enforce in application code.
   - Recommendation: Enforce in application code (check before insert/rename). Adding a UNIQUE index via ALTER TABLE is simple but creates a migration. Given the low frequency of library operations, application-level validation is sufficient and avoids a schema change.

## Sources

### Primary (HIGH confidence)
- Codebase analysis: `backend/database/database.go` (migration patterns, transaction handling)
- Codebase analysis: `backend/database/search.go` (FTS5 operations, RebuildSearchIndex)
- Codebase analysis: `backend/library/library.go` (scan pipeline, entity processing)
- Codebase analysis: `backend/library/rescan.go` (clearLibraryTables — reference for cleanup order)
- Codebase analysis: `backend/library/scan_queue.go` (ScanLibrary, queue coordination)
- Codebase analysis: `backend/database/sql/queries/libraries.sql` (existing CRUD queries)
- Codebase analysis: `backend/database/sql/schemas/*.sql` (all table schemas, FK relationships)
- Codebase analysis: `frontend/src/components/config-page/config-page.ts` (settings page structure)
- Codebase analysis: `frontend/src/components/library-manager/library-manager.ts` (existing library UI)
- Codebase analysis: `frontend/src/store/library-store.ts` (data caching, invalidation)
- Codebase analysis: `frontend/src/components/sidebar/app-sidebar.ts` (nav items including 'libraries')
- `.planning/research/ARCHITECTURE.md` (hybrid model decisions, orphan cleanup strategy)
- `.planning/research/PITFALLS.md` (P3: FTS5 contentless, P4: orphan cleanup complexity, P9: queue/now-playing during removal)

### Secondary (MEDIUM confidence)
- SQLite documentation on contentless FTS5 tables (content='') — DELETE not supported, rebuild required
- SQLite documentation on ON DELETE SET NULL and ON DELETE CASCADE behavior within transactions

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Zero new dependencies, all existing infrastructure
- Architecture: HIGH - Direct extension of existing patterns (clearLibraryTables, scan queue, event system)
- Pitfalls: HIGH - Directly verified against codebase FK relationships and existing code patterns

**Research date:** 2026-03-12
**Valid until:** 2026-04-12 (stable — no external dependencies to age)
