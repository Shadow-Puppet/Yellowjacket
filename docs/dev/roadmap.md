# Yellowjacket Development Roadmap

This document outlines the phased development plan for Yellowjacket, from current state to a fully-featured desktop music player.

## Project Vision

Yellowjacket aims to be a modern, cross-platform desktop music player inspired by MusicBee, featuring:
- High performance with large libraries (50,000+ tracks)
- MusicBrainz-powered metadata autotagging
- Device syncing with re-encoding
- Highly customizable UI with arrangeable components

## Current State

As of the start of this roadmap:
- Basic playback working (MP3, FLAC, OGG, WAV)
- Library scanning with metadata extraction
- Track list and album grid views
- Play/pause, seek, volume (backend)
- Configuration page

---

## Cross-Cutting Concern: Configuration/Settings Infrastructure

**IMPORTANT:** Settings and configuration should be considered at the forefront of every feature. Each new feature should have its configurable options designed alongside the feature itself, not bolted on afterwards.

### Settings Architecture Overview

The application needs a unified settings system that:
1. Stores preferences persistently (TOML config file on backend)
2. Exposes settings to both backend and frontend
3. Allows components to register their own settings sections
4. Provides a consistent UI for editing settings

### Backend Settings Infrastructure

#### Config Package Structure

The existing `backend/config/` package should be extended to support:

```go
// backend/config/config.go

type Config struct {
    Library  LibraryConfig  `toml:"library"`
    Player   PlayerConfig   `toml:"player"`
    Queue    QueueConfig    `toml:"queue"`
    UI       UIConfig       `toml:"ui"`
    // New sections added as features are built
}

type PlayerConfig struct {
    DefaultVolume     int    `toml:"default_volume"`      // 0-100
    ResumePlayback    bool   `toml:"resume_playback"`     // Resume on startup
    CrossfadeSeconds  int    `toml:"crossfade_seconds"`   // 0 = disabled
    ReplayGain        string `toml:"replay_gain"`         // "off", "track", "album"
}

type QueueConfig struct {
    RememberQueue     bool   `toml:"remember_queue"`      // Persist queue across sessions
    DefaultRepeat     string `toml:"default_repeat"`      // "none", "all", "one"
    DefaultShuffle    bool   `toml:"default_shuffle"`
}

type UIConfig struct {
    Theme             string            `toml:"theme"`
    SidebarWidth      int               `toml:"sidebar_width"`
    Layout            string            `toml:"layout_preset"`
    ColumnConfig      map[string][]string `toml:"column_config"`  // Per-view column selection
}
```

#### Config Change Notification

When config values change, components need to be notified:

```go
// Config change callback pattern (already exists for Library)
type ConfigSection interface {
    OnConfigChanged(newConfig any) error
}

// Or use events
runtime.EventsEmit(ctx, events.ConfigChanged, map[string]any{
    "section": "player",
    "key":     "default_volume", 
    "value":   75,
})
```

### Frontend Settings Infrastructure

#### SettingsStore

```typescript
// frontend/src/store/settings-store.ts

interface SettingsState {
    player: PlayerSettings;
    queue: QueueSettings;
    ui: UISettings;
    library: LibrarySettings;
    // Extensible for new features
}

class SettingsStore {
    private state: SettingsState;
    
    // Load all settings from backend on startup
    async initialize(): Promise<void>;
    
    // Get a specific setting
    get<T>(section: string, key: string): T;
    
    // Update a setting (persists to backend)
    async set(section: string, key: string, value: any): Promise<void>;
    
    // Subscribe to changes
    subscribe(callback: () => void): () => void;
}
```

#### Settings UI Component Architecture

Each feature's settings should be encapsulated in a dedicated component:

```typescript
// Pattern for settings sub-panels
interface SettingsPanel {
    id: string;           // e.g., "player-settings"
    title: string;        // e.g., "Playback"
    icon: string;         // Icon for settings nav
    component: typeof LitElement;  // The settings panel component
    order: number;        // Display order in settings nav
}

// Registry for settings panels
class SettingsRegistry {
    register(panel: SettingsPanel): void;
    getAll(): SettingsPanel[];
}
```

#### Unified Settings Window

```typescript
// frontend/src/components/settings/settings-window.ts

@customElement('settings-window')
class SettingsWindow extends LitElement {
    // Left sidebar: list of settings sections
    // Right panel: active settings section component
    // Each section component handles its own settings
}
```

### Settings Design Checklist for New Features

When implementing any new feature, consider:

1. **What user preferences exist?**
   - Default values
   - Behavior toggles
   - Display options

2. **Where should settings be stored?**
   - Backend config (persistent, affects backend behavior)
   - Frontend localStorage (UI-only preferences)
   - Both (synced)

3. **How are settings exposed?**
   - Add to appropriate Config struct section
   - Create settings panel component
   - Register with SettingsRegistry

4. **How do components react to changes?**
   - Subscribe to SettingsStore
   - Handle ConfigChanged events
   - Apply changes immediately vs. on restart

### Settings Infrastructure Tasks (Integrated with Features)

These tasks should be completed early and extended as features are added:

#### Task: Extend backend Config structure

As each feature is built, add its configuration section to `backend/config/config.go`. Follow the existing pattern used for `LibraryConfig`.

#### Task: Create SettingsStore on frontend

Create `frontend/src/store/settings-store.ts` following the same pattern as PlayerStore. Load settings from backend on app startup.

#### Task: Create settings panel registry

Create `frontend/src/registry/settings-registry.ts` to allow features to register their settings panels.

#### Task: Create unified settings window component

Create `frontend/src/components/settings/settings-window.ts` that:
- Shows navigation sidebar with all registered settings panels
- Renders the active panel
- Handles save/cancel/apply actions

#### Task: Migrate existing config page

The current HTMX-based config page (`/config`) should be migrated to use the new settings infrastructure, becoming the "Library" settings panel.

---

## Roadmap Phases

---

## Phase 1: Core Playback Experience

**Goal:** Complete the fundamental playback features that users expect from a music player.

**Settings to consider for this phase:**
- Default volume level
- Resume playback on startup (remember last track/position)
- Remember queue across sessions
- Default shuffle/repeat modes
- "Previous" button behavior (restart threshold in seconds)

### 1.1 Playlist System (Go Backend)

Implement a playlist system in the Go backend. The "Now Playing" queue is a special transient playlist that coordinates with the Player. All playlists (including the queue) share the same underlying data structures and operations.

**Key insight:** The queue is simply a playlist with special behavior:
- It's transient (not persisted by default, but optionally can be)
- It's always "active" (connected to the Player)
- It has shuffle/repeat modes that affect playback order

#### Task 1.1.1: Create playlist package structure

Create `backend/playlist/` package with the following files:
- `playlist.go` - Playlist struct and methods
- `queue.go` - Queue (active playlist) with Player integration
- `storage.go` - Persistence for saved playlists

#### Task 1.1.2: Design playlist database schema

Add to `backend/database/sql/schemas/`:

```sql
-- playlists.sql
CREATE TABLE IF NOT EXISTS playlists (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    is_smart BOOLEAN NOT NULL DEFAULT false,
    smart_rules TEXT,  -- JSON for smart playlist rules
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- playlist_tracks.sql
CREATE TABLE IF NOT EXISTS playlist_tracks (
    id INTEGER PRIMARY KEY,
    playlist_id INTEGER NOT NULL,
    audio_file_id INTEGER NOT NULL,
    position INTEGER NOT NULL,
    added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(playlist_id) REFERENCES playlists(id) ON DELETE CASCADE,
    FOREIGN KEY(audio_file_id) REFERENCES audio_files(id) ON DELETE CASCADE
);

CREATE INDEX idx_playlist_tracks_playlist ON playlist_tracks(playlist_id);
CREATE INDEX idx_playlist_tracks_position ON playlist_tracks(playlist_id, position);
```

#### Task 1.1.3: Define Playlist data structures

```go
// backend/playlist/playlist.go

type PlaylistTrack struct {
    ID          int64
    FilePath    string
    FileName    string
    Title       string
    Artist      string
    Album       string
    TrackLength int64  // milliseconds
    Position    int    // Position in playlist
}

type Playlist struct {
    ID          int64
    Name        string
    Description string
    IsSmart     bool
    SmartRules  string  // JSON
    Tracks      []PlaylistTrack
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

#### Task 1.1.4: Define Queue (Active Playlist) data structures

```go
// backend/playlist/queue.go

type RepeatMode string
const (
    RepeatNone RepeatMode = "none"
    RepeatAll  RepeatMode = "all"  
    RepeatOne  RepeatMode = "one"
)

// Queue is the "Now Playing" playlist - always exactly one exists
type Queue struct {
    ctx            context.Context
    logger         *slog.Logger
    db             *database.DB
    player         *player.Player
    
    tracks         []PlaylistTrack
    currentIndex   int
    originalOrder  []PlaylistTrack  // For unshuffle restoration
    shuffleOrder   []int            // Shuffled indices
    
    shuffleEnabled bool
    repeatMode     RepeatMode
    
    // Config-driven settings
    rememberQueue  bool  // Persist queue across sessions
}
```

#### Task 1.1.5: Implement Playlist CRUD operations

```go
// backend/playlist/storage.go

func (s *Storage) CreatePlaylist(name, description string) (*Playlist, error)
func (s *Storage) GetPlaylist(id int64) (*Playlist, error)
func (s *Storage) GetAllPlaylists() ([]Playlist, error)
func (s *Storage) UpdatePlaylist(playlist *Playlist) error
func (s *Storage) DeletePlaylist(id int64) error

func (s *Storage) AddTracksToPlaylist(playlistID int64, trackIDs []int64) error
func (s *Storage) RemoveTrackFromPlaylist(playlistID int64, position int) error
func (s *Storage) ReorderPlaylistTrack(playlistID int64, fromPos, toPos int) error
func (s *Storage) GetPlaylistTracks(playlistID int64) ([]PlaylistTrack, error)
```

#### Task 1.1.6: Implement playlist CRUD SQL queries

Add to `backend/database/sql/queries/playlists.sql`:

```sql
-- name: CreatePlaylist :one
INSERT INTO playlists (name, description, is_smart, smart_rules) 
VALUES (?, ?, ?, ?) RETURNING *;

-- name: GetPlaylist :one
SELECT * FROM playlists WHERE id = ?;

-- name: GetAllPlaylists :many
SELECT * FROM playlists WHERE is_smart = false ORDER BY name;

-- name: UpdatePlaylist :exec
UPDATE playlists SET name = ?, description = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: DeletePlaylist :exec
DELETE FROM playlists WHERE id = ?;

-- name: GetPlaylistTracks :many
SELECT 
    af.id, af.file_path, af.length_milliseconds,
    r.name as title, 
    COALESCE(ac.text, '') as artist,
    COALESCE(rg.name, '') as album,
    pt.position
FROM playlist_tracks pt
JOIN audio_files af ON pt.audio_file_id = af.id
JOIN recordings r ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN release_group_recordings rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
WHERE pt.playlist_id = ?
ORDER BY pt.position;

-- name: AddTrackToPlaylist :exec
INSERT INTO playlist_tracks (playlist_id, audio_file_id, position)
VALUES (?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM playlist_tracks WHERE playlist_id = ?));

-- name: RemoveTrackFromPlaylist :exec
DELETE FROM playlist_tracks WHERE playlist_id = ? AND position = ?;

-- name: ReorderPlaylistTracks :exec
UPDATE playlist_tracks SET position = ? WHERE playlist_id = ? AND audio_file_id = ?;
```

#### Task 1.1.7: Implement Queue constructor

```go
func NewQueue(ctx context.Context, logger *slog.Logger, db *database.DB, player *player.Player, config *config.QueueConfig) *Queue
```

- Initialize with empty tracks slice
- Set currentIndex to -1 (nothing playing)
- Load shuffle/repeat defaults from config
- Store reference to Player for playback control
- If `config.RememberQueue` is true, load persisted queue from DB

#### Task 1.1.8: Implement queue manipulation methods

```go
// Set the entire queue (e.g., when user clicks "Play All" or clicks a track)
func (q *Queue) Set(tracks []PlaylistTrack, startIndex int)

// Add tracks to end of queue
func (q *Queue) Add(tracks ...PlaylistTrack)

// Insert tracks at specific position  
func (q *Queue) InsertAt(index int, tracks ...PlaylistTrack)

// Remove track at index
func (q *Queue) Remove(index int)

// Clear entire queue
func (q *Queue) Clear()

// Move track from one position to another (for drag-and-drop reordering)
func (q *Queue) Move(fromIndex, toIndex int)

// Load from a saved playlist
func (q *Queue) LoadPlaylist(playlistID int64) error

// Save current queue as a new playlist
func (q *Queue) SaveAsPlaylist(name string) (*Playlist, error)
```

#### Task 1.1.9: Implement playback control methods

```go
// Play track at current index
func (q *Queue) PlayCurrent() error

// Skip to next track (respects shuffle and repeat modes)
func (q *Queue) Next() error

// Skip to previous track  
func (q *Queue) Previous() error

// Jump to specific index in queue
func (q *Queue) PlayAt(index int) error
```

**Logic for Next():**
1. If repeat mode is "one", restart current track
2. If shuffle enabled, pick next from shuffle order
3. Otherwise, increment currentIndex
4. If at end of queue:
   - If repeat mode is "all", go to index 0
   - Otherwise, stop playback
5. Call `q.player.LoadFile()` and `q.player.Play()`

**Logic for Previous():**
1. If current position > N seconds (configurable, default 3), restart current track
2. Otherwise, go to previous track (respecting shuffle order)
3. If at beginning, stay at index 0

#### Task 1.1.10: Implement shuffle functionality

```go
func (q *Queue) SetShuffle(enabled bool)
```

**When enabling shuffle:**
1. Store current order in `originalOrder`
2. Create shuffled index order (Fisher-Yates shuffle)
3. Keep current track at current position in shuffle order

**When disabling shuffle:**
1. Restore `originalOrder`
2. Find current track's position in original order
3. Set currentIndex to that position

#### Task 1.1.11: Implement repeat functionality

```go
func (q *Queue) SetRepeat(mode RepeatMode)
```

This just sets the mode; the logic is in `Next()`.

#### Task 1.1.12: Implement queue state getters

```go
func (q *Queue) GetTracks() []PlaylistTrack
func (q *Queue) GetCurrentIndex() int
func (q *Queue) GetCurrentTrack() *PlaylistTrack
func (q *Queue) IsShuffleEnabled() bool
func (q *Queue) GetRepeatMode() RepeatMode
func (q *Queue) GetDuration() int64  // Total queue duration in ms
```

#### Task 1.1.13: Register event handlers for queue

```go
func (q *Queue) registerEventHandlers() {
    // Listen for PlaybackFinished to auto-advance
    runtime.EventsOn(q.ctx, events.PlaybackFinished, func(_ ...any) {
        q.Next()
    })
    
    // Listen for frontend requests
    runtime.EventsOn(q.ctx, events.RequestNext, func(_ ...any) {
        q.Next()
    })
    
    runtime.EventsOn(q.ctx, events.RequestPrevious, func(_ ...any) {
        q.Previous()
    })
    
    runtime.EventsOn(q.ctx, events.RequestSetShuffle, func(data ...any) {
        enabled := data[0].(bool)
        q.SetShuffle(enabled)
    })
    
    runtime.EventsOn(q.ctx, events.RequestSetRepeat, func(data ...any) {
        mode := RepeatMode(data[0].(string))
        q.SetRepeat(mode)
    })
}
```

#### Task 1.1.14: Implement queue change event emission

```go
func (q *Queue) emitQueueChanged() {
    runtime.EventsEmit(q.ctx, events.QueueChanged, map[string]any{
        "tracks":       q.tracks,
        "currentIndex": q.currentIndex,
        "shuffle":      q.shuffleEnabled,
        "repeat":       string(q.repeatMode),
    })
}
```

Call this after any queue modification.

#### Task 1.1.15: Add queue and playlist events to events package

Update `backend/events/events.go`:

```go
// Queue events
const (
    QueueChanged       = "QueueChanged"
    RequestNext        = "RequestNext"
    RequestPrevious    = "RequestPrevious"
    RequestSetShuffle  = "RequestSetShuffle"
    RequestSetRepeat   = "RequestSetRepeat"
    RequestAddToQueue  = "RequestAddToQueue"
    RequestClearQueue  = "RequestClearQueue"
)

// Playlist events
const (
    PlaylistsChanged   = "PlaylistsChanged"   // When playlists are created/deleted/renamed
    PlaylistUpdated    = "PlaylistUpdated"    // When a playlist's tracks change
)
```

#### Task 1.1.16: Add events to frontend events.ts

Update `frontend/src/events.ts` to mirror backend events for queue and playlists.

#### Task 1.1.17: Integrate Queue and Playlist Storage into app.go

- Create playlist Storage in `NewYellowJacketApp()`
- Create Queue in `OnStartup()` after Player is created
- Pass Player reference and config to Queue constructor
- Add Queue and playlist Storage to FEBindings
- Ensure Queue's event handlers are registered

#### Task 1.1.18: Update PlayerStore to cache queue state

Update `frontend/src/store/player-store.ts`:

```typescript
interface PlayerState {
    // Existing...
    isPlaying: boolean;
    currentTrack: TrackInfo | null;
    volume: number;
    
    // New queue state
    queue: PlaylistTrack[];
    queueIndex: number;
    shuffleEnabled: boolean;
    repeatMode: 'none' | 'all' | 'one';
}
```

Add event listener for `QueueChanged`.

#### Task 1.1.19: Update PlayerController with queue actions

Add methods to PlayerController:
- `next()`, `previous()`
- `setShuffle(enabled: boolean)`
- `setRepeat(mode: string)`
- `addToQueue(tracks: Track[])`
- `clearQueue()`
- `loadPlaylist(playlistId: number)`
- `saveQueueAsPlaylist(name: string)`

#### Task 1.1.20: Create PlaylistStore for saved playlists

Create `frontend/src/store/playlist-store.ts`:

```typescript
interface PlaylistState {
    playlists: Playlist[];
    loading: boolean;
}

class PlaylistStore {
    // Load all playlists from backend
    async loadPlaylists(): Promise<void>;
    
    // CRUD operations (delegate to backend)
    async createPlaylist(name: string): Promise<Playlist>;
    async deletePlaylist(id: number): Promise<void>;
    async renamePlaylist(id: number, name: string): Promise<void>;
    
    // Track operations
    async addTracksToPlaylist(playlistId: number, trackIds: number[]): Promise<void>;
    async removeTrackFromPlaylist(playlistId: number, position: number): Promise<void>;
}
```

---

### 1.2 Playlist UI Components

#### Task 1.2.1: Create playlist sidebar section

Update `frontend/src/components/sidebar/app-sidebar.ts` or create new component:
- Show list of playlists below navigation
- "New Playlist" button
- Click playlist to view contents
- Right-click for context menu (rename, delete)

#### Task 1.2.2: Create playlist view component

Create `frontend/src/components/playlist/playlist-view.ts`:
- Display tracks in a playlist
- Drag to reorder tracks
- Remove track button  
- Play all / shuffle play buttons
- Edit playlist name/description

#### Task 1.2.3: Add "Add to Playlist" context menu

Create reusable context menu component:
- Right-click track -> Add to Playlist -> [list of playlists]
- Option to create new playlist

#### Task 1.2.4: Create "Now Playing" queue panel

Create `frontend/src/components/queue/queue-panel.ts`:
- Shows current queue
- Highlights currently playing track
- Drag to reorder
- Remove tracks
- Clear queue button
- Save as playlist button

---

### 1.3 Skip Next/Previous

#### Task 1.3.1: Add skip buttons to player-controls component

Update `frontend/src/components/audio-player/controls/player-controls.ts`:

- Add "previous" button (calls `controller.previous()`)
- Add "next" button (calls `controller.next()`)
- Use appropriate icons from webawesome

#### Task 1.3.2: Style skip buttons

Ensure buttons match existing play/pause styling.

---

### 1.4 Shuffle & Repeat Modes

#### Task 1.4.1: Add shuffle toggle to player-controls

- Add shuffle button that toggles `controller.setShuffle(!current)`
- Visual indicator when shuffle is enabled (icon color change or background)

#### Task 1.4.2: Add repeat toggle to player-controls

- Add repeat button that cycles through modes: none -> all -> one -> none
- Different icon or indicator for each mode:
  - none: repeat icon, dimmed
  - all: repeat icon, highlighted
  - one: repeat-one icon, highlighted

---

### 1.5 Keyboard Shortcuts

#### Task 1.5.1: Create keyboard shortcut handler

Create `frontend/src/utils/keyboard-shortcuts.ts`:

```typescript
import { playerStore } from '@store/player-store';

export function initKeyboardShortcuts() {
    document.addEventListener('keydown', (e) => {
        // Don't trigger if user is typing in an input
        if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) {
            return;
        }
        
        switch (e.code) {
            case 'Space':
                e.preventDefault();
                playerStore.getState().isPlaying ? playerStore.pause() : playerStore.play();
                break;
            case 'ArrowRight':
                if (e.ctrlKey || e.metaKey) {
                    playerStore.next();
                }
                break;
            case 'ArrowLeft':
                if (e.ctrlKey || e.metaKey) {
                    playerStore.previous();
                }
                break;
            // Add more shortcuts as needed
        }
    });
}
```

#### Task 1.5.2: Initialize shortcuts in index.ts

Call `initKeyboardShortcuts()` in `frontend/index.ts`.

#### Task 1.5.3: Document keyboard shortcuts

Consider adding a help modal or tooltip showing available shortcuts.

---

### 1.6 Volume UI

#### Task 1.6.1: Verify backend volume support

Check that `Player.SetVolume()` is working and exposed via FEBindings or events.

If not exposed via events, add:
- `RequestSetVolume` event in `backend/events/events.go`
- Event handler in Player that calls `SetVolume()`
- Emit `VolumeChanged` event after volume changes

#### Task 1.6.2: Implement volume-control component

Update `frontend/src/components/audio-player/volume-control/volume-control.ts`:

- Add PlayerController
- Render slider from 0-100
- Display current volume from `controller.volume`
- On change, call `controller.setVolume(value)`

#### Task 1.6.3: Add mute toggle

- Add mute button that sets volume to 0 (store previous volume)
- Click again to restore previous volume
- Icon changes based on volume level (muted, low, medium, high)

---

## Phase 2: Performance & Scale

**Goal:** Optimize the application for large music libraries (50,000+ tracks).

**Settings to consider for this phase:**
- Page size for virtualized lists
- Prefetch buffer size (how many pages to load ahead)
- Library scan behavior (auto-scan on startup, watch for changes)
- Cache settings (cover art cache size, etc.)

### 2.1 Database Optimization

#### Task 2.1.1: Add indexes to frequently queried columns

Create new migration file `backend/database/sql/schemas/indexes.sql`:

```sql
-- Index for audio_files queries
CREATE INDEX IF NOT EXISTS idx_audio_files_recording_id ON audio_files(recording_id);
CREATE INDEX IF NOT EXISTS idx_audio_files_file_type_id ON audio_files(file_type_id);

-- Index for recordings queries  
CREATE INDEX IF NOT EXISTS idx_recordings_artist_credit_id ON recordings(artist_credit_id);
CREATE INDEX IF NOT EXISTS idx_recordings_name ON recordings(name);
CREATE INDEX IF NOT EXISTS idx_recordings_year ON recordings(year);
CREATE INDEX IF NOT EXISTS idx_recordings_genre ON recordings(genre);

-- Index for release_groups queries
CREATE INDEX IF NOT EXISTS idx_release_groups_name ON release_groups(name);
CREATE INDEX IF NOT EXISTS idx_release_groups_album_artist_credit_id ON release_groups(album_artist_credit_id);
CREATE INDEX IF NOT EXISTS idx_release_groups_year ON release_groups(year);

-- Index for artist_credit
CREATE INDEX IF NOT EXISTS idx_artist_credit_text ON artist_credit(text);

-- Index for release_group_recordings
CREATE INDEX IF NOT EXISTS idx_rgr_release_group_id ON release_group_recordings(release_group_id);
CREATE INDEX IF NOT EXISTS idx_rgr_recording_id ON release_group_recordings(recording_id);
```

#### Task 2.1.2: Fix release_groups UNIQUE constraint issue

Current schema has `name TEXT NOT NULL UNIQUE` which breaks if two albums have the same name by different artists.

Options:
1. Remove UNIQUE constraint (allow duplicates, rely on other fields)
2. Create composite unique on (name, album_artist_credit_id, year)
3. Add a generated hash column for uniqueness

**Recommended:** Option 2 - composite unique constraint.

Create migration to alter table or recreate with proper constraints.

#### Task 2.1.3: Analyze query performance

Use SQLite `EXPLAIN QUERY PLAN` on common queries to verify indexes are being used:

```sql
EXPLAIN QUERY PLAN SELECT * FROM recordings WHERE artist_credit_id = ?;
```

---

### 2.2 Paginated Backend Queries

#### Task 2.2.1: Add paginated track query

Add to `backend/database/sql/queries/audio_files.sql`:

```sql
-- name: GetTracksPaginated :many
SELECT 
    af.id,
    af.file_path,
    af.length_milliseconds,
    r.name as title,
    COALESCE(ac.text, '') as artist,
    COALESCE(rg.name, '') as album
FROM audio_files af
JOIN recordings r ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN release_group_recordings rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
ORDER BY r.name
LIMIT ? OFFSET ?;

-- name: GetTracksCount :one
SELECT COUNT(*) FROM audio_files;
```

#### Task 2.2.2: Add filtered/sorted track query

```sql
-- name: GetTracksFiltered :many
SELECT ...
WHERE 
    (r.name LIKE ? OR ? = '') AND
    (ac.text LIKE ? OR ? = '') AND
    (rg.name LIKE ? OR ? = '')
ORDER BY 
    CASE WHEN ? = 'title' THEN r.name END,
    CASE WHEN ? = 'artist' THEN ac.text END,
    CASE WHEN ? = 'album' THEN rg.name END
LIMIT ? OFFSET ?;
```

#### Task 2.2.3: Update Library package with paginated methods

Add to `backend/library/query.go`:

```go
type TrackQuery struct {
    Offset    int
    Limit     int
    SortBy    string  // "title", "artist", "album", "year"
    SortDir   string  // "asc", "desc"
    Search    string  // Search across title, artist, album
}

type TracksResult struct {
    Tracks     []Track
    TotalCount int
}

func (l *Library) GetTracks(query TrackQuery) (TracksResult, error)
```

#### Task 2.2.4: Expose paginated query via Wails binding

Add `GetTracks(query TrackQuery)` to FEBindings.

---

### 2.3 Virtualized Lists

#### Task 2.3.1: Install @lit-labs/virtualizer

```bash
cd frontend && npm install @lit-labs/virtualizer
```

#### Task 2.3.2: Create virtualized track list component

Create `frontend/src/components/track-list/virtualized-track-list.ts`:

```typescript
import { LitElement, html, css } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@lit-labs/virtualizer';
import { flow } from '@lit-labs/virtualizer/layouts/flow.js';
import { GetTracks } from '@go/library/Library';

@customElement('virtualized-track-list')
export class VirtualizedTrackList extends LitElement {
    @state() private tracks: Track[] = [];
    @state() private totalCount = 0;
    
    private pageSize = 100;
    private loadedPages = new Set<number>();

    override async connectedCallback() {
        super.connectedCallback();
        await this.loadPage(0);
    }

    private async loadPage(page: number) {
        if (this.loadedPages.has(page)) return;
        
        const result = await GetTracks({
            offset: page * this.pageSize,
            limit: this.pageSize,
            sortBy: 'title',
            sortDir: 'asc',
            search: '',
        });
        
        this.totalCount = result.TotalCount;
        this.loadedPages.add(page);
        
        // Merge into sparse array
        const newTracks = [...this.tracks];
        result.Tracks.forEach((track, i) => {
            newTracks[page * this.pageSize + i] = track;
        });
        this.tracks = newTracks;
    }

    private onVisibilityChanged(e: CustomEvent) {
        const { first, last } = e;
        const firstPage = Math.floor(first / this.pageSize);
        const lastPage = Math.floor(last / this.pageSize);
        
        for (let p = firstPage; p <= lastPage + 1; p++) {
            this.loadPage(p);
        }
    }

    override render() {
        return html`
            <lit-virtualizer
                scroller
                .items=${Array(this.totalCount).fill(null).map((_, i) => this.tracks[i])}
                .renderItem=${(track: Track | undefined, index: number) => 
                    track 
                        ? html`<track-row .track=${track} @click=${() => this.onTrackClick(track)}></track-row>`
                        : html`<track-row-skeleton></track-row-skeleton>`
                }
                .layout=${flow()}
                @visibilityChanged=${this.onVisibilityChanged}
            ></lit-virtualizer>
        `;
    }
}
```

#### Task 2.3.3: Create track-row component

Create `frontend/src/components/track-list/track-row.ts` for individual track rendering.

#### Task 2.3.4: Create track-row-skeleton component

Loading placeholder while data is being fetched.

#### Task 2.3.5: Create virtualized album grid

Similar to track list but using `grid` layout:

```typescript
import { grid } from '@lit-labs/virtualizer/layouts/grid.js';

.layout=${grid({ itemSize: { width: '180px', height: '220px' } })}
```

#### Task 2.3.6: Replace existing track-list and cover-grid

Swap out the old components for virtualized versions.

---

## Phase 3: Library Organization

**Goal:** Provide powerful tools for organizing and finding music.

**Settings to consider for this phase:**
- Default sort order for track lists
- Default columns displayed
- Search behavior (instant vs. press enter, search scope)
- Smart playlist default settings

### 3.1 Search & Filtering

#### Task 3.1.1: Add search input component

Create `frontend/src/components/search/search-input.ts`:
- Text input with debounced onChange
- Dispatches search event or updates LibraryStore

#### Task 3.1.2: Create LibraryStore

Create `frontend/src/store/library-store.ts`:

```typescript
interface LibraryState {
    searchQuery: string;
    sortBy: string;
    sortDir: 'asc' | 'desc';
    filters: {
        genre?: string;
        year?: number;
        artist?: string;
    };
}
```

#### Task 3.1.3: Create LibraryController

Similar to PlayerController, connects components to LibraryStore.

#### Task 3.1.4: Integrate search with virtualized list

When search query changes:
1. Reset loaded pages
2. Update query parameters
3. Reload from page 0

#### Task 3.1.5: Add filter dropdowns

Genre, year, artist filters that update LibraryStore.

---

### 3.2 Custom Columns

#### Task 3.2.1: Define available columns

```typescript
interface ColumnDefinition {
    id: string;
    label: string;
    field: string;  // Path into track object
    width: number;
    sortable: boolean;
}

const availableColumns: ColumnDefinition[] = [
    { id: 'title', label: 'Title', field: 'name', width: 200, sortable: true },
    { id: 'artist', label: 'Artist', field: 'artistName', width: 150, sortable: true },
    { id: 'album', label: 'Album', field: 'albumName', width: 150, sortable: true },
    { id: 'duration', label: 'Duration', field: 'lengthMilliseconds', width: 80, sortable: true },
    { id: 'year', label: 'Year', field: 'year', width: 60, sortable: true },
    { id: 'genre', label: 'Genre', field: 'genre', width: 100, sortable: true },
    { id: 'trackNum', label: '#', field: 'trackNumber', width: 40, sortable: true },
    // ... more columns
];
```

#### Task 3.2.2: Create column selector UI

Modal or dropdown where user can:
- Check/uncheck columns to show
- Drag to reorder columns

#### Task 3.2.3: Persist column preferences

Save selected columns and order to config.

#### Task 3.2.4: Update track list to use dynamic columns

Read column configuration and render accordingly.

---

### 3.3 Smart Playlists

**Note:** Basic playlist functionality (database schema, CRUD, UI) is implemented in Phase 1. This section extends playlists with smart/dynamic features.

#### Task 3.3.1: Define smart playlist rule structure

```typescript
interface SmartPlaylistRule {
    field: string;      // 'genre', 'year', 'artist', 'playCount', etc.
    operator: string;   // 'equals', 'contains', 'greaterThan', 'lessThan'
    value: string | number;
}

interface SmartPlaylistRules {
    matchType: 'all' | 'any';  // AND vs OR
    rules: SmartPlaylistRule[];
    limit?: number;
    sortBy?: string;
}
```

#### Task 3.3.2: Implement smart playlist query builder

Convert rules to SQL WHERE clause dynamically.

#### Task 3.3.3: Create smart playlist editor UI

Form to add/remove rules, preview results.

---

### 3.4 Auto-Playlists

#### Task 3.4.1: Implement "Recently Added" auto-playlist

Query tracks sorted by date added, limit 100.

#### Task 3.4.2: Implement "Recently Played" auto-playlist

Requires tracking play history (new table).

#### Task 3.4.3: Add play history tracking

```sql
CREATE TABLE IF NOT EXISTS play_history (
    id INTEGER PRIMARY KEY,
    audio_file_id INTEGER NOT NULL,
    played_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(audio_file_id) REFERENCES audio_files(id)
);
```

Update Player to log plays.

---

## Phase 4: UI Customization

**Goal:** Allow users to arrange and customize the UI layout.

**Settings to consider for this phase:**
- Active layout preset
- Per-component configurations (each component can define its own settings)
- Theme/appearance settings
- Sidebar default widths
- Visibility toggles for UI elements

**Important:** This phase heavily integrates with the settings infrastructure. Each registered component should be able to define its own settings schema that appears in the settings window.

### 4.1 Component Registry System

#### Task 4.1.1: Define component registry interface

```typescript
interface RegisteredComponent {
    id: string;
    name: string;
    description: string;
    component: typeof LitElement;
    defaultSlot: 'main' | 'left-sidebar' | 'right-sidebar' | 'top-bar' | 'bottom-bar';
    allowedSlots: string[];
    defaultConfig: Record<string, any>;
}
```

#### Task 4.1.2: Create component registry

```typescript
// frontend/src/registry/component-registry.ts
class ComponentRegistry {
    private components = new Map<string, RegisteredComponent>();
    
    register(component: RegisteredComponent): void;
    get(id: string): RegisteredComponent | undefined;
    getAll(): RegisteredComponent[];
    getForSlot(slot: string): RegisteredComponent[];
}

export const componentRegistry = new ComponentRegistry();
```

#### Task 4.1.3: Register existing components

```typescript
componentRegistry.register({
    id: 'track-list',
    name: 'Track List',
    description: 'Display all tracks in a table',
    component: TrackList,
    defaultSlot: 'main',
    allowedSlots: ['main'],
    defaultConfig: {},
});

componentRegistry.register({
    id: 'now-playing',
    name: 'Now Playing',
    description: 'Show current track info',
    component: NowPlaying,
    defaultSlot: 'bottom-bar',
    allowedSlots: ['bottom-bar', 'left-sidebar', 'right-sidebar'],
    defaultConfig: {},
});
```

---

### 4.2 Layout Configuration System

#### Task 4.2.1: Define layout configuration structure

```typescript
interface LayoutConfig {
    'top-bar': ComponentPlacement[];
    'bottom-bar': ComponentPlacement[];
    'left-sidebar': ComponentPlacement[];
    'right-sidebar': ComponentPlacement[];
    'main': ComponentPlacement[];
}

interface ComponentPlacement {
    componentId: string;
    config: Record<string, any>;
    order: number;
}
```

#### Task 4.2.2: Create LayoutStore

Store current layout configuration, provide methods to modify.

#### Task 4.2.3: Create layout persistence

Save/load layout from config file or localStorage.

#### Task 4.2.4: Create dynamic slot renderer

Component that reads layout config and renders appropriate components in each slot.

```typescript
@customElement('layout-slot')
class LayoutSlot extends LitElement {
    @property() slotName: string;
    
    render() {
        const placements = layoutStore.getSlot(this.slotName);
        return html`
            ${placements.map(p => {
                const reg = componentRegistry.get(p.componentId);
                const tag = reg.component.tagName;
                return html`<${tag} .config=${p.config}></${tag}>`;
            })}
        `;
    }
}
```

---

### 4.3 Layout Editor UI

#### Task 4.3.1: Create layout editor modal

- Visual representation of slots
- Drag components between slots
- Add/remove components from slots

#### Task 4.3.2: Create component configurator

Per-component settings panel for components that support configuration.

#### Task 4.3.3: Add layout presets

Default layouts users can choose from:
- "Classic" (sidebar + main + bottom bar)
- "Minimal" (just player controls)
- "Full" (all panels visible)

---

## Phase 5: MusicBrainz Integration

**Goal:** Enable automatic metadata tagging via MusicBrainz.

**Settings to consider for this phase:**
- AcoustID API key
- Auto-tag behavior (prompt always, auto-accept high confidence, etc.)
- Minimum confidence threshold for auto-accept
- Which metadata fields to overwrite
- Backup original tags before overwriting

### 5.1 MusicBrainz API Client

#### Task 5.1.1: Create MusicBrainz package

`backend/musicbrainz/client.go`:
- HTTP client with rate limiting (1 req/sec per MB guidelines)
- User-Agent header with app name and contact

#### Task 5.1.2: Implement recording search

```go
func (c *Client) SearchRecordings(query string) ([]Recording, error)
func (c *Client) GetRecording(mbid string) (*Recording, error)
```

#### Task 5.1.3: Implement release search

```go
func (c *Client) SearchReleases(query string) ([]Release, error)
func (c *Client) GetRelease(mbid string) (*Release, error)
```

#### Task 5.1.4: Implement artist search

```go
func (c *Client) SearchArtists(query string) ([]Artist, error)
```

---

### 5.2 AcoustID Integration

#### Task 5.2.1: Integrate chromaprint for fingerprinting

Use chromaprint library to generate audio fingerprints.

#### Task 5.2.2: Create AcoustID client

```go
func (c *AcoustIDClient) Lookup(fingerprint string, duration int) ([]AcoustIDResult, error)
```

#### Task 5.2.3: Map AcoustID results to MusicBrainz

AcoustID returns MusicBrainz recording IDs; use those to fetch full metadata.

---

### 5.3 Autotag Workflow

#### Task 5.3.1: Create autotag service

`backend/autotag/autotag.go`:

```go
type AutotagResult struct {
    FilePath       string
    MatchConfidence float64
    CurrentMetadata TrackMetadata
    SuggestedMetadata TrackMetadata
    MBRecordingID  string
}

func (s *Service) AnalyzeTrack(filePath string) (*AutotagResult, error)
func (s *Service) AnalyzeAlbum(tracks []string) ([]AutotagResult, error)
func (s *Service) ApplyTags(result *AutotagResult) error
```

#### Task 5.3.2: Create autotag UI component

- Show current vs suggested metadata side-by-side
- Confidence indicator
- Accept/reject buttons
- Batch operations for albums

#### Task 5.3.3: Implement tag writing

Write accepted metadata back to audio files using tag library.

---

### 5.4 MusicBrainz Visual Browser

#### Task 5.4.1: Create artist browser view

- Search artists
- View artist discography
- Click release to see tracklist

#### Task 5.4.2: Create release browser view

- Album art (from Cover Art Archive)
- Track listing
- Credits and relationships

#### Task 5.4.3: Link local tracks to MB entities

Show which local tracks match MB recordings; allow manual linking.

---

## Phase 6: Device Sync

**Goal:** Sync music to Android devices with optional re-encoding.

**Settings to consider for this phase:**
- Per-device sync profiles (encoding quality, playlists to sync)
- Encoding cache location and size limit
- Sync behavior (delete removed tracks from device, etc.)
- Custom encoding profiles (advanced users)
- FFmpeg binary path (if not bundled)

### 6.1 Android Sync (MTP)

#### Task 6.1.1: Research MTP libraries for Go

Options:
- libmtp bindings
- gousb for raw USB
- Call external tools (jmtpfs, go-mtpfs)

#### Task 6.1.2: Create device detection

Detect connected MTP devices, list storage volumes.

#### Task 6.1.3: Create file transfer service

```go
type SyncService struct {
    // ...
}

func (s *SyncService) GetDevices() ([]Device, error)
func (s *SyncService) SyncPlaylist(device Device, playlist Playlist, profile EncodingProfile) error
func (s *SyncService) SyncTracks(device Device, tracks []Track, profile EncodingProfile) error
```

---

### 6.2 Re-encoding Pipeline

#### Task 6.2.1: Integrate FFmpeg

Use FFmpeg for transcoding. Options:
- Call ffmpeg binary
- Use go-ffmpeg bindings

#### Task 6.2.2: Define encoding profiles

```go
type EncodingProfile struct {
    Name       string
    Format     string  // "mp3", "aac", "opus"
    Bitrate    int     // kbps
    SampleRate int     // Hz
}

var presets = []EncodingProfile{
    {Name: "High Quality MP3", Format: "mp3", Bitrate: 320, SampleRate: 44100},
    {Name: "Balanced MP3", Format: "mp3", Bitrate: 192, SampleRate: 44100},
    {Name: "Space Saver", Format: "mp3", Bitrate: 128, SampleRate: 44100},
}
```

#### Task 6.2.3: Create encoding cache

Cache encoded files to avoid re-encoding on every sync:
- Hash source file + profile = cache key
- Store encoded files in cache directory

#### Task 6.2.4: Create sync progress UI

- Device selection
- Playlist/track selection  
- Encoding profile selection
- Progress bar with current file
- Cancel button

---

## Phase 7: Cross-Platform Polish

**Goal:** Ensure excellent experience on Windows and macOS.

**Settings to consider for this phase:**
- System tray behavior (minimize to tray, close to tray)
- Startup behavior (start minimized, start with system)
- Media key handling (enable/disable)
- Notification preferences (track change, etc.)
- File association preferences

### 7.1 Platform Testing

#### Task 7.1.1: Set up Windows build environment

- Windows VM or machine
- Go + Node.js toolchain
- Wails CLI

#### Task 7.1.2: Set up macOS build environment

- macOS machine (required for signing)
- Xcode command line tools
- Go + Node.js toolchain

#### Task 7.1.3: Fix platform-specific issues

Test and fix:
- File paths (forward vs backslash)
- System directories
- Audio device handling
- Window chrome differences

---

### 7.2 Platform Integration

#### Task 7.2.1: System media key support

Respond to keyboard media keys (play/pause, next, previous).

Research:
- Windows: RegisterHotKey or low-level keyboard hook
- macOS: SPMediaKeyTap or MediaKeySession
- Linux: D-Bus MPRIS

#### Task 7.2.2: MPRIS integration (Linux)

Implement MPRIS D-Bus interface for integration with desktop environments.

#### Task 7.2.3: System tray icon

Minimize to tray, show playback controls in tray menu.

#### Task 7.2.4: Native notifications

Show track change notifications using system notification APIs.

---

### 7.3 Distribution

#### Task 7.3.1: Create installer for Windows

- NSIS or WiX installer
- Start menu shortcut
- File associations (.mp3, .flac, etc.)

#### Task 7.3.2: Create DMG for macOS

- Signed and notarized app bundle
- Drag-to-Applications installer

#### Task 7.3.3: Create packages for Linux

- AppImage (universal)
- .deb (Debian/Ubuntu)
- .rpm (Fedora)
- Flatpak (sandboxed)

#### Task 7.3.4: Set up CI/CD for releases

GitHub Actions workflow to:
- Build for all platforms
- Run tests
- Create release artifacts
- Publish to GitHub Releases

---

## Technical Debt Items

These items should be addressed as time permits, integrated with feature work:

### Testing

- [ ] Unit tests for PlayerStore
- [ ] Unit tests for Go Queue package
- [ ] Unit tests for Go Library scanning
- [ ] Integration tests for Wails event flow
- [ ] E2E tests for critical user flows

### Documentation

- [ ] User documentation / help pages
- [ ] Developer setup guide
- [ ] Architecture documentation
- [ ] API documentation for plugin authors (future)

### Code Quality

- [ ] Consistent error handling patterns in Go
- [ ] Consistent logging throughout
- [ ] Performance profiling and optimization
- [ ] Accessibility audit (keyboard navigation, screen readers)

### Security

- [ ] Input validation for all user inputs
- [ ] Safe file path handling
- [ ] Sanitize metadata before display (XSS prevention)

---

## Decision Log

Key architectural decisions made during planning:

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Queue model | Queue as special playlist | Unified data structures, queue can be saved as playlist |
| Queue/Playlist location | Go backend | Tighter integration with Player, single source of truth |
| Frontend state | Custom store + Controllers | Production-ready, integrates with Wails events |
| Virtualization | @lit-labs/virtualizer | Native Lit integration, supports both list and grid |
| State management lib | None (custom) | Signals not production-ready, custom gives full control |
| Track per file | Yes (no deduplication) | Practical for real-world music libraries |
| Testing | Deferred | Focus on architecture first, add tests incrementally |
| Settings architecture | Unified, extensible | Each feature adds its own config section; single settings UI |
| Playlists in Phase 1 | Yes | Core feature, needed for queue; smart playlists in Phase 3 |
