package explore

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"yellowjacket/backend/database"
)

// lbPopularityServer serves both top-for-artist popularity endpoints
// with a fixed status and body, and counts what it was asked.
func lbPopularityServer(
	t *testing.T, status int, body string,
) (*ListenBrainzClient, *int) {
	t.Helper()

	requests := 0

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			requests++

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		},
	))

	t.Cleanup(srv.Close)

	lb := NewListenBrainzClient(NewRateLimiterN(1000), nil, slog.Default())
	lb.SetBaseURL(srv.URL)

	return lb, &requests
}

// TestTopFetchesSeparateEmptyFromFailed is the distinction the
// owned-artist backfill's mark rests on.  ListenBrainz answers 200 with
// `[]` for an artist it has no popularity data for — which is most of a
// long-tail library — and an empty answer is a *complete* one.  When
// discog_fetched was keyed on "did rows come back", those artists were
// never marked, stayed in unenrichedLibraryArtistMBIDs forever, and
// "Filling in artist details" re-ran for them on every single launch.
func TestTopFetchesSeparateEmptyFromFailed(t *testing.T) {
	t.Parallel()

	si := NewSearchIndex(database.NewTestDB(t), nil, nil, slog.Default())
	artist := lbSitewideArtist{
		ArtistMBID: "44444444-4444-4444-4444-444444444444",
		ArtistName: "Nobody Has Listened",
	}

	t.Run("empty is success", func(t *testing.T) {
		t.Parallel()

		lb, _ := lbPopularityServer(t, http.StatusOK, `[]`)

		rgs, err := si.fetchTopReleaseGroups(t.Context(), lb, artist, 50)
		if err != nil || len(rgs) != 0 {
			t.Errorf("top RGs: got %d rows, err %v; want 0 rows and no error", len(rgs), err)
		}

		recs, err := si.fetchTopRecordings(t.Context(), lb, artist, 200)
		if err != nil || len(recs) != 0 {
			t.Errorf("top recordings: got %d rows, err %v; want 0 rows and no error",
				len(recs), err,
			)
		}
	})

	// Below indexMinPopularity is the same shape one step in: the
	// endpoint answered, we simply keep none of it.
	t.Run("everything below the popularity floor is success", func(t *testing.T) {
		t.Parallel()

		lb, _ := lbPopularityServer(t, http.StatusOK,
			`[{"release_group_mbid":"rg-1","total_listen_count":3,`+
				`"release_group":{"name":"Obscure"}}]`)

		rgs, err := si.fetchTopReleaseGroups(t.Context(), lb, artist, 50)
		if err != nil || len(rgs) != 0 {
			t.Errorf("top RGs: got %d rows, err %v; want 0 rows and no error", len(rgs), err)
		}
	})

	// A failure must stay a failure, or the retry this is built on goes
	// away and a throttled run marks artists it never fetched.
	t.Run("HTTP error is a failure", func(t *testing.T) {
		t.Parallel()

		lb, _ := lbPopularityServer(t, http.StatusServiceUnavailable, `nope`)

		if _, err := si.fetchTopReleaseGroups(t.Context(), lb, artist, 50); err == nil {
			t.Error("top RGs reported success on a 503")
		}

		if _, err := si.fetchTopRecordings(t.Context(), lb, artist, 200); err == nil {
			t.Error("top recordings reported success on a 503")
		}
	})

	t.Run("unparseable body is a failure", func(t *testing.T) {
		t.Parallel()

		lb, _ := lbPopularityServer(t, http.StatusOK, `{"not":"an array"}`)

		if _, err := si.fetchTopReleaseGroups(t.Context(), lb, artist, 50); err == nil {
			t.Error("top RGs reported success on a body it could not read")
		}
	})
}
