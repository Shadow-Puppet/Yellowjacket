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

// MBRecordingHit is the minimal projection of a MusicBrainz recording
// search result — used by the in-app search path (singletons).
type MBRecordingHit struct {
	MBID         string
	Title        string
	ArtistCredit string
	LengthMillis int64
}

// MBReleaseRef is a slim reference to one release a recording appears
// on.  The resolver ranks these to pick a representative release and
// then resolves it in full via ResolveOneReleaseMBID.
type MBReleaseRef struct {
	MBID   string
	Title  string
	Status string
	Date   string
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
	// SearchReleaseGroupsLocal searches the offline dump-derived
	// catalog for release groups matching albumName — no network
	// round-trip.  ok is false when the local catalog isn't
	// populated yet (or the implementation has no offline index),
	// telling the caller to rely on the network cascade alone; ok
	// true with zero hits means the catalog was consulted and
	// genuinely has nothing.
	SearchReleaseGroupsLocal(
		ctx context.Context,
		albumName string,
		limit int,
	) (hits []MBReleaseGroupHit, ok bool)
	SearchRecordings(
		ctx context.Context,
		query string,
		limit int,
	) ([]MBRecordingHit, int, error)
	LookupRecordingReleases(ctx context.Context, recordingMBID string) ([]MBReleaseRef, error)
	BrowseReleases(ctx context.Context, releaseGroupMBID string) ([]MBRelease, error)
	LookupRelease(ctx context.Context, releaseMBID string) (MBRelease, error)
	LookupReleaseGroup(ctx context.Context, releaseGroupMBID string) (MBReleaseGroupHit, error)
}

// mbidVariousArtists is the MusicBrainz artist MBID for the special
// "Various Artists" entity — used as an arid filter when a group
// looks like a compilation.
const mbidVariousArtists = "89ad4ac3-39f7-470e-963a-56509c546377"

// cascadeSufficient is the merged-best score at which the cascade
// stops issuing looser queries.  "First step with any hits" is the
// wrong stop condition — a strict query can return plausible-but-
// wrong release groups and starve the looser steps of the chance to
// surface the right one.  Scoring is free; searches and browses are
// rate-limited network calls, so the cascade pays for another step
// only while the best candidate so far is still mediocre.
const cascadeSufficient = 0.70

// hitBrowseFloor is the minimum title-or-artist similarity a search
// hit needs before the resolver pays a rate-limited BrowseReleases
// call for it.  Hits failing both checks are junk from MB's fuzzy
// tokenizer.
const hitBrowseFloor = 0.30

// MBResolver orchestrates MusicBrainz lookups for a tagging group.
// Strategy: use recording MBIDs already present in local tags when
// possible (exact, cheap); otherwise issue a cascade of
// progressively looser Lucene queries, merging results until a
// candidate scores well enough to stop.
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
// Runs the Lucene query cascade, accumulating deduplicated
// candidates across steps and stopping once the best merged
// candidate scores at least cascadeSufficient against the group.
func (r *MBResolver) ResolveMB(ctx context.Context, g Group) ([]Candidate, error) {
	nAlbum := Normalize(g.AlbumName)
	if nAlbum == "" {
		return nil, nil
	}

	nArtist := Normalize(groupArtist(g))

	seen := make(map[string]bool)

	var merged []Candidate

	// Local-index pass: the offline dump-derived catalog covers
	// essentially every popular release group, so try it before
	// spending any rate-limited search calls.  This never skips
	// BrowseReleases (the catalog doesn't carry per-release
	// tracklists) but it very often means the network Lucene
	// cascade below never has to run at all.
	if localHits, ok := r.client.SearchReleaseGroupsLocal(ctx, g.AlbumName, r.limit); ok {
		added := r.fanOutBrowse(ctx, g, localHits, "index", seen, &merged)

		r.logger.Debug(
			"local index search done",
			"hits", len(localHits), "new_candidates", added,
		)

		if added > 0 {
			ranked := RankCandidates(g, merged)
			if len(ranked) > 0 && ranked[0].Score >= cascadeSufficient {
				r.logger.Info(
					"MB cascade stopped — sufficient local-index candidate",
					"score", ranked[0].Score,
				)

				return merged, nil
			}
		}
	}

	steps := buildMBQueryCascade(nAlbum, nArtist, len(g.Tracks), vaLikely(g))

	for _, step := range steps {
		hits, _, err := r.client.SearchReleaseGroups(ctx, step.query, r.limit)
		if err != nil {
			r.logger.Warn(
				"MB search step failed — trying next",
				"step", step.label, "query", step.query, "err", err,
			)

			continue
		}

		added := r.fanOutBrowse(ctx, g, hits, step.label, seen, &merged)

		r.logger.Debug(
			"MB search step done",
			"step", step.label, "hits", len(hits), "new_candidates", added,
		)

		if added == 0 {
			continue
		}

		// Score what we have so far; good enough means the looser
		// (noisier, costlier) steps aren't needed.
		ranked := RankCandidates(g, merged)
		if len(ranked) > 0 && ranked[0].Score >= cascadeSufficient {
			r.logger.Info(
				"MB cascade stopped — sufficient candidate",
				"step", step.label, "score", ranked[0].Score,
			)

			break
		}
	}

	return merged, nil
}

// fanOutBrowse iterates search hits, fetches each plausible
// release-group's releases, and appends previously-unseen ones to
// merged as Candidates.  Returns how many candidates were added.
// Errors on individual browses are logged and skipped.
func (r *MBResolver) fanOutBrowse(
	ctx context.Context,
	g Group,
	hits []MBReleaseGroupHit,
	step string,
	seen map[string]bool,
	merged *[]Candidate,
) int {
	added := 0

	for _, h := range hits {
		if seen["rg:"+h.MBID] {
			continue
		}

		seen["rg:"+h.MBID] = true

		// Don't pay a rate-limited browse for a hit that resembles
		// neither the folder's album name nor its artist.
		if !hitPlausible(g, h) {
			r.logger.Debug(
				"skipping implausible search hit",
				"title", h.Title, "artist", h.ArtistCredit,
			)

			continue
		}

		releases, err := r.client.BrowseReleases(ctx, h.MBID)
		if err != nil {
			r.logger.Warn(
				"browse releases failed — skipping release group",
				"release_group_mbid", h.MBID, "err", err,
			)

			continue
		}

		for _, rel := range releases {
			if rel.MBID != "" && seen[rel.MBID] {
				continue
			}

			seen[rel.MBID] = true

			*merged = append(*merged, mkCandidate(h, rel, step))
			added++
		}
	}

	return added
}

// hitPlausible reports whether a release-group search hit is worth
// a BrowseReleases round-trip: its title or artist must bear at
// least a loose resemblance to the group's.  Unknown local fields
// never disqualify a hit.
func hitPlausible(g Group, h MBReleaseGroupHit) bool {
	if g.AlbumName != "" && h.Title != "" &&
		titleSimilarity(g.AlbumName, h.Title) >= hitBrowseFloor {
		return true
	}

	artist := groupArtist(g)
	if artist != "" && h.ArtistCredit != "" &&
		titleSimilarity(artist, h.ArtistCredit) >= hitBrowseFloor {
		return true
	}

	// Nothing to compare against (or a VA credit): stay permissive.
	return g.AlbumName == "" || h.Title == "" || isVAName(h.ArtistCredit)
}

// ResolveByRecordingMBIDs resolves candidates from recording MBIDs
// already present in the local tags — the highest-precision signal
// available, and the reason previously-tagged files should never
// need a fuzzy search.  Each recording is looked up, releases are
// counted as votes, and the best-voted release (Official and
// earliest among ties) is resolved in full with provenance "id".
// Returns nil when no recording resolves to any release.
func (r *MBResolver) ResolveByRecordingMBIDs(
	ctx context.Context, recordingMBIDs []string,
) ([]Candidate, error) {
	votes := make(map[string]int)
	refs := make(map[string]MBReleaseRef)

	for _, id := range recordingMBIDs {
		rels, err := r.client.LookupRecordingReleases(ctx, id)
		if err != nil {
			r.logger.Warn(
				"recording lookup failed — skipping",
				"recording_mbid", id, "err", err,
			)

			continue
		}

		counted := make(map[string]bool, len(rels))

		for _, ref := range rels {
			if ref.MBID == "" || counted[ref.MBID] {
				continue
			}

			counted[ref.MBID] = true
			votes[ref.MBID]++
			refs[ref.MBID] = ref
		}
	}

	if len(votes) == 0 {
		return nil, nil
	}

	// Highest vote count wins; betterRelease breaks ties so the
	// pick is deterministic and favours Official + earliest.
	var (
		bestMBID  string
		bestVotes int
	)

	for mbid, n := range votes {
		switch {
		case n > bestVotes:
			bestMBID, bestVotes = mbid, n
		case n == bestVotes && betterRelease(refs[mbid], refs[bestMBID]):
			bestMBID = mbid
		}
	}

	cand, err := r.ResolveOneReleaseMBID(ctx, bestMBID)
	if err != nil {
		return nil, fmt.Errorf("resolve voted release %s: %w", bestMBID, err)
	}

	cand.Provenance = "id"

	return []Candidate{cand}, nil
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
		PrimaryType:      h.PrimaryType,
		TrackCount:       len(rel.Tracks),
		Tracks:           rel.Tracks,
		Source:           SourceMusicBrainz,
		Provenance:       step,
	}
}

// SearchReleaseGroupHits runs a single release-group search from a
// user-supplied album + artist (the in-app "suggest a candidate"
// path).  Both fields are normalized and phrase-quoted; artist is
// dropped from the query when empty.
func (r *MBResolver) SearchReleaseGroupHits(
	ctx context.Context, album, artist string,
) ([]MBReleaseGroupHit, error) {
	query := "release:" + luceneQuote(Normalize(album))
	if a := Normalize(artist); a != "" {
		query += " AND artist:" + luceneQuote(a)
	}

	hits, _, err := r.client.SearchReleaseGroups(ctx, query, r.limit)
	if err != nil {
		return nil, fmt.Errorf("search release groups: %w", err)
	}

	return hits, nil
}

// SearchRecordingHits runs a single recording search from a user-
// supplied title + artist — the singleton path, where the folder has
// one track and release-group search is too coarse.
func (r *MBResolver) SearchRecordingHits(
	ctx context.Context, title, artist string,
) ([]MBRecordingHit, error) {
	query := "recording:" + luceneQuote(Normalize(title))
	if a := Normalize(artist); a != "" {
		query += " AND artist:" + luceneQuote(a)
	}

	hits, _, err := r.client.SearchRecordings(ctx, query, r.limit)
	if err != nil {
		return nil, fmt.Errorf("search recordings: %w", err)
	}

	return hits, nil
}

// ResolveOneRecordingMBID turns a picked recording into a fully-scored
// Candidate by resolving it to a representative release (so the
// existing release-based diff + Apply pipeline works unchanged).
// Picks the release the same way a human would default: prefer an
// Official status, then the earliest date.  Provenance is
// "search-recording" so the UI can label where it came from.
func (r *MBResolver) ResolveOneRecordingMBID(
	ctx context.Context, recordingMBID string,
) (Candidate, error) {
	refs, err := r.client.LookupRecordingReleases(ctx, recordingMBID)
	if err != nil {
		return Candidate{}, fmt.Errorf("lookup recording releases: %w", err)
	}

	best := pickRepresentativeRelease(refs)
	if best.MBID == "" {
		return Candidate{}, fmt.Errorf("%w: %s", errNoReleasesForRecording, recordingMBID)
	}

	cand, err := r.ResolveOneReleaseMBID(ctx, best.MBID)
	if err != nil {
		return Candidate{}, err
	}

	cand.Provenance = "search-recording"

	return cand, nil
}

// pickRepresentativeRelease chooses the release most likely to be the
// one the user means: an Official release beats a non-Official one,
// and among equals the earliest date wins (favouring the original
// over later reissues).  Returns the zero value for an empty slice.
func pickRepresentativeRelease(refs []MBReleaseRef) MBReleaseRef {
	var best MBReleaseRef

	for _, ref := range refs {
		if best.MBID == "" || betterRelease(ref, best) {
			best = ref
		}
	}

	return best
}

// betterRelease reports whether a should be preferred over b.
func betterRelease(a, b MBReleaseRef) bool {
	aOfficial := strings.EqualFold(a.Status, "Official")
	bOfficial := strings.EqualFold(b.Status, "Official")

	if aOfficial != bOfficial {
		return aOfficial
	}

	// Same official-ness: earlier date wins.  Empty dates sort last
	// so a dated release beats an undated one.
	switch {
	case a.Date == "":
		return false
	case b.Date == "":
		return true
	default:
		return a.Date < b.Date
	}
}

// errNoReleasesForRecording signals a recording that resolved to zero
// releases — nothing to diff or apply against.
var errNoReleasesForRecording = errors.New("autotag: recording has no releases")

// buildMBQueryCascade returns the Lucene queries to try in order.
// Cascade:
//
//  1. Full: release + artist (or VA arid) + tracks:N
//  2. Drop tracks:N (bonus tracks, live editions, etc.)
//  3. Drop artist entirely (wrong artist tag is common)
//  4. Fuzzy title (unquoted; Lucene does token/prefix match)
//
// VA-likely groups filter on the Various Artists arid instead of an
// artist name — per-track artists on a compilation say nothing
// about the release's artist credit.  Each step is only added when
// it would differ from the previous.
func buildMBQueryCascade(
	normAlbum, normArtist string,
	trackCount int,
	va bool,
) []mbQueryStep {
	var steps []mbQueryStep

	release := "release:" + luceneQuote(normAlbum)
	artistClause := ""

	switch {
	case va:
		artistClause = "arid:" + mbidVariousArtists
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
