package download

import (
	"math"
	"math/rand/v2"
	"time"
)

// A Want is a persistent "I want this", stored as a MusicBrainz ID and
// almost nothing else.
//
// The distinction from Request is the whole point of this file.  A
// Request is one attempt: it searches, it grabs, it succeeds or fails,
// and then it is history.  A Want outlives every attempt made on its
// behalf.  Nothing being findable today is the normal case for obscure
// music, and the correct response is to try again next week, not to
// show the user a failed row they have to remember to retry.
//
// Because a Want is only an MBID, it stays true when everything around
// it changes: the explore index is rebuilt, a provider is swapped out,
// the release the user originally saw is superseded by a remaster.  The
// display fields are a cache for the list view and are never consulted
// for matching.

// Entity says what a want's MBID names, and is the only type
// distinction the wanted list makes.
type Entity string

// Want entity types.
const (
	// EntityArtist is a subscription rather than a thing to fetch: it
	// is never satisfied, and each reconcile expands the artist's
	// discography into child wants.
	EntityArtist Entity = "artist"

	// EntityReleaseGroup is an album in the abstract — any release of
	// it satisfies the want, which is what a user means by "I want this
	// album".
	EntityReleaseGroup Entity = "release-group"

	// EntityRelease is one specific edition, used when the user picked
	// a particular pressing.
	EntityRelease Entity = "release"

	// EntityRecording is a single track.
	EntityRecording Entity = "recording"
)

// Valid reports whether e is a known entity type.
func (e Entity) Valid() bool {
	switch e {
	case EntityArtist, EntityReleaseGroup, EntityRelease, EntityRecording:
		return true
	default:
		return false
	}
}

// Expands reports whether this entity produces child wants rather than
// being downloaded directly.
func (e Entity) Expands() bool {
	return e == EntityArtist
}

// WantState is where a want sits.  There is deliberately no "failed":
// an attempt can fail, a want cannot.  A want that has tried and not
// found anything is still wanted, with attempts and last_error
// recording why it is taking a while.
type WantState string

// Want states.
const (
	// WantStateWanted is the active state: due for another attempt when
	// its backoff elapses.
	WantStateWanted WantState = "wanted"

	// WantStateSatisfied means the library owns it.  How it got there —
	// downloaded here, ripped, bought elsewhere — does not matter.
	WantStateSatisfied WantState = "satisfied"

	// WantStatePaused is the user saying "keep this on the list but
	// stop trying".
	WantStatePaused WantState = "paused"
)

// WantScope applies to artist wants only.
type WantScope string

// Artist want scopes.
const (
	// ScopeFuture takes only releases first published after the artist
	// was added.  Default, because subscribing to an artist should not
	// silently queue their entire back catalogue.
	ScopeFuture WantScope = "future"

	// ScopeAll backfills the whole discography as well.
	ScopeAll WantScope = "all"
)

// Want is one row of the wanted list.
type Want struct {
	ID        int64  `json:"id"`
	MBID      string `json:"mbid"`
	Entity    Entity `json:"entity"`
	LibraryID int64  `json:"libraryId"`

	// Artist and Title are display cache only.  Matching always uses
	// the MBID.
	Artist string `json:"artist"`
	Title  string `json:"title"`

	Scope WantScope `json:"scope"`

	// Secondary includes compilations, live albums and remixes in an
	// artist want's expansion.
	Secondary bool `json:"secondary"`

	State WantState `json:"state"`

	// ParentID is set on wants the reconciler derived from an artist
	// subscription.  A want the user pinned directly has none, so
	// removing the artist leaves it alone.
	ParentID int64 `json:"parentId,omitempty"`

	Attempts    int       `json:"attempts"`
	LastError   string    `json:"lastError,omitempty"`
	LastTriedAt time.Time `json:"lastTriedAt,omitempty"`
	NextTryAt   time.Time `json:"nextTryAt,omitempty"`

	// ExternalIDs maps provider row ID (as a string, because JSON
	// object keys are strings) to that provider's own identifier for
	// this want.  Only set for providers that keep a persistent list of
	// their own.
	ExternalIDs map[string]string `json:"externalIds,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Anchored is always true for a want: it is an MBID by construction.
// The method exists so wants and requests read the same at call sites.
func (w Want) Anchored() bool { return w.MBID != "" }

// Label is the wanted list's one-line description of a want.
func (w Want) Label() string {
	switch {
	case w.Artist != "" && w.Title != "":
		return w.Artist + " — " + w.Title
	case w.Title != "":
		return w.Title
	case w.Artist != "":
		return w.Artist
	default:
		return string(w.Entity) + " " + w.MBID
	}
}

// Retry backoff.  A want that cannot be found is usually one that will
// not be findable for a while — a pre-release, something only ever on
// physical media, an artist no source indexes — so the schedule climbs
// fast and then sits at a weekly poll rather than hammering providers
// with the same fruitless search.
const (
	// wantRetryBase is the delay after the first unsuccessful attempt.
	wantRetryBase = 6 * time.Hour

	// wantRetryMax caps the backoff.  A weekly retry on a list of a few
	// hundred wants is a handful of searches a day, which every
	// provider tolerates.
	wantRetryMax = 7 * 24 * time.Hour

	// wantRetryJitter spreads retries so a list added in one sitting
	// does not come due in one burst.
	wantRetryJitter = 0.2
)

// nextRetry returns when a want with the given attempt count should be
// tried again: exponential from wantRetryBase, capped at wantRetryMax,
// jittered so a batch added together does not stay in lockstep forever.
func nextRetry(now time.Time, attempts int) time.Time {
	if attempts < 1 {
		attempts = 1
	}

	// Cap the exponent before shifting so a long-lived want cannot
	// overflow the duration into something negative.
	const maxExp = 16

	exp := min(attempts-1, maxExp)

	delay := float64(wantRetryBase) * math.Pow(2, float64(exp))
	if delay > float64(wantRetryMax) {
		delay = float64(wantRetryMax)
	}

	jitter := delay * wantRetryJitter * (rand.Float64()*2 - 1) //nolint:gosec // spreading retries, not a secret

	return now.Add(time.Duration(delay + jitter))
}

// wantSource is the request source recorded for reconciler-raised
// requests, so the downloads list can tell them apart from the ones a
// user started by hand.
const wantSource = "wanted"

// ToRequest builds the download request that would satisfy this want.
// Expected is filled by the caller from the catalog, since resolving a
// tracklist is I/O and this is not.
func (w Want) ToRequest(id string) Request {
	req := Request{
		ID:        id,
		LibraryID: w.LibraryID,
		Artist:    w.Artist,
		Album:     w.Title,
		WantID:    w.ID,
		Source:    wantSource,
	}

	switch w.Entity {
	case EntityRelease:
		req.ReleaseMBID = w.MBID
	case EntityReleaseGroup:
		req.ReleaseGroupMBID = w.MBID
	case EntityRecording:
		// A recording has no release anchor, so ranking has only the
		// title to go on and auto-pick stays off.  The MBID is still
		// carried in RecordingMBID so a provider that can use it does.
		req.RecordingMBID = w.MBID
	case EntityArtist:
		// Artist wants expand into children and are never turned into
		// a request directly; this case exists so the switch is
		// exhaustive rather than because it can happen.
	}

	return req
}
