package autotag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"yellowjacket/backend/database/sql/sqlcgen"
)

// TagChanges mirrors tagwriter.TagChanges — redefined here so the
// autotag package does not import tagwriter (and so tests can
// construct changes without pulling in the write pipeline).  The
// adapter in app wiring converts this to the concrete tagwriter
// shape.
type TagChanges map[string]any

// Field name constants matching tagwriter's canonical names.  Keep
// these in sync with tagwriter/tagwriter.go — the runtime adapter
// passes the map through unchanged.
const (
	FieldTitle       = "title"
	FieldArtist      = "artist"
	FieldAlbum       = "album"
	FieldAlbumArtist = "album_artist"
	FieldYear        = "year"
	FieldTrackNumber = "track_number"
	FieldDiscNumber  = "disc_number"
	FieldCoverArt    = "cover_art"
)

// TagWriter is the subset of tagwriter.TagWriter the apply pipeline
// needs.  Defined here so scorer_test.go can stub it and autotag
// stays import-acyclic with tagwriter.
type TagWriter interface {
	WriteTrackTagsByPath(filePath string, changes TagChanges) error
}

// ApplyPlan summarises what Apply is about to do for one group.
// Returned from PrepareApply so the UI can preview (and, in the
// future, pass through a dry-run flag).
type ApplyPlan struct {
	GroupKey  string
	Candidate Candidate
	Tracks    []TrackApply
}

// TrackApply is the per-track slice of an ApplyPlan: the local
// audio file, the aligned candidate track, and the changes the
// writer will actually emit.
type TrackApply struct {
	Local          LocalTrack
	CandidateTrack CandidateTrack
	Changes        TagChanges
	Aligned        bool // false when no candidate track matched (skipped)
}

// ApplyResult summarises the outcome after the writes ran.
type ApplyResult struct {
	GroupKey  string
	Succeeded int
	Failed    int
	Failures  []ApplyFailure
}

// ApplyFailure captures one track that failed during apply.
type ApplyFailure struct {
	FilePath string
	Error    string
}

// ErrNoCandidate is returned when Apply is asked to apply a release
// MBID that isn't in the group's current candidate list.
var ErrNoCandidate = errors.New("autotag: candidate not found for apply")

// Applier runs the per-track tag writes + DB MBID updates when the
// user accepts a candidate.  Cover-art embedding is delegated to a
// separate helper (CoverArtEmbedder) that's optional — tests pass
// nil to skip the CAA path.
type Applier struct {
	q        *sqlcgen.Queries
	tw       TagWriter
	coverArt CoverArtEmbedder
	log      *slog.Logger
}

// CoverArtEmbedder is the autotag pipeline's view of cover-art
// fetching.  Split into two operations so the Applier can fetch
// the release group's art exactly once per album and only consult
// each file's existing-art state per-track:
//
//   - FetchArt is a network operation: hit CAA for the release
//     group, validate dimensions, return the bytes ready to embed
//     (or nil when CAA has nothing / the result is below the
//     minimum size).  Idempotent — call once per album.
//   - HasEmbeddedArt is a per-file probe: read the local file's
//     tags and report whether it already carries a picture.  Cheap
//     compared to FetchArt; called per track.
//
// The Applier merges FieldCoverArt into a track's changes only
// when FetchArt produced bytes AND HasEmbeddedArt returned false
// for that track — preserving the rule "never replace existing
// art".
type CoverArtEmbedder interface {
	FetchArt(ctx context.Context, releaseGroupMBID string) ([]byte, error)
	HasEmbeddedArt(filePath string) bool
}

// NewApplier wires up the apply pipeline.  Pass coverArt=nil to
// skip CAA integration (useful for tests).
func NewApplier(
	q *sqlcgen.Queries,
	tw TagWriter,
	coverArt CoverArtEmbedder,
	logger *slog.Logger,
) *Applier {
	return &Applier{q: q, tw: tw, coverArt: coverArt, log: logger}
}

// BuildPlan constructs the per-track change set for a given
// candidate without executing any writes.  The UI can call this to
// preview the diff before confirming.  When releaseMBID doesn't
// match any candidate in score, ErrNoCandidate is returned.
func (a *Applier) BuildPlan(
	score *GroupScore, releaseMBID string,
) (*ApplyPlan, error) {
	var picked *Candidate

	for i := range score.Candidates {
		c := &score.Candidates[i]
		if c.ReleaseMBID == releaseMBID ||
			(releaseMBID == "" && i == 0) ||
			(c.ReleaseMBID == "" && c.ReleaseGroupMBID == releaseMBID) {
			picked = c

			break
		}
	}

	if picked == nil {
		return nil, ErrNoCandidate
	}

	tracks := make([]TrackApply, 0, len(score.LocalTracks))

	for li, local := range score.LocalTracks {
		align := findAlignment(picked.Alignments, li)
		if align == nil || align.Status == AlignmentUnmatched {
			tracks = append(tracks, TrackApply{Local: local, Aligned: false})

			continue
		}

		candTrack := CandidateTrack{
			Position:     align.CandidatePosition,
			DiscNumber:   align.CandidateDiscNumber,
			Title:        align.CandidateTitle,
			LengthMillis: align.CandidateLength,
			MBID:         align.CandidateMBID,
		}

		tracks = append(tracks, TrackApply{
			Local:          local,
			CandidateTrack: candTrack,
			Changes:        buildChanges(local, *picked, candTrack),
			Aligned:        true,
		})
	}

	return &ApplyPlan{
		GroupKey:  score.GroupKey,
		Candidate: *picked,
		Tracks:    tracks,
	}, nil
}

// ApplyProgress is the optional per-track callback Apply invokes
// after each track is processed.  Hosts pass nil to opt out (used
// in tests and the legacy synchronous Apply path); the
// autotagservice layer wires this to a Wails event emit so the
// review UI can render a progress ring.
//
// counts: current is 1-indexed (the track that just finished),
// total is len(aligned tracks), succeeded/failed are running
// totals.
type ApplyProgress func(current, total, succeeded, failed int)

// Apply runs the plan: for each aligned track, write file tags,
// update DB MBID columns, stamp audio_files.tag_status =
// 'user_confirmed'.  Cover art is fetched once for the whole
// album (idempotent) and merged into each track that has no
// existing embedded art.  Tracks whose changes map ends up empty
// are skipped silently and counted as succeeded — that keeps a
// "Leave as-is on a perfect match" path from spuriously failing
// every track on errNoChanges.
//
// onProgress is called once per aligned track as it completes
// (success or failure); pass nil to opt out.
//
// On completion, tagging_items status flips to 'confirmed' if at
// least one track succeeded; failed-everything jobs leave the
// group in 'pending' so it remains in the review queue.
func (a *Applier) Apply(
	ctx context.Context, plan *ApplyPlan, onProgress ApplyProgress,
) (*ApplyResult, error) {
	result := &ApplyResult{GroupKey: plan.GroupKey}

	// Fetch the release-group's cover art once up front — same
	// album, same JPEG, no point hitting CAA per track.  The
	// per-file existing-art check still runs inside the loop so
	// we never overwrite an existing picture.
	var albumArt []byte

	if a.coverArt != nil && plan.Candidate.ReleaseGroupMBID != "" {
		art, err := a.coverArt.FetchArt(ctx, plan.Candidate.ReleaseGroupMBID)
		if err != nil {
			a.log.Warn(
				"cover art fetch failed — proceeding without embed",
				"release_group_mbid", plan.Candidate.ReleaseGroupMBID, "err", err,
			)
		} else {
			albumArt = art
		}
	}

	// Total of aligned tracks for progress reporting.
	total := 0

	for _, tr := range plan.Tracks {
		if tr.Aligned {
			total++
		}
	}

	current := 0

	for _, tr := range plan.Tracks {
		if !tr.Aligned {
			continue
		}

		current++

		changes := tr.Changes

		if albumArt != nil && a.coverArt != nil && !a.coverArt.HasEmbeddedArt(tr.Local.FilePath) {
			// Copy the map so we don't mutate a slice of shared
			// maps on subsequent iterations.
			merged := make(TagChanges, len(changes)+1)
			for k, v := range changes {
				merged[k] = v
			}

			merged[FieldCoverArt] = albumArt
			changes = merged
		}

		// Skip the file write entirely when no field would change.
		// The tagwriter would return errNoChanges and we'd record
		// it as a failure — wrong outcome for a track that's
		// already correct.  The DB sync still runs so MBIDs land.
		if len(changes) > 0 {
			if err := a.tw.WriteTrackTagsByPath(tr.Local.FilePath, changes); err != nil {
				result.Failed++
				result.Failures = append(result.Failures, ApplyFailure{
					FilePath: tr.Local.FilePath,
					Error:    err.Error(),
				})

				a.log.Warn(
					"apply: tag write failed",
					"path", tr.Local.FilePath, "err", err,
				)

				if onProgress != nil {
					onProgress(current, total, result.Succeeded, result.Failed)
				}

				continue
			}
		}

		if err := a.syncDBMBIDs(ctx, tr, plan.Candidate); err != nil {
			a.log.Warn(
				"apply: MBID DB sync failed (file tags written OK)",
				"path", tr.Local.FilePath, "err", err,
			)
		}

		if err := a.q.SetAudioFileTagStatus(ctx, sqlcgen.SetAudioFileTagStatusParams{
			TagStatus: "user_confirmed",
			ID:        tr.Local.AudioFileID,
		}); err != nil {
			a.log.Warn(
				"apply: tag_status update failed",
				"audio_file_id", tr.Local.AudioFileID, "err", err,
			)
		}

		result.Succeeded++

		if onProgress != nil {
			onProgress(current, total, result.Succeeded, result.Failed)
		}
	}

	// Flip the group's status only when at least one write landed.
	if result.Succeeded > 0 {
		if err := a.q.SetTaggingItemStatus(ctx, sqlcgen.SetTaggingItemStatusParams{
			Status:   "confirmed",
			GroupKey: plan.GroupKey,
		}); err != nil {
			return result, fmt.Errorf("mark group confirmed: %w", err)
		}
	}

	return result, nil
}

// syncDBMBIDs writes the recording + release-group MBIDs from the
// candidate to the local DB so the app's MBID-driven features (MB
// browser "in library" indicator, smart-playlist MBID rule fields)
// pick up the new identity without needing a rescan.  File-level
// MBID frames are *not* written by tagwriter today — they're a
// follow-up; the DB copy is what the app reads.
func (a *Applier) syncDBMBIDs(
	ctx context.Context, tr TrackApply, cand Candidate,
) error {
	// Look up recording row via audio_file.
	af, err := a.q.GetAudioFile(ctx, tr.Local.AudioFileID)
	if err != nil {
		return fmt.Errorf("get audio_file: %w", err)
	}

	if tr.CandidateTrack.MBID != "" {
		if err := a.q.SetRecordingMBID(ctx, sqlcgen.SetRecordingMBIDParams{
			Mbid: sql.NullString{String: tr.CandidateTrack.MBID, Valid: true},
			ID:   af.RecordingID,
		}); err != nil {
			return fmt.Errorf("set recording mbid: %w", err)
		}
	}

	if cand.ReleaseGroupMBID != "" {
		rgID, err := a.q.GetRecordingReleaseGroupID(ctx, af.RecordingID)
		if err == nil && rgID > 0 {
			if err := a.q.SetReleaseGroupMBID(ctx, sqlcgen.SetReleaseGroupMBIDParams{
				Mbid: sql.NullString{String: cand.ReleaseGroupMBID, Valid: true},
				ID:   rgID,
			}); err != nil {
				return fmt.Errorf("set release group mbid: %w", err)
			}

			// Stamp the release-group's original-release year too —
			// this is what the tracklist / smart-playlist year rule
			// surfaces by default once the user accepts a candidate.
			if year := parseYear(cand.OriginalDate); year > 0 {
				if err := a.q.SetReleaseGroupOriginalYear(
					ctx, sqlcgen.SetReleaseGroupOriginalYearParams{
						OriginalYear: sql.NullInt64{Int64: int64(year), Valid: true},
						ID:           rgID,
					},
				); err != nil {
					return fmt.Errorf("set release group original year: %w", err)
				}
			}
		}
	}

	return nil
}

// findAlignment returns the alignment row that corresponds to the
// given local-track index, or nil if the track wasn't aligned
// (status=missing).
func findAlignment(alignments []TrackAlignment, localIdx int) *TrackAlignment {
	for i := range alignments {
		if alignments[i].LocalIndex == localIdx {
			return &alignments[i]
		}
	}

	return nil
}

// buildChanges returns the whitelisted field diff for one track
// given its aligned candidate track and the parent release.  Only
// non-empty candidate values become changes — we don't overwrite
// local tags with empty strings from incomplete MB data.
func buildChanges(
	local LocalTrack, cand Candidate, track CandidateTrack,
) TagChanges {
	changes := make(TagChanges, 8) //nolint:mnd

	if track.Title != "" && track.Title != local.Title {
		changes[FieldTitle] = track.Title
	}

	if cand.ArtistCredit != "" {
		changes[FieldArtist] = cand.ArtistCredit
		changes[FieldAlbumArtist] = cand.ArtistCredit
	}

	if cand.Title != "" {
		changes[FieldAlbum] = cand.Title
	}

	if year := parseYear(cand.Date); year > 0 {
		changes[FieldYear] = year
	}

	if track.Position > 0 && track.Position != local.TrackNumber {
		changes[FieldTrackNumber] = track.Position
	}

	if track.DiscNumber > 0 && track.DiscNumber != local.DiscNumber {
		changes[FieldDiscNumber] = track.DiscNumber
	}

	return changes
}
