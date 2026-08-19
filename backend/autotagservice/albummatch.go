package autotagservice

import (
	"database/sql"
	"fmt"

	"yellowjacket/backend/autotag"
)

// AlbumMatchView is "the autotagger already has a confident match for
// the album you are looking at".
//
// It is deliberately not a score. The album page renders a suggestion,
// and a suggestion has to be actionable: which release, what it is
// called, and whether acting on it here would do the whole album or
// only part of it.
type AlbumMatchView struct {
	// GroupKey is the tagging group the actions operate on.
	GroupKey string `json:"groupKey"`

	// Recommendation is the tier, as a string, for a caller that
	// wants to render the strength rather than trust the filter.
	Recommendation string `json:"recommendation"`

	// Score is the top candidate's raw score, 0..1.
	Score float64 `json:"score"`

	// ReleaseMBID is the release Apply would write.
	ReleaseMBID string `json:"releaseMbid"`

	// Title and ArtistCredit name that release, so the banner can say
	// what it is offering rather than "a match".
	Title        string `json:"title"`
	ArtistCredit string `json:"artistCredit"`

	// TrackCount is the group's local track count.
	TrackCount int64 `json:"trackCount"`

	// GroupCount is how many tagging groups this album spans.
	//
	// More than one means a multi-disc album (one group per disc), and
	// it is the reason this is a field rather than an implementation
	// detail: applying "the album" from a single button would retag
	// one disc of three and leave the folder holding a mix of old and
	// new tags. The caller offers review instead.
	GroupCount int `json:"groupCount"`
}

// MatchForAlbum answers "does the autotagger have something confident
// to say about this album", for the album detail page.
//
// Three things about it are load-bearing.
//
// **It costs no MusicBrainz request.** Everything it needs is already
// on disk: `tagging_items` carries the top score and release from the
// background prefetch, and `tagging_candidates` durably holds the
// scored list. The rate limiters here are shared with every page the
// user can open, so a lookup that fires on page load must not join
// that queue — which also means this returns nothing for a folder
// nobody has scored yet, rather than scoring it now. That is the
// right trade: the prefetch will get to it, and a page that silently
// spends a minute of somebody's MusicBrainz budget to draw a banner
// is worse than a page that says nothing.
//
// **The tier is computed, not read.** `tagging_items.score` is the raw
// number and `Recommend` is what turns it into a claim — capping it
// for an ambiguous runner-up, an incomplete alignment or a folder too
// small to corroborate itself. Filtering on the raw score would
// promise confidence the scorer had explicitly withheld.
//
// **Nothing is said about an album the user has already answered
// for.** Only a `pending` group qualifies: `confirmed` covers both a
// finished apply and an explicit "leave as is", and `skipped` is the
// user saying not now. Re-offering either is nagging, and "leave as
// is" would be actively wrong to argue with.
func (s *Service) MatchForAlbum(albumID int64) (*AlbumMatchView, error) {
	if albumID <= 0 {
		return nil, nil //nolint:nilnil // "no album" is not an error.
	}

	rows, err := s.db.Queries.GetTaggingItemsForAlbum(
		s.ctx, sql.NullInt64{Int64: albumID, Valid: true},
	)
	if err != nil {
		return nil, fmt.Errorf("tagging items for album: %w", err)
	}

	pending := rows[:0:0]

	for _, row := range rows {
		if row.Status == "pending" {
			pending = append(pending, row)
		}
	}

	if len(pending) == 0 {
		return nil, nil //nolint:nilnil // nothing to say is not an error.
	}

	// Rows arrive best-score-first, so the first pending one is the
	// group worth describing. On a multi-disc album that is one disc
	// of several and GroupCount says so.
	best := pending[0]

	cands := s.lookupCachedCandidates(best.GroupKey)
	if len(cands) == 0 {
		return nil, nil //nolint:nilnil // not scored yet; see the doc comment.
	}

	locals, err := s.scorer.LocalTracksForGroup(s.ctx, best.GroupKey)
	if err != nil {
		return nil, fmt.Errorf("local tracks for group: %w", err)
	}

	group := autotag.Group{
		AlbumName:   best.AlbumName,
		AlbumArtist: best.AlbumArtist,
		Tracks:      locals,
		Synthetic:   best.Synthetic != 0,
	}

	rec := autotag.Recommend(group, cands)
	if !autotag.Confident(rec) {
		return nil, nil //nolint:nilnil // not confident enough to interrupt.
	}

	top := cands[0]

	// The release the banner names must be the release Apply would
	// write. Apply with an empty MBID takes the top cached candidate,
	// which is what this reads — but it is passed explicitly anyway,
	// so a rescore between the page rendering and the user clicking
	// cannot swap the album out from under a button they have already
	// read.
	return &AlbumMatchView{
		GroupKey:       best.GroupKey,
		Recommendation: string(rec),
		Score:          top.Score,
		ReleaseMBID:    top.ReleaseMBID,
		Title:          top.Title,
		ArtistCredit:   top.ArtistCredit,
		TrackCount:     best.TrackCount,
		GroupCount:     len(pending),
	}, nil
}
