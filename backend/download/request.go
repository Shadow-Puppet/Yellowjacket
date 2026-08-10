package download

import (
	"math"
	"math/rand/v2"
	"time"
)

// A Request is a persistent "I want this", stored as a MusicBrainz ID
// and almost nothing else.
//
// The distinction from Download is the whole point of this file.  A
// Download is one attempt: it searches, it grabs, it succeeds or fails,
// and then it is history.  A Request outlives every attempt made on its
// behalf.  Nothing being findable today is the normal case for obscure
// music, and the correct response is to try again next week, not to
// show the user a failed row they have to remember to retry.
//
// Because a Request is only an MBID, it stays true when everything
// around it changes: the explore index is rebuilt, a provider is
// swapped out, the release the user originally saw is superseded by a
// remaster.  The display fields are a cache for the list view and are
// never consulted for matching.

// Entity says what a request's MBID names, and is the only type
// distinction the durable request list makes.
type Entity string

// Request entity types.
const (
	// EntityArtist is a subscription rather than a thing to fetch: it
	// is never satisfied, and each reconcile expands the artist's
	// discography into child requests.
	EntityArtist Entity = "artist"

	// EntityReleaseGroup is an album in the abstract — any release of
	// it satisfies the request, which is what a user means by "I want
	// this album".
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

// Expands reports whether this entity produces child requests rather
// than being downloaded directly.
func (e Entity) Expands() bool {
	return e == EntityArtist
}

// RequestState is where a request sits.  There is deliberately no
// "failed": an attempt can fail, a request cannot.  A request that has
// tried and not found anything is still wanted, with attempts and
// last_error recording why it is taking a while.
type RequestState string

// Request states.
const (
	// RequestStateWanted is the active state: due for another attempt
	// when its backoff elapses.
	RequestStateWanted RequestState = "wanted"

	// RequestStateSatisfied means the library owns it.  How it got
	// there — downloaded here, ripped, bought elsewhere — does not
	// matter.
	RequestStateSatisfied RequestState = "satisfied"

	// RequestStatePaused is the user saying "keep this on the list but
	// stop trying".
	RequestStatePaused RequestState = "paused"
)

// RequestScope applies to artist requests only.
type RequestScope string

// Artist request scopes.
const (
	// ScopeFuture takes only releases first published after the artist
	// was added.  Default, because subscribing to an artist should not
	// silently queue their entire back catalogue.
	ScopeFuture RequestScope = "future"

	// ScopeAll backfills the whole discography as well.
	ScopeAll RequestScope = "all"
)

// Request is one row of the durable request list.
type Request struct {
	ID        int64  `json:"id"`
	MBID      string `json:"mbid"`
	Entity    Entity `json:"entity"`
	LibraryID int64  `json:"libraryId"`

	// Artist and Title are display cache only.  Matching always uses
	// the MBID.
	Artist string `json:"artist"`
	Title  string `json:"title"`

	Scope RequestScope `json:"scope"`

	// Secondary includes compilations, live albums and remixes in an
	// artist request's expansion.
	Secondary bool `json:"secondary"`

	State RequestState `json:"state"`

	// ParentID is set on requests the reconciler derived from an artist
	// subscription.  A request the user pinned directly has none, so
	// removing the artist leaves it alone.
	ParentID int64 `json:"parentId,omitempty"`

	Attempts    int       `json:"attempts"`
	LastError   string    `json:"lastError,omitempty"`
	LastTriedAt time.Time `json:"lastTriedAt,omitempty"`
	NextTryAt   time.Time `json:"nextTryAt,omitempty"`

	// ExternalIDs maps provider row ID (as a string, because JSON
	// object keys are strings) to that provider's own identifier for
	// this request.  Only set for providers that keep a persistent
	// list of their own.
	ExternalIDs map[string]string `json:"externalIds,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Anchored is always true for a request: it is an MBID by construction.
// The method exists so requests and downloads read the same at call
// sites.
func (r Request) Anchored() bool { return r.MBID != "" }

// Label is the request list's one-line description of a request.
func (r Request) Label() string {
	switch {
	case r.Artist != "" && r.Title != "":
		return r.Artist + " — " + r.Title
	case r.Title != "":
		return r.Title
	case r.Artist != "":
		return r.Artist
	default:
		return string(r.Entity) + " " + r.MBID
	}
}

// Retry backoff.  A request that cannot be found is usually one that
// will not be findable for a while — a pre-release, something only
// ever on physical media, an artist no source indexes — so the
// schedule climbs fast and then sits at a weekly poll rather than
// hammering providers with the same fruitless search.
const (
	// requestRetryBase is the delay after the first unsuccessful
	// attempt.
	requestRetryBase = 6 * time.Hour

	// requestRetryMax caps the backoff.  A weekly retry on a list of a
	// few hundred requests is a handful of searches a day, which every
	// provider tolerates.
	requestRetryMax = 7 * 24 * time.Hour

	// requestRetryJitter spreads retries so a list added in one sitting
	// does not come due in one burst.
	requestRetryJitter = 0.2
)

// nextRetry returns when a request with the given attempt count should
// be tried again: exponential from requestRetryBase, capped at
// requestRetryMax, jittered so a batch added together does not stay in
// lockstep forever.
func nextRetry(now time.Time, attempts int) time.Time {
	if attempts < 1 {
		attempts = 1
	}

	// Cap the exponent before shifting so a long-lived request cannot
	// overflow the duration into something negative.
	const maxExp = 16

	exp := min(attempts-1, maxExp)

	delay := float64(requestRetryBase) * math.Pow(2, float64(exp))
	if delay > float64(requestRetryMax) {
		delay = float64(requestRetryMax)
	}

	jitter := delay * requestRetryJitter * (rand.Float64()*2 - 1) //nolint:gosec // spreading retries, not a secret

	return now.Add(time.Duration(delay + jitter))
}

// requestSource is the download source recorded for reconciler-raised
// downloads, so the downloads list can tell them apart from the ones a
// user started by hand.
const requestSource = "wanted"

// ToDownload builds the download that would satisfy this request.
// Expected is filled by the caller from the catalog, since resolving a
// tracklist is I/O and this is not.
func (r Request) ToDownload(id string) Download {
	d := Download{
		ID:        id,
		LibraryID: r.LibraryID,
		Artist:    r.Artist,
		Album:     r.Title,
		RequestID: r.ID,
		Source:    requestSource,
	}

	switch r.Entity {
	case EntityRelease:
		d.ReleaseMBID = r.MBID
	case EntityReleaseGroup:
		d.ReleaseGroupMBID = r.MBID
	case EntityRecording:
		// A recording has no release anchor, so ranking has only the
		// title to go on and auto-pick stays off.  The MBID is still
		// carried in RecordingMBID so a provider that can use it does.
		d.RecordingMBID = r.MBID
	case EntityArtist:
		// Artist requests expand into children and are never turned
		// into a download directly; this case exists so the switch is
		// exhaustive rather than because it can happen.
	}

	return d
}
