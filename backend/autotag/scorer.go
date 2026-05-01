package autotag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"yellowjacket/backend/database/sql/sqlcgen"
)

// ErrGroupNotFound is returned when ScoreGroup is asked to score
// a group_key that isn't in tagging_items.
var ErrGroupNotFound = errors.New("autotag: tagging group not found")

// Scorer ties the local resolver and the optional MB resolver
// together, producing ranked candidates for a tagging group and
// optionally persisting the top pick back to tagging_items.
type Scorer struct {
	q     *sqlcgen.Queries
	local *LocalResolver
	mb    *MBResolver // may be nil for dry-run / offline scoring
	log   *slog.Logger
}

// NewScorer wires up a Scorer.  Pass mb=nil to disable MB calls
// entirely (useful for tuning / tests).
func NewScorer(q *sqlcgen.Queries, mb MBClient, logger *slog.Logger) *Scorer {
	var resolver *MBResolver
	if mb != nil {
		resolver = NewMBResolver(mb, logger)
	}

	return &Scorer{
		q:     q,
		local: NewLocalResolver(q),
		mb:    resolver,
		log:   logger,
	}
}

// ScoreGroup produces the full GroupScore for one tagging item.
// Always runs both the local resolver (free) and the MB resolver
// (cache-first, so repeats cost nothing).  Candidates from both
// sources are merged and ranked — the UI displays them with
// provenance badges so the user can compare.
func (s *Scorer) ScoreGroup(
	ctx context.Context, groupKey string,
) (*GroupScore, error) {
	item, err := s.q.GetTaggingItem(ctx, groupKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrGroupNotFound
		}

		return nil, fmt.Errorf("get tagging item: %w", err)
	}

	locals, err := s.local.LocalTracksForGroup(ctx, groupKey)
	if err != nil {
		return nil, err
	}

	localHits, err := s.local.ResolveLocal(ctx, item.AlbumName)
	if err != nil {
		return nil, err
	}

	var mbHits []Candidate
	if s.mb != nil {
		mbHits, err = s.mb.ResolveMB(
			ctx,
			item.AlbumName,
			item.AlbumArtist,
			len(locals),
			guessArtistMBID(locals),
		)
		if err != nil {
			s.log.Warn(
				"MB resolve failed — returning local-only candidates",
				"group_key", groupKey,
				"err", err,
			)
		}
	}

	candidates := RankCandidates(locals, append(localHits, mbHits...))

	return &GroupScore{
		GroupKey:    groupKey,
		LocalTracks: locals,
		Candidates:  candidates,
	}, nil
}

// LocalTracksForGroup exposes the local resolver so callers that
// already have a candidate list (e.g. from the service-layer
// candidate cache) can build a GroupScore without re-running the
// whole scorer.
func (s *Scorer) LocalTracksForGroup(
	ctx context.Context, groupKey string,
) ([]LocalTrack, error) {
	return s.local.LocalTracksForGroup(ctx, groupKey)
}

// PersistBest writes the top candidate's release MBID and score
// onto the tagging_items row, bumping status to 'matched' when a
// candidate exists.  No-op when candidates is empty (keeps the
// current status — likely 'pending').
func (s *Scorer) PersistBest(
	ctx context.Context, score *GroupScore,
) error {
	if score == nil || len(score.Candidates) == 0 {
		return nil
	}

	top := score.Candidates[0]

	mbid := top.ReleaseMBID
	if mbid == "" {
		mbid = top.ReleaseGroupMBID
	}

	return s.q.SetTaggingItemBestMatch(ctx, sqlcgen.SetTaggingItemBestMatchParams{
		BestMatchReleaseMbid: sql.NullString{String: mbid, Valid: mbid != ""},
		Score:                sql.NullFloat64{Float64: top.Score, Valid: true},
		Status:               "matched",
		GroupKey:             score.GroupKey,
	})
}

// PersistScore writes the top candidate's release MBID and score
// onto the tagging_items row WITHOUT touching its status.  Use
// this from paths that want the sidebar pill / sort to reflect a
// fresh score but must leave 'pending' folders pending — namely
// the live re-score on folder open and the background prefetch
// worker.  No-op when candidates is empty.
func (s *Scorer) PersistScore(
	ctx context.Context, score *GroupScore,
) error {
	if score == nil || len(score.Candidates) == 0 {
		return nil
	}

	top := score.Candidates[0]

	mbid := top.ReleaseMBID
	if mbid == "" {
		mbid = top.ReleaseGroupMBID
	}

	return s.q.SetTaggingItemScore(ctx, sqlcgen.SetTaggingItemScoreParams{
		BestMatchReleaseMbid: sql.NullString{String: mbid, Valid: mbid != ""},
		Score:                sql.NullFloat64{Float64: top.Score, Valid: true},
		GroupKey:             score.GroupKey,
	})
}

// guessArtistMBID returns the first non-empty recording MBID-
// derived artist hint we can find.  Tracks carry recording MBIDs,
// not artist MBIDs, but existing partial tags are often consistent
// enough that any non-empty MBID signals "this album already has
// some MB lineage".  A follow-up in 010 can resolve actual artist
// MBIDs via the artists table.
func guessArtistMBID(tracks []LocalTrack) string {
	// Phase 009 doesn't wire up per-track artist MBIDs through
	// the LocalTrack struct yet — keeping the hook so the MB
	// resolver still compiles with the empty hint.  Real artist
	// MBIDs flow in once 010 wires them into LocalTrack.
	_ = tracks

	return ""
}
