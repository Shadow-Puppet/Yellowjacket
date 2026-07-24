package explore

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"yellowjacket/backend/database"
)

// newTestLRCLib builds an LRCLIB client whose HTTP calls hit the given
// test server, sharing a real (in-memory) cache so caching behaviour is
// exercised.
func newTestLRCLib(t *testing.T, handler http.HandlerFunc) (*LRCLibClient, *httptest.Server) {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	db := database.NewTestDB(t)
	cache := NewCache(db, testLogger())

	c := NewLRCLibClient(cache, testLogger())
	// Point the client at the test server instead of the real API by
	// overriding its transport to rewrite the host.
	c.http = srv.Client()
	c.baseURL = srv.URL

	return c, srv
}

func TestLRCLibGetLyrics(t *testing.T) {
	t.Parallel()

	var calls int

	c, _ := newTestLRCLib(t, func(w http.ResponseWriter, r *http.Request) {
		calls++

		if r.URL.Path != "/api/get" {
			http.Error(w, "not found", http.StatusNotFound)

			return
		}

		if r.URL.Query().Get("track_name") == "Missing" {
			http.Error(w, "not found", http.StatusNotFound)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": 42,
			"trackName": "Yesterday",
			"artistName": "The Beatles",
			"albumName": "Help!",
			"duration": 125,
			"instrumental": false,
			"plainLyrics": "Yesterday, all my troubles seemed so far away",
			"syncedLyrics": "[00:00.00] Yesterday"
		}`))
	})

	t.Run("hit returns plain + synced lyrics", func(t *testing.T) {
		lyrics, err := c.GetLyrics(context.Background(), "The Beatles", "Yesterday", "Help!", 125)
		if err != nil {
			t.Fatalf("GetLyrics: %v", err)
		}

		if lyrics.Plain == "" || lyrics.Synced == "" {
			t.Errorf("expected plain and synced lyrics, got %+v", lyrics)
		}

		if lyrics.Instrumental {
			t.Error("expected non-instrumental")
		}
	})

	t.Run("second identical request is served from cache", func(t *testing.T) {
		before := calls

		if _, err := c.GetLyrics(
			context.Background(),
			"The Beatles",
			"Yesterday",
			"Help!",
			125,
		); err != nil {
			t.Fatalf("GetLyrics: %v", err)
		}

		if calls != before {
			t.Errorf("expected cache hit (no new HTTP call), calls went %d → %d", before, calls)
		}
	})

	t.Run("404 maps to ErrLyricsNotFound", func(t *testing.T) {
		_, err := c.GetLyrics(context.Background(), "Nobody", "Missing", "", 0)
		if !errors.Is(err, ErrLyricsNotFound) {
			t.Errorf("expected ErrLyricsNotFound, got %v", err)
		}
	})

	t.Run("missing artist/title short-circuits without a request", func(t *testing.T) {
		before := calls

		if _, err := c.GetLyrics(
			context.Background(),
			"",
			"Yesterday",
			"",
			0,
		); !errors.Is(
			err,
			ErrLyricsNotFound,
		) {
			t.Errorf("expected ErrLyricsNotFound, got %v", err)
		}

		if calls != before {
			t.Error("expected no HTTP call for empty artist")
		}
	})
}
