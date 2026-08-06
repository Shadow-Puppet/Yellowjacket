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

// The reconciler is the only thing that turns wants into downloads.
//
// It runs on a slow loop rather than reacting to events, because
// everything it cares about changes slowly: a release the user wants
// appears on a source days or weeks after they asked, an artist puts
// out an album once a year, and a library gains files by scan rather
// than by notification.  A loop that wakes a few times a day is
// sufficient for all of it and costs nothing, where an event-driven
// design here would mean subscribing to three subsystems to learn the
// same facts later anyway.
//
// Each pass does four things, in this order and for this reason:
//
//  1. Expand artist subscriptions into per-album wants, so step 2 sees
//     them this pass rather than next.
//  2. Retire wants the library already owns — including ones the user
//     satisfied by other means, which is why ownership is checked
//     rather than assumed from our own downloads.
//  3. Push the list to clients that keep their own (Lidarr), so the
//     user's intent is expressed in both places.
//  4. Attempt a bounded batch of due wants.
//
// Nothing here fails a want.  A want that cannot be found gets an
// attempt recorded and a longer backoff, and stays exactly as wanted as
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
	// should contain.  This is what makes a want's download safe to
	// complete unattended, so a want with no tracklist is never
	// auto-grabbed.
	Tracklist(
		ctx context.Context,
		entity Entity,
		mbid string,
	) ([]ExpectedTrack, error)

	// Owns reports whether the library already has the thing an MBID
	// names.
	Owns(ctx context.Context, entity Entity, mbid string) (bool, error)

	// Describe fills in display text for a want the user added by MBID
	// alone.  Best-effort: an unknown MBID returns false and the want
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
	// friends.  Their presence is what an artist want's default scope
	// filters out.
	SecondaryTypes []string

	// FirstReleaseDate is a MusicBrainz partial date: "1997",
	// "1997-04", or "1997-04-22".
	FirstReleaseDate string

	InLibrary bool
}

// Reconciler defaults.
const (
	// defaultReconcileInterval is how often the wanted list is worked.
	// Four times a day is far more often than new music appears and far
	// less often than any provider would object to.
	defaultReconcileInterval = 6 * time.Hour

	// startupDelay lets the app finish starting — library scan, explore
	// index, provider construction — before the first pass.  A wanted
	// list worked against an index that has not loaded yet would record
	// a pile of pointless attempts.
	startupDelay = 3 * time.Minute

	// maxExpandPerArtist bounds how many child wants one artist
	// subscription creates in a single pass, so switching an artist to
	// full-discography scope does not enqueue four hundred albums at
	// once.  The remainder is picked up next pass.
	maxExpandPerArtist = 40
)

// Reconciler works the wanted list.
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
	// the same want twice.
	runMu sync.Mutex
}

// NewReconciler builds a reconciler.  catalog may be nil, in which case
// the wanted list still stores and lists wants but never acts on them —
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

// SetBatch overrides how many wants one pass attempts.
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
// caller.  Used when the user adds a want and expects something to
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
			r.logger.Warn("wanted list reconcile failed", "error", err)
		}
	}
}

// Summary reports what a pass did, for logging and for the UI.
type Summary struct {
	// Expanded is how many child wants artist subscriptions produced.
	Expanded int `json:"expanded"`

	// Satisfied is how many wants the library turned out to own.
	Satisfied int `json:"satisfied"`

	// Attempted is how many wants were searched for.
	Attempted int `json:"attempted"`

	// Started is how many of those found a clear enough winner to
	// download unattended.
	Started int `json:"started"`

	// Synced is how many wants were pushed to an external list.
	Synced int `json:"synced"`
}

// changed reports whether the pass altered anything worth refreshing
// the UI for.
func (s Summary) changed() bool {
	return s.Expanded > 0 || s.Satisfied > 0 || s.Started > 0
}

// RunOnce works the wanted list once.  It is safe to call directly, and
// the "search now" button does.
func (r *Reconciler) RunOnce(ctx context.Context) (Summary, error) {
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

	attempted, started, err := r.attemptDue(ctx)
	if err != nil {
		return summary, err
	}

	summary.Attempted = attempted
	summary.Started = started

	r.logger.Info(
		"reconciled wanted list",
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

// expandArtists turns artist subscriptions into per-album wants.
//
// The expansion is idempotent: child wants are upserted on (mbid,
// library), so re-running adds only what is genuinely new.  That is
// what makes an artist want a standing subscription rather than a
// one-time queue-filling operation — an album released next year gets
// picked up by the same code path that ran today.
func (r *Reconciler) expandArtists(ctx context.Context) (int, error) {
	artists, err := r.store.ListArtistWants(ctx)
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
				"could not expand artist want",
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
func (r *Reconciler) expandArtist(ctx context.Context, artist Want) (int, error) {
	groups, err := r.catalog.ReleaseGroupsForArtist(ctx, artist.MBID)
	if err != nil {
		return 0, err
	}

	created := 0

	for _, rg := range groups {
		if created >= maxExpandPerArtist {
			break
		}

		if !wantsReleaseGroup(artist, rg) {
			continue
		}

		// Existence is checked before inserting rather than relying on
		// the upsert, because "how many albums are new this pass" is
		// the number the UI reports and an upsert cannot tell an insert
		// from a no-op.  It also means a want the user pinned by hand
		// is never quietly reparented under the artist.
		if _, exists, err := r.store.FindWant(
			ctx, rg.MBID, artist.LibraryID,
		); err != nil || exists {
			continue
		}

		credit := rg.Artist
		if credit == "" {
			credit = artist.Artist
		}

		if _, err := r.store.AddWant(ctx, Want{
			MBID:      rg.MBID,
			Entity:    EntityReleaseGroup,
			LibraryID: artist.LibraryID,
			Artist:    credit,
			Title:     rg.Title,
			ParentID:  artist.ID,
		}); err != nil {
			r.logger.Warn(
				"could not add derived want",
				"release_group", rg.MBID,
				"error", err,
			)

			continue
		}

		created++
	}

	return created, nil
}

// wantsReleaseGroup applies an artist subscription's filters to one
// release group.
func wantsReleaseGroup(artist Want, rg CatalogItem) bool {
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

// retireOwned satisfies wants the library turns out to own.
//
// Ownership is asked of the library rather than inferred from our own
// completed downloads on purpose: the user may have bought the album,
// ripped their CD, or copied it in from another machine, and a wanted
// list that keeps hunting for music already sitting on disk is worse
// than no wanted list at all.
func (r *Reconciler) retireOwned(ctx context.Context) (int, error) {
	wants, err := r.store.ListWants(ctx)
	if err != nil {
		return 0, err
	}

	satisfied := 0

	for _, w := range wants {
		if w.State != WantStateWanted || w.Entity.Expands() {
			continue
		}

		owned, err := r.catalog.Owns(ctx, w.Entity, w.MBID)
		if err != nil {
			r.logger.Debug(
				"ownership check failed", "want", w.MBID, "error", err,
			)

			continue
		}

		if !owned {
			continue
		}

		if err := r.store.SatisfyWant(ctx, w.ID); err != nil {
			r.logger.Warn("could not satisfy want", "want", w.ID, "error", err)

			continue
		}

		satisfied++
	}

	return satisfied, nil
}

// ---------------------------------------------------------------------------
// Attempting downloads
// ---------------------------------------------------------------------------

// attemptDue searches for a bounded batch of due wants and grabs the
// ones with a clear winner.
func (r *Reconciler) attemptDue(ctx context.Context) (attempted, started int, err error) {
	due, err := r.store.ListDueWants(ctx, r.batch)
	if err != nil {
		return 0, 0, err
	}

	for _, w := range due {
		select {
		case <-ctx.Done():
			return attempted, started, nil
		default:
		}

		attempted++

		ok, reason := r.attempt(ctx, w)
		if ok {
			started++

			continue
		}

		if err := r.store.RecordAttempt(
			ctx, w.ID, w.Attempts, reason,
		); err != nil {
			r.logger.Warn(
				"could not record want attempt", "want", w.ID, "error", err,
			)
		}
	}

	return attempted, started, nil
}

// tracklistFor resolves what a want should contain, which is the
// evidence an unattended download is checked against.
//
// A track want is its own tracklist: one entry, built from the title
// the want already carries.  That single expected title is what lets
// filename matching score a track download at all — without it a
// request for one song would be scored as an album with no tracks and
// could never clear the auto-pick bar.
func (r *Reconciler) tracklistFor(
	ctx context.Context,
	w Want,
) ([]ExpectedTrack, error) {
	if w.Entity != EntityRecording {
		return r.catalog.Tracklist(ctx, w.Entity, w.MBID)
	}

	if w.Title == "" {
		return nil, nil
	}

	return []ExpectedTrack{{
		Position: 1,
		Title:    w.Title,
		Artist:   w.Artist,
	}}, nil
}

// attempt tries one want.  It returns false with a human-readable
// reason rather than an error, because none of the ways this does not
// work out are failures: no providers configured yet, nothing on any
// source, or nothing good enough to take without asking are all just
// "not today".
func (r *Reconciler) attempt(ctx context.Context, w Want) (bool, string) {
	expected, err := r.tracklistFor(ctx, w)
	if err != nil || len(expected) == 0 {
		// Without a tracklist an unattended grab has nothing to verify
		// itself against, so this want waits rather than guessing.  The
		// tracklist usually arrives on its own once the explore index
		// fetches the release.
		return false, "waiting for the tracklist to resolve"
	}

	req := w.ToRequest(newID())
	req.Expected = expected

	if req.Artist == "" || req.Album == "" {
		if item, ok := r.catalog.Describe(ctx, w.Entity, w.MBID); ok {
			if req.Artist == "" {
				req.Artist = item.Artist
			}

			if req.Album == "" {
				req.Album = item.Title
			}
		}
	}

	started, reason, err := r.manager.Attempt(ctx, req)
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

// ---------------------------------------------------------------------------
// External list sync
// ---------------------------------------------------------------------------

// syncExternalLists pushes wants to providers that keep a persistent
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

	wants, err := r.store.ListWants(ctx)
	if err != nil {
		r.logger.Warn("could not list wants for sync", "error", err)

		return 0
	}

	synced := 0

	for _, w := range wants {
		if w.State != WantStateWanted {
			continue
		}

		external := w.ExternalIDs
		if external == nil {
			external = map[string]string{}
		}

		changed := false

		for id, l := range listers {
			key := strconv.FormatInt(id, 10)
			if _, done := external[key]; done {
				continue
			}

			externalID, err := l.PushWant(ctx, w)
			if err != nil {
				r.logger.Debug(
					"could not push want to external list",
					"want", w.MBID,
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

		if err := r.store.SetWantExternalIDs(ctx, w.ID, external); err != nil {
			r.logger.Warn(
				"could not record external want ids", "want", w.ID, "error", err,
			)
		}
	}

	return synced
}

// ImportExternal pulls an external manager's own list into the wanted
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

	external, err := l.ListWants(ctx)
	if err != nil {
		return 0, fmt.Errorf("list external wants: %w", err)
	}

	imported := 0

	for _, w := range external {
		w.LibraryID = libraryID

		if _, err := r.store.AddWant(ctx, w); err != nil {
			r.logger.Warn(
				"could not import external want", "mbid", w.MBID, "error", err,
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
