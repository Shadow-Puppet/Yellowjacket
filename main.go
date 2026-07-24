// Package main is the entry point for the Yellowjacket application.
package main

import (
	"embed"
	"log/slog"
	"os"
	"strings"

	"github.com/golang-cz/devslog"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/linux"

	"yellowjacket/backend"
	"yellowjacket/backend/assets"
	"yellowjacket/backend/config"
	"yellowjacket/backend/logging"
	"yellowjacket/backend/profiling"
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

	// Create application with options
	winCfg := yjApp.WindowConfig()

	err = wails.Run(&options.App{
		Title:  "yellowjacket",
		Width:  winCfg.Width,
		Height: winCfg.Height,
		Logger: logging.NewLogger(
			sLogger,
			[]string{},
		),
		AssetServer:      assetHandler.Options,
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        yjApp.OnStartup,
		OnDomReady:       yjApp.OnDomReady,
		OnBeforeClose:    yjApp.OnBeforeClose,
		OnShutdown:       yjApp.OnShutdown,
		Bind:             yjApp.FEBindings,
		MinWidth:         config.MinWidth,
		MinHeight:        config.MinHeight,
		MaxWidth:         0,
		MaxHeight:        0,
		Linux: &linux.Options{
			WebviewGpuPolicy: linux.WebviewGpuPolicyAlways,
		},
	})

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
