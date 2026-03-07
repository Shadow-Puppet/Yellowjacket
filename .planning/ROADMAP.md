# Roadmap: YellowJacket

**Created:** 2026-02-27
**Last updated:** 2026-03-06
**Current milestone:** v1.1 Features & Extensibility

## Milestones

- ✅ **v1.0 Consolidation** — Phases 1-8 (shipped 2026-03-05) — [archive](milestones/v1.0-ROADMAP.md)
- 🔄 **v1.1 Features & Extensibility** — Phases 9-14 (in progress)

## Phases

<details>
<summary>✅ v1.0 Consolidation (Phases 1-8) — SHIPPED 2026-03-05</summary>

- [x] Phase 1: Concurrency Race Fixes (1/1 plans) — completed 2026-02-28
- [x] Phase 2: Backend Correctness (2/2 plans) — completed 2026-03-03
- [x] Phase 3: Test Infrastructure (1/1 plans) — completed 2026-03-04
- [x] Phase 4: Queue, Config & Player Tests (2/2 plans) — completed 2026-03-04
- [x] Phase 5: Database & Library Tests (2/2 plans) — completed 2026-03-04
- [x] Phase 6: SQL Consolidation & Code Quality (3/3 plans) — completed 2026-03-04
- [x] Phase 7: Backend Performance (2/2 plans) — completed 2026-03-05
- [x] Phase 8: Frontend Performance & UX (4/4 plans) — completed 2026-03-05

</details>

### v1.1 Features & Extensibility (Phases 9-14)

- [ ] **Phase 9: Scan Cancellation & Keyboard Shortcuts** — Cancellable library scans and configurable keyboard shortcuts
- [ ] **Phase 10: Tag Editing** — Edit track metadata and write changes to audio files
- [ ] **Phase 11: Smart Playlists** — Auto-generated playlists with filter rules
- [ ] **Phase 12: Gapless Playback & Crossfade** — Seamless track transitions with optional crossfade
- [ ] **Phase 13: MusicBrainz Browser** — Browse the MusicBrainz catalog from within the app
- [ ] **Phase 14: Layout Customization & Plugin Foundation** — Section-based UI customization and JS plugin system

## Phase Details

### Phase 9: Scan Cancellation & Keyboard Shortcuts
**Goal:** Users can control library scans (cancel/pause/resume) and operate the entire app via keyboard
**Depends on:** Nothing (builds on v1.0 foundation)
**Requirements:** SCAN-01, SCAN-02, SCAN-03, KEY-01, KEY-02, KEY-03, KEY-04, KEY-05
**Success Criteria** (what must be TRUE):
  1. User can click a cancel button during a library scan and the scan stops within seconds — no database corruption, no orphaned tracks
  2. User can pause a running scan and resume it later without re-processing files that were already scanned
  3. Default keyboard shortcuts work immediately after install — play/pause, next/prev, volume up/down, search focus, queue toggle, shuffle, repeat all respond to keys
  4. User can open a settings UI, rebind any shortcut to a different key, and the new binding takes effect immediately — conflicts are warned about before saving
  5. Keyboard shortcuts are context-aware — typing in a search box doesn't trigger player shortcuts (except Escape to blur)
**Plans:** 5 plans
Plans:
- [x] 09-01-PLAN.md — Backend scan control (cancel/pause/resume methods, events, metrics)
- [x] 09-02-PLAN.md — Backend shortcuts config + frontend keyboard shortcut service
- [x] 09-03-PLAN.md — Frontend scan control UI (buttons, cancel dialog)
- [ ] 09-04-PLAN.md — Frontend shortcut settings UI (record-style capture, conflict detection)
- [ ] 09-05-PLAN.md — Integration verification checkpoint

### Phase 10: Tag Editing
**Goal:** Users can edit track metadata from within the app and changes are written to the actual audio files
**Depends on:** Phase 9 (scan cancellation validates context patterns; tag edits must be blocked during active scans)
**Requirements:** TAG-01, TAG-02, TAG-03, TAG-04, TAG-05, TAG-06, TAG-07
**Success Criteria** (what must be TRUE):
  1. User can select a track, edit its title/artist/album/genre/year/track number in a UI form, and save — the changes appear immediately in the library without requiring a rescan
  2. User can select multiple tracks, edit shared fields (e.g., album name, genre), and the batch edit applies to all selected tracks
  3. Tag changes are persisted to the actual MP3 (ID3v2) and FLAC (Vorbis Comments) files on disk — verified by re-reading the file's metadata
  4. User can assign or replace embedded cover art from an image file, and the new art displays immediately
  5. A file that is currently playing cannot have its tags edited — the UI shows a clear indication that the edit is blocked until playback moves on
**Plans:** TBD

### Phase 11: Smart Playlists
**Goal:** Users can create rule-based playlists that automatically populate based on their music library metadata
**Depends on:** Phase 10 (tag editing validates DB update + event pipeline; edited metadata affects smart playlist membership)
**Requirements:** SMRT-01, SMRT-02, SMRT-03, SMRT-04, SMRT-05
**Success Criteria** (what must be TRUE):
  1. User can create a smart playlist with one or more filter rules (genre equals "Jazz", year > 2000, artist contains "Miles") and see matching tracks
  2. Multiple rules combine with AND logic — adding a second rule narrows the results
  3. User can set random ordering and a result limit (e.g., "Random 50 Jazz tracks") and the playlist respects both
  4. Smart playlists appear in the sidebar alongside regular playlists with a distinct icon, and their rules persist across app restarts
**Plans:** TBD

### Phase 12: Gapless Playback & Crossfade
**Goal:** Tracks transition seamlessly with no audible gap, and users can optionally enable crossfade between tracks
**Depends on:** Phase 9 (keyboard shortcuts needed for testing audio transitions; no direct code dependency but risk isolation — this is the highest-risk phase)
**Requirements:** GAP-01, GAP-02, GAP-03, GAP-04
**Success Criteria** (what must be TRUE):
  1. When playing an album, tracks transition with no audible silence gap — the audio stream is continuous
  2. The next track is pre-decoded before the current track ends so the transition is instantaneous
  3. User can enable crossfade in settings with a configurable duration (1-10 seconds), and tracks blend smoothly during auto-advance
  4. Crossfade only applies on auto-advance (track finishes naturally) — manual skip/next produces an immediate clean switch
**Plans:** TBD

### Phase 13: MusicBrainz Browser
**Goal:** Users can browse the MusicBrainz music catalog (artists, albums, tracks) directly from within the app
**Depends on:** Phase 11 (smart playlists validate dynamic DB query patterns reused by MB cache; no hard dependency but ordering isolates network feature)
**Requirements:** MB-01, MB-02, MB-03, MB-04, MB-05, MB-06, MB-07
**Success Criteria** (what must be TRUE):
  1. User can search for an artist by name and see a list of matching results from MusicBrainz
  2. User can select an artist and browse their discography — albums, EPs, and singles displayed as release groups
  3. User can view the track listing for a specific release and see different editions (original, reissue, deluxe) of a release group
  4. Album cover art from the Cover Art Archive is displayed alongside release information
  5. The app respects MusicBrainz rate limits (1 req/sec), caches responses in SQLite (24hr searches, 7 days entities), and works gracefully when offline or rate-limited
**Plans:** TBD

### Phase 14: Layout Customization & Plugin Foundation
**Goal:** Users can customize the app's layout (resize, show/hide, rearrange panels) and developers can extend the app with JavaScript plugins
**Depends on:** Phase 13 (layout and plugin systems wrap all existing features — needs stable component set and API surface)
**Requirements:** LAYOUT-01, LAYOUT-02, LAYOUT-03, LAYOUT-04, LAYOUT-05, LAYOUT-06, PLUG-01, PLUG-02, PLUG-03, PLUG-04, PLUG-05, PLUG-06
**Success Criteria** (what must be TRUE):
  1. User can drag to resize the sidebar and queue panels, and the sizes persist across app restarts
  2. User can show/hide sidebar sections and the queue panel, and choose which component is displayed in each layout section
  3. Layout presets (Compact, Full, Mini player) are available and the user can quick-switch between them
  4. A JS/TS plugin loaded from the user's plugin directory can access player state, queue data, and library data via a defined API, and can register a custom UI component into the layout
  5. An example plugin ships with the app demonstrating the plugin API (manifest, configuration, event hooks, UI registration)
**Plans:** TBD

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Concurrency Race Fixes | v1.0 | 1/1 | Complete | 2026-02-28 |
| 2. Backend Correctness | v1.0 | 2/2 | Complete | 2026-03-03 |
| 3. Test Infrastructure | v1.0 | 1/1 | Complete | 2026-03-04 |
| 4. Queue, Config & Player Tests | v1.0 | 2/2 | Complete | 2026-03-04 |
| 5. Database & Library Tests | v1.0 | 2/2 | Complete | 2026-03-04 |
| 6. SQL Consolidation & Code Quality | v1.0 | 3/3 | Complete | 2026-03-04 |
| 7. Backend Performance | v1.0 | 2/2 | Complete | 2026-03-05 |
| 8. Frontend Performance & UX | v1.0 | 4/4 | Complete | 2026-03-05 |
| 9. Scan Cancellation & Keyboard Shortcuts | v1.1 | 0/5 | Planning complete | - |
| 10. Tag Editing | v1.1 | 0/? | Not started | - |
| 11. Smart Playlists | v1.1 | 0/? | Not started | - |
| 12. Gapless Playback & Crossfade | v1.1 | 0/? | Not started | - |
| 13. MusicBrainz Browser | v1.1 | 0/? | Not started | - |
| 14. Layout Customization & Plugin Foundation | v1.1 | 0/? | Not started | - |

---
*Roadmap created: 2026-02-27*
*Last updated: 2026-03-06 — v1.1 phases 9-14 added*
