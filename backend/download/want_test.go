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
		{attempts: 1, nominal: wantRetryBase},
		{attempts: 2, nominal: 2 * wantRetryBase},
		{attempts: 3, nominal: 4 * wantRetryBase},
		{attempts: 20, nominal: wantRetryMax},
		{attempts: 500, nominal: wantRetryMax},
	}

	for _, tt := range tests {
		got := nextRetry(now, tt.attempts).Sub(now)

		lo := time.Duration(float64(tt.nominal) * (1 - wantRetryJitter))
		hi := time.Duration(float64(tt.nominal) * (1 + wantRetryJitter))

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

func TestWantsReleaseGroupFilters(t *testing.T) {
	t.Parallel()

	subscribed := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)

	future := Want{Scope: ScopeFuture, CreatedAt: subscribed}
	all := Want{Scope: ScopeAll, CreatedAt: subscribed}
	allSecondary := Want{
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
		artist Want
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
		if got := wantsReleaseGroup(tt.artist, tt.rg); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestWantToRequestAnchors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		entity Entity
		check  func(Request) string
	}{
		{
			entity: EntityReleaseGroup,
			check: func(r Request) string {
				return r.ReleaseGroupMBID
			},
		},
		{
			entity: EntityRelease,
			check:  func(r Request) string { return r.ReleaseMBID },
		},
		{
			entity: EntityRecording,
			check:  func(r Request) string { return r.RecordingMBID },
		},
	}

	for _, tt := range tests {
		w := Want{ID: 7, MBID: "mbid-x", Entity: tt.entity, LibraryID: 1}

		req := w.ToRequest("req-1")

		if got := tt.check(req); got != "mbid-x" {
			t.Errorf("%s: anchor = %q, want mbid-x", tt.entity, got)
		}

		if !req.Anchored() {
			t.Errorf("%s: request is not anchored", tt.entity)
		}

		if req.WantID != 7 {
			t.Errorf("%s: WantID = %d, want 7", tt.entity, req.WantID)
		}

		if req.Source != wantSource {
			t.Errorf("%s: Source = %q, want %q", tt.entity, req.Source, wantSource)
		}
	}
}

// An anchored request with no tracklist has nothing to verify itself
// against, so it must not be auto-picked no matter how good the
// candidate looks.
func TestAutoPickableRequiresTracklist(t *testing.T) {
	t.Parallel()

	req := Request{ReleaseGroupMBID: "rg-1", Artist: "A", Album: "B"}

	ranked := []Candidate{{
		Match:   MatchScore{Overall: 0.99, Anchored: true},
		Quality: QualityScore{Overall: 0.9},
		Score:   0.95,
	}}

	if AutoPickable(req, ranked) {
		t.Error("auto-picked a request with no expected tracklist")
	}

	req.Expected = []ExpectedTrack{{Position: 1, Title: "T"}}

	if !AutoPickable(req, ranked) {
		t.Error("did not auto-pick a well-anchored, well-matched request")
	}
}

// The wanted list's identity is the MBID, so the same one arriving
// twice — in a different case, with whitespace — is one row.
func TestAddWantNormalizesAndDeduplicates(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)
	ctx := context.Background()

	first, err := f.store.AddWant(ctx, Want{
		MBID:      "  ABC-123  ",
		Entity:    EntityReleaseGroup,
		LibraryID: 1,
		Title:     "OK Computer",
	})
	if err != nil {
		t.Fatalf("AddWant: %v", err)
	}

	second, err := f.store.AddWant(ctx, Want{
		MBID:      "abc-123",
		Entity:    EntityReleaseGroup,
		LibraryID: 1,
	})
	if err != nil {
		t.Fatalf("AddWant again: %v", err)
	}

	if first != second {
		t.Errorf("ids %d and %d, want the same row", first, second)
	}

	// Re-adding with no title must not wipe the one we have.
	w, err := f.store.GetWant(ctx, first)
	if err != nil {
		t.Fatalf("GetWant: %v", err)
	}

	if w.Title != "OK Computer" {
		t.Errorf("title = %q, want it preserved", w.Title)
	}

	if w.MBID != "abc-123" {
		t.Errorf("mbid = %q, want normalized", w.MBID)
	}
}

func TestWantStoreLifecycle(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)
	ctx := context.Background()

	id, err := f.store.AddWant(ctx, Want{
		MBID:      "rg-1",
		Entity:    EntityReleaseGroup,
		LibraryID: 1,
		Artist:    "Radiohead",
		Title:     "OK Computer",
	})
	if err != nil {
		t.Fatalf("AddWant: %v", err)
	}

	// A brand new want is due immediately.
	due, err := f.store.ListDueWants(ctx, 10)
	if err != nil {
		t.Fatalf("ListDueWants: %v", err)
	}

	if len(due) != 1 {
		t.Fatalf("got %d due wants, want 1", len(due))
	}

	// Recording an attempt pushes it out of the due set without
	// changing its state: a want that was not found is still wanted.
	if err := f.store.RecordAttempt(ctx, id, 0, "no source has it yet"); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	due, err = f.store.ListDueWants(ctx, 10)
	if err != nil {
		t.Fatalf("ListDueWants after attempt: %v", err)
	}

	if len(due) != 0 {
		t.Errorf("got %d due wants after an attempt, want 0", len(due))
	}

	w, err := f.store.GetWant(ctx, id)
	if err != nil {
		t.Fatalf("GetWant: %v", err)
	}

	if w.State != WantStateWanted {
		t.Errorf("state = %q, want it still wanted", w.State)
	}

	if w.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", w.Attempts)
	}

	if w.LastError == "" {
		t.Error("last error was not recorded")
	}

	if err := f.store.SatisfyWant(ctx, id); err != nil {
		t.Fatalf("SatisfyWant: %v", err)
	}

	w, err = f.store.GetWant(ctx, id)
	if err != nil {
		t.Fatalf("GetWant after satisfy: %v", err)
	}

	if w.State != WantStateSatisfied {
		t.Errorf("state = %q, want satisfied", w.State)
	}
}

// Removing an artist subscription takes the albums it derived with it,
// so a user who unsubscribes does not keep downloading that artist.
func TestDeleteArtistWantCascadesToChildren(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)
	ctx := context.Background()

	artist, err := f.store.AddWant(ctx, Want{
		MBID:      "artist-1",
		Entity:    EntityArtist,
		LibraryID: 1,
		Artist:    "Radiohead",
	})
	if err != nil {
		t.Fatalf("AddWant artist: %v", err)
	}

	if _, err := f.store.AddWant(ctx, Want{
		MBID:      "rg-1",
		Entity:    EntityReleaseGroup,
		LibraryID: 1,
		ParentID:  artist,
	}); err != nil {
		t.Fatalf("AddWant child: %v", err)
	}

	children, err := f.store.ListChildWants(ctx, artist)
	if err != nil {
		t.Fatalf("ListChildWants: %v", err)
	}

	if len(children) != 1 {
		t.Fatalf("got %d children, want 1", len(children))
	}

	if err := f.store.DeleteWant(ctx, artist); err != nil {
		t.Fatalf("DeleteWant: %v", err)
	}

	all, err := f.store.ListWants(ctx)
	if err != nil {
		t.Fatalf("ListWants: %v", err)
	}

	if len(all) != 0 {
		t.Errorf("got %d wants after deleting the artist, want 0", len(all))
	}
}
