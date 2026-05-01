package autotag

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// MBReleaseGroupHit is the minimal projection of a MusicBrainz
// release-group search result that the scorer consumes.  Defined
// here (rather than imported from explore) to keep the autotag
// package's external surface small and swappable.
type MBReleaseGroupHit struct {
	MBID         string
	Title        string
	ArtistCredit string
	FirstDate    string
	PrimaryType  string
}

// MBRelease is the scorer's projection of a MusicBrainz release
// (one specific edition with its track list).
type MBRelease struct {
	MBID         string
	Title        string
	Date         string
	Country      string
	Status       string
	ArtistCredit string
	Tracks       []CandidateTrack
}

// MBClient is the subset of the explore.MusicBrainzClient surface
// the autotagger depends on.  Implementations must be cache-first
// — repeated calls with the same inputs must not repeat network
// round-trips.
type MBClient interface {
	SearchReleaseGroups(
		ctx context.Context,
		query string,
		limit int,
	) ([]MBReleaseGroupHit, int, error)
	BrowseReleases(ctx context.Context, releaseGroupMBID string) ([]MBRelease, error)
	LookupRelease(ctx context.Context, releaseMBID string) (MBRelease, error)
	LookupReleaseGroup(ctx context.Context, releaseGroupMBID string) (MBReleaseGroupHit, error)
	LookupArtist(ctx context.Context, mbid string) (string, error) // returns sort name or name
}

// MBResolver orchestrates MusicBrainz lookups for a tagging group.
// Strategy: normalize the user-provided album/artist first, then
// issue a cascade of progressively looser Lucene queries, stopping
// at the first one that yields enough candidates.
type MBResolver struct {
	client MBClient
	logger *slog.Logger
	limit  int
}

// NewMBResolver wires up the resolver with a default search limit.
func NewMBResolver(client MBClient, logger *slog.Logger) *MBResolver {
	const defaultLimit = 5

	return &MBResolver{client: client, logger: logger, limit: defaultLimit}
}

// mbQueryStep describes one cascade level.  `label` is surfaced in
// candidate provenance (so the UI can show "via fuzzy title").
type mbQueryStep struct {
	label string
	query string
}

// ResolveMB returns MB-sourced candidates for a tagging group.
// Runs a cascade of Lucene queries, returning at the first step
// that produces results.  Each search hit fans out to
// BrowseReleases (one per release-group) for track-level data.
func (r *MBResolver) ResolveMB(
	ctx context.Context,
	albumName, albumArtist string,
	trackCount int,
	knownArtistMBID string,
) ([]Candidate, error) {
	if albumName == "" {
		return nil, nil
	}

	nAlbum := Normalize(albumName)
	nArtist := Normalize(albumArtist)

	if nAlbum == "" {
		return nil, nil
	}

	steps := buildMBQueryCascade(nAlbum, nArtist, trackCount, knownArtistMBID)

	for _, step := range steps {
		hits, _, err := r.client.SearchReleaseGroups(ctx, step.query, r.limit)
		if err != nil {
			r.logger.Warn(
				"MB search step failed — trying next",
				"step", step.label, "query", step.query, "err", err,
			)

			continue
		}

		if len(hits) == 0 {
			r.logger.Debug(
				"MB search step empty — trying next",
				"step", step.label, "query", step.query,
			)

			continue
		}

		r.logger.Info(
			"MB search step succeeded",
			"step", step.label, "hits", len(hits),
		)

		return r.fanOutBrowse(ctx, hits, step.label), nil
	}

	return nil, nil
}

// fanOutBrowse iterates search hits, fetches each release-group's
// releases, and returns them as Candidates.  Errors on individual
// browses are logged and skipped.
func (r *MBResolver) fanOutBrowse(
	ctx context.Context, hits []MBReleaseGroupHit, step string,
) []Candidate {
	var out []Candidate

	for _, h := range hits {
		releases, err := r.client.BrowseReleases(ctx, h.MBID)
		if err != nil {
			r.logger.Warn(
				"browse releases failed — skipping release group",
				"release_group_mbid", h.MBID, "err", err,
			)

			continue
		}

		for _, rel := range releases {
			out = append(out, mkCandidate(h, rel, step))
		}
	}

	return out
}

// ResolveOneReleaseMBID fetches a single release by MBID and
// returns it as a fully-populated Candidate.  Used by the paste-
// URL escape hatch.  Falls back to LookupReleaseGroup when the
// MBID resolves to a release group instead.
func (r *MBResolver) ResolveOneReleaseMBID(
	ctx context.Context, mbid string,
) (Candidate, error) {
	rel, err := r.client.LookupRelease(ctx, mbid)
	if err == nil && rel.MBID != "" {
		return Candidate{
			ReleaseMBID:      rel.MBID,
			ReleaseGroupMBID: "",
			Title:            rel.Title,
			ArtistCredit:     rel.ArtistCredit,
			Date:             rel.Date,
			Country:          rel.Country,
			Status:           rel.Status,
			TrackCount:       len(rel.Tracks),
			Tracks:           rel.Tracks,
			Source:           SourceMusicBrainz,
			Provenance:       "paste",
		}, nil
	}

	rgHit, rgErr := r.client.LookupReleaseGroup(ctx, mbid)
	if rgErr != nil {
		return Candidate{}, fmt.Errorf("lookup release or RG: %w / %w", err, rgErr)
	}

	releases, bErr := r.client.BrowseReleases(ctx, rgHit.MBID)
	if bErr != nil {
		return Candidate{}, fmt.Errorf("browse RG %s: %w", rgHit.MBID, bErr)
	}

	if len(releases) == 0 {
		return Candidate{}, fmt.Errorf("%w: %s", errEmptyBrowseResult, mbid)
	}

	return mkCandidate(rgHit, releases[0], "paste"), nil
}

// mkCandidate combines a search hit with one of its releases into
// a Candidate ready for scoring.  Date is the release-specific date
// (re-issue year for remasters), OriginalDate is the release-group's
// first-release-date (the album's original year).
func mkCandidate(h MBReleaseGroupHit, rel MBRelease, step string) Candidate {
	return Candidate{
		ReleaseMBID:      rel.MBID,
		ReleaseGroupMBID: h.MBID,
		Title:            firstNonEmpty(rel.Title, h.Title),
		ArtistCredit:     firstNonEmpty(rel.ArtistCredit, h.ArtistCredit),
		Date:             firstNonEmpty(rel.Date, h.FirstDate),
		OriginalDate:     h.FirstDate,
		Country:          rel.Country,
		Status:           rel.Status,
		TrackCount:       len(rel.Tracks),
		Tracks:           rel.Tracks,
		Source:           SourceMusicBrainz,
		Provenance:       step,
	}
}

// buildMBQueryCascade returns the Lucene queries to try in order.
// Cascade:
//
//  1. Full: release + arid/artist + tracks:N
//  2. Drop tracks:N (bonus tracks, live editions, etc.)
//  3. Drop artist entirely (wrong artist tag is common)
//  4. Fuzzy title (unquoted; Lucene does token/prefix match)
//
// Each step is only added when it would differ from the previous.
func buildMBQueryCascade(
	normAlbum, normArtist string,
	trackCount int,
	artistMBID string,
) []mbQueryStep {
	var steps []mbQueryStep

	release := "release:" + luceneQuote(normAlbum)
	artistClause := ""

	switch {
	case artistMBID != "":
		artistClause = "arid:" + artistMBID
	case normArtist != "":
		artistClause = "artist:" + luceneQuote(normArtist)
	}

	tracksClause := ""
	if trackCount > 0 {
		tracksClause = fmt.Sprintf("tracks:%d", trackCount)
	}

	// Step 1: full query (only include parts we actually have).
	steps = append(steps, mbQueryStep{
		label: "strict",
		query: joinNonEmpty(release, artistClause, tracksClause),
	})

	// Step 2: drop tracks:N if we had one.
	if tracksClause != "" {
		steps = append(steps, mbQueryStep{
			label: "no-track-count",
			query: joinNonEmpty(release, artistClause),
		})
	}

	// Step 3: drop the artist clause.
	if artistClause != "" {
		steps = append(steps, mbQueryStep{
			label: "title-only",
			query: release,
		})
	}

	// Step 4: fuzzy / unquoted title.  MB's Lucene tokenizer will
	// do prefix + fuzzy matching on bare tokens.
	fuzzy := "release:" + luceneTokens(normAlbum)
	if fuzzy != release {
		steps = append(steps, mbQueryStep{
			label: "fuzzy-title",
			query: fuzzy,
		})
	}

	return steps
}

// luceneQuote wraps a phrase in quotes and escapes embedded quotes
// and backslashes.  Used for exact-phrase clauses.
func luceneQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)

	return `"` + s + `"`
}

// luceneTokens returns the phrase as space-separated tokens with
// Lucene reserved characters escaped.  MB's analyzer applies
// tokenization + fuzzy matching across bare tokens, so this is
// the right shape for our loosest cascade level.
func luceneTokens(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	// Escape Lucene-reserved glyphs that aren't stripped by our
	// Normalize() (paren, bracket, colon, etc.  Normalize already
	// drops most of these, but belt + braces).
	reserved := `+-&|!(){}[]^"~*?:\/`

	var b strings.Builder

	b.Grow(len(s))

	for _, r := range s {
		if strings.ContainsRune(reserved, r) {
			b.WriteRune('\\')
		}

		b.WriteRune(r)
	}

	return b.String()
}

// joinNonEmpty joins non-empty parts with " AND ".
func joinNonEmpty(parts ...string) string {
	kept := make([]string, 0, len(parts))

	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}

	return strings.Join(kept, " AND ")
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}

	return b
}

// errEmptyBrowseResult signals that LookupReleaseGroup succeeded
// but BrowseReleases returned nothing — unusual, but not fatal.
var errEmptyBrowseResult = errors.New("autotag: release group has no releases")
