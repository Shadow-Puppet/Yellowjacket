# Testing Patterns

**Analysis Date:** 2026-02-26

## Test Framework

**Runner:**
- Go standard `testing` package
- No external test frameworks (no testify assertions — uses raw `t.Errorf`/`t.Fatalf`)
- golangci-lint `testifylint` is enabled but unused (no testify dependency)

**Assertion Library:**
- Standard library only — `t.Errorf`, `t.Fatalf`, `t.Fatal`, `t.Logf`
- Custom equality helpers in test files (e.g., `slicesEqual`)

**Run Commands:**
```bash
make test                                                        # All tests (preferred)
go test -tags webkit2_41 -race -count=1 -timeout 120s ./...      # All tests manually
go test -tags webkit2_41 ./backend/player/                       # Single package
go test -tags webkit2_41 -run TestFunctionName ./backend/player/ # Single test
go test -tags webkit2_41 -v -run TestFunctionName ./backend/...  # Verbose single test
```

## Build Tags Requirement

**Critical:** All `go test` invocations require `-tags webkit2_41`. The Makefile handles this automatically. Without this tag, compilation fails because the Wails v2 framework depends on WebKit bindings.

```bash
# Correct:
go test -tags webkit2_41 ./...

# Wrong — will fail to compile:
go test ./...
```

The `Makefile` test target includes all recommended flags:

```makefile
test:
    go test -tags webkit2_41 -race -count=1 -timeout 120s ./...
```

- `-race` — Race detector enabled
- `-count=1` — Disable test caching (always run)
- `-timeout 120s` — 2-minute timeout

## Test File Organization

**Location:** Colocated with source as `*_test.go` in the same package:

```
backend/player/player.go
backend/player/player_test.go

backend/metadata/genre.go
backend/metadata/genre_test.go
backend/metadata/mp3duration.go
backend/metadata/mp3duration_test.go
backend/metadata/flacduration.go
backend/metadata/flacduration_test.go

backend/coverart/coverart.go
backend/coverart/coverart_test.go

backend/playlist/m3u.go
backend/playlist/m3u_test.go
backend/playlist/match.go
backend/playlist/match_test.go
```

**Exception:** `backend/coverart/coverart_test.go` uses `package coverart_test` (external test package) to test only the exported API.

**All other test files** use the same package as the source (internal tests), allowing access to unexported functions:

```go
package metadata  // internal test — can call unexported getMP3Duration()
package playlist  // internal test — can call unexported sanitizeFilename()
```

## Test Fixtures

**Location:** `test_data/` at the project root.

**Contents:** Real audio files (MP3, FLAC) used by metadata and player tests.

**Access pattern:** Tests use relative paths from the package directory:

```go
// From backend/player/player_test.go
var testQueue = []string{
    "../../test_data/music_library_test/other_music/03 PONPONPON.mp3",
    "../../test_data/music_library_test/01 Some Chords.mp3",
    "../../test_data/music_library_test/03 anything.mp3",
}

// From backend/metadata/mp3duration_test.go
root := filepath.Join("..", "..", "test_data")
```

**Test helper functions** scan the fixture directory for files of the right type:

```go
// backend/metadata/mp3duration_test.go
func testMP3Files(t *testing.T) []string {
    t.Helper()
    root := filepath.Join("..", "..", "test_data")
    var files []string
    err := filepath.Walk(root, func(
        path string, info os.FileInfo, err error,
    ) error {
        if !info.IsDir() && filepath.Ext(path) == ".mp3" {
            files = append(files, path)
        }
        return nil
    })
    if len(files) == 0 {
        t.Skip("no .mp3 test fixtures found in test_data/")
    }
    return files
}

// backend/metadata/flacduration_test.go
func testFlacFiles(t *testing.T) []string {
    t.Helper()
    root := filepath.Join("..", "..", "test_data")
    // same pattern for .flac files
}
```

**`t.TempDir()`** is used for tests that write files:

```go
dir := t.TempDir()
tmpPath := filepath.Join(dir, "multi_id3v2.mp3")
os.WriteFile(tmpPath, out, 0o644)
```

## Hardware-Dependent Test Skipping

### Integration Tests (Audio Device + Wails Runtime)

The player test requires both a Wails runtime context and an audio output device. It skips unless explicitly opted in:

```go
// backend/player/player_test.go
func TestPlayer(t *testing.T) {
    if os.Getenv("YELLOWJACKET_INTEGRATION") == "" {
        t.Skip(
            "skipping: integration test requires Wails runtime and audio device " +
            "(set YELLOWJACKET_INTEGRATION=1 to run)",
        )
    }
    // ...
}
```

**To run integration tests:**
```bash
YELLOWJACKET_INTEGRATION=1 go test -tags webkit2_41 -v ./backend/player/
```

### Fixture-Dependent Tests

Tests that need audio fixtures skip gracefully when none are found:

```go
if len(files) == 0 {
    t.Skip("no .mp3 test fixtures found in test_data/")
}
```

## Test Structure Patterns

### Table-Driven Tests

The predominant pattern across the codebase. Use a slice of anonymous structs with `t.Run` subtests:

```go
// backend/metadata/genre_test.go
func TestParseGenres(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name string
        raw  string
        want []string
    }{
        {
            name: "single genre",
            raw:  "Rock",
            want: []string{"Rock"},
        },
        {
            name: "semicolon separated",
            raw:  "Rock; Electronic",
            want: []string{"Rock", "Electronic"},
        },
        // ...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()

            got := ParseGenres(tt.raw)
            if !slicesEqual(got, tt.want) {
                t.Errorf(
                    "ParseGenres(%q) = %v, want %v",
                    tt.raw, got, tt.want,
                )
            }
        })
    }
}
```

### Parallel Tests

Use `t.Parallel()` at both the suite and subtest level. All unit tests use parallel execution:

```go
func TestSanitizeFilename(t *testing.T) {
    t.Parallel()       // top-level parallel

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()   // subtest parallel
            // ...
        })
    }
}
```

### File-Iteration Tests

For tests that iterate over real fixture files, use `t.Run` with the filename:

```go
// backend/metadata/mp3duration_test.go
func TestGetMP3Duration_MatchesBeepDecode(t *testing.T) {
    for _, path := range testMP3Files(t) {
        t.Run(filepath.Base(path), func(t *testing.T) {
            // compare fast parser vs full decode
            refMS, err := GetTrackLengthMillis(path)
            // ...
            if diffMS > toleranceMS {
                t.Errorf(
                    "duration mismatch: beep=%dms fast=%dms "+
                        "(diff %dms exceeds %dms tolerance)",
                    refMS, fastMS, diffMS, toleranceMS,
                )
            }
        })
    }
}
```

### Integration Test Pattern

The player integration test creates a real player instance and exercises it:

```go
// backend/player/player_test.go
func TestPlayer(t *testing.T) {
    if os.Getenv("YELLOWJACKET_INTEGRATION") == "" {
        t.Skip("skipping: integration test requires ...")
    }

    p := NewPlayer(slog.Default(), nil)

    if err := p.InitSpeaker(); err != nil {
        t.Fatalf("could not initialize speaker: %s", err.Error())
    }

    p.SetContext(t.Context())

    for _, track := range testQueue {
        if err := p.LoadFile(track); err != nil {
            t.Fatalf("could not load file %s: %s", track, err.Error())
        }
        if err := p.Play(); err != nil {
            t.Fatalf("could not play file %s: %s", track, err.Error())
        }
    }
}
```

## Mocking

**No mocking framework is used.** The codebase relies on:

1. **Interfaces for injection:** The `TrackLoader` interface in `backend/queue/queue.go` allows the queue to work with any player implementation:

```go
type TrackLoader interface {
    LoadFile(filePath string) error
    Play() error
    IsPlaying() bool
    CurrentPositionSeconds() (int, error)
    UnloadTrack()
}
```

2. **`nil` dependencies:** Tests pass `nil` for dependencies not needed:

```go
p := NewPlayer(slog.Default(), nil)  // nil database
```

3. **Real implementations:** Most tests exercise real code against test fixtures rather than mocks.

4. **Callback injection:** Cross-cutting behavior uses function callbacks rather than interface mocks:

```go
// Injected callback avoids queue→player circular dependency:
p.SetPlaybackFinishedHandler(handler func())

// Hook-based coordination:
l.SetRescanHooks(library.RescanHooks{
    PreClear: yj.queue.Clear,
    PostScan: yj.playlist.RestoreAllPlaylists,
})
```

## Test Helpers

### Custom Equality Functions

Since no assertion library is used, test files include local equality helpers:

```go
// backend/metadata/genre_test.go
func slicesEqual(a, b []string) bool {
    if len(a) == 0 && len(b) == 0 {
        return true
    }
    if len(a) != len(b) {
        return false
    }
    for i := range a {
        if a[i] != b[i] {
            return false
        }
    }
    return true
}

// backend/playlist/match_test.go
func stringSliceEqual(a, b []string) bool {
    // identical implementation
}
```

### Test File Builders

The `buildID3v2Header` helper in `backend/metadata/flacduration_test.go` creates synthetic audio file structures for testing:

```go
func buildID3v2Header(payloadSize int) []byte {
    header := []byte{
        'I', 'D', '3', // signature
        3, 0,           // version 2.3.0
        0,              // flags
        0, 0, 0, 0,     // size (syncsafe, filled below)
    }
    header[6] = byte((payloadSize >> 21) & 0x7F)
    header[7] = byte((payloadSize >> 14) & 0x7F)
    header[8] = byte((payloadSize >> 7) & 0x7F)
    header[9] = byte(payloadSize & 0x7F)
    return header
}
```

### `t.Helper()` Usage

Test helper functions call `t.Helper()` so failure line numbers point to the caller:

```go
func testMP3Files(t *testing.T) []string {
    t.Helper()
    // ...
}
```

### `t.Context()` Usage

Integration tests use `t.Context()` for the test context (enforced by `usetesting` linter):

```go
p.SetContext(t.Context())
```

### `//nolint` Annotations

Tests use `//nolint:mnd` for magic numbers in test data construction:

```go
//nolint:mnd // synthetic tag construction.
tag1Size := 1024
tag2Size := 2048

//nolint:mnd // expected offset after first tag.
expectedFirst := int64(10 + 100)

//nolint:mnd // byte values from manual FLAC spec packing.
var si [streamInfoLength]byte
si[10] = 0x0A
```

## Error Assertion Patterns

### Fatal vs Error

- `t.Fatalf` for setup failures that prevent the test from continuing
- `t.Errorf` for check failures that should be reported but allow remaining checks to run

```go
// Setup failure — stop immediately:
f, err := os.Open(path)
if err != nil {
    t.Fatalf("open: %v", err)
}

// Assertion failure — continue checking other fields:
if got != tt.want {
    t.Errorf(
        "SizedFilename(%q, %q) = %q, want %q",
        tt.filename, tt.suffix, got, tt.want,
    )
}
```

### Error Expectation

Tests that expect errors check for `nil`/`non-nil`:

```go
func TestWriteM3U8EmptyDir(t *testing.T) {
    t.Parallel()

    err := writeM3U8("", 1, "test", nil)
    if err == nil {
        t.Fatal("expected error for empty dir path")
    }
}
```

## Frontend Type Checking

No frontend test framework is configured. TypeScript correctness is verified via type checking:

```bash
cd frontend && pnpm exec tsc --noEmit
```

This validates all TypeScript files against the strict `tsconfig.json` settings without producing output files.

## Test Coverage

**Requirements:** No enforced coverage target.

**Coverage command:**
```bash
go test -tags webkit2_41 -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Test Types Summary

**Unit Tests:**
- All tests in `backend/metadata/`, `backend/coverart/`, `backend/playlist/`
- Test pure functions with table-driven patterns
- Use `t.Parallel()` for concurrent execution
- No external dependencies (except test fixtures)

**Integration Tests:**
- `backend/player/player_test.go`
- Requires audio hardware and Wails runtime
- Gated behind `YELLOWJACKET_INTEGRATION=1` env var
- Not run in CI

**E2E Tests:**
- Not implemented

**Frontend Tests:**
- Not implemented (type checking only via `tsc --noEmit`)

---

*Testing analysis: 2026-02-26*
