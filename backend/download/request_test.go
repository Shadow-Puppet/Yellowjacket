package download

import (
	"context"
	"testing"
	"time"
)

func TestNextRetryClimbsAndCaps(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	// The jitter makes exact equality wrong to assert, so each step is
	// checked as a band around its nominal delay.
	tests := []struct {
		attempts int
		nominal  time.Duration
	}{
		{attempts: 1, nominal: requestRetryBase},
		{attempts: 2, nominal: 2 * requestRetryBase},
		{attempts: 3, nominal: 4 * requestRetryBase},
		{attempts: 20, nominal: requestRetryMax},
		{attempts: 500, nominal: requestRetryMax},
	}

	for _, tt := range tests {
		got := nextRetry(now, tt.attempts).Sub(now)

		lo := time.Duration(float64(tt.nominal) * (1 - requestRetryJitter))
		hi := time.Duration(float64(tt.nominal) * (1 + requestRetryJitter))

		if got < lo || got > hi {
			t.Errorf(
				"attempts=%d delay=%v, want within [%v, %v]",
				tt.attempts, got, lo, hi,
			)
		}
	}
}

// A want that has been retried for years must not overflow into a
// negative delay, which would make it due forever.
func TestNextRetryNeverGoesBackwards(t *testing.T) {
	t.Parallel()

	now := time.Now()

	for _, attempts := range []int{0, 1, 64, 1000, 1 << 20} {
		if next := nextRetry(now, attempts); !next.After(now) {
			t.Errorf("attempts=%d scheduled %v, not after now", attempts, next)
		}
	}
}

func TestReleaseDateAfter(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		date string
		want bool
	}{
		{name: "later full date", date: "2026-06-01", want: true},
		{name: "earlier full date", date: "2026-01-01", want: false},
		{name: "same day is not after", date: "2026-03-15", want: false},
		{name: "later year", date: "2027", want: true},
		// A bare year is read as its 1 January, so the year of the
		// subscription itself does not count as new.
		{name: "same year, bare", date: "2026", want: false},
		{name: "later month, bare", date: "2026-08", want: true},
		{name: "earlier month, bare", date: "2026-02", want: false},
		{name: "unknown date is old", date: "", want: false},
	}

	for _, tt := range tests {
		if got := releaseDateAfter(tt.date, since); got != tt.want {
			t.Errorf("%s: releaseDateAfter(%q) = %v, want %v",
				tt.name, tt.date, got, tt.want)
		}
	}
}

func TestRequestsReleaseGroupFilters(t *testing.T) {
	t.Parallel()

	subscribed := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)

	future := Request{Scope: ScopeFuture, CreatedAt: subscribed}
	all := Request{Scope: ScopeAll, CreatedAt: subscribed}
	allSecondary := Request{
		Scope: ScopeAll, Secondary: true, CreatedAt: subscribed,
	}

	newAlbum := CatalogItem{MBID: "a", FirstReleaseDate: "2026-09-01"}
	oldAlbum := CatalogItem{MBID: "b", FirstReleaseDate: "1997-04-22"}
	ownedAlbum := CatalogItem{
		MBID: "c", FirstReleaseDate: "2026-09-01", InLibrary: true,
	}
	liveAlbum := CatalogItem{
		MBID:             "d",
		FirstReleaseDate: "2026-09-01",
		SecondaryTypes:   []string{"Live"},
	}

	tests := []struct {
		name   string
		artist Request
		rg     CatalogItem
		want   bool
	}{
		{name: "future takes new", artist: future, rg: newAlbum, want: true},
		{name: "future skips old", artist: future, rg: oldAlbum, want: false},
		{name: "all takes old", artist: all, rg: oldAlbum, want: true},
		{name: "owned is never wanted", artist: all, rg: ownedAlbum, want: false},
		{name: "secondary off skips live", artist: all, rg: liveAlbum, want: false},
		{
			name:   "secondary on takes live",
			artist: allSecondary,
			rg:     liveAlbum,
			want:   true,
		},
		{
			name:   "no mbid is unusable",
			artist: all,
			rg:     CatalogItem{FirstReleaseDate: "2026-09-01"},
			want:   false,
		},
	}

	for _, tt := range tests {
		if got := requestsReleaseGroup(tt.artist, tt.rg); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestRequestToDownloadAnchors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		entity Entity
		check  func(Download) string
	}{
		{
			entity: EntityReleaseGroup,
			check: func(r Download) string {
				return r.ReleaseGroupMBID
			},
		},
		{
			entity: EntityRelease,
			check:  func(r Download) string { return r.ReleaseMBID },
		},
		{
			entity: EntityRecording,
			check:  func(r Download) string { return r.RecordingMBID },
		},
	}

	for _, tt := range tests {
		req := Request{ID: 7, MBID: "mbid-x", Entity: tt.entity, LibraryID: 1}

		dl := req.ToDownload("dl-1")

		if got := tt.check(dl); got != "mbid-x" {
			t.Errorf("%s: anchor = %q, want mbid-x", tt.entity, got)
		}

		if !dl.Anchored() {
			t.Errorf("%s: download is not anchored", tt.entity)
		}

		if dl.RequestID != 7 {
			t.Errorf("%s: RequestID = %d, want 7", tt.entity, dl.RequestID)
		}

		if dl.Source != requestSource {
			t.Errorf("%s: Source = %q, want %q", tt.entity, dl.Source, requestSource)
		}
	}
}

// An anchored request with no tracklist has nothing to verify itself
// against, so it must not be auto-picked no matter how good the
// candidate looks.
func TestAutoPickableRequiresTracklist(t *testing.T) {
	t.Parallel()

	dl := Download{ReleaseGroupMBID: "rg-1", Artist: "A", Album: "B"}

	ranked := []Candidate{{
		Match:   MatchScore{Overall: 0.99, Anchored: true},
		Quality: QualityScore{Overall: 0.9},
		Score:   0.95,
	}}

	if AutoPickable(dl, ranked, AutoDownloadPrefs{}) {
		t.Error("auto-picked a download with no expected tracklist")
	}

	dl.Expected = []ExpectedTrack{{Position: 1, Title: "T"}}

	if !AutoPickable(dl, ranked, AutoDownloadPrefs{}) {
		t.Error("did not auto-pick a well-anchored, well-matched download")
	}
}

// The wanted list's identity is the MBID, so the same one arriving
// twice — in a different case, with whitespace — is one row.
func TestAddRequestNormalizesAndDeduplicates(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)
	ctx := context.Background()

	first, err := f.store.AddRequest(ctx, Request{
		MBID:      "  ABC-123  ",
		Entity:    EntityReleaseGroup,
		LibraryID: 1,
		Title:     "OK Computer",
	})
	if err != nil {
		t.Fatalf("AddRequest: %v", err)
	}

	second, err := f.store.AddRequest(ctx, Request{
		MBID:      "abc-123",
		Entity:    EntityReleaseGroup,
		LibraryID: 1,
	})
	if err != nil {
		t.Fatalf("AddRequest again: %v", err)
	}

	if first != second {
		t.Errorf("ids %d and %d, want the same row", first, second)
	}

	// Re-adding with no title must not wipe the one we have.
	req, err := f.store.GetRequest(ctx, first)
	if err != nil {
		t.Fatalf("GetRequest: %v", err)
	}

	if req.Title != "OK Computer" {
		t.Errorf("title = %q, want it preserved", req.Title)
	}

	if req.MBID != "abc-123" {
		t.Errorf("mbid = %q, want normalized", req.MBID)
	}
}

func TestRequestStoreLifecycle(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)
	ctx := context.Background()

	id, err := f.store.AddRequest(ctx, Request{
		MBID:      "rg-1",
		Entity:    EntityReleaseGroup,
		LibraryID: 1,
		Artist:    "Radiohead",
		Title:     "OK Computer",
	})
	if err != nil {
		t.Fatalf("AddRequest: %v", err)
	}

	// A brand new want is due immediately.
	due, err := f.store.ListDueRequests(ctx, 10)
	if err != nil {
		t.Fatalf("ListDueRequests: %v", err)
	}

	if len(due) != 1 {
		t.Fatalf("got %d due requests, want 1", len(due))
	}

	// Recording an attempt pushes it out of the due set without
	// changing its state: a want that was not found is still wanted.
	if err := f.store.RecordAttempt(ctx, id, 0, "no source has it yet"); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	due, err = f.store.ListDueRequests(ctx, 10)
	if err != nil {
		t.Fatalf("ListDueRequests after attempt: %v", err)
	}

	if len(due) != 0 {
		t.Errorf("got %d due requests after an attempt, want 0", len(due))
	}

	req, err := f.store.GetRequest(ctx, id)
	if err != nil {
		t.Fatalf("GetRequest: %v", err)
	}

	if req.State != RequestStateWanted {
		t.Errorf("state = %q, want it still wanted", req.State)
	}

	if req.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", req.Attempts)
	}

	if req.LastError == "" {
		t.Error("last error was not recorded")
	}

	if err := f.store.SatisfyRequest(ctx, id); err != nil {
		t.Fatalf("SatisfyRequest: %v", err)
	}

	req, err = f.store.GetRequest(ctx, id)
	if err != nil {
		t.Fatalf("GetRequest after satisfy: %v", err)
	}

	if req.State != RequestStateSatisfied {
		t.Errorf("state = %q, want satisfied", req.State)
	}
}

// Removing an artist subscription takes the albums it derived with it,
// so a user who unsubscribes does not keep downloading that artist.
func TestDeleteArtistRequestCascadesToChildren(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)
	ctx := context.Background()

	artist, err := f.store.AddRequest(ctx, Request{
		MBID:      "artist-1",
		Entity:    EntityArtist,
		LibraryID: 1,
		Artist:    "Radiohead",
	})
	if err != nil {
		t.Fatalf("AddRequest artist: %v", err)
	}

	if _, err := f.store.AddRequest(ctx, Request{
		MBID:      "rg-1",
		Entity:    EntityReleaseGroup,
		LibraryID: 1,
		ParentID:  artist,
	}); err != nil {
		t.Fatalf("AddRequest child: %v", err)
	}

	children, err := f.store.ListChildRequests(ctx, artist)
	if err != nil {
		t.Fatalf("ListChildRequests: %v", err)
	}

	if len(children) != 1 {
		t.Fatalf("got %d children, want 1", len(children))
	}

	if err := f.store.DeleteRequest(ctx, artist); err != nil {
		t.Fatalf("DeleteRequest: %v", err)
	}

	all, err := f.store.ListRequests(ctx)
	if err != nil {
		t.Fatalf("ListRequests: %v", err)
	}

	if len(all) != 0 {
		t.Errorf("got %d requests after deleting the artist, want 0", len(all))
	}
}
