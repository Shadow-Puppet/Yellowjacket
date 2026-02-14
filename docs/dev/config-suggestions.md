# Config Improvement Suggestions

Remaining suggestions for improving the configuration system in YellowJacket.

## 2. Thread Safety Concerns

The current `Config` struct lacks synchronization:
- `Load()` and `Save()` can race with concurrent reads
- `handleConfigUpdate()` in library mutates `l.conf.DirectoryPath` without locks

**Suggestion:** Add a `sync.RWMutex` to protect config access, especially if config is read during scans.

```go
type Config struct {
    mu       sync.RWMutex
    ctx      context.Context
    logger   *slog.Logger
    // ...
}

func (c *Config) Load() error {
    c.mu.Lock()
    defer c.mu.Unlock()
    // ...
}
```

## 3. Nil Safety in Validation

In `config.go`, validation only runs if `c.Library != nil`, but `handleConfigPost` dereferences `postedConfig.Library` without checking for nil:

```go
if postedConfig.Library != nil {
    c.Library = postedConfig.Library
    // ...
}
```

**Status:** Partially addressed in the event refactor, but consider adding explicit nil checks in `Validate()` as well.

## 4. Inconsistent Error Handling on HTTP Responses

In `httphandler.go:28-31`, `WriteHeader` is called *after* rendering the error template, which won't work as expected (headers must be set before writing body):

```go
c.formSubmitError(err.Error()).Render(r.Context(), w)
w.WriteHeader(http.StatusInternalServerError)  // Too late!
```

**Fix:** Set the status code before rendering:

```go
w.WriteHeader(http.StatusInternalServerError)
c.formSubmitError(err.Error()).Render(r.Context(), w)
```

## 5. Make `scanWorkerCount` Configurable

There's a TODO at `library.go:289`:
```go
// TODO: make configurable via Config.
var scanWorkerCount = goruntime.NumCPU()
```

**Suggestion:** Add this to `library.Config`:

```go
type Config struct {
    DirectoryPath Directory `form:"Directory" schema:"directory,required"`
    ScanWorkers   int       `form:"ScanWorkers" schema:"scan_workers"`
}
```

Then in `NewLibrary()` or `Scan()`:

```go
workers := l.conf.ScanWorkers
if workers <= 0 {
    workers = goruntime.NumCPU()
}
```

## 6. Consider Config Defaults

Currently if no config exists, an empty one is saved. Consider providing sensible defaults (e.g., common music directories like `~/Music`).

```go
func (c *Config) setDefaults() {
    if c.Library == nil {
        c.Library = &library.Config{}
    }
    if c.Library.DirectoryPath == "" {
        // Try common music directories
        home, _ := os.UserHomeDir()
        musicDir := filepath.Join(home, "Music")
        if info, err := os.Stat(musicDir); err == nil && info.IsDir() {
            c.Library.DirectoryPath = library.Directory(musicDir)
        }
    }
}
```

## 7. Config Reload/Watch Capability

The config is only loaded at startup. Consider adding:
- File watcher for external config changes (using `fsnotify`)
- Explicit reload method callable from UI

```go
func (c *Config) Watch() error {
    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        return err
    }
    
    go func() {
        for event := range watcher.Events {
            if event.Op&fsnotify.Write == fsnotify.Write {
                c.Load()
                // Emit event for listeners
            }
        }
    }()
    
    return watcher.Add(c.filePath)
}
```

## 8. Validation Should Return Structured Errors

Currently validation returns combined errors. Consider returning a structured validation result that the UI can map to specific fields for better user feedback.

```go
type ValidationError struct {
    Field   string
    Message string
}

type ValidationResult struct {
    Valid  bool
    Errors []ValidationError
}

func (c *Config) ValidateStructured() ValidationResult {
    var result ValidationResult
    result.Valid = true
    
    if c.Library != nil {
        if err := c.Library.Validate(); err != nil {
            result.Valid = false
            result.Errors = append(result.Errors, ValidationError{
                Field:   "Library.DirectoryPath",
                Message: err.Error(),
            })
        }
    }
    
    return result
}
```

## 9. Use Standard Library for Config Paths

The path construction in `system/userdata.go` doesn't respect `$XDG_CONFIG_HOME` on Linux or use the standard Go `os.UserConfigDir()`.

**Current implementation:**
```go
case "linux":
    return fmt.Sprintf("/home/%s/%s/yellowjacket", username, unixSubdirs[dt]), nil
```

**Suggested improvement:**
```go
func GetUserConfigDirPath() (string, error) {
    baseDir, err := os.UserConfigDir()  // Respects XDG_CONFIG_HOME
    if err != nil {
        return "", fmt.Errorf("could not get user config directory: %w", err)
    }
    
    path := filepath.Join(baseDir, "yellowjacket")
    
    if err := os.MkdirAll(path, 0o755); err != nil {
        return "", fmt.Errorf("could not create config directory: %w", err)
    }
    
    return path, nil
}
```

This approach:
- Respects `$XDG_CONFIG_HOME` on Linux
- Uses proper macOS paths (`~/Library/Application Support`)
- Uses `%AppData%` on Windows
- Is more portable and follows platform conventions
