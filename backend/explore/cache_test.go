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

func TestCacheTTLExpiry(t *testing.T) {
	c := newTestCache(t)

	data := []byte(`{"ephemeral":true}`)
	c.Set("ttl-test-key", data, 1*time.Second, "", "")

	// Verify it's there immediately.
	if _, ok := c.Get("ttl-test-key"); !ok {
		t.Fatal("expected cache hit immediately after set")
	}

	// Wait for expiry.
	time.Sleep(2 * time.Second)

	if _, ok := c.Get("ttl-test-key"); ok {
		t.Error("expected cache miss after TTL expiry, got hit")
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
