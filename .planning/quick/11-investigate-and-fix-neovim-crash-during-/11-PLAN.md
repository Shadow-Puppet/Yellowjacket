---
phase: quick-11
plan: 11
type: execute
wave: 1
depends_on: []
files_modified:
  - main.go
  - Makefile
autonomous: true
requirements: []
must_haves:
  truths:
    - "Library scan no longer floods stdout with per-file debug lines at default dev log level"
    - "Dev mode defaults to Info-level logging instead of Debug"
    - "User can opt into Debug logging via YJ_LOG_LEVEL=debug environment variable"
    - "make dev continues to work as before (just quieter by default)"
  artifacts:
    - path: "main.go"
      provides: "Configurable slog level via YJ_LOG_LEVEL env var, defaulting to Info in dev"
      contains: "YJ_LOG_LEVEL"
  key_links:
    - from: "main.go"
      to: "slog.New"
      via: "YJ_LOG_LEVEL env var parsing"
      pattern: "YJ_LOG_LEVEL"
---

<objective>
Fix neovim crash/glitch during library scan by reducing stdout log volume.

Purpose: During a full library scan, the app emits 3-6+ Debug log lines per audio file to stdout
(queueing, saving, indexing, cover art processing). For a library with thousands of files, this
produces tens of thousands of lines flooding stdout. When neovim's overseer plugin captures the
`make dev` process output, this overwhelms the terminal buffer, corrupting neovim's display — the
user sees their terminal beneath a partially-rendered neovim window and has to `clear` and reopen.

The root cause is that dev mode hardcodes `slog.LevelDebug` with no way to override it. The fix:
1. Change dev mode default from Debug to Info (scan progress is already reported via Info-level
   "beginning library scan" and "library scan complete" messages)
2. Add YJ_LOG_LEVEL env var to allow opting into Debug when actually debugging
3. Add a convenience `make dev-debug` target for when verbose logging is needed

Output: Modified main.go with configurable log level, updated Makefile with dev-debug target.
</objective>

<execution_context>
@/home/caleb/.config/opencode/get-shit-done/workflows/execute-plan.md
@/home/caleb/.config/opencode/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
</context>

<tasks>

<task type="auto">
  <name>Task 1: Add configurable log level via YJ_LOG_LEVEL env var</name>
  <files>main.go</files>
  <action>
In main.go, replace the hardcoded log level logic:

Current code (lines 33-38):
```go
var loglevel slog.Level
if isDev {
    loglevel = slog.LevelDebug
} else {
    loglevel = slog.LevelInfo
}
```

Replace with env-var-based log level resolution:
```go
loglevel := resolveLogLevel(isDev)
```

Add a `resolveLogLevel` function (in main.go, before or after `main()`):

```go
// resolveLogLevel determines the slog level.  In dev mode the default
// is Info (not Debug) to avoid flooding stdout during library scans.
// Set YJ_LOG_LEVEL=debug to restore verbose logging.
//
// Accepted values: debug, info, warn, error (case-insensitive).
// Production builds always default to Info.
func resolveLogLevel(isDev bool) slog.Level {
	if env := os.Getenv("YJ_LOG_LEVEL"); env != "" {
		switch strings.ToLower(env) {
		case "debug":
			return slog.LevelDebug
		case "info":
			return slog.LevelInfo
		case "warn":
			return slog.LevelWarn
		case "error":
			return slog.LevelError
		}
	}

	// Default: Info for both dev and prod.
	return slog.LevelInfo
}
```

Add `"strings"` to the import block if not already present.

This changes dev default from Debug to Info. The ~14 Debug log lines per audio file during scan
will no longer appear, dramatically reducing stdout volume. Info-level messages like
"beginning library scan", "library scan complete", and "library data cleared successfully"
still appear so the user knows what's happening.
  </action>
  <verify>go build -tags webkit2_41 ./... compiles without errors</verify>
  <done>
  - Dev mode defaults to Info-level logging (not Debug)
  - YJ_LOG_LEVEL=debug restores verbose logging
  - YJ_LOG_LEVEL accepts debug/info/warn/error (case-insensitive)
  - No debug log flood during library scan at default level
  </done>
</task>

<task type="auto">
  <name>Task 2: Add make dev-debug convenience target</name>
  <files>Makefile</files>
  <action>
Add a `dev-debug` target after the existing `dev` target in Makefile:

```makefile
dev-debug: setup generate clean
	WEBKIT_DISABLE_DMABUF_RENDERER=1 YJ_LOG_LEVEL=debug go tool wails dev -tags webkit2_41 -loglevel Debug -v 2
```

This gives a one-command way to get the old verbose behavior when actually debugging.
The existing `dev` target stays unchanged (it now runs quieter because the Go app defaults to Info).
  </action>
  <verify>make -n dev-debug shows the correct command with YJ_LOG_LEVEL=debug</verify>
  <done>
  - `make dev-debug` target exists and sets YJ_LOG_LEVEL=debug
  - `make dev` continues to work unchanged (but quieter due to Task 1)
  </done>
</task>

</tasks>

<verification>
- `go build -tags webkit2_41 ./...` compiles cleanly
- `make -n dev` shows normal command (no YJ_LOG_LEVEL)
- `make -n dev-debug` shows command with YJ_LOG_LEVEL=debug
- Grep main.go for `resolveLogLevel` function and `YJ_LOG_LEVEL` usage
</verification>

<success_criteria>
- Dev mode no longer floods stdout with Debug-level per-file scan logs
- User can opt into Debug logging via YJ_LOG_LEVEL=debug or `make dev-debug`
- No behavioral changes to the application itself (only log verbosity)
</success_criteria>

<output>
After completion, create `.planning/quick/11-investigate-and-fix-neovim-crash-during-/11-SUMMARY.md`
</output>
