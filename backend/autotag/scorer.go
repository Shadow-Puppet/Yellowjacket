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

// localSufficient is the local-candidate score above which a
// local-first caller skips the MusicBrainz round-trip entirely: a
// local candidate at or above this is a strong match (the same album
// already tagged correctly in another library), so the MB cascade
// wouldn't change the top pick and isn't worth a rate-limited network
// call.  Interactive scoring ignores this and always consults MB so
// the review UI can show both sources side by side.
const localSufficient = 0.90

// idSufficient is the score at which an ID-resolved candidate (built
// from recording MBIDs already present in the local tags) makes the
// fuzzy search cascade unnecessary — mirrors beets, where a strong
// mb_albumid match returns immediately without a text search.
const idSufficient = 0.90

// maxIDSampleTracks caps how many local recording MBIDs the ID-first
// path looks up — three spread across the folder corroborate a
// release without paying for a lookup per track.
const maxIDSampleTracks = 3

// ScoreGroup produces the full GroupScore for one tagging item,
// always consulting MusicBrainz (when a client is configured) so the
// review UI can display local + MB candidates side by side with
// provenance badges.  Use this on interactive paths where the user is
// looking at the result.
func (s *Scorer) ScoreGroup(
	ctx context.Context, groupKey string,
) (*GroupScore, error) {
	return s.scoreGroup(ctx, groupKey, false)
}

// ScoreGroupLocalFirst is the cheap variant for background work
// (prefetch): it scores local candidates first and skips the
// MusicBrainz cascade when the best local candidate is already a
// strong match (score >= localSufficient).  Falls back to the full
// MB-consulting path otherwise, so albums with no strong local match
// still get a real score for the sidebar pill.
func (s *Scorer) ScoreGroupLocalFirst(
	ctx context.Context, groupKey string,
) (*GroupScore, error) {
	return s.scoreGroup(ctx, groupKey, true)
}

func (s *Scorer) scoreGroup(
	ctx context.Context, groupKey string, localFirst bool,
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

	g := Group{
		AlbumName:   item.AlbumName,
		AlbumArtist: item.AlbumArtist,
		Tracks:      locals,
		Synthetic:   item.Synthetic != 0,
	}

	localHits, err := s.local.ResolveLocal(ctx, item.AlbumName)
	if err != nil {
		return nil, err
	}

	// Local-first short-circuit: pre-score the free local candidates
	// and, if one is already a strong match, skip the MB round-trip.
	var localCandidates []Candidate

	skipMB := false

	if localFirst && s.mb != nil {
		localCandidates = RankCandidates(g, localHits)
		skipMB = len(localCandidates) > 0 && localCandidates[0].Score >= localSufficient
	}

	var mbHits []Candidate

	if s.mb != nil && !skipMB {
		mbHits = s.resolveMBCandidates(ctx, g, groupKey)
	}

	// Reuse the pre-ranked local list when we skipped MB; otherwise
	// rank the merged set.
	candidates := localCandidates
	if !skipMB {
		candidates = RankCandidates(g, append(localHits, mbHits...))
	}

	return &GroupScore{
		GroupKey:       groupKey,
		AlbumName:      item.AlbumName,
		AlbumArtist:    item.AlbumArtist,
		LocalTracks:    locals,
		Candidates:     candidates,
		Recommendation: Recommend(g, candidates),
		Synthetic:      g.Synthetic,
	}, nil
}

// resolveMBCandidates gathers MusicBrainz candidates for a group:
// ID-first (recording MBIDs already in the tags), then the search
// cascade when the ID path didn't produce a strong match.  Failures
// on either path degrade to fewer candidates, never to an error —
// local candidates must still surface when MB is unreachable.
func (s *Scorer) resolveMBCandidates(
	ctx context.Context, g Group, groupKey string,
) []Candidate {
	var out []Candidate

	if ids := sampleRecordingMBIDs(g.Tracks); len(ids) > 0 {
		idCands, err := s.mb.ResolveByRecordingMBIDs(ctx, ids)
		if err != nil {
			s.log.Warn(
				"MB ID-first resolve failed — falling back to search",
				"group_key", groupKey, "err", err,
			)
		}

		if len(idCands) > 0 {
			ranked := RankCandidates(g, idCands)
			if ranked[0].Score >= idSufficient {
				s.log.Info(
					"MB ID-first match — skipping search cascade",
					"group_key", groupKey, "score", ranked[0].Score,
				)

				return idCands
			}

			out = idCands
		}
	}

	searchHits, err := s.mb.ResolveMB(ctx, g)
	if err != nil {
		s.log.Warn(
			"MB resolve failed — returning local-only candidates",
			"group_key", groupKey, "err", err,
		)

		return out
	}

	return append(out, dropDuplicateReleases(out, searchHits)...)
}

// dropDuplicateReleases filters from `extra` any candidate whose
// release MBID already appears in `have`.
func dropDuplicateReleases(have, extra []Candidate) []Candidate {
	if len(have) == 0 {
		return extra
	}

	seen := make(map[string]bool, len(have))

	for _, c := range have {
		if c.ReleaseMBID != "" {
			seen[c.ReleaseMBID] = true
		}
	}

	out := make([]Candidate, 0, len(extra))

	for _, c := range extra {
		if c.ReleaseMBID != "" && seen[c.ReleaseMBID] {
			continue
		}

		out = append(out, c)
	}

	return out
}

// sampleRecordingMBIDs picks up to maxIDSampleTracks distinct
// recording MBIDs spread across the group (first, middle, last) —
// enough to corroborate a release via voting without a lookup per
// track.
func sampleRecordingMBIDs(tracks []LocalTrack) []string {
	distinct := make([]string, 0, len(tracks))
	seen := make(map[string]bool, len(tracks))

	for _, t := range tracks {
		if t.RecordingMBID == "" || seen[t.RecordingMBID] {
			continue
		}

		seen[t.RecordingMBID] = true

		distinct = append(distinct, t.RecordingMBID)
	}

	if len(distinct) <= maxIDSampleTracks {
		return distinct
	}

	return []string{
		distinct[0],
		distinct[len(distinct)/2],
		distinct[len(distinct)-1],
	}
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
