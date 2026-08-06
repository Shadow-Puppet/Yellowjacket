package download

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// Lidarr's Lister role: mirroring this app's wanted list into Lidarr's
// own monitoring.
//
// Lidarr already models exactly what a want is — a monitored artist or
// a monitored album — so the mapping is direct and, more usefully, it
// means an artist subscription made here keeps working through Lidarr's
// own release-checking even while this app is closed.  That is the
// whole reason the Lister role exists: a desktop player is not running
// most of the time, and a always-on system that already watches for new
// releases is a better place for a subscription to live than a loop
// that only ticks when someone opens the app.
//
// The mapping is deliberately lossy in one direction only:
//
//	artist want         -> Lidarr artist, monitored
//	release-group want  -> Lidarr album, monitored (artist added if new)
//	release want        -> same, at release-group granularity
//	recording want      -> not pushed; Lidarr has no concept of wanting
//	                       one track, and monitoring the whole album to
//	                       get it would download far more than asked.
//
// Nothing here searches.  Pushing a want expresses intent; Lidarr
// decides when to act on it, which is the point of delegating.

// PushWant records a want in Lidarr's own monitoring.
//
// It is idempotent because Lidarr is: adding an artist that already
// exists returns the existing record, and monitoring an already-
// monitored album is a no-op.  Callers rely on that — the reconciler
// pushes on every pass until it gets an ID back.
func (l *lidarr) PushWant(ctx context.Context, w Want) (string, error) {
	switch w.Entity {
	case EntityArtist:
		return l.pushArtistWant(ctx, w)
	case EntityReleaseGroup, EntityRelease:
		return l.pushAlbumWant(ctx, w)
	case EntityRecording:
		// Deliberately unsupported rather than approximated.  See the
		// mapping note above.
		return "", nil
	default:
		return "", fmt.Errorf("%w: entity %q", ErrUnsupported, w.Entity)
	}
}

// pushArtistWant makes Lidarr monitor an artist.
//
// Scope is honoured through Lidarr's own monitor option rather than by
// pushing each album separately: "future" maps to monitoring new
// releases only, "all" to monitoring everything missing.  Letting
// Lidarr apply the policy means it stays applied to albums released
// after this push, which is what a subscription is for.
func (l *lidarr) pushArtistWant(ctx context.Context, w Want) (string, error) {
	existing, err := l.findArtistByMBID(ctx, w.MBID)
	if err != nil {
		return "", err
	}

	monitor := "future"
	if w.Scope == ScopeAll {
		monitor = "missing"
	}

	if existing.ID != 0 {
		return strconv.Itoa(existing.ID), nil
	}

	root, err := l.resolveRootFolder(ctx)
	if err != nil {
		return "", err
	}

	quality, metadata, err := l.profiles(ctx)
	if err != nil {
		return "", err
	}

	name := w.Artist
	if name == "" {
		name = w.Title
	}

	body := map[string]any{
		"foreignArtistId":   w.MBID,
		"artistName":        name,
		"qualityProfileId":  quality,
		"metadataProfileId": metadata,
		"rootFolderPath":    root,
		"monitored":         true,
		"addOptions": map[string]any{
			"monitor": monitor,
			// Searching is left off even for a full-discography
			// subscription: adding an artist should not launch forty
			// simultaneous searches on a system the user shares with
			// their own queue.  Lidarr picks the albums up on its next
			// scheduled search.
			"searchForMissingAlbums": false,
		},
	}

	var created struct {
		ID int `json:"id"`
	}

	if err := l.client.post(ctx, "/api/v1/artist", body, &created); err != nil {
		return "", err
	}

	return strconv.Itoa(created.ID), nil
}

// pushAlbumWant makes Lidarr monitor one album, adding its artist if
// Lidarr has never heard of them.
func (l *lidarr) pushAlbumWant(ctx context.Context, w Want) (string, error) {
	album, err := l.findAlbum(ctx, Request{
		ReleaseGroupMBID: w.MBID,
		Artist:           w.Artist,
		Album:            w.Title,
	})
	if err != nil {
		return "", err
	}

	if album.ID == 0 {
		album, err = l.addArtistForAlbum(ctx, album)
		if err != nil {
			return "", err
		}
	}

	if !album.Monitored {
		if err := l.monitorAlbum(ctx, album.ID); err != nil {
			return "", err
		}
	}

	return strconv.Itoa(album.ID), nil
}

// RemoveWant stops Lidarr monitoring something.
//
// It unmonitors rather than deletes: the user's Lidarr may have been
// monitoring that artist long before this app existed, and removing a
// want here is not permission to tear down their setup.  An unmonitored
// artist stays in their library with its files intact.
func (l *lidarr) RemoveWant(ctx context.Context, externalID string) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("%w: bad lidarr id %q", ErrLidarrNoMatch, externalID)
	}

	// The ID may name an album or an artist and the caller does not
	// track which, so try the album endpoint first and fall back.
	if err := l.client.put(ctx, "/api/v1/album/monitor", map[string]any{
		"albumIds":  []int{id},
		"monitored": false,
	}, nil); err == nil {
		return nil
	}

	var artist map[string]any

	endpoint := "/api/v1/artist/" + strconv.Itoa(id)

	if err := l.client.get(ctx, endpoint, &artist); err != nil {
		return err
	}

	artist["monitored"] = false

	return l.client.put(ctx, endpoint, artist, nil)
}

// ListWants reads Lidarr's monitored artists back, for the deliberate
// "import what Lidarr is already watching" action.
//
// Only artists are imported, not their individual monitored albums: an
// artist is the durable statement of intent, and importing every
// monitored album alongside it would produce a wanted list that is
// mostly redundant with the subscription that generated it.
func (l *lidarr) ListWants(ctx context.Context) ([]Want, error) {
	var artists []struct {
		lidarrArtist

		Monitored bool `json:"monitored"`
	}

	if err := l.client.get(ctx, "/api/v1/artist", &artists); err != nil {
		return nil, err
	}

	out := make([]Want, 0, len(artists))

	for _, a := range artists {
		if !a.Monitored || a.ForeignArtistID == "" {
			continue
		}

		out = append(out, Want{
			MBID:   a.ForeignArtistID,
			Entity: EntityArtist,
			Artist: a.ArtistName,
			Title:  a.ArtistName,
			// Imported subscriptions take the conservative scope: the
			// user can widen it, but silently queueing a back catalogue
			// on import would be a nasty surprise.
			Scope: ScopeFuture,
		})
	}

	return out, nil
}

// findArtistByMBID looks up an artist Lidarr already has.
func (l *lidarr) findArtistByMBID(
	ctx context.Context,
	mbid string,
) (lidarrArtist, error) {
	var artists []lidarrArtist

	endpoint := "/api/v1/artist?mbId=" + url.QueryEscape(mbid)

	if err := l.client.get(ctx, endpoint, &artists); err != nil {
		return lidarrArtist{}, err
	}

	for _, a := range artists {
		if a.ForeignArtistID == mbid {
			return a, nil
		}
	}

	return lidarrArtist{}, nil
}

// resolveRootFolder returns the configured root folder, or Lidarr's
// first if none is configured.
func (l *lidarr) resolveRootFolder(ctx context.Context) (string, error) {
	if l.rootFolderPath != "" {
		return l.rootFolderPath, nil
	}

	folders, err := l.rootFolders(ctx)
	if err != nil {
		return "", err
	}

	if len(folders) == 0 {
		return "", ErrLidarrNoRootFolder
	}

	return folders[0].Path, nil
}
