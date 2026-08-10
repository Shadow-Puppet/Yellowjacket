package download

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
)

// ErrNotFound is returned when a request or item ID is unknown.
var ErrNotFound = errors.New("not found")

// Store is the download subsystem's persistence layer.  It owns the
// JSON encoding of the blob columns so nothing above it has to know
// that candidates are stored as text.
type Store struct {
	db *database.DB
}

// NewStore returns a Store over the application database.
func NewStore(db *database.DB) *Store {
	return &Store{db: db}
}

// ---------------------------------------------------------------------------
// Providers
// ---------------------------------------------------------------------------

// ListProviders returns every configured provider, best priority first.
func (s *Store) ListProviders(ctx context.Context) ([]Config, error) {
	rows, err := s.db.ReadQueries.ListDownloadProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list download providers: %w", err)
	}

	out := make([]Config, 0, len(rows))

	for _, r := range rows {
		out = append(out, providerRowToConfig(r))
	}

	return out, nil
}

// GetProvider returns one provider's config.
func (s *Store) GetProvider(ctx context.Context, id int64) (Config, error) {
	row, err := s.db.ReadQueries.GetDownloadProvider(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Config{}, fmt.Errorf("%w: provider %d", ErrNotFound, id)
		}

		return Config{}, fmt.Errorf("get download provider: %w", err)
	}

	return providerRowToConfig(row), nil
}

// CreateProvider inserts a provider and returns its new ID.
func (s *Store) CreateProvider(ctx context.Context, cfg Config) (int64, error) {
	settings, err := json.Marshal(cfg.Settings)
	if err != nil {
		return 0, fmt.Errorf("encode provider settings: %w", err)
	}

	id, err := s.db.Queries.CreateDownloadProvider(
		ctx,
		sqlcgen.CreateDownloadProviderParams{
			Kind:     string(cfg.Kind),
			Name:     cfg.Name,
			Enabled:  boolToInt(cfg.Enabled),
			Priority: int64(cfg.Priority),
			Settings: string(settings),
		},
	)
	if err != nil {
		return 0, fmt.Errorf("create download provider: %w", err)
	}

	return id, nil
}

// UpdateProvider saves changes to an existing provider.
func (s *Store) UpdateProvider(ctx context.Context, cfg Config) error {
	settings, err := json.Marshal(cfg.Settings)
	if err != nil {
		return fmt.Errorf("encode provider settings: %w", err)
	}

	if err := s.db.Queries.UpdateDownloadProvider(
		ctx,
		sqlcgen.UpdateDownloadProviderParams{
			Name:     cfg.Name,
			Enabled:  boolToInt(cfg.Enabled),
			Priority: int64(cfg.Priority),
			Settings: string(settings),
			ID:       cfg.ID,
		},
	); err != nil {
		return fmt.Errorf("update download provider: %w", err)
	}

	return nil
}

// DeleteProvider removes a provider row.  Its secrets are removed
// separately by the manager, which owns the secret store.
func (s *Store) DeleteProvider(ctx context.Context, id int64) error {
	if err := s.db.Queries.DeleteDownloadProvider(ctx, id); err != nil {
		return fmt.Errorf("delete download provider: %w", err)
	}

	return nil
}

// providerRowToConfig decodes a stored provider row.  A settings blob
// that fails to parse yields an empty map rather than an error: the
// provider will fail its own Check with a useful message, which beats
// making the whole settings page unloadable.
func providerRowToConfig(r sqlcgen.DownloadProvider) Config {
	settings := map[string]string{}
	_ = json.Unmarshal([]byte(r.Settings), &settings)

	return Config{
		ID:       r.ID,
		Kind:     Kind(r.Kind),
		Name:     r.Name,
		Enabled:  r.Enabled != 0,
		Priority: int(r.Priority),
		Settings: settings,
	}
}

// ---------------------------------------------------------------------------
// Downloads
// ---------------------------------------------------------------------------

// CreateDownload persists a new download.
func (s *Store) CreateDownload(ctx context.Context, dl Download) error {
	expected, err := json.Marshal(dl.Expected)
	if err != nil {
		return fmt.Errorf("encode expected tracks: %w", err)
	}

	source := dl.Source
	if source == "" {
		source = "manual"
	}

	requestID := sql.NullInt64{}
	if dl.RequestID != 0 {
		requestID = sql.NullInt64{Int64: dl.RequestID, Valid: true}
	}

	if err := s.db.Queries.CreateDownload(
		ctx,
		sqlcgen.CreateDownloadParams{
			ID:               dl.ID,
			LibraryID:        dl.LibraryID,
			Source:           source,
			RequestID:        requestID,
			ReleaseMbid:      toNullString(dl.ReleaseMBID),
			ReleaseGroupMbid: toNullString(dl.ReleaseGroupMBID),
			RecordingMbid:    toNullString(dl.RecordingMBID),
			Artist:           dl.Artist,
			Album:            dl.Album,
			Query:            dl.Query,
			Expected:         string(expected),
			State:            string(StateSearching),
		},
	); err != nil {
		return fmt.Errorf("create download: %w", err)
	}

	return nil
}

// GetDownload loads a download by ID.
func (s *Store) GetDownload(ctx context.Context, id string) (Download, error) {
	row, err := s.db.ReadQueries.GetDownload(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Download{}, fmt.Errorf("%w: download %s", ErrNotFound, id)
		}

		return Download{}, fmt.Errorf("get download: %w", err)
	}

	return downloadRowToDownload(row), nil
}

// GetDownloadState returns a download's current state and error text.
// Kept separate from GetDownload because state is the one field that
// changes constantly while the rest of the row is immutable.
func (s *Store) GetDownloadState(
	ctx context.Context,
	id string,
) (State, string, error) {
	row, err := s.db.ReadQueries.GetDownload(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", fmt.Errorf("%w: download %s", ErrNotFound, id)
		}

		return "", "", fmt.Errorf("get download state: %w", err)
	}

	return State(row.State), row.Error, nil
}

// ListDownloads returns the most recent downloads, newest first.
func (s *Store) ListDownloads(ctx context.Context, limit int) ([]Download, error) {
	rows, err := s.db.ReadQueries.ListDownloads(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list downloads: %w", err)
	}

	out := make([]Download, 0, len(rows))

	for _, r := range rows {
		out = append(out, downloadRowToDownload(r))
	}

	return out, nil
}

// SetDownloadState updates a download's state and error text.
func (s *Store) SetDownloadState(
	ctx context.Context,
	id string,
	state State,
	errText string,
) error {
	if err := s.db.Queries.SetDownloadState(
		ctx,
		sqlcgen.SetDownloadStateParams{
			State: string(state),
			Error: errText,
			ID:    id,
		},
	); err != nil {
		return fmt.Errorf("set download state: %w", err)
	}

	return nil
}

// DeleteDownload removes a download and, by cascade, its items.
func (s *Store) DeleteDownload(ctx context.Context, id string) error {
	if err := s.db.Queries.DeleteDownload(ctx, id); err != nil {
		return fmt.Errorf("delete download: %w", err)
	}

	return nil
}

// ClearFinished removes every terminal download.
func (s *Store) ClearFinished(ctx context.Context) error {
	if err := s.db.Queries.DeleteFinishedDownloads(ctx); err != nil {
		return fmt.Errorf("clear finished downloads: %w", err)
	}

	return nil
}

// downloadRowToDownload decodes a stored download row.
func downloadRowToDownload(r sqlcgen.DownloadDownload) Download {
	var expected []ExpectedTrack

	_ = json.Unmarshal([]byte(r.Expected), &expected)

	return Download{
		ID:               r.ID,
		LibraryID:        r.LibraryID,
		Source:           r.Source,
		RequestID:        r.RequestID.Int64,
		ReleaseMBID:      r.ReleaseMbid.String,
		ReleaseGroupMBID: r.ReleaseGroupMbid.String,
		RecordingMBID:    r.RecordingMbid.String,
		Artist:           r.Artist,
		Album:            r.Album,
		Query:            r.Query,
		Expected:         expected,
		CreatedAt:        r.CreatedAt,
	}
}

// ---------------------------------------------------------------------------
// Items
// ---------------------------------------------------------------------------

// DownloadItem is one grab attempt, as stored.
//
// deliberate: distinguishes it from download.Download (the attempt) and
// download.Request (the durable record) at every call site, which a bare
// "Item" would not.
//
//nolint:revive // stutters as download.DownloadItem, but the name is
type DownloadItem struct {
	ID         string    `json:"id"`
	DownloadID string    `json:"downloadId"`
	ProviderID int64     `json:"providerId"`
	Transport  int64     `json:"transportId,omitempty"`
	ExternalID string    `json:"externalId,omitempty"`
	Candidate  Candidate `json:"candidate"`
	State      State     `json:"state"`
	StagingDir string    `json:"-"`
	BytesDone  int64     `json:"bytesDone"`
	BytesTotal int64     `json:"bytesTotal"`
	Imported   []string  `json:"-"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// CreateItem persists a grab attempt.
func (s *Store) CreateItem(ctx context.Context, item DownloadItem) error {
	candidate, err := json.Marshal(item.Candidate)
	if err != nil {
		return fmt.Errorf("encode candidate: %w", err)
	}

	transport := sql.NullInt64{}
	if item.Transport != 0 {
		transport = sql.NullInt64{Int64: item.Transport, Valid: true}
	}

	if err := s.db.Queries.CreateDownloadItem(
		ctx,
		sqlcgen.CreateDownloadItemParams{
			ID:          item.ID,
			DownloadID:  item.DownloadID,
			ProviderID:  item.ProviderID,
			TransportID: transport,
			ExternalID:  item.ExternalID,
			Candidate:   string(candidate),
			State:       string(item.State),
			StagingDir:  item.StagingDir,
			BytesTotal:  item.BytesTotal,
		},
	); err != nil {
		return fmt.Errorf("create download item: %w", err)
	}

	return nil
}

// GetItem loads one item.
func (s *Store) GetItem(ctx context.Context, id string) (DownloadItem, error) {
	row, err := s.db.ReadQueries.GetDownloadItem(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DownloadItem{}, fmt.Errorf("%w: item %s", ErrNotFound, id)
		}

		return DownloadItem{}, fmt.Errorf("get download item: %w", err)
	}

	return itemRowToItem(row), nil
}

// ListItemsForDownload returns a download's grab attempts, oldest
// first.
func (s *Store) ListItemsForDownload(
	ctx context.Context,
	downloadID string,
) ([]DownloadItem, error) {
	rows, err := s.db.ReadQueries.ListDownloadItemsForDownload(ctx, downloadID)
	if err != nil {
		return nil, fmt.Errorf("list download items: %w", err)
	}

	out := make([]DownloadItem, 0, len(rows))

	for _, r := range rows {
		out = append(out, itemRowToItem(r))
	}

	return out, nil
}

// ListLiveItems returns every non-terminal item.  Called at startup to
// decide what to resume, reconcile or abandon.
func (s *Store) ListLiveItems(ctx context.Context) ([]DownloadItem, error) {
	rows, err := s.db.ReadQueries.ListLiveDownloadItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("list live download items: %w", err)
	}

	out := make([]DownloadItem, 0, len(rows))

	for _, r := range rows {
		out = append(out, itemRowToItem(r))
	}

	return out, nil
}

// SetItemState updates an item's state and error text.
func (s *Store) SetItemState(
	ctx context.Context,
	id string,
	state State,
	errText string,
) error {
	if err := s.db.Queries.SetDownloadItemState(
		ctx,
		sqlcgen.SetDownloadItemStateParams{
			State: string(state),
			Error: errText,
			ID:    id,
		},
	); err != nil {
		return fmt.Errorf("set download item state: %w", err)
	}

	return nil
}

// SetItemProgress records transfer progress.
func (s *Store) SetItemProgress(
	ctx context.Context,
	id string,
	done, total int64,
) error {
	if err := s.db.Queries.SetDownloadItemProgress(
		ctx,
		sqlcgen.SetDownloadItemProgressParams{
			BytesDone:  done,
			BytesTotal: total,
			ID:         id,
		},
	); err != nil {
		return fmt.Errorf("set download item progress: %w", err)
	}

	return nil
}

// SetItemExternalID records a delegating manager's own identifier.
func (s *Store) SetItemExternalID(
	ctx context.Context,
	id, externalID string,
) error {
	if err := s.db.Queries.SetDownloadItemExternalID(
		ctx,
		sqlcgen.SetDownloadItemExternalIDParams{
			ExternalID: externalID,
			ID:         id,
		},
	); err != nil {
		return fmt.Errorf("set download item external id: %w", err)
	}

	return nil
}

// SetItemImported records the library paths files landed at and marks
// the item complete.
func (s *Store) SetItemImported(
	ctx context.Context,
	id string,
	paths []string,
) error {
	encoded, err := json.Marshal(paths)
	if err != nil {
		return fmt.Errorf("encode imported paths: %w", err)
	}

	if err := s.db.Queries.SetDownloadItemImported(
		ctx,
		sqlcgen.SetDownloadItemImportedParams{
			ImportedPaths: string(encoded),
			ID:            id,
		},
	); err != nil {
		return fmt.Errorf("set download item imported: %w", err)
	}

	return nil
}

// itemRowToItem decodes a stored item row.
func itemRowToItem(r sqlcgen.DownloadItem) DownloadItem {
	var (
		candidate Candidate
		imported  []string
	)

	_ = json.Unmarshal([]byte(r.Candidate), &candidate)
	_ = json.Unmarshal([]byte(r.ImportedPaths), &imported)

	return DownloadItem{
		ID:         r.ID,
		DownloadID: r.DownloadID,
		ProviderID: r.ProviderID,
		Transport:  r.TransportID.Int64,
		ExternalID: r.ExternalID,
		Candidate:  candidate,
		State:      State(r.State),
		StagingDir: r.StagingDir,
		BytesDone:  r.BytesDone,
		BytesTotal: r.BytesTotal,
		Imported:   imported,
		Error:      r.Error,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
	}
}

// boolToInt converts a bool to SQLite's integer boolean.
func boolToInt(b bool) int64 {
	if b {
		return 1
	}

	return 0
}

// toNullString wraps a possibly-empty string for a nullable column.
func toNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
