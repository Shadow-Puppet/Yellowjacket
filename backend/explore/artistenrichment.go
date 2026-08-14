package explore

import (
	"context"
	"strings"
	"time"
)

// artistEnrichment records which catalog passes have completed for one
// artist.  See sql/schemas/artist_enrichment.sql for why these live
// beside explore_index rather than in it.
type artistEnrichment struct {
	// Browsed is true once the full MusicBrainz browse has landed.
	Browsed bool
	// Similar is true once similar_artist_map has been filled.
	Similar bool
}

// enrichmentFor reads an artist's marks.  A missing row is the zero
// value — nothing done — so a read error degrades to re-fetching rather
// than to skipping, which is the safe direction for a resumable pass.
func (si *SearchIndex) enrichmentFor(mbid string) artistEnrichment {
	var mark artistEnrichment

	rows, err := si.db.QueryContext(
		"SELECT browsed_at IS NOT NULL, similar_at IS NOT NULL "+
			"FROM artist_enrichment WHERE artist_mbid = ?",
		mbid,
	)
	if err != nil {
		return mark
	}

	defer func() { _ = rows.Close() }()

	if rows.Next() {
		_ = rows.Scan(&mark.Browsed, &mark.Similar)
	}

	return mark
}

// artistBrowsed reports whether the full MB browse has run for an
// artist.
func (si *SearchIndex) artistBrowsed(mbid string) bool {
	return si.enrichmentFor(mbid).Browsed
}

// markArtistBrowsed records that the full MB browse has landed.
func (si *SearchIndex) markArtistBrowsed(mbid string) {
	si.markArtistEnrichment(mbid, "browsed_at")
}

// markArtistSimilar records that similar artists have been persisted.
func (si *SearchIndex) markArtistSimilar(mbid string) {
	si.markArtistEnrichment(mbid, "similar_at")
}

// markArtistEnrichment stamps one column, leaving the other alone.  The
// column name is never user input — the two callers above are the only
// ones, and each passes a literal.
func (si *SearchIndex) markArtistEnrichment(mbid, column string) {
	if mbid == "" {
		return
	}

	now := time.Now().UTC()

	_, err := si.db.ExecContext(
		"INSERT INTO artist_enrichment (artist_mbid, "+column+") VALUES (?, ?) "+
			"ON CONFLICT(artist_mbid) DO UPDATE SET "+column+" = excluded."+column,
		mbid, now,
	)
	if err != nil {
		si.logger.Warn("artist enrichment: mark failed",
			"mbid", mbid, "column", column, "error", err,
		)
	}
}

// SetMusicBrainz wires the shared MB client so the owned-artist backfill
// can complete a discography the ListenBrainz endpoints can only sketch.
// Without it the backfill still runs; it just skips the browse.
func (si *SearchIndex) SetMusicBrainz(mb *MusicBrainzClient) {
	si.mu.Lock()
	si.mb = mb
	si.mu.Unlock()
}

// musicBrainz returns the wired MB client, or nil.
func (si *SearchIndex) musicBrainz() *MusicBrainzClient {
	si.mu.RLock()
	defer si.mu.RUnlock()

	return si.mb
}

// browseFullDiscography fetches every release group MusicBrainz has for
// an artist and merges it into the index, then marks the artist browsed.
//
// This is what makes an owned artist's discography *complete* and
// *typed*: `fetchTopReleaseGroups` takes ListenBrainz's top 50 by listen
// count, above a popularity floor, and LB returns no secondary types at
// all — so without this an artist's page shows their popular albums as
// one undifferentiated list, with the tail missing entirely.
//
// The MBID-keyed mark, rather than the old "does any row have secondary
// types" heuristic, is what makes it run once: an artist whose every
// release is a plain album has no secondary types to find, so the
// heuristic was permanently unsatisfied and re-browsed forever.
func (si *SearchIndex) browseFullDiscography(ctx context.Context, mbid string) bool {
	mb := si.musicBrainz()
	if mb == nil || mbid == "" {
		return false
	}

	rgs, err := mb.BrowseReleaseGroupsAll(ctx, mbid)
	if err != nil {
		si.logger.Debug("discography backfill: browse failed",
			"mbid", mbid, "error", err,
		)

		return false
	}

	// An artist with genuinely no release groups is still browsed —
	// marking it stops the pass asking again every run.  Only an error
	// above leaves it unmarked.
	if len(rgs) > 0 {
		si.AddFromCache(si.browsedArtistName(mbid, rgs), mbid, rgs)
	}

	si.markArtistBrowsed(mbid)

	return true
}

// browsedArtistName picks the name AddFromCache will stamp onto every
// release group a browse returned.
//
// MB's browse-by-artist does not echo the artist credit on each item
// (the artist is the query parameter), so the name has to come from
// somewhere.  The canonical local name is preferred — the index title,
// then the library row, both via artistDisplayName — and a release
// group's own credit is the fallback for an artist neither knows.
//
// The featuring guard is why that fallback is not simply "the first
// credit": a release group credited "X feat. Y" names a collaboration,
// not the artist whose page this is, and stamping it on every row is
// how one artist's discography came to be filed under a collaboration's
// name.  Only true featuring markers count — "&", "x" and "," appear
// inside real artist names.
func (si *SearchIndex) browsedArtistName(mbid string, rgs []MBReleaseGroup) string {
	if name := si.artistDisplayName(mbid); name != "" && name != mbid {
		return name
	}

	for _, rg := range rgs {
		if rg.ArtistCredit != "" && !looksLikeFeaturingCredit(rg.ArtistCredit) {
			return rg.ArtistCredit
		}
	}

	// Empty rather than the MBID: AddFromCache's "non-empty wins" rule
	// then leaves whatever real name arrives later untouched.
	return ""
}

// looksLikeFeaturingCredit reports whether a credit string carries a
// "featuring" clause — i.e. it names a collaboration rather than a
// single artist.
func looksLikeFeaturingCredit(credit string) bool {
	lower := strings.ToLower(credit)
	for _, sep := range []string{" feat. ", " feat ", " featuring ", " ft. ", " ft "} {
		if strings.Contains(lower, sep) {
			return true
		}
	}

	return false
}
