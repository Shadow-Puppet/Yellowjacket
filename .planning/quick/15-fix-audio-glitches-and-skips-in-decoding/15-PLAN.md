---
phase: 15-fix-audio-glitches
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/player/buffered_streamer.go
  - backend/player/buffered_streamer_test.go
  - backend/player/player.go
autonomous: true
requirements: [AUDIO-BUFFER]

must_haves:
  truths:
    - "Decoder I/O stalls do not cause audible glitches in speaker output"
    - "Read-ahead goroutine pre-fills buffer so speaker callback never starves"
    - "Seek operations flush the buffer and resume read-ahead from new position"
    - "Track end-of-stream propagates correctly through buffer to speaker"
    - "Speaker buffer provides secondary protection at 200ms instead of 100ms"
  artifacts:
    - path: "backend/player/buffered_streamer.go"
      provides: "Ring-buffer streamer with goroutine read-ahead"
      exports: ["NewBufferedStreamer"]
    - path: "backend/player/buffered_streamer_test.go"
      provides: "Unit tests for BufferedStreamer"
    - path: "backend/player/player.go"
      provides: "Updated streamer chain with BufferedStreamer insertion"
  key_links:
    - from: "backend/player/player.go"
      to: "backend/player/buffered_streamer.go"
      via: "NewBufferedStreamer wrapping resampled streamer"
      pattern: "NewBufferedStreamer"
    - from: "backend/player/buffered_streamer.go"
      to: "beep.Streamer interface"
      via: "implements Stream() and Err()"
      pattern: "func.*Stream\\(samples"
---

<objective>
Fix audio glitches and skips by inserting a read-ahead buffered streamer between the decoder/resampler and the speaker output, and increasing the speaker buffer from 100ms to 200ms.

Purpose: The current pipeline has zero buffering between the file decoder and speaker output. The speaker's goroutine pulls samples directly through the entire chain (decode → resample → ctrl → volume). If any step stalls (disk I/O, GC pause, CPU scheduling), the speaker underruns and produces audible glitches. A read-ahead buffer decouples decode timing from audio output timing.

Output: `BufferedStreamer` implementation + updated player pipeline + increased speaker buffer
</objective>

<execution_context>
@/home/caleb/.config/Claude/get-shit-done/workflows/execute-plan.md
@/home/caleb/.config/Claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@backend/player/player.go
@backend/player/player_test.go

<interfaces>
<!-- beep.Streamer interface (from gopxl/beep/v2 interface.go) -->
```go
type Streamer interface {
    // Returns n samples copied, ok=false when drained.
    // 3 valid patterns: (n==len, ok), (0<n<len, ok), (0, false)
    Stream(samples [][2]float64) (n int, ok bool)
    Err() error
}
```

<!-- Current streamer chain in player.go updateStreamers() -->
```go
// line 306-331: current chain
p.resampled = beep.Resample(4, sr, speakerSampleRate, p.baseStreamer)
p.control = &beep.Ctrl{Streamer: p.resampled}
p.volume = &effects.Volume{Streamer: p.control, Base: 2, ...}
p.speakerStreamer = p.volume
```

<!-- Speaker init in player.go InitSpeaker() -->
```go
// line 128-131: current 100ms buffer
speaker.Init(p.format.SampleRate, p.format.SampleRate.N(time.Second/10))
```
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Create BufferedStreamer with goroutine read-ahead</name>
  <files>backend/player/buffered_streamer.go, backend/player/buffered_streamer_test.go</files>
  <action>
Create `backend/player/buffered_streamer.go` implementing a ring-buffer streamer that decouples the source streamer from the consumer (speaker).

**Design:**
- `BufferedStreamer` struct with fields: `mu sync.Mutex`, `source beep.Streamer`, `ring [][2]float64` (ring buffer), `readPos int`, `writePos int`, `count int` (samples buffered), `done bool` (source drained), `err error`, `closed chan struct{}`
- Constructor: `NewBufferedStreamer(source beep.Streamer, bufferSize int) *BufferedStreamer` — allocates ring buffer of `bufferSize` samples, starts read-ahead goroutine
- The read-ahead goroutine runs in a loop:
  1. Lock mutex, check if buffer has space (count < len(ring)) and not closed
  2. If buffer is full, unlock and sleep briefly (1ms) to avoid busy-spin, then retry
  3. If space available, determine how many samples to read: `min(available_space, 512)` — read in chunks to avoid holding lock too long during source.Stream()
  4. **Unlock mutex before calling source.Stream()** — the source read (disk I/O) must NOT hold the lock, as that would block the speaker goroutine
  5. After reading from source (outside lock), re-lock and copy samples into ring buffer, advance writePos, increment count
  6. If source returns (0, false), set `done = true` and exit goroutine
  7. If source.Err() != nil, set `err` and `done = true` and exit
- `Stream(samples [][2]float64) (int, bool)` method:
  1. Lock mutex
  2. If count == 0 and done: return 0, false
  3. If count == 0 and !done: return 0, true (buffer temporarily empty — return silence/zero samples to avoid blocking the speaker callback; the speaker will call again)
  4. Copy min(len(samples), count) samples from ring at readPos, advance readPos (wrapping), decrement count
  5. Unlock, return n, true
  6. **IMPORTANT**: When count == 0 and !done, we must still fill the samples with zeros (silence) so the speaker doesn't get garbage. Copy zeros into samples[:requested] and return len(samples), true. This prevents the speaker from interpreting an empty return as "drained". Brief silence is far better than a glitch or premature track end.
- `Err() error` — returns stored error under lock
- `Close()` — closes the `closed` channel to signal the goroutine to stop; the goroutine should `select` on `closed` during its sleep

**Buffer size**: Default to `44100 * 2 = 88200` samples (~2 seconds at 44100 Hz). This gives ample runway to absorb I/O stalls and GC pauses.

**Ring buffer math**: readPos and writePos wrap with modulo len(ring). When writing, if contiguous space to end of ring is less than chunk size, write in two parts (wrap around).

**Thread safety**: The mutex protects ring buffer metadata (readPos, writePos, count, done, err). Source reads happen OUTSIDE the lock. Speaker reads (Stream) hold the lock only while copying from ring — never during I/O.

**IMPORTANT — do NOT touch lock-sensitive paths in player.go**: This streamer is self-contained. It does not interact with speaker.Lock() or p.mu. It only wraps a beep.Streamer.

Create `backend/player/buffered_streamer_test.go` with unit tests:
1. **TestBufferedStreamer_BasicStream**: Create a finite beep.StreamerFunc that produces N known samples (e.g., incrementing values). Wrap in BufferedStreamer. Read all samples back via Stream(). Verify all samples received in order, final call returns (0, false).
2. **TestBufferedStreamer_SmallReads**: Same source but read with very small buffer (e.g., 1 sample at a time). Verify all samples eventually received.
3. **TestBufferedStreamer_SourceDrained**: Source that produces exactly 100 samples. Verify BufferedStreamer eventually returns (0, false) after all 100 consumed.
4. **TestBufferedStreamer_EmptyBufferReturnsSilence**: Create a slow source (sleeps 50ms per Stream call). Immediately call BufferedStreamer.Stream() before read-ahead fills buffer. Verify it returns len(samples), true (silence) rather than blocking or returning (0, false).
5. **TestBufferedStreamer_Close**: Verify Close() causes goroutine to exit (use runtime.NumGoroutine before/after or simply verify no deadlock within timeout).
  </action>
  <verify>
    <automated>cd backend/player && go test -run TestBufferedStreamer -v -count=1 -timeout=10s</automated>
  </verify>
  <done>BufferedStreamer passes all 5 unit tests. Implements beep.Streamer interface. Read-ahead goroutine pre-fills from source without blocking speaker callback.</done>
</task>

<task type="auto">
  <name>Task 2: Insert BufferedStreamer into player pipeline and increase speaker buffer</name>
  <files>backend/player/player.go</files>
  <action>
Two targeted changes in `player.go`:

**Change 1: Insert BufferedStreamer in updateStreamers() (around line 306-331)**

After creating the resampled streamer and BEFORE wrapping in beep.Ctrl, insert a BufferedStreamer:

```go
// resample file stream to match speaker
p.resampled = beep.Resample(4, sr, speakerSampleRate, p.baseStreamer)

// Buffer resampled audio to decouple disk I/O from speaker timing.
// 2 seconds of read-ahead at speaker sample rate absorbs I/O stalls
// and GC pauses without audible glitches.
p.buffered = NewBufferedStreamer(p.resampled, int(speakerSampleRate)*2)

// wrap in ctrl streamer to allow play/pause
p.control = &beep.Ctrl{Streamer: p.buffered}
```

Add `buffered *BufferedStreamer` field to the Player struct (after the `resampled` field, around line 50).

**In UnloadTrack()** (around line 609): Add `p.buffered.Close()` before setting `p.buffered = nil` to stop the read-ahead goroutine when unloading. Place this after pausing control but before closing the file. Add nil check: `if p.buffered != nil { p.buffered.Close() }` then `p.buffered = nil`.

**In loadFileLocked()** (around line 426-433): When stopping existing playback before loading new file, close the old buffered streamer: after pausing control and before closing currentFile, add `if p.buffered != nil { p.buffered.Close() }`.

**Change 2: Increase speaker buffer in InitSpeaker() (line 130)**

Change:
```go
p.format.SampleRate.N(time.Second/10),
```
To:
```go
p.format.SampleRate.N(time.Second/5),
```

This doubles the speaker buffer from ~100ms (4410 samples) to ~200ms (8820 samples). Update the TODO comment to reflect the new default.

**DO NOT change any lock ordering or mutex patterns.** These are purely additive insertions in the streamer chain and a constant change in InitSpeaker.
  </action>
  <verify>
    <automated>cd backend && go build ./... && go vet ./player/...</automated>
  </verify>
  <done>Player pipeline includes BufferedStreamer between resampler and ctrl. Speaker buffer is 200ms. `go build` and `go vet` pass clean. Old buffered streamer is properly closed on track unload and track change.</done>
</task>

</tasks>

<verification>
1. `cd backend && go build ./...` — compiles without errors
2. `cd backend && go vet ./player/...` — no vet issues
3. `cd backend/player && go test -v -count=1 -timeout=10s` — all tests pass (unit tests run; integration test skipped without YELLOWJACKET_INTEGRATION=1)
4. Manual: Play several tracks in sequence, verify no glitches at track boundaries and during playback. Seek mid-track and verify audio resumes smoothly.
</verification>

<success_criteria>
- BufferedStreamer implemented with goroutine read-ahead and ring buffer
- 5 unit tests pass covering: basic streaming, small reads, source drain, empty-buffer silence, close cleanup
- Player streamer chain: decode → resample → **BufferedStreamer** → ctrl → volume → speaker
- Speaker buffer increased from 100ms to 200ms
- No changes to lock ordering or mutex-sensitive code paths
- `go build`, `go vet`, `go test` all pass
</success_criteria>

<output>
After completion, create `.planning/quick/15-fix-audio-glitches-and-skips-in-decoding/15-SUMMARY.md`
</output>
