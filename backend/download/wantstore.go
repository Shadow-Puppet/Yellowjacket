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

// The wanted list's persistence.  Kept apart from the request/item
// storage in store.go because the two have opposite lifetimes: items
// are written constantly and swept, wants are written rarely and kept.

// defaultDueBatch bounds how many wants one reconcile pass picks up.
// The list can be thousands of rows after a discography backfill, and a
// pass that tried to search all of them would take a day and annoy
// every provider on the way.
const defaultDueBatch = 25

// AddWant inserts a want, or returns the existing row's ID if the same
// MBID is already wanted in this library.  Asking twice is not two
// wants, and re-asking must not reset a backoff that is deliberately
// long.
func (s *Store) AddWant(ctx context.Context, w Want) (int64, error) {
	if !w.Entity.Valid() {
		return 0, fmt.Errorf("%w: entity %q", ErrUnsupported, w.Entity)
	}

	if w.Scope == "" {
		w.Scope = ScopeFuture
	}

	// The MBID is the identity of a want, so it is normalized here
	// rather than at each call site: the same identifier arriving from
	// an Explore page and from a pasted URL must be one row, or the
	// uniqueness constraint that makes artist expansion idempotent
	// stops holding.
	w.MBID = strings.ToLower(strings.TrimSpace(w.MBID))

	if w.MBID == "" {
		return 0, fmt.Errorf("%w: a want needs an MBID", ErrUnsupported)
	}

	parent := sql.NullInt64{}
	if w.ParentID != 0 {
		parent = sql.NullInt64{Int64: w.ParentID, Valid: true}
	}

	id, err := s.db.Queries.UpsertDownloadWant(
		ctx,
		sqlcgen.UpsertDownloadWantParams{
			Mbid:      w.MBID,
			Entity:    string(w.Entity),
			LibraryID: w.LibraryID,
			Artist:    w.Artist,
			Title:     w.Title,
			Scope:     string(w.Scope),
			Secondary: boolToInt(w.Secondary),
			ParentID:  parent,
		},
	)
	if err != nil {
		return 0, fmt.Errorf("add download want: %w", err)
	}

	return id, nil
}

// GetWant loads one want.
func (s *Store) GetWant(ctx context.Context, id int64) (Want, error) {
	row, err := s.db.ReadQueries.GetDownloadWant(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Want{}, fmt.Errorf("%w: want %d", ErrNotFound, id)
		}

		return Want{}, fmt.Errorf("get download want: %w", err)
	}

	return wantRowToWant(row), nil
}

// FindWant looks a want up by what it names rather than by row ID,
// which is how callers holding an MBID (the Explore pages, a provider
// sync) ask "is this already wanted?".
func (s *Store) FindWant(
	ctx context.Context,
	mbid string,
	libraryID int64,
) (Want, bool, error) {
	row, err := s.db.ReadQueries.GetDownloadWantByMBID(
		ctx,
		sqlcgen.GetDownloadWantByMBIDParams{Mbid: mbid, LibraryID: libraryID},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Want{}, false, nil
		}

		return Want{}, false, fmt.Errorf("find download want: %w", err)
	}

	return wantRowToWant(row), true, nil
}

// ListWants returns the whole wanted list, active first.
func (s *Store) ListWants(ctx context.Context) ([]Want, error) {
	rows, err := s.db.ReadQueries.ListDownloadWants(ctx)
	if err != nil {
		return nil, fmt.Errorf("list download wants: %w", err)
	}

	return wantRowsToWants(rows), nil
}

// ListArtistWants returns active artist subscriptions, which are what
// the reconciler expands.
func (s *Store) ListArtistWants(ctx context.Context) ([]Want, error) {
	rows, err := s.db.ReadQueries.ListDownloadWantsByEntity(
		ctx,
		sqlcgen.ListDownloadWantsByEntityParams{
			Entity: string(EntityArtist),
			State:  string(WantStateWanted),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list artist wants: %w", err)
	}

	return wantRowsToWants(rows), nil
}

// ListDueWants returns downloadable wants whose backoff has elapsed,
// least-attempted first so a new addition is not stuck behind a
// hundred long-shot retries.
func (s *Store) ListDueWants(ctx context.Context, limit int) ([]Want, error) {
	if limit <= 0 {
		limit = defaultDueBatch
	}

	rows, err := s.db.ReadQueries.ListDueDownloadWants(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list due download wants: %w", err)
	}

	return wantRowsToWants(rows), nil
}

// ListChildWants returns the wants an artist subscription produced.
func (s *Store) ListChildWants(
	ctx context.Context,
	parentID int64,
) ([]Want, error) {
	rows, err := s.db.ReadQueries.ListChildDownloadWants(
		ctx,
		sql.NullInt64{Int64: parentID, Valid: true},
	)
	if err != nil {
		return nil, fmt.Errorf("list child download wants: %w", err)
	}

	return wantRowsToWants(rows), nil
}

// SetWantState moves a want between wanted, paused and satisfied.
func (s *Store) SetWantState(
	ctx context.Context,
	id int64,
	state WantState,
	errText string,
) error {
	if err := s.db.Queries.SetDownloadWantState(
		ctx,
		sqlcgen.SetDownloadWantStateParams{
			State:     string(state),
			LastError: errText,
			ID:        id,
		},
	); err != nil {
		return fmt.Errorf("set download want state: %w", err)
	}

	return nil
}

// RecordAttempt notes an unsuccessful pass over a want and schedules
// the next one.  The want stays wanted: not finding something is a fact
// about today's providers, not a verdict on the request.
func (s *Store) RecordAttempt(
	ctx context.Context,
	id int64,
	attempts int,
	reason string,
) error {
	next := nextRetry(time.Now(), attempts+1)

	if err := s.db.Queries.RecordDownloadWantAttempt(
		ctx,
		sqlcgen.RecordDownloadWantAttemptParams{
			LastError: reason,
			NextTryAt: sql.NullTime{Time: next, Valid: true},
			ID:        id,
		},
	); err != nil {
		return fmt.Errorf("record download want attempt: %w", err)
	}

	return nil
}

// SatisfyWant marks a want as owned.
func (s *Store) SatisfyWant(ctx context.Context, id int64) error {
	if err := s.db.Queries.SatisfyDownloadWant(ctx, id); err != nil {
		return fmt.Errorf("satisfy download want: %w", err)
	}

	return nil
}

// SetWantExternalIDs records the identifiers external managers gave
// this want in their own persistent lists.
func (s *Store) SetWantExternalIDs(
	ctx context.Context,
	id int64,
	ids map[string]string,
) error {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("encode want external ids: %w", err)
	}

	if err := s.db.Queries.SetDownloadWantExternalIDs(
		ctx,
		sqlcgen.SetDownloadWantExternalIDsParams{
			ExternalIds: string(encoded),
			ID:          id,
		},
	); err != nil {
		return fmt.Errorf("set want external ids: %w", err)
	}

	return nil
}

// DeleteWant removes a want and, by cascade, anything an artist want
// derived.
func (s *Store) DeleteWant(ctx context.Context, id int64) error {
	if err := s.db.Queries.DeleteDownloadWant(ctx, id); err != nil {
		return fmt.Errorf("delete download want: %w", err)
	}

	return nil
}

// ClearSatisfiedWants drops everything already owned.
func (s *Store) ClearSatisfiedWants(ctx context.Context) error {
	if err := s.db.Queries.DeleteSatisfiedDownloadWants(ctx); err != nil {
		return fmt.Errorf("clear satisfied download wants: %w", err)
	}

	return nil
}

// wantRowsToWants decodes a slice of stored rows.
func wantRowsToWants(rows []sqlcgen.DownloadWant) []Want {
	out := make([]Want, 0, len(rows))

	for _, r := range rows {
		out = append(out, wantRowToWant(r))
	}

	return out
}

// wantRowToWant decodes a stored want row.  A malformed external-ID
// blob yields an empty map rather than an error: losing the link to a
// Lidarr row is recoverable on the next sync, making the wanted list
// unreadable is not.
func wantRowToWant(r sqlcgen.DownloadWant) Want {
	external := map[string]string{}
	_ = json.Unmarshal([]byte(r.ExternalIds), &external)

	return Want{
		ID:          r.ID,
		MBID:        r.Mbid,
		Entity:      Entity(r.Entity),
		LibraryID:   r.LibraryID,
		Artist:      r.Artist,
		Title:       r.Title,
		Scope:       WantScope(r.Scope),
		Secondary:   r.Secondary != 0,
		State:       WantState(r.State),
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
