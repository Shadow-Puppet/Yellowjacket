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
// Requests
// ---------------------------------------------------------------------------

// CreateRequest persists a new request.
func (s *Store) CreateRequest(ctx context.Context, req Request) error {
	expected, err := json.Marshal(req.Expected)
	if err != nil {
		return fmt.Errorf("encode expected tracks: %w", err)
	}

	source := req.Source
	if source == "" {
		source = "manual"
	}

	wantID := sql.NullInt64{}
	if req.WantID != 0 {
		wantID = sql.NullInt64{Int64: req.WantID, Valid: true}
	}

	if err := s.db.Queries.CreateDownloadRequest(
		ctx,
		sqlcgen.CreateDownloadRequestParams{
			ID:               req.ID,
			LibraryID:        req.LibraryID,
			Source:           source,
			WantID:           wantID,
			ReleaseMbid:      toNullString(req.ReleaseMBID),
			ReleaseGroupMbid: toNullString(req.ReleaseGroupMBID),
			RecordingMbid:    toNullString(req.RecordingMBID),
			Artist:           req.Artist,
			Album:            req.Album,
			Query:            req.Query,
			Expected:         string(expected),
			State:            string(StateSearching),
		},
	); err != nil {
		return fmt.Errorf("create download request: %w", err)
	}

	return nil
}

// GetRequest loads a request by ID.
func (s *Store) GetRequest(ctx context.Context, id string) (Request, error) {
	row, err := s.db.ReadQueries.GetDownloadRequest(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Request{}, fmt.Errorf("%w: request %s", ErrNotFound, id)
		}

		return Request{}, fmt.Errorf("get download request: %w", err)
	}

	return requestRowToRequest(row), nil
}

// GetRequestState returns a request's current state and error text.
// Kept separate from GetRequest because state is the one field that
// changes constantly while the rest of the row is immutable.
func (s *Store) GetRequestState(
	ctx context.Context,
	id string,
) (State, string, error) {
	row, err := s.db.ReadQueries.GetDownloadRequest(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", fmt.Errorf("%w: request %s", ErrNotFound, id)
		}

		return "", "", fmt.Errorf("get download request state: %w", err)
	}

	return State(row.State), row.Error, nil
}

// ListRequests returns the most recent requests, newest first.
func (s *Store) ListRequests(ctx context.Context, limit int) ([]Request, error) {
	rows, err := s.db.ReadQueries.ListDownloadRequests(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list download requests: %w", err)
	}

	out := make([]Request, 0, len(rows))

	for _, r := range rows {
		out = append(out, requestRowToRequest(r))
	}

	return out, nil
}

// SetRequestState updates a request's state and error text.
func (s *Store) SetRequestState(
	ctx context.Context,
	id string,
	state State,
	errText string,
) error {
	if err := s.db.Queries.SetDownloadRequestState(
		ctx,
		sqlcgen.SetDownloadRequestStateParams{
			State: string(state),
			Error: errText,
			ID:    id,
		},
	); err != nil {
		return fmt.Errorf("set download request state: %w", err)
	}

	return nil
}

// DeleteRequest removes a request and, by cascade, its items.
func (s *Store) DeleteRequest(ctx context.Context, id string) error {
	if err := s.db.Queries.DeleteDownloadRequest(ctx, id); err != nil {
		return fmt.Errorf("delete download request: %w", err)
	}

	return nil
}

// ClearFinished removes every terminal request.
func (s *Store) ClearFinished(ctx context.Context) error {
	if err := s.db.Queries.DeleteFinishedDownloadRequests(ctx); err != nil {
		return fmt.Errorf("clear finished download requests: %w", err)
	}

	return nil
}

// requestRowToRequest decodes a stored request row.
func requestRowToRequest(r sqlcgen.DownloadRequest) Request {
	var expected []ExpectedTrack

	_ = json.Unmarshal([]byte(r.Expected), &expected)

	return Request{
		ID:               r.ID,
		LibraryID:        r.LibraryID,
		Source:           r.Source,
		WantID:           r.WantID.Int64,
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

// Item is one grab attempt, as stored.
type Item struct {
	ID         string    `json:"id"`
	RequestID  string    `json:"requestId"`
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
func (s *Store) CreateItem(ctx context.Context, item Item) error {
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
			RequestID:   item.RequestID,
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
func (s *Store) GetItem(ctx context.Context, id string) (Item, error) {
	row, err := s.db.ReadQueries.GetDownloadItem(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Item{}, fmt.Errorf("%w: item %s", ErrNotFound, id)
		}

		return Item{}, fmt.Errorf("get download item: %w", err)
	}

	return itemRowToItem(row), nil
}

// ListItemsForRequest returns a request's grab attempts, oldest first.
func (s *Store) ListItemsForRequest(
	ctx context.Context,
	requestID string,
) ([]Item, error) {
	rows, err := s.db.ReadQueries.ListDownloadItemsForRequest(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("list download items: %w", err)
	}

	out := make([]Item, 0, len(rows))

	for _, r := range rows {
		out = append(out, itemRowToItem(r))
	}

	return out, nil
}

// ListLiveItems returns every non-terminal item.  Called at startup to
// decide what to resume, reconcile or abandon.
func (s *Store) ListLiveItems(ctx context.Context) ([]Item, error) {
	rows, err := s.db.ReadQueries.ListLiveDownloadItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("list live download items: %w", err)
	}

	out := make([]Item, 0, len(rows))

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
func itemRowToItem(r sqlcgen.DownloadItem) Item {
	var (
		candidate Candidate
		imported  []string
	)

	_ = json.Unmarshal([]byte(r.Candidate), &candidate)
	_ = json.Unmarshal([]byte(r.ImportedPaths), &imported)

	return Item{
		ID:         r.ID,
		RequestID:  r.RequestID,
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
