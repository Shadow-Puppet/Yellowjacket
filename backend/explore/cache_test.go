package explore

import (
	"database/sql"
	"log/slog"
	"testing"
	"time"

	"yellowjacket/backend/database"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()

	db := database.NewTestDB(t)

	return NewCache(db, slog.Default())
}

func TestCacheSetGet(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)

	data := []byte(`{"artist":"Radiohead"}`)
	c.Set("https://musicbrainz.org/ws/2/artist?query=radiohead", data, 5*time.Minute, "", "")

	got, ok := c.Get("https://musicbrainz.org/ws/2/artist?query=radiohead")
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}

	if string(got) != string(data) {
		t.Errorf("got %q, want %q", string(got), string(data))
	}
}

func TestCacheMiss(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)

	_, ok := c.Get("https://nonexistent.example.com/api")
	if ok {
		t.Error("expected cache miss, got hit")
	}
}

// TestCacheTTLExpiry checks both halves of the TTL contract, and uses two
// entries to do it.
//
// **No assertion here may depend on an upper bound of elapsed wall-clock
// time**, which is what the single-entry version of this test did: it set
// a 1s TTL and immediately asserted a *hit*, so on a loaded runner — one
// goroutine descheduled for over a second while the rest of the suite
// runs — the entry was correctly gone and the test failed with "expected
// cache hit immediately after set".  It did exactly that in CI while
// passing five times out of five locally.
//
// Sleeping *past* a TTL is always safe, so the expiry half keeps a short
// one; the presence half gets a TTL nothing can outrun.
func TestCacheTTLExpiry(t *testing.T) {
	c := newTestCache(t)

	data := []byte(`{"ephemeral":true}`)
	c.Set("ttl-live-key", data, time.Hour, "", "")
	c.Set("ttl-expiring-key", data, 1*time.Second, "", "")

	if _, ok := c.Get("ttl-live-key"); !ok {
		t.Fatal("expected a cache hit on an entry with an hour to live")
	}

	// Wait for the short one to expire.
	time.Sleep(2 * time.Second)

	if _, ok := c.Get("ttl-expiring-key"); ok {
		t.Error("expected cache miss after TTL expiry, got hit")
	}

	// And the long-lived entry is still there, which is what says the
	// sweep above expired an entry rather than the cache.
	if _, ok := c.Get("ttl-live-key"); !ok {
		t.Error("the hour-long entry expired too")
	}
}

func TestCacheMBID(t *testing.T) {
	t.Parallel()

	// explore_cache was replaced by http_cache + artist_metadata in
	// migration 27; this test queries the old table directly and is
	// obsolete until rewritten against the new schemas.
	t.Skip("explore_cache dropped by migration 27; test is obsolete")

	c := newTestCache(t)

	data := []byte(`{"name":"OK Computer"}`)
	c.Set(
		"mbid-test-key",
		data,
		10*time.Minute,
		"b3b40b1b-3c03-4b8a-8291-8e1f2d09e211",
		"release_group",
	)

	// Query the MBID column directly to verify it was stored.
	db := c.db

	rows, err := db.QueryContext(
		"SELECT mbid, entity_type FROM explore_cache WHERE url_key = ?",
		"mbid-test-key",
	)
	if err != nil {
		t.Fatalf("query explore_cache: %v", err)
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		t.Fatal("explore_cache row not found")
	}

	var (
		mbid       sql.NullString
		entityType sql.NullString
	)

	if err := rows.Scan(&mbid, &entityType); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if !mbid.Valid || mbid.String != "b3b40b1b-3c03-4b8a-8291-8e1f2d09e211" {
		t.Errorf("mbid = %v, want b3b40b1b-3c03-4b8a-8291-8e1f2d09e211", mbid)
	}

	if !entityType.Valid || entityType.String != "release_group" {
		t.Errorf("entity_type = %v, want release_group", entityType)
	}
}

func TestCacheEvict(t *testing.T) {
	// explore_cache was replaced by http_cache + artist_metadata in
	// migration 27; this test queries the old table directly and is
	// obsolete until rewritten against the new schemas.
	t.Skip("explore_cache dropped by migration 27; test is obsolete")

	c := newTestCache(t)

	// Insert an entry that expires in 1 second.
	c.Set("evict-key", []byte(`{}`), 1*time.Second, "", "")

	time.Sleep(2 * time.Second)

	// Evict expired entries.
	c.Evict()

	// Verify the row is gone entirely (not just expired-but-present).
	db := c.db

	rows, err := db.QueryContext(
		"SELECT COUNT(*) FROM explore_cache WHERE url_key = ?",
		"evict-key",
	)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		t.Fatal("no row returned")
	}

	var count int64
	if err := rows.Scan(&count); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 rows after evict, got %d", count)
	}
}
