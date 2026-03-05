---
phase: quick-13
plan: 13
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/database/testhelper.go
  - backend/database/search_test.go
  - backend/events/cmd/genevents/main.go
  - backend/library/library.go
  - backend/library/scan_test.go
  - backend/config/config_test.go
  - backend/queue/navigation_test.go
  - backend/queue/queue_test.go
autonomous: true
must_haves:
  truths:
    - "golangci-lint run ./... reports 0 issues"
    - "All existing tests still pass"
  artifacts:
    - path: "backend/database/testhelper.go"
      provides: "errcheck fix for db.Close()"
    - path: "backend/database/search_test.go"
      provides: "Remove unused types, fix wsl/golines issues"
    - path: "backend/events/cmd/genevents/main.go"
      provides: "Fix errcheck, nlreturn, wsl issues"
    - path: "backend/library/library.go"
      provides: "Fix gofumpt and wsl issues"
    - path: "backend/config/config_test.go"
      provides: "Fix golines and wsl issues"
    - path: "backend/queue/navigation_test.go"
      provides: "Fix intrange issue"
    - path: "backend/queue/queue_test.go"
      provides: "Fix intrange issue"
    - path: "backend/library/scan_test.go"
      provides: "Fix wsl trailing whitespace"
  key_links: []
---

<objective>
Fix all 31 golangci-lint issues across 8 files. All fixes are mechanical (whitespace, error checking, unused code removal, loop modernization) with zero behavior change.

Purpose: Clean lint output for the codebase.
Output: Zero lint issues from golangci-lint.
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
  <name>Task 1: Fix lint issues in main source files (library.go, genevents/main.go, testhelper.go)</name>
  <files>
    backend/library/library.go
    backend/events/cmd/genevents/main.go
    backend/database/testhelper.go
  </files>
  <action>
  **backend/database/testhelper.go** (1 errcheck):
  - Line 66: Change `db.Close()` to `_ = db.Close()` inside the t.Cleanup func

  **backend/events/cmd/genevents/main.go** (10 issues: 4 errcheck, 3 nlreturn, 3+ wsl):
  - Line 35: Add blank line before `return`
  - Line 61: Add blank line before `if err != nil` (wsl: only one cuddle assignment before if)
  - Line 85: Add blank line before `for i, name := range vs.Names {`
  - Line 89: Move `bl, ok := ...` assignment so it's not cuddled incorrectly — add blank line before it
  - Line 90: Add blank line before `if !ok || bl.Kind != token.STRING`
  - Line 113: Add blank line before `return s`
  - Line 129: Add blank line before `for _, c := range g.Consts`
  - Line 150: Add blank line before `if err != nil`
  - Line 153: Add blank line before `tmpName := tmp.Name()`
  - Line 156: Change `tmp.Close()` to `_ = tmp.Close()` (errcheck)
  - Line 157: Change `os.Remove(tmpName)` to `_ = os.Remove(tmpName)` (errcheck)
  - Line 160: Add blank line before `if err := tmp.Close()` 
  - Line 161: Change `os.Remove(tmpName)` to `_ = os.Remove(tmpName)` (errcheck)
  - Line 164: Add blank line before `return os.Rename(tmpName, path)`

  **backend/library/library.go** (2 issues: 1 gofumpt, 1 wsl):
  - Line 232: Add blank line before `workChan := make(...)` 
  - Line 534-536: Reformat the ScanProgress struct literal so gofumpt is happy — put opening brace on same line as `runtime.EventsEmit(l.ctx, events.LibraryScanProgress,` and format the struct fields properly (run gofumpt to check exact formatting needed)
  </action>
  <verify>golangci-lint run ./backend/database/ ./backend/events/... ./backend/library/ 2>&1 | grep -E "errcheck|nlreturn|gofumpt|wsl" | grep -E "testhelper|main\.go|library\.go" | wc -l should be 0</verify>
  <done>All errcheck, nlreturn, gofumpt, and wsl issues fixed in the 3 main source files</done>
</task>

<task type="auto">
  <name>Task 2: Fix lint issues in test files</name>
  <files>
    backend/database/search_test.go
    backend/config/config_test.go
    backend/queue/navigation_test.go
    backend/queue/queue_test.go
    backend/library/scan_test.go
  </files>
  <action>
  **backend/database/search_test.go** (8 issues: 2 unused, 2 golines, 4 wsl):
  - Lines 57-65: Remove the unused `artistEntry` and `albumEntry` type definitions entirely
  - Line 47: Break long track initialization line into multiple lines (golines)
  - Line 69: Add blank line before `var artistID, albumID int64`
  - Line 107: Add blank line before `var genreID int64`
  - Line 371: Add blank line before `for _, r := range results`
  - Line 435: Add blank line before `for _, r := range results`
  - Lines 783-784: Add blank line before `t.Fatal(...)` 
  - Lines 788-789: Add blank line before `t.Fatalf(...)`

  **backend/config/config_test.go** (2 issues: 1 golines, 1 wsl):
  - Line 75: Break long t.Errorf line across multiple lines
  - Line 212: Remove trailing blank line before closing `}`

  **backend/queue/navigation_test.go** (1 intrange):
  - Line 17: Change `for i := 0; i < tracks; i++` to `for i := range tracks`

  **backend/queue/queue_test.go** (1 intrange):
  - Line 56: Change `for i := 0; i < count; i++` to `for i := range count`

  **backend/library/scan_test.go** (1 wsl):
  - Line 659: Remove trailing blank line before closing `}`
  </action>
  <verify>golangci-lint run ./... 2>&1 | grep -c "issue" should show "0 issues" and go test ./backend/... should pass</verify>
  <done>All 31 lint issues resolved, golangci-lint reports 0 issues, all tests pass</done>
</task>

</tasks>

<verification>
golangci-lint run ./... 2>&1 — should report 0 issues (excluding deprecation warnings)
go test ./backend/... — all tests pass
</verification>

<success_criteria>
- golangci-lint run ./... reports 0 issues
- All existing tests continue to pass
- No behavioral changes to any code
</success_criteria>

<output>
After completion, create `.planning/quick/13-fix-linting-issues-without-significant-c/13-SUMMARY.md`
</output>
