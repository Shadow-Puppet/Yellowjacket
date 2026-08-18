package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The reconciler is the only thing that turns requests into downloads.
//
// It runs on a slow loop rather than reacting to events, because
// everything it cares about changes slowly: a release the user requests
// appears on a source days or weeks after they asked, an artist puts
// out an album once a year, and a library gains files by scan rather
// than by notification.  A loop that wakes a few times a day is
// sufficient for all of it and costs nothing, where an event-driven
// design here would mean subscribing to three subsystems to learn the
// same facts later anyway.
//
// Each pass does four things, in this order and for this reason:
//
//  1. Expand artist subscriptions into per-album requests, so step 2 sees
//     them this pass rather than next.
//  2. Retire requests the library already owns — including ones the user
//     satisfied by other means, which is why ownership is checked
//     rather than assumed from our own downloads.
//  3. Push the list to clients that keep their own (Lidarr), so the
//     user's intent is expressed in both places.
//  4. Attempt a bounded batch of due requests.
//
// Nothing here fails a request.  A request that cannot be found gets an
// attempt recorded and a longer backoff, and stays exactly as requested as
// it was.

// CatalogPort is what the reconciler needs to know about the world of
// music, kept narrow so the download package does not depend on the
// explore package (and so tests can answer these four questions from a
// map).
type CatalogPort interface {
	// ReleaseGroupsForArtist returns an artist's discography.  An empty
	// result is not an error: the explore index fetches discographies
	// lazily, so the honest answer is often "not yet".
	ReleaseGroupsForArtist(
		ctx context.Context,
		artistMBID string,
	) ([]CatalogItem, error)

	// Tracklist resolves a release group or release to the tracks it
	// should contain.  This is what makes a request's download safe to
	// complete unattended, so a request with no tracklist is never
	// auto-grabbed.
	Tracklist(
		ctx context.Context,
		entity Entity,
		mbid string,
	) ([]ExpectedTrack, error)

	// Owns reports whether the library already has the thing an MBID
	// names.
	Owns(ctx context.Context, entity Entity, mbid string) (bool, error)

	// Describe fills in display text for a request the user added by MBID
	// alone.  Best-effort: an unknown MBID returns false and the request
	// is still perfectly valid.
	Describe(
		ctx context.Context,
		entity Entity,
		mbid string,
	) (CatalogItem, bool)
}

// CatalogItem is one thing the catalog knows about, in the download
// package's own terms.
type CatalogItem struct {
	MBID       string
	Title      string
	Artist     string
	ArtistMBID string

	// PrimaryType is the MusicBrainz release-group type ("Album",
	// "Single", "EP").
	PrimaryType string

	// SecondaryTypes carries "Compilation", "Live", "Remix" and
	// friends.  Their presence is what an artist request's default scope
	// filters out.
	SecondaryTypes []string

	// FirstReleaseDate is a MusicBrainz partial date: "1997",
	// "1997-04", or "1997-04-22".
	FirstReleaseDate string

	InLibrary bool
}

// Reconciler defaults.
const (
	// defaultReconcileInterval is how often the request list is worked.
	// Four times a day is far more often than new music appears and far
	// less often than any provider would object to.
	defaultReconcileInterval = 6 * time.Hour

	// startupDelay lets the app finish starting — library scan, explore
	// index, provider construction — before the first pass.  A requested
	// list worked against an index that has not loaded yet would record
	// a pile of pointless attempts.
	startupDelay = 3 * time.Minute

	// maxExpandPerArtist bounds how many child requests one artist
	// subscription creates in a single pass, so switching an artist to
	// full-discography scope does not enqueue four hundred albums at
	// once.  The remainder is picked up next pass.
	maxExpandPerArtist = 40
)

// Reconciler works the request list.
type Reconciler struct {
	logger  *slog.Logger
	store   *Store
	manager *Manager
	catalog CatalogPort

	interval time.Duration
	batch    int

	// now is injectable so tests can drive backoff without waiting.
	now func() time.Time

	// trigger is a nudge for an out-of-band pass, buffered to one
	// because more than one pending "run now" is the same as one.
	trigger chan struct{}

	// onChange fires after any pass that altered the list, so the UI
	// can refresh without polling.
	onChange func()

	stopOnce sync.Once
	stop     chan struct{}

	// runMu serializes passes: two reconcilers racing would search for
	// the same request twice.
	runMu sync.Mutex
}

// NewReconciler builds a reconciler.  catalog may be nil, in which case
// the request list still stores and lists requests but never acts on them —
// which is the right behaviour when the explore index is unavailable.
func NewReconciler(
	logger *slog.Logger,
	store *Store,
	manager *Manager,
	catalog CatalogPort,
) *Reconciler {
	return &Reconciler{
		logger:   logger,
		store:    store,
		manager:  manager,
		catalog:  catalog,
		interval: defaultReconcileInterval,
		batch:    defaultDueBatch,
		now:      time.Now,
		trigger:  make(chan struct{}, 1),
		stop:     make(chan struct{}),
	}
}

// SetInterval overrides the pass interval.
func (r *Reconciler) SetInterval(d time.Duration) {
	if d > 0 {
		r.interval = d
	}
}

// SetBatch overrides how many requests one pass attempts.
func (r *Reconciler) SetBatch(n int) {
	if n > 0 {
		r.batch = n
	}
}

// SetOnChange registers a callback fired after a pass that changed the
// list.
func (r *Reconciler) SetOnChange(fn func()) {
	r.onChange = fn
}

// Start runs the reconcile loop until ctx is done or Stop is called.
func (r *Reconciler) Start(ctx context.Context) {
	go r.loop(ctx)
}

// Stop ends the loop.
func (r *Reconciler) Stop() {
	r.stopOnce.Do(func() { close(r.stop) })
}

// Trigger asks for a pass as soon as possible without blocking the
// caller.  Used when the user adds a request and expects something to
// happen.
func (r *Reconciler) Trigger() {
	select {
	case r.trigger <- struct{}{}:
	default:
	}
}

// loop is the reconcile timer.
func (r *Reconciler) loop(ctx context.Context) {
	first := time.NewTimer(startupDelay)
	defer first.Stop()

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stop:
			return
		case <-first.C:
		case <-ticker.C:
		case <-r.trigger:
		}

		if _, err := r.RunOnce(ctx); err != nil {
			r.logger.Warn("request list reconcile failed", "error", err)
		}
	}
}

// Summary reports what a pass did, for logging and for the UI.
type Summary struct {
	// Expanded is how many child requests artist subscriptions produced.
	Expanded int `json:"expanded"`

	// Satisfied is how many requests the library turned out to own.
	Satisfied int `json:"satisfied"`

	// Attempted is how many requests were searched for.
	Attempted int `json:"attempted"`

	// Started is how many of those found a clear enough winner to
	// download unattended.
	Started int `json:"started"`

	// Synced is how many requests were pushed to an external list.
	Synced int `json:"synced"`

	// Waiting is how many requests are on the list and still being
	// looked for.  A pass that did nothing is the normal case, and the
	// UI can only say so honestly if it knows the list was not empty.
	Waiting int `json:"waiting"`

	// NoProviders reports that nothing could be searched because no
	// download client is enabled — the one "nothing happened" the user
	// can actually fix.
	NoProviders bool `json:"noProviders"`
}

// changed reports whether the pass altered anything worth refreshing
// the UI for.
func (s Summary) changed() bool {
	return s.Expanded > 0 || s.Satisfied > 0 || s.Started > 0
}

// RunOnce works the request list once, honouring each request's
// backoff.  This is what the loop calls.
func (r *Reconciler) RunOnce(ctx context.Context) (Summary, error) {
	return r.run(ctx, false)
}

// RunNow works the request list ignoring backoff.  This is what the
// "check now" button calls: a scheduled retry is a promise to the
// provider, not to the user, and a person who presses a button expects
// their list to actually be searched rather than to be told it is not
// due yet.
func (r *Reconciler) RunNow(ctx context.Context) (Summary, error) {
	return r.run(ctx, true)
}

func (r *Reconciler) run(ctx context.Context, force bool) (Summary, error) {
	r.runMu.Lock()
	defer r.runMu.Unlock()

	var summary Summary

	if r.catalog == nil {
		return summary, nil
	}

	expanded, err := r.expandArtists(ctx)
	if err != nil {
		return summary, err
	}

	summary.Expanded = expanded

	satisfied, err := r.retireOwned(ctx)
	if err != nil {
		return summary, err
	}

	summary.Satisfied = satisfied

	summary.Synced = r.syncExternalLists(ctx)

	// Nothing is searched for when there is nothing to search with, and
	// the point is what that *does not* do to the list.
	//
	// Attempting anyway is not merely wasted work: every request comes
	// back "no download clients are enabled", which RecordAttempt writes
	// down as an attempt and schedules a retry for -- so a user who has
	// deliberately built a wanted list with no client watched their
	// requests accrue failures and announce "next check in 6 hours"
	// about a check that cannot happen. Wanting something without a way
	// to fetch it is a supported thing to do; being told it is being
	// looked for is a lie.
	//
	// Everything above this line still runs: an artist subscription
	// still expands, and a request the user satisfied by some other
	// route -- ripped, bought, copied in -- is still retired, because
	// neither needs a provider.
	summary.NoProviders = len(r.manager.enabledProviders()) == 0

	if !summary.NoProviders {
		attempted, started, err := r.attemptDue(ctx, force)
		if err != nil {
			return summary, err
		}

		summary.Attempted = attempted
		summary.Started = started
	}

	summary.Waiting = r.countWaiting(ctx)

	r.logger.Info(
		"reconciled request list",
		"expanded", summary.Expanded,
		"satisfied", summary.Satisfied,
		"attempted", summary.Attempted,
		"started", summary.Started,
		"synced", summary.Synced,
	)

	if summary.changed() && r.onChange != nil {
		r.onChange()
	}

	return summary, nil
}

// ---------------------------------------------------------------------------
// Artist expansion
// ---------------------------------------------------------------------------

// expandArtists turns artist subscriptions into per-album requests.
//
// The expansion is idempotent: child requests are upserted on (mbid,
// library), so re-running adds only what is genuinely new.  That is
// what makes an artist request a standing subscription rather than a
// one-time queue-filling operation — an album released next year gets
// picked up by the same code path that ran today.
func (r *Reconciler) expandArtists(ctx context.Context) (int, error) {
	artists, err := r.store.ListActiveRequests(ctx)
	if err != nil {
		return 0, err
	}

	created := 0

	for _, artist := range artists {
		n, err := r.expandArtist(ctx, artist)
		if err != nil {
			// One artist whose discography will not resolve must not
			// stop the rest of the list.
			r.logger.Warn(
				"could not expand artist request",
				"artist", artist.Label(),
				"mbid", artist.MBID,
				"error", err,
			)

			continue
		}

		created += n
	}

	return created, nil
}

// expandArtist expands one subscription.
func (r *Reconciler) expandArtist(ctx context.Context, artist Request) (int, error) {
	groups, err := r.catalog.ReleaseGroupsForArtist(ctx, artist.MBID)
	if err != nil {
		return 0, err
	}

	created := 0

	for _, rg := range groups {
		if created >= maxExpandPerArtist {
			break
		}

		if !requestsReleaseGroup(artist, rg) {
			continue
		}

		// Existence is checked before inserting rather than relying on
		// the upsert, because "how many albums are new this pass" is
		// the number the UI reports and an upsert cannot tell an insert
		// from a no-op.  It also means a request the user pinned by hand
		// is never quietly reparented under the artist.
		if _, exists, err := r.store.FindRequest(
			ctx, rg.MBID, artist.LibraryID,
		); err != nil || exists {
			continue
		}

		credit := rg.Artist
		if credit == "" {
			credit = artist.Artist
		}

		if _, err := r.store.AddRequest(ctx, Request{
			MBID:      rg.MBID,
			Entity:    EntityReleaseGroup,
			LibraryID: artist.LibraryID,
			Artist:    credit,
			Title:     rg.Title,
			ParentID:  artist.ID,
		}); err != nil {
			r.logger.Warn(
				"could not add derived request",
				"release_group", rg.MBID,
				"error", err,
			)

			continue
		}

		created++
	}

	return created, nil
}

// requestsReleaseGroup applies an artist subscription's filters to one
// release group.
func requestsReleaseGroup(artist Request, rg CatalogItem) bool {
	if rg.MBID == "" || rg.InLibrary {
		return false
	}

	if !artist.Secondary && len(rg.SecondaryTypes) > 0 {
		return false
	}

	if artist.Scope == ScopeAll {
		return true
	}

	// ScopeFuture: only releases the artist put out after the user
	// subscribed.  A partial MusicBrainz date is compared as a string,
	// which sorts correctly for ISO dates and treats a bare year as the
	// first of January — the conservative reading, since a release
	// dated only "2026" against a subscription made in March 2026
	// should not be assumed to be new.
	return releaseDateAfter(rg.FirstReleaseDate, artist.CreatedAt)
}

// releaseDateAfter compares a MusicBrainz partial date against a
// timestamp.  An unknown or unparseable date is treated as not-after,
// because a release with no date is almost always an old one.
func releaseDateAfter(date string, since time.Time) bool {
	date = strings.TrimSpace(date)
	if date == "" {
		return false
	}

	// Pad a partial date to a full one so string comparison works:
	// "1997" becomes "1997-01-01", "1997-04" becomes "1997-04-01".
	switch len(date) {
	case 4:
		date += "-01-01"
	case 7: //nolint:mnd // length of "YYYY-MM"
		date += "-01"
	}

	return date > since.Format(time.DateOnly)
}

// ---------------------------------------------------------------------------
// Retiring what the library already has
// ---------------------------------------------------------------------------

// retireOwned satisfies requests the library turns out to own.
//
// Ownership is asked of the library rather than inferred from our own
// completed downloads on purpose: the user may have bought the album,
// ripped their CD, or copied it in from another machine, and a requested
// list that keeps hunting for music already sitting on disk is worse
// than no request list at all.
func (r *Reconciler) retireOwned(ctx context.Context) (int, error) {
	requests, err := r.store.ListRequests(ctx)
	if err != nil {
		return 0, err
	}

	satisfied := 0

	for _, req := range requests {
		if req.State != RequestStateWanted || req.Entity.Expands() {
			continue
		}

		owned, err := r.catalog.Owns(ctx, req.Entity, req.MBID)
		if err != nil {
			r.logger.Debug(
				"ownership check failed", "request", req.MBID, "error", err,
			)

			continue
		}

		if !owned {
			continue
		}

		if err := r.store.SatisfyRequest(ctx, req.ID); err != nil {
			r.logger.Warn("could not satisfy request", "request", req.ID, "error", err)

			continue
		}

		satisfied++
	}

	return satisfied, nil
}

// ---------------------------------------------------------------------------
// Attempting downloads
// ---------------------------------------------------------------------------

// attemptDue searches for a bounded batch of requests and grabs the
// ones with a clear winner.  force takes requests whose backoff has not
// elapsed as well.
func (r *Reconciler) attemptDue(
	ctx context.Context,
	force bool,
) (attempted, started int, err error) {
	list := r.store.ListDueRequests
	if force {
		list = r.store.ListWantedRequests
	}

	due, err := list(ctx, r.batch)
	if err != nil {
		return 0, 0, err
	}

	for _, req := range due {
		select {
		case <-ctx.Done():
			return attempted, started, nil
		default:
		}

		attempted++

		ok, reason := r.attempt(ctx, req)
		if ok {
			started++

			continue
		}

		if err := r.store.RecordAttempt(
			ctx, req.ID, req.Attempts, reason,
		); err != nil {
			r.logger.Warn(
				"could not record request attempt", "request", req.ID, "error", err,
			)
		}
	}

	return attempted, started, nil
}

// tracklistFor resolves what a request should contain, which is the
// evidence an unattended download is checked against.
//
// A track request is its own tracklist: one entry, built from the title
// the request already carries.  That single expected title is what lets
// filename matching score a track download at all — without it a
// request for one song would be scored as an album with no tracks and
// could never clear the auto-pick bar.
func (r *Reconciler) tracklistFor(
	ctx context.Context,
	req Request,
) ([]ExpectedTrack, error) {
	if req.Entity != EntityRecording {
		return r.catalog.Tracklist(ctx, req.Entity, req.MBID)
	}

	if req.Title == "" {
		return nil, nil
	}

	return []ExpectedTrack{{
		Position: 1,
		Title:    req.Title,
		Artist:   req.Artist,
	}}, nil
}

// attempt tries one request.  It returns false with a human-readable
// reason rather than an error, because none of the ways this does not
// work out are failures: no providers configured yet, nothing on any
// source, or nothing good enough to take without asking are all just
// "not today".
func (r *Reconciler) attempt(ctx context.Context, req Request) (bool, string) {
	expected, err := r.tracklistFor(ctx, req)
	if err != nil || len(expected) == 0 {
		// Without a tracklist an unattended grab has nothing to verify
		// itself against, so this request waits rather than guessing.  The
		// tracklist usually arrives on its own once the explore index
		// fetches the release.
		return false, "waiting for the tracklist to resolve"
	}

	dl := req.ToDownload(newID())
	dl.Expected = expected

	if dl.Artist == "" || dl.Album == "" {
		if item, ok := r.catalog.Describe(ctx, req.Entity, req.MBID); ok {
			if dl.Artist == "" {
				dl.Artist = item.Artist
			}

			if dl.Album == "" {
				dl.Album = item.Title
			}
		}
	}

	started, reason, err := r.manager.Attempt(ctx, dl)
	if err != nil {
		if errors.Is(err, ErrNoProviders) {
			return false, "no download clients are enabled"
		}

		if errors.Is(err, ErrNoCandidates) {
			return false, "no source has it yet"
		}

		return false, err.Error()
	}

	return started, reason
}

// countWaiting reports how many non-artist requests are still being
// looked for, so "nothing happened" can be reported as "nothing new
// for the twelve things on your list" rather than as silence.
func (r *Reconciler) countWaiting(ctx context.Context) int {
	requests, err := r.store.ListRequests(ctx)
	if err != nil {
		return 0
	}

	waiting := 0

	for _, req := range requests {
		if req.State == RequestStateWanted && !req.Entity.Expands() {
			waiting++
		}
	}

	return waiting
}

// ---------------------------------------------------------------------------
// External list sync
// ---------------------------------------------------------------------------

// syncExternalLists pushes requests to providers that keep a persistent
// list of their own.
//
// The sync is one-directional by design.  Two systems that both accept
// edits to the same list need conflict resolution, and the honest
// version of that here is "whichever the user touched last", which is
// not something we can observe.  So this app's list is the source of
// truth and the external one is a projection of it — with the single
// exception of ImportExternal below, which the user runs deliberately.
func (r *Reconciler) syncExternalLists(ctx context.Context) int {
	listers := r.manager.listers()
	if len(listers) == 0 {
		return 0
	}

	requests, err := r.store.ListRequests(ctx)
	if err != nil {
		r.logger.Warn("could not list requests for sync", "error", err)

		return 0
	}

	synced := 0

	for _, req := range requests {
		if req.State != RequestStateWanted {
			continue
		}

		external := req.ExternalIDs
		if external == nil {
			external = map[string]string{}
		}

		changed := false

		for id, l := range listers {
			key := strconv.FormatInt(id, 10)
			if _, done := external[key]; done {
				continue
			}

			externalID, err := l.PushRequest(ctx, req)
			if err != nil {
				r.logger.Debug(
					"could not push request to external list",
					"request", req.MBID,
					"provider", id,
					"error", err,
				)

				continue
			}

			if externalID == "" {
				continue
			}

			external[key] = externalID
			changed = true
			synced++
		}

		if !changed {
			continue
		}

		if err := r.store.SetRequestExternalIDs(ctx, req.ID, external); err != nil {
			r.logger.Warn(
				"could not record external request ids", "request", req.ID, "error", err,
			)
		}
	}

	return synced
}

// ImportExternal pulls an external manager's own list into the requested
// list.  This is the one place data flows the other way, and it is a
// deliberate user action ("import my monitored Lidarr artists") rather
// than part of the loop, because silently adopting whatever another
// system is monitoring is not something to do behind the user's back.
func (r *Reconciler) ImportExternal(
	ctx context.Context,
	providerID int64,
	libraryID int64,
) (int, error) {
	listers := r.manager.listers()

	l, ok := listers[providerID]
	if !ok {
		return 0, fmt.Errorf(
			"%w: provider %d keeps no list", ErrUnsupported, providerID,
		)
	}

	external, err := l.ListRequests(ctx)
	if err != nil {
		return 0, fmt.Errorf("list external requests: %w", err)
	}

	imported := 0

	for _, req := range external {
		req.LibraryID = libraryID

		if _, err := r.store.AddRequest(ctx, req); err != nil {
			r.logger.Warn(
				"could not import external request", "mbid", req.MBID, "error", err,
			)

			continue
		}

		imported++
	}

	if imported > 0 && r.onChange != nil {
		r.onChange()
	}

	r.Trigger()

	return imported, nil
}
