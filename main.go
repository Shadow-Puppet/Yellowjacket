// Package main is the entry point for the Yellowjacket application.
package main

import (
	"embed"
	"log/slog"
	"os"
	"strings"

	"github.com/golang-cz/devslog"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"yellowjacket/backend"
	"yellowjacket/backend/assets"
	"yellowjacket/backend/config"
	"yellowjacket/backend/profiling"
	"yellowjacket/backend/system"
	"yellowjacket/internal/dev"
)

// version and commit are set at build time via ldflags.
var (
	version = "dev"
	commit  = "unknown"
)

//go:embed all:frontend/dist
var frontendDistAssets embed.FS

func main() {
	// **Mobile has no home directory, and this must run before anything
	// asks for a path.** backend/system resolves config and data from
	// $HOME or the OS equivalent, and on Android there is neither: its
	// switch on runtime.GOOS took the default branch and returned
	// errUnsupportedOS, so NewYellowJacketApp failed and main() exited
	// six milliseconds after the JNI bridge came up -- with no panic and
	// no tombstone, because os.Exit is not a crash.
	//
	// StoragePath() is the platform's own answer (getFilesDir() on
	// Android, Application Support on iOS) and returns "" on desktop,
	// where UseHomeOverride is then a no-op -- so this needs no build
	// tag and changes nothing off mobile.
	system.UseHomeOverride(application.Mobile.StoragePath())

	// WebKitGTK's DMABuf renderer crashes on NVIDIA GPUs under Wayland.
	// Only disable it for that specific combo so AMD/Intel and X11 users
	// keep full hardware-accelerated buffer sharing.  Users can also
	// force the workaround with WEBKIT_DISABLE_DMABUF_RENDERER=1.
	if os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "" && isNVIDIAWayland() {
		_ = os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	}

	isDev := dev.IsDev
	// create sLogger
	loglevel := resolveLogLevel(isDev)

	sLogger := slog.New(devslog.NewHandler(os.Stdout, &devslog.Options{
		HandlerOptions: &slog.HandlerOptions{
			Level: loglevel,
		},
	}))
	slog.SetDefault(sLogger)
	sLogger.Info("starting yellowjacket", "version", version, "commit", commit)

	// Start profiling server (pprof + trace). In production builds this
	// is a no-op — the compiler eliminates all profiling code.
	stopProfiler := profiling.Start(sLogger)

	// create asset handler
	assetHandler, err := assets.NewAssetHandler(sLogger, frontendDistAssets)
	if err != nil {
		sLogger.Error("could not create asset handler", "err", err.Error())
		stopProfiler()
		os.Exit(1)
	}

	yjApp, err := backend.NewYellowJacketApp(sLogger, assetHandler)
	if err != nil {
		sLogger.Error("problem initializing yellowjacket", "err", err.Error())
		stopProfiler()
		os.Exit(1)
	}

	winCfg := yjApp.WindowConfig()

	app := application.New(application.Options{
		Name:        "yellowjacket",
		Description: "A cross-platform desktop music player",
		Services:    yjApp.Services,
		Assets:      assetHandler.Options,
		// v3 logs through slog directly, so v2's logger.Logger adapter
		// (backend/logging) is gone rather than ported.
		Logger:     sLogger,
		ShouldQuit: yjApp.ShouldQuit,
		OnShutdown: yjApp.OnShutdown,
	})

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "yellowjacket",
		Width:            winCfg.Width,
		Height:           winCfg.Height,
		MinWidth:         config.MinWidth,
		MinHeight:        config.MinHeight,
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
		Linux: application.LinuxWindow{
			WebviewGpuPolicy: application.WebviewGpuPolicyAlways,
		},
	})

	// The window's size has to be read while the window still exists,
	// which OnShutdown is too late for.
	window.OnWindowEvent(
		events.Common.WindowClosing,
		func(*application.WindowEvent) { yjApp.SaveWindowState(window) },
	)

	err = app.Run()

	stopProfiler()

	if err != nil {
		sLogger.Error("application error", "err", err.Error())
		os.Exit(1)
	}
}

// resolveLogLevel determines the slog level.  In dev mode the default
// is Info (not Debug) to avoid flooding stdout during library scans.
// Set YJ_LOG_LEVEL=debug to restore verbose logging.
//
// Accepted values: debug, info, warn, error (case-insensitive).
// Production builds always default to Info.
func resolveLogLevel(_ bool) slog.Level {
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

// isNVIDIAWayland returns true when running under a Wayland session with
// an NVIDIA GPU.  This combination triggers DMABuf rendering crashes in
// WebKitGTK, so we need to disable the DMABuf renderer for it.
func isNVIDIAWayland() bool {
	// Not Wayland → safe.
	if os.Getenv("WAYLAND_DISPLAY") == "" && os.Getenv("XDG_SESSION_TYPE") != "wayland" {
		return false
	}

	// Check for NVIDIA kernel modules (works even without nvidia-smi).
	if data, err := os.ReadFile("/proc/driver/nvidia/version"); err == nil {
		_ = data

		return true
	}

	// Fallback: check if the nvidia module is loaded.
	if data, err := os.ReadFile("/proc/modules"); err == nil {
		if strings.Contains(string(data), "nvidia") {
			return true
		}
	}

	return false
}
