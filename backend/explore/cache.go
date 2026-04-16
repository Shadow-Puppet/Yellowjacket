package explore

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"yellowjacket/backend/database"
)

// Cache provides a SQLite-backed response cache with TTL expiry.
// Used for short-lived HTTP response caching of search, lookup,
// and popularity API calls.
//
// For long-lived artist metadata (fanart.tv, audiodb, wikidata,
// wikipedia), use ArtistMetadataStore instead — it uses a separate
// table with no TTL and per-source indexing.
//
// All operations use the shared database.DB connection and its
// single-writer constraint (SetMaxOpenConns(1)).
type Cache struct {
	db     *database.DB
	logger *slog.Logger
}

// NewCache returns a cache backed by the given database connection.
func NewCache(db *database.DB, logger *slog.Logger) *Cache {
	return &Cache{db: db, logger: logger}
}

// artistMetadataSources lists cache key prefixes that should be
// redirected to the artist_metadata store (long-lived, keyed by
// mbid+source).  These are enrichment data that changes rarely.
var artistMetadataSources = map[string]bool{ //nolint:gochecknoglobals
	"audiodb":        true,
	"fanart":         true,
	"wikidata-p18":   true,
	"wikipedia-lead": true,
	"mb:artist-rels": true,
}

// isArtistMetadataKey returns true if the given cache key should
// route to artist_metadata instead of http_cache.
func isArtistMetadataKey(key string) (string, string, bool) {
	for prefix := range artistMetadataSources {
		if strings.HasPrefix(key, prefix+":") {
			return prefix, strings.TrimPrefix(key, prefix+":"), true
		}
	}

	return "", "", false
}

// Get returns the cached response for the given URL key if it
// exists and has not expired. Returns (data, true) on a cache hit
// and (nil, false) on a miss or expired entry.
func (c *Cache) Get(key string) ([]byte, bool) {
	// Long-lived artist metadata goes to the dedicated table.
	if source, mbid, ok := isArtistMetadataKey(key); ok {
		return c.getArtistMetadata(source, mbid)
	}

	rows, err := c.db.QueryContext(
		"SELECT response FROM http_cache WHERE url_key = ? AND expires_at > datetime('now')",
		key,
	)
	if err != nil {
		c.logger.Warn("http cache get error",
			"key", key,
			"err", err,
		)

		return nil, false
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return nil, false
	}

	var response string

	if err := rows.Scan(&response); err != nil {
		c.logger.Warn("http cache scan error",
			"key", key,
			"err", err,
		)

		return nil, false
	}

	return []byte(response), true
}

// Set stores a response in the cache with the given TTL.
func (c *Cache) Set(
	key string,
	data []byte,
	ttl time.Duration,
	mbid string,
	entityType string,
) {
	// Long-lived artist metadata goes to the dedicated table (no TTL).
	if source, itemMBID, ok := isArtistMetadataKey(key); ok {
		c.setArtistMetadata(source, itemMBID, data)

		return
	}

	seconds := int(ttl.Seconds())
	if seconds < 1 {
		seconds = 1
	}

	expr := fmt.Sprintf("datetime('now', '+%d seconds')", seconds)

	query := fmt.Sprintf(
		`INSERT OR REPLACE INTO http_cache
		 (url_key, response, entity_mbid, entity_type, expires_at)
		 VALUES (?, ?, ?, ?, %s)`,
		expr,
	)

	if _, err := c.db.ExecContext(query, key, string(data), mbid, entityType); err != nil {
		c.logger.Warn("http cache set error",
			"key", key,
			"err", err,
		)
	}
}

// getArtistMetadata reads a row from the artist_metadata table.
func (c *Cache) getArtistMetadata(source, mbid string) ([]byte, bool) {
	rows, err := c.db.QueryContext(
		"SELECT data FROM artist_metadata WHERE source = ? AND mbid = ?",
		source, mbid,
	)
	if err != nil {
		return nil, false
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return nil, false
	}

	var data []byte
	if err := rows.Scan(&data); err != nil {
		return nil, false
	}

	return data, true
}

// setArtistMetadata writes a row to the artist_metadata table.
func (c *Cache) setArtistMetadata(source, mbid string, data []byte) {
	if _, err := c.db.ExecContext(
		`INSERT OR REPLACE INTO artist_metadata (source, mbid, data, fetched_at)
		 VALUES (?, ?, ?, CURRENT_TIMESTAMP)`,
		source, mbid, data,
	); err != nil {
		c.logger.Warn("artist_metadata set error",
			"source", source,
			"mbid", mbid,
			"err", err,
		)
	}
}

// Evict removes all expired entries from the http_cache.  Does not
// touch artist_metadata (which has no TTL).
func (c *Cache) Evict() {
	result, err := c.db.ExecContext(
		"DELETE FROM http_cache WHERE expires_at < datetime('now')",
	)
	if err != nil {
		c.logger.Warn("http cache evict error", "err", err)

		return
	}

	if n, _ := result.RowsAffected(); n > 0 {
		c.logger.Info("http cache evicted expired entries",
			"count", n,
		)
	}
}
