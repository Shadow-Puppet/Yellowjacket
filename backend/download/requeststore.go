package download

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"yellowjacket/backend/database/sql/sqlcgen"
)

// The durable request list's persistence.  Kept apart from the
// download/item storage in store.go because the two have opposite
// lifetimes: downloads and items are written constantly and swept,
// requests are written rarely and kept.

// defaultDueBatch bounds how many requests one reconcile pass picks up.
// The list can be thousands of rows after a discography backfill, and a
// pass that tried to search all of them would take a day and annoy
// every provider on the way.
const defaultDueBatch = 25

// AddRequest inserts a request, or returns the existing row's ID if the
// same MBID is already requested in this library.  Asking twice is not
// two requests, and re-asking must not reset a backoff that is
// deliberately long.
func (s *Store) AddRequest(ctx context.Context, r Request) (int64, error) {
	if !r.Entity.Valid() {
		return 0, fmt.Errorf("%w: entity %q", ErrUnsupported, r.Entity)
	}

	if r.Scope == "" {
		r.Scope = ScopeFuture
	}

	// The MBID is the identity of a request, so it is normalized here
	// rather than at each call site: the same identifier arriving from
	// an Explore page and from a pasted URL must be one row, or the
	// uniqueness constraint that makes artist expansion idempotent
	// stops holding.
	r.MBID = strings.ToLower(strings.TrimSpace(r.MBID))

	if r.MBID == "" {
		return 0, fmt.Errorf("%w: a request needs an MBID", ErrUnsupported)
	}

	parent := sql.NullInt64{}
	if r.ParentID != 0 {
		parent = sql.NullInt64{Int64: r.ParentID, Valid: true}
	}

	id, err := s.db.Queries.UpsertDownloadRequest(
		ctx,
		sqlcgen.UpsertDownloadRequestParams{
			Mbid:      r.MBID,
			Entity:    string(r.Entity),
			LibraryID: r.LibraryID,
			Artist:    r.Artist,
			Title:     r.Title,
			Scope:     string(r.Scope),
			Secondary: boolToInt(r.Secondary),
			ParentID:  parent,
		},
	)
	if err != nil {
		return 0, fmt.Errorf("add download request: %w", err)
	}

	return id, nil
}

// GetRequest loads one request.
func (s *Store) GetRequest(ctx context.Context, id int64) (Request, error) {
	row, err := s.db.ReadQueries.GetDownloadRequest(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Request{}, fmt.Errorf("%w: request %d", ErrNotFound, id)
		}

		return Request{}, fmt.Errorf("get download request: %w", err)
	}

	return requestRowToRequest(row), nil
}

// FindRequest looks a request up by what it names rather than by row
// ID, which is how callers holding an MBID (the Explore pages, a
// provider sync) ask "is this already requested?".
func (s *Store) FindRequest(
	ctx context.Context,
	mbid string,
	libraryID int64,
) (Request, bool, error) {
	row, err := s.db.ReadQueries.GetDownloadRequestByMBID(
		ctx,
		sqlcgen.GetDownloadRequestByMBIDParams{Mbid: mbid, LibraryID: libraryID},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Request{}, false, nil
		}

		return Request{}, false, fmt.Errorf("find download request: %w", err)
	}

	return requestRowToRequest(row), true, nil
}

// ListRequests returns the whole durable request list, active first.
func (s *Store) ListRequests(ctx context.Context) ([]Request, error) {
	rows, err := s.db.ReadQueries.ListDownloadRequests(ctx)
	if err != nil {
		return nil, fmt.Errorf("list download requests: %w", err)
	}

	return requestRowsToRequests(rows), nil
}

// ListActiveRequests returns active artist subscriptions, which are
// what the reconciler expands.
func (s *Store) ListActiveRequests(ctx context.Context) ([]Request, error) {
	rows, err := s.db.ReadQueries.ListDownloadRequestsByEntity(
		ctx,
		sqlcgen.ListDownloadRequestsByEntityParams{
			Entity: string(EntityArtist),
			State:  string(RequestStateWanted),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list artist requests: %w", err)
	}

	return requestRowsToRequests(rows), nil
}

// ListDueRequests returns downloadable requests whose backoff has
// elapsed, least-attempted first so a new addition is not stuck behind
// a hundred long-shot retries.
func (s *Store) ListDueRequests(ctx context.Context, limit int) ([]Request, error) {
	if limit <= 0 {
		limit = defaultDueBatch
	}

	rows, err := s.db.ReadQueries.ListDueDownloadRequests(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list due download requests: %w", err)
	}

	return requestRowsToRequests(rows), nil
}

// ListWantedRequests returns downloadable requests regardless of their
// backoff, least-attempted first.  Only a user-initiated pass uses
// this: the loop honours the schedule, a person pressing "check now"
// is the schedule.
func (s *Store) ListWantedRequests(ctx context.Context, limit int) ([]Request, error) {
	if limit <= 0 {
		limit = defaultDueBatch
	}

	rows, err := s.db.ReadQueries.ListWantedDownloadRequests(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list wanted download requests: %w", err)
	}

	return requestRowsToRequests(rows), nil
}

// ListChildRequests returns the requests an artist subscription
// produced.
func (s *Store) ListChildRequests(
	ctx context.Context,
	parentID int64,
) ([]Request, error) {
	rows, err := s.db.ReadQueries.ListChildDownloadRequests(
		ctx,
		sql.NullInt64{Int64: parentID, Valid: true},
	)
	if err != nil {
		return nil, fmt.Errorf("list child download requests: %w", err)
	}

	return requestRowsToRequests(rows), nil
}

// SetRequestState moves a request between wanted, paused and satisfied.
func (s *Store) SetRequestState(
	ctx context.Context,
	id int64,
	state RequestState,
	errText string,
) error {
	if err := s.db.Queries.SetDownloadRequestState(
		ctx,
		sqlcgen.SetDownloadRequestStateParams{
			State:     string(state),
			LastError: errText,
			ID:        id,
		},
	); err != nil {
		return fmt.Errorf("set download request state: %w", err)
	}

	return nil
}

// RecordAttempt notes an unsuccessful pass over a request and schedules
// the next one.  The request stays wanted: not finding something is a
// fact about today's providers, not a verdict on the request.
func (s *Store) RecordAttempt(
	ctx context.Context,
	id int64,
	attempts int,
	reason string,
) error {
	next := nextRetry(time.Now(), attempts+1)

	if err := s.db.Queries.RecordDownloadRequestAttempt(
		ctx,
		sqlcgen.RecordDownloadRequestAttemptParams{
			LastError: reason,
			NextTryAt: sql.NullTime{Time: next, Valid: true},
			ID:        id,
		},
	); err != nil {
		return fmt.Errorf("record download request attempt: %w", err)
	}

	return nil
}

// SatisfyRequest marks a request as owned.
func (s *Store) SatisfyRequest(ctx context.Context, id int64) error {
	if err := s.db.Queries.SatisfyDownloadRequest(ctx, id); err != nil {
		return fmt.Errorf("satisfy download request: %w", err)
	}

	return nil
}

// SetRequestExternalIDs records the identifiers external managers gave
// this request in their own persistent lists.
func (s *Store) SetRequestExternalIDs(
	ctx context.Context,
	id int64,
	ids map[string]string,
) error {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("encode request external ids: %w", err)
	}

	if err := s.db.Queries.SetDownloadRequestExternalIDs(
		ctx,
		sqlcgen.SetDownloadRequestExternalIDsParams{
			ExternalIds: string(encoded),
			ID:          id,
		},
	); err != nil {
		return fmt.Errorf("set request external ids: %w", err)
	}

	return nil
}

// DeleteRequest removes a request and, by cascade, anything an artist
// request derived.
func (s *Store) DeleteRequest(ctx context.Context, id int64) error {
	if err := s.db.Queries.DeleteDownloadRequest(ctx, id); err != nil {
		return fmt.Errorf("delete download request: %w", err)
	}

	return nil
}

// ClearSatisfiedRequests drops everything already owned.
func (s *Store) ClearSatisfiedRequests(ctx context.Context) error {
	if err := s.db.Queries.DeleteSatisfiedDownloadRequests(ctx); err != nil {
		return fmt.Errorf("clear satisfied download requests: %w", err)
	}

	return nil
}

// requestRowsToRequests decodes a slice of stored rows.
func requestRowsToRequests(rows []sqlcgen.DownloadRequest) []Request {
	out := make([]Request, 0, len(rows))

	for _, r := range rows {
		out = append(out, requestRowToRequest(r))
	}

	return out
}

// requestRowToRequest decodes a stored request row.  A malformed
// external-ID blob yields an empty map rather than an error: losing the
// link to a Lidarr row is recoverable on the next sync, making the
// request list unreadable is not.
func requestRowToRequest(r sqlcgen.DownloadRequest) Request {
	external := map[string]string{}
	_ = json.Unmarshal([]byte(r.ExternalIds), &external)

	return Request{
		ID:          r.ID,
		MBID:        r.Mbid,
		Entity:      Entity(r.Entity),
		LibraryID:   r.LibraryID,
		Artist:      r.Artist,
		Title:       r.Title,
		Scope:       RequestScope(r.Scope),
		Secondary:   r.Secondary != 0,
		State:       RequestState(r.State),
		ParentID:    r.ParentID.Int64,
		Attempts:    int(r.Attempts),
		LastError:   r.LastError,
		LastTriedAt: r.LastTriedAt.Time,
		NextTryAt:   r.NextTryAt.Time,
		ExternalIDs: external,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}
