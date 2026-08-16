package explore

import (
	"context"
	"errors"

	"yellowjacket/backend/database"
)

// LyricsResult is a single lyric-search hit, mapped from the DB layer
// into the camelCase shape the frontend consumes.
type LyricsResult struct {
	AudioFileID int64  `json:"audioFileId"`
	FilePath    string `json:"filePath"`
	LengthMs    int64  `json:"lengthMs"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
}

// TrackLyrics is the stored or freshly-fetched lyrics for one track.
// Source is "embedded" (from the file's tags / library DB), "lrclib"
// (fetched on demand), or "" when none are available.
type TrackLyrics struct {
	Plain        string `json:"plain"`
	Synced       string `json:"synced"`
	Instrumental bool   `json:"instrumental"`
	Source       string `json:"source"`
}

const (
	// lyricsSearchLimit caps lyric-search hits returned to the UI.
	lyricsSearchLimit = 30

	// lyricsBackfillBatch is how many missing-lyrics recordings the
	// background backfill processes per pass.
	lyricsBackfillBatch = 200

	// lyricsBackfillMaxPasses bounds a single backfill run so it can't
	// loop forever on a huge library; the next launch resumes where
	// this one left off (candidates with lyrics now filled are skipped).
	lyricsBackfillMaxPasses = 25
)

// SearchLyrics finds library tracks whose lyrics contain the given
// fragment, ranked by relevance.  Pure local FTS — no network.
func (e *Service) SearchLyrics(query string) []LyricsResult {
	hits, err := e.db.SearchLyrics(query, lyricsSearchLimit)
	if err != nil {
		e.logger.Warn("lyrics search failed", "query", query, "err", err)

		return nil
	}

	out := make([]LyricsResult, 0, len(hits))
	for _, h := range hits {
		out = append(out, LyricsResult{
			AudioFileID: h.AudioFileID,
			FilePath:    h.FilePath,
			LengthMs:    h.LengthMilliseconds,
			Title:       h.Title,
			Artist:      h.Artist,
			Album:       h.Album,
		})
	}

	return out
}

// GetTrackLyrics returns lyrics for a file.  If the library
// already has them (from embedded tags) they're returned as-is;
// otherwise it fetches from LRCLIB, persists them (updating the FTS
// index), and returns them.  Never returns an error to the frontend —
// a miss just yields an empty result.
func (e *Service) GetTrackLyrics(audioFileID int64) TrackLyrics {
	stored, err := e.db.GetLyrics(audioFileID)
	if err == nil && stored != "" {
		return TrackLyrics{Plain: stored, Source: "embedded"}
	}

	lookup, err := e.db.FileLyricLookup(audioFileID)
	if err != nil || lookup == nil {
		return TrackLyrics{}
	}

	fetched := e.fetchAndStoreLyrics(e.ctx, *lookup)
	if fetched == nil {
		return TrackLyrics{}
	}

	return TrackLyrics{
		Plain:        fetched.Plain,
		Synced:       fetched.Synced,
		Instrumental: fetched.Instrumental,
		Source:       "lrclib",
	}
}

// RebuildLyricsIndex rebuilds the FTS lyrics index from the current
// library.  Cheap; safe to call after every scan.
func (e *Service) RebuildLyricsIndex() {
	if err := e.db.RebuildLyricsIndex(); err != nil {
		e.logger.Warn("lyrics index rebuild failed", "err", err)

		return
	}

	e.index.setMeta(lyricsIndexReadyKey, "1")
	e.logger.Info("lyrics index rebuilt")
}

// RebuildLyricsIndexIfNeeded rebuilds the lyrics FTS only when it has not
// been built since the last library change.  The backfill keeps the index
// in sync incrementally thereafter, so on an unchanged library the full
// rebuild is redundant; the scan-completion path calls the unconditional
// form.
func (e *Service) RebuildLyricsIndexIfNeeded() {
	if e.index.hasMeta(lyricsIndexReadyKey) {
		return
	}

	e.RebuildLyricsIndex()
}

// BackfillLibraryLyrics fetches lyrics from LRCLIB for library tracks
// that don't have them, in the background.  Idempotent and bounded —
// each recording is tried once (a miss is cached), and a run stops
// after a fixed number of passes, resuming on the next launch.
func (e *Service) BackfillLibraryLyrics() {
	go e.backfillLibraryLyrics(e.ctx)
}

func (e *Service) backfillLibraryLyrics(ctx context.Context) {
	total := 0

	// LRCLIB has its own limiter, so this starves nothing — but it is
	// still work nobody asked for, and it was the last background pass
	// with no way to see or stop it.  The job is registered lazily,
	// after the first batch proves there is something to do, because on
	// a covered library every launch would otherwise put an empty job
	// in the indicator.
	ctx = WithBackgroundPriority(ctx)

	var job *backfillJob

	defer func() { job.finish(ctx) }()

	for range lyricsBackfillMaxPasses {
		if ctx.Err() != nil {
			return
		}

		candidates, err := e.db.FilesMissingLyrics(lyricsBackfillBatch)
		if err != nil {
			e.logger.Warn("lyrics backfill: query failed", "err", err)

			return
		}

		if len(candidates) == 0 {
			break
		}

		if job == nil {
			job, ctx = startBackfillJob(
				ctx, e.index.jobRegistry(), lyricsBackfillJobID,
				"Looking up lyrics",
				"Tracks in your library with no lyrics yet",
				len(candidates),
			)
		}

		filled := 0

		for i, c := range candidates {
			if ctx.Err() != nil {
				return
			}

			job.progress(i, len(candidates))

			if e.fetchAndStoreLyrics(ctx, c) != nil {
				filled++
				total++
			}
		}

		// If a whole batch produced no stored lyrics, every remaining
		// candidate is a cached miss with no new data — stop early
		// rather than spinning through identical misses.
		if filled == 0 {
			break
		}
	}

	if total > 0 {
		e.logger.Info("lyrics backfill complete", "filled", total)
	}
}

// fetchAndStoreLyrics looks up a single candidate on LRCLIB and, on a
// non-instrumental hit with plain lyrics, persists it to the recording
// (which also updates the FTS index).  Returns the fetched lyrics, or
// nil on any miss/error.  Instrumental hits are recorded as an empty
// lyrics string so they still count as "resolved" and aren't retried.
func (e *Service) fetchAndStoreLyrics(
	ctx context.Context, c database.LyricsCandidate,
) *Lyrics {
	durationSec := int(c.LengthMilliseconds / 1000) //nolint:mnd

	lyrics, err := e.lrclib.GetLyrics(ctx, c.Artist, c.Title, c.Album, durationSec)
	if err != nil {
		if !errors.Is(err, ErrLyricsNotFound) {
			e.logger.Debug("lyrics fetch failed",
				"artist", c.Artist, "title", c.Title, "err", err,
			)
		}

		return nil
	}

	if lyrics.Instrumental || lyrics.Plain == "" {
		return nil
	}

	// Marked `lrclib` rather than `tag`: these came off the network and
	// a rebuild that discards them pays for them again.
	if err := e.db.SetLyrics(
		c.AudioFileID, lyrics.Plain, "lrclib", c.RecordingMBID,
	); err != nil {
		e.logger.Warn("lyrics store failed", "audioFileId", c.AudioFileID, "err", err)

		return nil
	}

	return lyrics
}
