package autotag

import (
	"context"
	"fmt"

	"yellowjacket/backend/database/sql/sqlcgen"
)

// LocalResolver turns a tagging group's album name into zero-cost
// candidate releases by looking for local release_groups (with
// MBIDs) whose normalized name matches.  The user's own tagged
// albums become free candidates — if they already have another
// library where the same album was tagged correctly, reuse that.
//
// The resolver talks to the DB through the sqlc-generated Queries
// type, not the database package's DB wrapper — keeps the import
// graph acyclic with the database package (which already depends
// on autotag.GroupKey for migration backfills).
type LocalResolver struct {
	q *sqlcgen.Queries
}

// NewLocalResolver returns a resolver bound to the given Queries.
func NewLocalResolver(q *sqlcgen.Queries) *LocalResolver {
	return &LocalResolver{q: q}
}

// LocalTracksForGroup returns the local audio files in the given
// tagging group, projected into the scorer-ready shape.
func (r *LocalResolver) LocalTracksForGroup(
	ctx context.Context, groupKey string,
) ([]LocalTrack, error) {
	rows, err := r.q.ListAudioFilesInTaggingGroup(ctx, groupKey)
	if err != nil {
		return nil, fmt.Errorf("list local tracks: %w", err)
	}

	out := make([]LocalTrack, 0, len(rows))
	for _, row := range rows {
		out = append(out, LocalTrack{
			AudioFileID:   row.ID,
			FilePath:      row.FilePath,
			Title:         row.Title,
			Artist:        row.ArtistName,
			TrackNumber:   int(row.TrackNumber),
			DiscNumber:    int(row.DiscNumber),
			LengthMillis:  row.LengthMilliseconds,
			RecordingMBID: row.RecordingMbid,
		})
	}

	return out, nil
}

// ResolveLocal returns candidate releases sourced from the local
// DB's release_groups rows (filtered to those carrying an MBID)
// whose normalized name matches the tagging item's album name.
// No network calls.  Candidates carry all tracks flat; caller runs
// AlignTracks on each to produce per-track alignments.
func (r *LocalResolver) ResolveLocal(
	ctx context.Context, albumName string,
) ([]Candidate, error) {
	if albumName == "" {
		return nil, nil
	}

	rows, err := r.q.ListLocalReleaseGroupCandidates(ctx, albumName)
	if err != nil {
		return nil, fmt.Errorf("list local candidates: %w", err)
	}

	normalizedTarget := Normalize(albumName)
	byID := make(map[int64]*Candidate, 4)             //nolint:mnd
	tracksByID := make(map[int64][]CandidateTrack, 4) //nolint:mnd

	for _, row := range rows {
		// Case-insensitive SQL match is a cheap pre-filter; we
		// still apply our full normalization rule in Go to reject
		// false positives like "Greatest Hits" vs "Greatest Hits".
		if Normalize(row.AlbumName) != normalizedTarget {
			continue
		}

		if _, ok := byID[row.ReleaseGroupID]; !ok {
			byID[row.ReleaseGroupID] = localCandidate(row)
		}

		tracksByID[row.ReleaseGroupID] = append(
			tracksByID[row.ReleaseGroupID],
			CandidateTrack{
				Position:     int(row.TrackNumber),
				DiscNumber:   int(row.DiscNumber),
				Title:        row.TrackTitle,
				LengthMillis: row.LengthMilliseconds,
				MBID:         row.RecordingMbid,
			},
		)
	}

	out := make([]Candidate, 0, len(byID))
	for id, c := range byID {
		c.Tracks = tracksByID[id]
		c.TrackCount = len(c.Tracks)
		out = append(out, *c)
	}

	return out, nil
}

// localCandidate converts one sqlc row (minus track-level fields)
// into a Candidate shell.  Track fields and alignments are filled
// in by the caller.
func localCandidate(row sqlcgen.ListLocalReleaseGroupCandidatesRow) *Candidate {
	date := ""
	if row.Year > 0 {
		date = fmt.Sprintf("%04d", row.Year)
	}

	mbid := ""
	if row.ReleaseGroupMbid.Valid {
		mbid = row.ReleaseGroupMbid.String
	}

	return &Candidate{
		ReleaseGroupMBID: mbid,
		Title:            row.AlbumName,
		ArtistCredit:     row.ArtistCredit,
		Date:             date,
		Source:           SourceLocal,
		Provenance:       "local",
	}
}
