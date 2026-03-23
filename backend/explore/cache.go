package explore

import (
	"fmt"
	"log/slog"
	"time"

	"yellowjacket/backend/database"
)

// Cache provides a SQLite-backed response cache with TTL expiry.
// It stores raw JSON API responses keyed by URL and supports
// optional MBID columns for future autotagging lookups.
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

// Get returns the cached response for the given URL key if it
// exists and has not expired. Returns (data, true) on a cache hit
// and (nil, false) on a miss or expired entry.
func (c *Cache) Get(key string) ([]byte, bool) {
	rows, err := c.db.QueryContext(
		"SELECT response FROM explore_cache WHERE url_key = ? AND expires_at > datetime('now')",
		key,
	)
	if err != nil {
		c.logger.Warn("explore cache get error",
			"key", key,
			"err", err,
		)

		return nil, false
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		c.logger.Debug("explore cache miss", "key", key)

		return nil, false
	}

	var response string

	if err := rows.Scan(&response); err != nil {
		c.logger.Warn("explore cache scan error",
			"key", key,
			"err", err,
		)

		return nil, false
	}

	c.logger.Debug("explore cache hit", "key", key)

	return []byte(response), true
}

// Set stores a response in the cache with the given TTL.  If mbid
// and entityType are non-empty they are stored for future
// autotagging lookups; otherwise they are stored as NULL.
func (c *Cache) Set(
	key string,
	data []byte,
	ttl time.Duration,
	mbid string,
	entityType string,
) {
	seconds := int(ttl.Seconds())
	if seconds < 1 {
		seconds = 1
	}

	expr := fmt.Sprintf("datetime('now', '+%d seconds')", seconds)

	query := fmt.Sprintf(
		`INSERT OR REPLACE INTO explore_cache
		 (url_key, response, mbid, entity_type, expires_at)
		 VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''), %s)`,
		expr,
	)

	if _, err := c.db.ExecContext(query, key, string(data), mbid, entityType); err != nil {
		c.logger.Warn("explore cache set error",
			"key", key,
			"err", err,
		)
	} else {
		c.logger.Debug("explore cache set",
			"key", key,
			"ttl", ttl,
			"mbid", mbid,
			"entityType", entityType,
		)
	}
}

// Evict removes all expired entries from the cache.
func (c *Cache) Evict() {
	result, err := c.db.ExecContext(
		"DELETE FROM explore_cache WHERE expires_at < datetime('now')",
	)
	if err != nil {
		c.logger.Warn("explore cache evict error", "err", err)

		return
	}

	if n, _ := result.RowsAffected(); n > 0 {
		c.logger.Info("explore cache evicted expired entries",
			"count", n,
		)
	}
}
