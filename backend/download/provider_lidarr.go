package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Lidarr is the delegate shape, and it is genuinely different from the
// other two: we do not search, we do not move bytes, and we do not own
// the import.  We hand Lidarr an album and ask periodically whether it
// is done.
//
// The consequence that shapes this adapter: when Lidarr finishes, the
// files are already in Lidarr's library, tagged by Lidarr, at paths
// Lidarr chose.  Re-importing them would mean moving files out from
// under a system that is actively managing them.  So a completed
// delegate returns the paths Lidarr reports and the pipeline skips
// tagging and moving — it reconciles rather than imports.

// Lidarr provider errors.
var (
	// ErrLidarrUnreachable means the instance did not answer.
	ErrLidarrUnreachable = errors.New("lidarr is unreachable")

	// ErrLidarrAuth means the API key was rejected.
	ErrLidarrAuth = errors.New("lidarr rejected the API key")

	// ErrLidarrNoMatch means Lidarr could not find the release.
	ErrLidarrNoMatch = errors.New("lidarr could not find this release")

	// ErrLidarrNoRootFolder means no root folder is configured, so
	// Lidarr has nowhere to put anything it finds.
	ErrLidarrNoRootFolder = errors.New("lidarr has no root folder configured")
)

// lidarrHTTPTimeout bounds one API call.
const lidarrHTTPTimeout = 30 * time.Second

func init() {
	Register(
		Descriptor{
			Kind: KindLidarr,
			Name: "Lidarr",
			Summary: "Hand album requests to an existing Lidarr instance " +
				"and let it do the searching and importing.",
			RequiresExternal: "Lidarr",
			Caps: Caps{
				CanDelegate: true,
				CanCancel:   true,
				CanList:     true,
			},
			Fields: []Field{
				{
					Key:         "url",
					Label:       "Lidarr URL",
					Placeholder: "http://localhost:8686",
					Required:    true,
					Default:     "http://localhost:8686",
				},
				{
					Key:      "apiKey",
					Label:    "API key",
					Secret:   true,
					Required: true,
					Help:     "Lidarr → Settings → General → API Key.",
				},
				{
					Key:   "qualityProfileId",
					Label: "Quality profile ID",
					Help: "Numeric ID of the Lidarr quality profile to use. " +
						"Leave blank to use the first one.",
				},
				{
					Key:   "metadataProfileId",
					Label: "Metadata profile ID",
					Help:  "Leave blank to use the first one.",
				},
				{
					Key:   "rootFolderPath",
					Label: "Root folder",
					Help: "Leave blank to use Lidarr's first configured " +
						"root folder.",
				},
			},
		},
		newLidarr,
	)
}

// lidarr is the Lidarr delegate provider.
type lidarr struct {
	info   ProviderInfo
	logger *slog.Logger
	client *apiClient

	qualityProfileID  int
	metadataProfileID int
	rootFolderPath    string
}

// newLidarr builds the provider from config.
func newLidarr(
	cfg Config,
	secrets SecretLookup,
	logger *slog.Logger,
) (Provider, error) {
	base := strings.TrimRight(cfg.Setting("url", ""), "/")
	if base == "" {
		return nil, fmt.Errorf("%w: Lidarr URL is required", ErrNotConfigured)
	}

	apiKey := ""

	if secrets != nil {
		key, err := secrets("apiKey")
		if err != nil {
			return nil, fmt.Errorf("%w: no API key stored", ErrNotConfigured)
		}

		apiKey = key
	}

	quality, _ := strconv.Atoi(cfg.Setting("qualityProfileId", "0"))
	metadata, _ := strconv.Atoi(cfg.Setting("metadataProfileId", "0"))

	return &lidarr{
		info: ProviderInfo{
			ID:       cfg.ID,
			Kind:     KindLidarr,
			Name:     cfg.Name,
			Enabled:  cfg.Enabled,
			Priority: cfg.Priority,
			Caps: Caps{
				CanDelegate: true,
				CanCancel:   true,
				CanList:     true,
			},
		},
		logger: logger.With("provider", "lidarr"),
		client: newAPIClient(
			base, "X-Api-Key", apiKey, lidarrHTTPTimeout,
			ErrLidarrUnreachable, ErrLidarrAuth,
		),
		qualityProfileID:  quality,
		metadataProfileID: metadata,
		rootFolderPath:    cfg.Setting("rootFolderPath", ""),
	}, nil
}

// Info returns the provider's identity.
func (l *lidarr) Info() ProviderInfo {
	return l.info
}

// Close is a no-op.
func (l *lidarr) Close() error {
	return nil
}

// Check verifies the instance answers and has somewhere to put music.
func (l *lidarr) Check(ctx context.Context) error {
	var status struct {
		Version string `json:"version"`
	}

	if err := l.client.get(ctx, "/api/v1/system/status", &status); err != nil {
		return err
	}

	folders, err := l.rootFolders(ctx)
	if err != nil {
		return err
	}

	if len(folders) == 0 {
		return ErrLidarrNoRootFolder
	}

	return nil
}

// ---------------------------------------------------------------------------
// API types
// ---------------------------------------------------------------------------

// lidarrAlbum is the subset of Lidarr's album resource used here.
type lidarrAlbum struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	ForeignAlbum string `json:"foreignAlbumId"`
	Monitored    bool   `json:"monitored"`
	ArtistID     int    `json:"artistId"`

	Artist     lidarrArtist     `json:"artist"`
	Statistics lidarrAlbumStats `json:"statistics"`
}

// lidarrArtist is the artist an album belongs to.  Lidarr models albums
// as children of artists, so an album it does not yet know about cannot
// be monitored until its artist exists.
type lidarrArtist struct {
	ID              int    `json:"id"`
	ArtistName      string `json:"artistName"`
	ForeignArtistID string `json:"foreignArtistId"`
}

// lidarrAlbumStats is how Lidarr reports import progress.
type lidarrAlbumStats struct {
	TrackFileCount  int     `json:"trackFileCount"`
	TrackCount      int     `json:"trackCount"`
	PercentOfTracks float64 `json:"percentOfTracks"`
}

// lidarrRootFolder is a configured library root.
type lidarrRootFolder struct {
	ID   int    `json:"id"`
	Path string `json:"path"`
}

// lidarrTrackFile is one imported file.
type lidarrTrackFile struct {
	ID      int    `json:"id"`
	Path    string `json:"path"`
	AlbumID int    `json:"albumId"`
}

// ---------------------------------------------------------------------------
// Delegate
// ---------------------------------------------------------------------------

// Delegate adds the album to Lidarr, monitors it and triggers a search.
// The external ID returned is Lidarr's album ID, which is what Poll
// needs and what survives a restart.
func (l *lidarr) Delegate(ctx context.Context, dl Download) (string, error) {
	album, err := l.findAlbum(ctx, dl)
	if err != nil {
		return "", err
	}

	// An album Lidarr already knows about only needs monitoring turned
	// on; one it does not needs its artist added first, because Lidarr
	// models albums as children of artists.
	if album.ID == 0 {
		added, err := l.addArtistForAlbum(ctx, album)
		if err != nil {
			return "", err
		}

		album = added
	}

	if !album.Monitored {
		if err := l.monitorAlbum(ctx, album.ID); err != nil {
			return "", err
		}
	}

	if err := l.command(ctx, map[string]any{
		"name":     "AlbumSearch",
		"albumIds": []int{album.ID},
	}); err != nil {
		return "", err
	}

	return strconv.Itoa(album.ID), nil
}

// findAlbum looks for the requested album, preferring the MusicBrainz
// release-group ID because that is unambiguous where a title search is
// not.
func (l *lidarr) findAlbum(
	ctx context.Context,
	dl Download,
) (lidarrAlbum, error) {
	term := dl.SearchText()

	if dl.ReleaseGroupMBID != "" {
		term = "lidarr:" + dl.ReleaseGroupMBID
	}

	var results []struct {
		Album lidarrAlbum `json:"album"`
	}

	endpoint := "/api/v1/search?term=" + url.QueryEscape(term)

	if err := l.client.get(ctx, endpoint, &results); err != nil {
		return lidarrAlbum{}, err
	}

	for _, r := range results {
		if r.Album.Title != "" {
			return r.Album, nil
		}
	}

	return lidarrAlbum{}, fmt.Errorf("%w: %s", ErrLidarrNoMatch, dl.SearchText())
}

// addArtistForAlbum adds the album's artist so the album becomes a real
// record Lidarr can monitor.
func (l *lidarr) addArtistForAlbum(
	ctx context.Context,
	album lidarrAlbum,
) (lidarrAlbum, error) {
	root := l.rootFolderPath

	if root == "" {
		folders, err := l.rootFolders(ctx)
		if err != nil {
			return lidarrAlbum{}, err
		}

		if len(folders) == 0 {
			return lidarrAlbum{}, ErrLidarrNoRootFolder
		}

		root = folders[0].Path
	}

	quality, metadata, err := l.profiles(ctx)
	if err != nil {
		return lidarrAlbum{}, err
	}

	body := map[string]any{
		"foreignArtistId":   album.Artist.ForeignArtistID,
		"artistName":        album.Artist.ArtistName,
		"qualityProfileId":  quality,
		"metadataProfileId": metadata,
		"rootFolderPath":    root,
		"monitored":         true,
		"addOptions": map[string]any{
			// Monitor nothing by default and turn on just the requested
			// album below.  Adding an artist with everything monitored
			// would kick off downloads of their entire discography,
			// which is emphatically not what the user asked for.
			"monitor":                "none",
			"searchForMissingAlbums": false,
		},
	}

	var created struct {
		ID int `json:"id"`
	}

	if err := l.client.post(ctx, "/api/v1/artist", body, &created); err != nil {
		return lidarrAlbum{}, err
	}

	// Re-resolve the album now that its artist exists.
	var albums []lidarrAlbum

	endpoint := "/api/v1/album?artistId=" + strconv.Itoa(created.ID)

	if err := l.client.get(ctx, endpoint, &albums); err != nil {
		return lidarrAlbum{}, err
	}

	for _, a := range albums {
		if a.ForeignAlbum == album.ForeignAlbum {
			return a, nil
		}
	}

	return lidarrAlbum{}, fmt.Errorf(
		"%w: album not present after adding artist", ErrLidarrNoMatch,
	)
}

// monitorAlbum turns on monitoring for one album.
func (l *lidarr) monitorAlbum(ctx context.Context, albumID int) error {
	body := map[string]any{
		"albumIds":  []int{albumID},
		"monitored": true,
	}

	return l.client.put(ctx, "/api/v1/album/monitor", body, nil)
}

// Poll reports whether Lidarr has finished with the album.
//
// Completion is judged by imported track files rather than by queue
// state: the queue empties when a download finishes, which is before
// the import happens, and reporting success then would have the
// pipeline reconcile files that are not there yet.
func (l *lidarr) Poll(
	ctx context.Context,
	externalID string,
) (DelegateStatus, error) {
	albumID, err := strconv.Atoi(externalID)
	if err != nil {
		return DelegateStatus{}, fmt.Errorf(
			"%w: bad album id %q", ErrLidarrNoMatch, externalID,
		)
	}

	var album lidarrAlbum

	endpoint := "/api/v1/album/" + strconv.Itoa(albumID)

	if err := l.client.get(ctx, endpoint, &album); err != nil {
		return DelegateStatus{}, err
	}

	total := album.Statistics.TrackCount
	got := album.Statistics.TrackFileCount

	progress := -1.0
	if total > 0 {
		progress = float64(got) / float64(total)
	}

	if total > 0 && got >= total {
		paths, err := l.trackFilePaths(ctx, albumID)
		if err != nil {
			return DelegateStatus{}, err
		}

		return DelegateStatus{
			State:         StateComplete,
			Progress:      1,
			ImportedPaths: paths,
			Message: fmt.Sprintf(
				"Lidarr imported %d of %d tracks", got, total,
			),
		}, nil
	}

	return DelegateStatus{
		State:    StateGrabbing,
		Progress: progress,
		Message: fmt.Sprintf(
			"Lidarr has %d of %d tracks", got, total,
		),
	}, nil
}

// trackFilePaths returns the on-disk paths Lidarr imported.
func (l *lidarr) trackFilePaths(
	ctx context.Context,
	albumID int,
) ([]string, error) {
	var files []lidarrTrackFile

	endpoint := "/api/v1/trackfile?albumId=" + strconv.Itoa(albumID)

	if err := l.client.get(ctx, endpoint, &files); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(files))

	for _, f := range files {
		if f.Path != "" {
			out = append(out, f.Path)
		}
	}

	return out, nil
}

// Withdraw stops monitoring the album so Lidarr gives up on it.  The
// album record is left in place: deleting it would be a bigger action
// than the user asked for, and it may predate this request.
func (l *lidarr) Withdraw(ctx context.Context, externalID string) error {
	albumID, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("%w: bad album id %q", ErrLidarrNoMatch, externalID)
	}

	body := map[string]any{
		"albumIds":  []int{albumID},
		"monitored": false,
	}

	return l.client.put(ctx, "/api/v1/album/monitor", body, nil)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// rootFolders lists Lidarr's configured library roots.
func (l *lidarr) rootFolders(ctx context.Context) ([]lidarrRootFolder, error) {
	var folders []lidarrRootFolder

	if err := l.client.get(ctx, "/api/v1/rootfolder", &folders); err != nil {
		return nil, err
	}

	return folders, nil
}

// profiles resolves the quality and metadata profile IDs to use,
// falling back to the first of each when unconfigured.
func (l *lidarr) profiles(ctx context.Context) (quality, metadata int, err error) {
	quality, metadata = l.qualityProfileID, l.metadataProfileID

	if quality == 0 {
		var profiles []struct {
			ID int `json:"id"`
		}

		if err := l.client.get(ctx, "/api/v1/qualityprofile", &profiles); err != nil {
			return 0, 0, err
		}

		if len(profiles) == 0 {
			return 0, 0, fmt.Errorf(
				"%w: no quality profiles configured", ErrNotConfigured,
			)
		}

		quality = profiles[0].ID
	}

	if metadata == 0 {
		var profiles []struct {
			ID int `json:"id"`
		}

		if err := l.client.get(ctx, "/api/v1/metadataprofile", &profiles); err != nil {
			return 0, 0, err
		}

		if len(profiles) == 0 {
			return 0, 0, fmt.Errorf(
				"%w: no metadata profiles configured", ErrNotConfigured,
			)
		}

		metadata = profiles[0].ID
	}

	return quality, metadata, nil
}

// command posts to Lidarr's command endpoint.
func (l *lidarr) command(ctx context.Context, body map[string]any) error {
	return l.client.post(ctx, "/api/v1/command", body, nil)
}
