# Plan: Track List FTS Search (#1) & Genre Details Query (#4)

## Feature #1: Track List FTS Search

### Goal

When the user types in the track list search bar, delegate to the backend
FTS5 index instead of filtering all tracks in-memory in JavaScript.
Backend-only search with debounce. FTS5 index stays as-is (title, artist,
album, file_path — no expansion).

### Current flow

1. All tracks fetched once via `Library.GetAllTracks()` → cached in
   `libraryStore`
2. On each keystroke, `computeFilteredTracks()` in `track-list.ts` runs
   `toLowerCase().includes(term)` across every track's active columns
3. Virtual scrolling renders only visible rows

### Proposed flow

1. All tracks still fetched and cached (needed for empty-search display,
   sorting, column rendering)
2. When search term is non-empty, call new backend method
   `Library.SearchTracks(query)` which uses FTS5 internally
3. Backend returns `[]library.Track` (same 16-field type as `GetAllTracks`)
4. Frontend uses these results directly instead of client-side filtering
5. Frontend debounces the backend call (~200-250ms) to avoid excessive
   round-trips on fast typing

### Backend changes

#### 1. `backend/database/search.go` — New method `SearchFTSTracks`

Add `SearchFTSTracks(query string, limit int)` method on `*DB`.

- Uses `buildFTSQuery(query)` to tokenise the user input
- Runs FTS5 MATCH against `search_index`
- JOINs to all the same tables as `GetAllTracksWithFullMetadata`:
  `audio_files`, `recordings`, `artist_credit`, `release_group_recordings`,
  `release_groups`, `file_types`
- Includes the `GROUP_CONCAT` subquery for genres
- Returns all 16 columns needed for `library.Track`
- Returns a new `SearchTrackRow` struct (or reuse generated types if
  practical)

Query shape:

```sql
SELECT
    af.file_path,
    af.length_milliseconds,
    COALESCE(r.name, '')              AS title,
    COALESCE(ac.text, '')             AS artist_name,
    r.track_number,
    r.disc_number,
    COALESCE(rg.name, '')             AS album,
    CAST(COALESCE(
        (SELECT GROUP_CONCAT(g.name, '||')
         FROM recording_genres rg_sub
         JOIN genres g ON rg_sub.genre_id = g.id
         WHERE rg_sub.recording_id = r.id),
        ''
    ) AS TEXT)                        AS genre,
    COALESCE(r.year, 0)               AS year,
    COALESCE(r.composer, '')           AS composer,
    COALESCE(ft.extension, '')         AS file_type,
    af.sample_rate,
    af.bit_depth,
    af.channels,
    af.bitrate,
    af.file_size
FROM search_index si
JOIN audio_files af        ON af.id = si.rowid
JOIN recordings r          ON af.recording_id = r.id
JOIN artist_credit ac      ON r.artist_credit_id = ac.id
LEFT JOIN release_group_recordings rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rg            ON rgr.release_group_id = rg.id
LEFT JOIN file_types ft                ON af.file_type_id = ft.id
WHERE search_index MATCH ?
ORDER BY rank
LIMIT ?
```

Define a `SearchTrackRow` struct with all 16 fields (using `sql.NullInt64`
for track_number, disc_number, year; `sql.NullString` for composer).

#### 2. `backend/library/query.go` — New Wails-bound method `SearchTracks`

```go
func (l *Library) SearchTracks(query string) ([]Track, error)
```

- Calls `l.db.SearchFTSTracks(query, 200)` (cap at 200 results)
- Maps each `SearchTrackRow` to `library.Track` using the same logic as
  `GetAllTracks` (splitGenres, NullInt64 unwrap, etc.)
- Reuse or extract common row-mapping into a shared helper to avoid
  duplication with `GetAllTracks`

### Frontend changes

#### 3. `frontend/src/store/library-store.ts` — Add search method + state

Add to `LibraryStore`:

- `async searchTracks(query: string): Promise<library.Track[]>` — calls
  the Wails-bound `Library.SearchTracks(query)` and returns results
- Clear any cached search results on `invalidate()` (library scan)

#### 4. `frontend/src/components/track-list/track-list.ts` — Switch to backend search

Changes to the search flow:

- Remove `computeFilteredTracks()` (the in-memory filter)
- Add `@state() private searchResults: library.Track[] | null = null`
- Add `@state() private searchLoading = false`
- Add a debounced method `debouncedSearch(term: string)` (~200ms) that:
  - If term is empty → sets `searchResults = null` (show all tracks)
  - Otherwise → calls `libraryStore.searchTracks(term)`, stores results in
    `searchResults`
- In `recomputeTrackCaches()` (or `willUpdate`): if `searchResults` is
  non-null, use it as the filtered track set; otherwise use `this.tracks`
- Trigger `debouncedSearch` from the `SearchController` when the term
  changes
- The sort step (`computeSortedTracks`) still runs on the filtered set

#### 5. Wails bindings — Auto-regenerated

After adding the Go method, run `wails generate` (or `make dev` / build)
to regenerate `frontend/wailsjs/go/library/Library.js` and `.d.ts`.

---

## Feature #4: Genre Details Query

### Goal

Replace the fetch-all-then-filter pattern in `genre-details.ts` with a
dedicated SQL query. Also add a `GetAllGenresWithCounts` query to eliminate
the other fetch-all-tracks dependency in `genres-view.ts`.

### Current flow (genre details)

1. `genre-details.ts` calls `libraryCtrl.getTracks()` → fetches ALL tracks
2. Filters in JS: `tracks.filter(t => t.Genre.includes(genreName))`

### Proposed flow (genre details)

1. `genre-details.ts` calls new `Library.GetTracksByGenre(genreName)`
2. Backend runs a JOIN query filtered by genre name
3. Returns `[]library.Track` — same 16-field type

### Current flow (genre list)

1. `genres-view.ts` calls `libraryCtrl.getTracks()` → fetches ALL tracks
2. `extractGenres()` iterates every track, counts genre occurrences,
   returns sorted `Genre[]`

### Proposed flow (genre list)

1. `genres-view.ts` calls new `Library.GetAllGenresWithCounts()`
2. Backend runs a simple GROUP BY query
3. Returns `[]GenreWithCount` (name + track count)

### Backend changes

#### 6. `backend/database/sql/queries/genres.sql` — Two new sqlc queries

**Query 1: `GetTracksByGenre`**

```sql
-- name: GetTracksByGenre :many
SELECT
    af.file_path,
    af.length_milliseconds,
    COALESCE(r.name, '')              AS title,
    COALESCE(ac.text, '')             AS artist_name,
    r.track_number,
    r.disc_number,
    COALESCE(rg.name, '')             AS album,
    CAST(COALESCE(
        (SELECT GROUP_CONCAT(g2.name, '||')
         FROM recording_genres rg2
         JOIN genres g2 ON rg2.genre_id = g2.id
         WHERE rg2.recording_id = r.id),
        ''
    ) AS TEXT)                        AS genre,
    COALESCE(r.year, 0)               AS year,
    COALESCE(r.composer, '')           AS composer,
    COALESCE(ft.extension, '')         AS file_type,
    af.sample_rate,
    af.bit_depth,
    af.channels,
    af.bitrate,
    af.file_size
FROM genres g
JOIN recording_genres rg       ON g.id = rg.genre_id
JOIN recordings r              ON rg.recording_id = r.id
JOIN audio_files af            ON af.recording_id = r.id
JOIN artist_credit ac          ON r.artist_credit_id = ac.id
LEFT JOIN release_group_recordings rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rlg           ON rgr.release_group_id = rlg.id
LEFT JOIN file_types ft                ON af.file_type_id = ft.id
WHERE g.name = ?
ORDER BY r.name;
```

Uses `idx_recording_genres_genre_id` for the initial genre lookup.

**Query 2: `GetAllGenresWithCounts`**

```sql
-- name: GetAllGenresWithCounts :many
SELECT g.name, COUNT(rg.recording_id) AS track_count
FROM genres g
JOIN recording_genres rg ON g.id = rg.genre_id
GROUP BY g.id, g.name
ORDER BY g.name;
```

#### 7. `backend/library/query.go` — Two new Wails-bound methods

**Method 1: `GetTracksByGenre`**

```go
func (l *Library) GetTracksByGenre(genreName string) ([]Track, error)
```

- Calls the sqlc-generated `l.db.Queries.GetTracksByGenre(ctx, genreName)`
- Maps rows to `[]Track` using the same row-mapping helper as
  `GetAllTracks` and `SearchTracks`

**Method 2: `GetAllGenresWithCounts`**

```go
type GenreWithCount struct {
    Name       string `json:"Name"`
    TrackCount int64  `json:"TrackCount"`
}

func (l *Library) GetAllGenresWithCounts() ([]GenreWithCount, error)
```

- Calls the sqlc-generated
  `l.db.Queries.GetAllGenresWithCounts(ctx)`
- Maps rows to `[]GenreWithCount`

#### 8. Run `make generate` to regenerate sqlc output

After adding the queries to `genres.sql`, run `make generate` to produce
the Go types and query methods in `backend/database/sql/sqlcgen/`.

### Frontend changes

#### 9. `frontend/src/components/genre-details/genre-details.ts` — Use new endpoint

Replace `loadTracks()`:

```typescript
private async loadTracks() {
    if (!this.genreName) return;
    try {
        this.tracks = await GetTracksByGenre(this.genreName);
    } catch (error) {
        console.error('Error loading genre tracks:', error);
        this.tracks = [];
    } finally {
        this.loading = false;
    }
}
```

- Import `GetTracksByGenre` from `@go/library/Library`
- Remove `libraryCtrl.getTracks()` call and in-memory filter
- Remove the `lastTracksRef` cache-invalidation pattern (no longer
  needed — each call fetches fresh data for the specific genre)
- Still listen for `LibraryScanComplete` to re-trigger `loadTracks()`
  if the genre details view is open during a rescan

#### 10. `frontend/src/components/genres-view/genres-view.ts` — Use new endpoint

Replace `loadGenres()`:

- Call `Library.GetAllGenresWithCounts()` instead of fetching all tracks
- Map results directly to the local `Genre[]` array (name + trackCount)
- Remove `extractGenres()` method
- Remove `this.allTracks` state (no longer needed for genre extraction)
- Note: `allTracks` may still be needed for other purposes in the
  component — check if it's used elsewhere (e.g. for passing to
  genre-details). If genre-details fetches its own tracks, this
  dependency chain can be fully removed.

#### 11. Wails bindings — Auto-regenerated

Run `wails generate` to produce the new TypeScript bindings for
`GetTracksByGenre`, `GetAllGenresWithCounts`, and `SearchTracks`.

---

## Shared refactoring: Row-mapping helper

`GetAllTracks`, `SearchTracks`, and `GetTracksByGenre` all map database
rows with the same 16 columns into `library.Track`. Currently this logic
lives inline in `GetAllTracks`. Extract it into a shared helper:

```go
func mapTrackRow(
    filePath string,
    lengthMs int64,
    title, artistName string,
    trackNumber, discNumber sql.NullInt64,
    album, genre string,
    year sql.NullInt64,
    composer, fileType string,
    sampleRate, bitDepth, channels, bitrate, fileSize int64,
) Track
```

This avoids tripling the row-mapping code across three methods.

---

## Implementation order

1. Backend: extract row-mapping helper in `query.go`
2. Backend: add `SearchFTSTracks` to `search.go` + `SearchTracks` to
   `query.go`
3. Backend: add sqlc queries to `genres.sql` + `make generate`
4. Backend: add `GetTracksByGenre` + `GetAllGenresWithCounts` to `query.go`
5. Verify: `make lint && make test`
6. Frontend: update `genre-details.ts` to use `GetTracksByGenre`
7. Frontend: update `genres-view.ts` to use `GetAllGenresWithCounts`
8. Frontend: update `library-store.ts` with `searchTracks` method
9. Frontend: update `track-list.ts` with debounced backend search
10. Verify: `pnpm exec tsc --noEmit`
11. Full verify: `make lint && make test`

---

## Files touched (summary)

| File | Action |
|---|---|
| `backend/database/search.go` | Add `SearchFTSTracks`, `SearchTrackRow` |
| `backend/library/query.go` | Add `SearchTracks`, `GetTracksByGenre`, `GetAllGenresWithCounts`, `GenreWithCount`, extract `mapTrackRow` helper |
| `backend/database/sql/queries/genres.sql` | Add `GetTracksByGenre`, `GetAllGenresWithCounts` |
| `backend/database/sql/sqlcgen/*` | Regenerated via `make generate` |
| `frontend/src/store/library-store.ts` | Add `searchTracks` method |
| `frontend/src/components/track-list/track-list.ts` | Replace in-memory filter with debounced backend FTS search |
| `frontend/src/components/genre-details/genre-details.ts` | Replace fetch-all-then-filter with `GetTracksByGenre` |
| `frontend/src/components/genres-view/genres-view.ts` | Replace `extractGenres` with `GetAllGenresWithCounts` |
| `frontend/wailsjs/go/library/Library.js` + `.d.ts` | Auto-regenerated |
