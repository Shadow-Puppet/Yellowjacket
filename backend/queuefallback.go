package backend

import (
	"context"

	"yellowjacket/backend/config"
	"yellowjacket/backend/explore"
	"yellowjacket/backend/playlist"
	"yellowjacket/backend/queue"
)

// queueFallbackAdapter implements queue.FallbackSource, translating
// the queue's generic "what plays next" question into the configured
// mode plus whichever service (playlist or explore) can answer it —
// so the queue package itself never needs to import config, playlist
// or explore.
type queueFallbackAdapter struct {
	config   *config.Config
	playlist *playlist.Service
	explore  *explore.Service
}

// ResolveFallback implements queue.FallbackSource.
//
// A dynamic mix in progress is not interrupted by a mode change —
// once started, it keeps extending itself regardless of what the
// configured mode currently says — everything else (a real selection
// running out, or a Favorites fallback finishing) resolves fresh
// according to the configured mode exactly once.
func (a *queueFallbackAdapter) ResolveFallback(
	ctx context.Context,
	fctx queue.FallbackContext,
) ([]string, queue.Source, error) {
	continuing := fctx.PreviousSource.Type == "dynamicMix"

	mode := config.QueueFallback(a.config.GetQueueFallback())
	if continuing {
		mode = config.QueueFallbackDynamicMix
	}

	switch mode {
	case config.QueueFallbackFavorites:
		return a.resolveFavorites()
	case config.QueueFallbackDynamicMix:
		return a.resolveDynamicMix(ctx, fctx.SeedPaths, continuing)
	case config.QueueFallbackStop:
		return nil, queue.Source{}, nil
	default:
		return nil, queue.Source{}, nil
	}
}

func (a *queueFallbackAdapter) resolveFavorites() ([]string, queue.Source, error) {
	paths, err := a.playlist.GetDefaultPlaylistTrackPaths()
	if err != nil || len(paths) == 0 {
		return nil, queue.Source{}, err
	}

	info, err := a.playlist.GetDefaultPlaylistInfo()
	if err != nil {
		return nil, queue.Source{}, err
	}

	return paths, queue.Source{Type: "playlist", ID: info.ID, Label: info.Name}, nil
}

func (a *queueFallbackAdapter) resolveDynamicMix(
	ctx context.Context,
	seedPaths []string,
	continuing bool,
) ([]string, queue.Source, error) {
	paths, label, err := a.explore.GenerateMix(ctx, seedPaths, continuing)
	if err != nil || len(paths) == 0 {
		return nil, queue.Source{}, err
	}

	return paths, queue.Source{Type: "dynamicMix", Label: label}, nil
}
