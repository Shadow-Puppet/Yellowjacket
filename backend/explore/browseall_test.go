package explore

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"yellowjacket/backend/database"
)

// browseServer serves a paged release-group browse for an artist with
// `total` release groups, and records how many requests it received.
func browseServer(t *testing.T, total int) (*httptest.Server, *int) {
	t.Helper()

	requests := 0

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			requests++

			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

			if limit <= 0 {
				limit = 25
			}

			end := min(offset+limit, total)

			groups := make([]map[string]any, 0, max(0, end-offset))

			for i := offset; i < end; i++ {
				groups = append(groups, map[string]any{
					"id":               fmt.Sprintf("rg-%03d", i),
					"title":            "Release " + strconv.Itoa(i),
					"primary-type":     "Album",
					"secondary-types":  []string{"Live"},
					"first-release-da": "",
				})
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"release-group-count":  total,
				"release-group-offset": offset,
				"release-groups":       groups,
			})
		},
	))

	t.Cleanup(srv.Close)

	return srv, &requests
}

// browseClient points a real MusicBrainzClient at a test server.
func browseClient(t *testing.T, srv *httptest.Server) *MusicBrainzClient {
	t.Helper()

	db := database.NewTestDB(t)
	// Fast limiter: this test is about paging, not pacing.
	c := NewMusicBrainzClient(
		NewCache(db, slog.Default()), NewRateLimiterN(1000), slog.Default(),
	)
	c.mb.SetBaseURL(strings.TrimSuffix(srv.URL, "/") + "/ws/2/")

	return c
}

// TestBrowseReleaseGroupsAllPages is the bug 011 is built on: the
// single-page browse asks for MaxLimit and takes what comes back, so a
// prolific artist's discography was silently cut at 100 — and a hundred
// albums looks like a complete answer unless you count.
func TestBrowseReleaseGroupsAllPages(t *testing.T) {
	t.Parallel()

	const total = 237

	srv, requests := browseServer(t, total)
	c := browseClient(t, srv)

	all, err := c.BrowseReleaseGroupsAll(t.Context(), "artist-mbid")
	if err != nil {
		t.Fatalf("BrowseReleaseGroupsAll: %v", err)
	}

	if len(all) != total {
		t.Errorf("got %d release groups, want %d", len(all), total)
	}

	// 100 + 100 + 37: the short third page ends it, with no fourth
	// request to discover that it is over.
	if *requests != 3 {
		t.Errorf("made %d requests, want 3", *requests)
	}
}

// TestBrowseReleaseGroupsAllExactMultiple covers the boundary the
// short-page terminator exists for: a total that is an exact multiple
// of the page size needs one more (empty) request to know it is done,
// and must not loop past it.
func TestBrowseReleaseGroupsAllExactMultiple(t *testing.T) {
	t.Parallel()

	srv, requests := browseServer(t, 200)
	c := browseClient(t, srv)

	all, err := c.BrowseReleaseGroupsAll(t.Context(), "artist-mbid")
	if err != nil {
		t.Fatalf("BrowseReleaseGroupsAll: %v", err)
	}

	if len(all) != 200 {
		t.Errorf("got %d release groups, want 200", len(all))
	}

	if *requests != 3 {
		t.Errorf("made %d requests, want 3 (two full pages and an empty one)", *requests)
	}
}

// TestBrowseReleaseGroupsAllCachesForSinglePageReader checks the half
// that makes this worth doing interactively: the complete list is
// written under the key the single-page browse reads, so the next
// ordinary browse is served all of it without a request.
func TestBrowseReleaseGroupsAllCachesForSinglePageReader(t *testing.T) {
	t.Parallel()

	srv, requests := browseServer(t, 150)
	c := browseClient(t, srv)

	if _, err := c.BrowseReleaseGroupsAll(t.Context(), "artist-mbid"); err != nil {
		t.Fatalf("BrowseReleaseGroupsAll: %v", err)
	}

	before := *requests

	cached, err := c.BrowseReleaseGroups(t.Context(), "artist-mbid")
	if err != nil {
		t.Fatalf("BrowseReleaseGroups: %v", err)
	}

	if len(cached) != 150 {
		t.Errorf("cached read got %d release groups, want 150", len(cached))
	}

	if *requests != before {
		t.Errorf("cached read made %d extra requests, want 0", *requests-before)
	}
}
