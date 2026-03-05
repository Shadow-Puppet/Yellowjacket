---
phase: 15-fix-audio-glitches
plan: 01
subsystem: player
tags: [audio, buffering, performance, glitch-fix]
dependency_graph:
  requires: []
  provides: [BufferedStreamer, read-ahead-buffering]
  affects: [player-pipeline, speaker-init]
tech_stack:
  added: []
  patterns: [ring-buffer, goroutine-read-ahead, channel-signaling]
key_files:
  created:
    - backend/player/buffered_streamer.go
    - backend/player/buffered_streamer_test.go
  modified:
    - backend/player/player.go
decisions:
  - "2-second ring buffer (88200 samples at 44100 Hz) provides sufficient runway for I/O stalls and GC pauses"
  - "Source reads happen outside mutex lock to avoid blocking speaker callback"
  - "Empty buffer returns silence rather than blocking or signaling end-of-stream"
  - "Speaker buffer doubled from 100ms to 200ms as secondary underrun protection"
metrics:
  duration: "10m 38s"
  completed: "2026-03-05"
  tasks: 2
  files_changed: 3
---

# Quick Task 15: Fix Audio Glitches and Skips in Decoding Summary

Ring-buffer `BufferedStreamer` with goroutine read-ahead between decoder/resampler and speaker output, plus 200ms speaker buffer — eliminates glitches from disk I/O stalls, GC pauses, and CPU scheduling delays.

## Tasks Completed

| # | Task | Commit | Key Changes |
|---|------|--------|-------------|
| 1 | Create BufferedStreamer with goroutine read-ahead | 85b23ac | New `BufferedStreamer` type with ring buffer, read-ahead goroutine, silence-on-empty, Close() cleanup; 5 unit tests |
| 2 | Insert BufferedStreamer into player pipeline and increase speaker buffer | 8a0b16a | Chain: decode→resample→**BufferedStreamer**→ctrl→volume→speaker; speaker buffer 100ms→200ms; Close on unload/track-change |

## Implementation Details

### BufferedStreamer Design

- **Ring buffer**: Pre-allocated `[][2]float64` of configurable size (default 88200 samples ≈ 2 seconds at 44100 Hz)
- **Read-ahead goroutine**: Reads from source in 512-sample chunks outside the mutex, copies into ring under lock
- **Thread safety**: Mutex protects ring metadata only; source I/O never holds the lock, so the speaker goroutine is never blocked by disk
- **Empty buffer handling**: Returns silence (`len(samples), true`) when buffer temporarily empty — brief silence is far better than a glitch or premature track end
- **Shutdown**: `Close()` signals goroutine via channel; safe to call multiple times; called on track change and unload

### Player Pipeline Changes

- `buffered *BufferedStreamer` field added to Player struct
- Inserted between `beep.Resample` and `beep.Ctrl` in `updateStreamers()`
- `loadFileLocked()` closes old BufferedStreamer before loading new track
- `UnloadTrack()` closes and nils BufferedStreamer to prevent goroutine leaks
- Speaker buffer changed from `time.Second/10` (100ms) to `time.Second/5` (200ms)

### Lock Safety

No changes to lock ordering or mutex-sensitive code paths. The BufferedStreamer is self-contained and does not interact with `speaker.Lock()` or `p.mu`.

## Verification Results

- `go build ./backend/...` — PASS
- `go vet ./backend/player/...` — PASS
- `go test ./backend/player/ -v` — all 12 tests pass (5 BufferedStreamer + 7 existing)

## Deviations from Plan

None — plan executed exactly as written.

## Self-Check: PASSED

All created files exist, all commits verified.
