//go:build dev

package testctl

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"yellowjacket/backend/events"
	"yellowjacket/backend/system"
)

// handleHealth answers the one question every spec starts with: is the
// backend up, and is it looking at the library it should be?
//
// The frontend can answer parts of this, but only after it has rendered
// — which is exactly the thing under test.  This answers before a
// single component has mounted, so it is usable as a gate.
func handleHealth(d Deps, _ *http.Request) (any, error) {
	out := map[string]any{
		"ok":      true,
		"home":    os.Getenv("YJ_HOME"),
		"dbPath":  dbPath(),
		"pid":     os.Getpid(),
		"context": d.Context() != nil,
	}

	counts := map[string]int64{}

	// The catalog is asked whether it has rows, not how many.  A real
	// one is ~1.1M rows over ~400 MB, and a cold `COUNT(*)` on it is a
	// full scan off disk: **65 seconds** on the first call after a seed
	// is extracted, then 7 ms once the page cache is warm.  That is a
	// health endpoint every spec gates on, so the first spec to run
	// timed out and the rest passed - which reads as one flaky spec.
	//
	// This is the same rule the app itself follows for this table
	// (`GetIndexStatus().TotalRows` is stale and `IsReady()` is set
	// once, so the shelves ask `SELECT 1 ... LIMIT 1`).  Nothing wants
	// the exact number: the only caller asks whether it is > 0.
	for table, query := range map[string]string{
		"tracks":      "SELECT COUNT(*) FROM audio_files",
		"libraries":   "SELECT COUNT(*) FROM libraries",
		"playlists":   "SELECT COUNT(*) FROM playlists",
		"queueTracks": "SELECT COUNT(*) FROM queue_tracks",
		"exploreIndex": "SELECT COUNT(*) FROM " +
			"(SELECT 1 FROM explore_index LIMIT 1)",
	} {
		var n int64
		if err := d.DB.QueryRowWriter(query).Scan(&n); err != nil {
			counts[table] = -1

			continue
		}

		counts[table] = n
	}

	out["counts"] = counts

	libs, err := libraryRows(d)
	if err != nil {
		return nil, err
	}

	out["libraries"] = libs

	return out, nil
}

// libraryRows lists the configured libraries by name and path, so a
// spec can assert it is driving the fixture library and not somebody's
// real music collection.
func libraryRows(d Deps) ([]map[string]any, error) {
	rows, err := d.DB.QueryContext(
		"SELECT id, name, path FROM libraries ORDER BY id",
	)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	out := []map[string]any{}

	for rows.Next() {
		var (
			id         int64
			name, path string
		)

		if err := rows.Scan(&id, &name, &path); err != nil {
			return nil, err
		}

		out = append(out, map[string]any{
			"id": id, "name": name, "path": path,
		})
	}

	return out, rows.Err()
}

// handleEmit pushes a backend event into every connected frontend.
//
// This is the biggest lever the surface has.  Half this app is
// push-driven, and several of those events are only produced by work
// that takes minutes to hours (a full scan, a download, an artifact
// import).  Emitting one directly renders the view that consumes it
// without staging the work that would normally produce it.
//
//	POST /__test/emit  {"name":"LibraryScanProgress","data":[{"...":1}]}
func handleEmit(d Deps, r *http.Request) (any, error) {
	var body struct {
		Name string `json:"name"`
		Data []any  `json:"data"`
	}

	if err := decode(r, &body); err != nil {
		return nil, err
	}

	if body.Name == "" {
		return nil, errNoEventName
	}

	// events.Deliver rather than events.Emit: an ordinary emitter wants
	// an event with nowhere to go dropped, but this endpoint exists to
	// impersonate one, and reporting a 200 for an event that never
	// reached a frontend would send a caller debugging the wrong half of
	// the app.
	if err := events.Deliver(d.Context(), body.Name, body.Data...); err != nil {
		return nil, fmt.Errorf("emit %s: %w", body.Name, err)
	}

	return map[string]any{"emitted": body.Name, "args": len(body.Data)}, nil
}

// handleSQL runs a statement against the writer connection.
//
// One general escape hatch rather than a bespoke endpoint per piece of
// forced state — "mark this track played", "insert a wanted-list row",
// "age this cache entry" — each of which would otherwise arrive one at
// a time and never be removed.
//
//	POST /__test/sql  {"sql":"UPDATE ...","args":[1,"x"]}
func handleSQL(d Deps, r *http.Request) (any, error) {
	var body struct {
		SQL  string `json:"sql"`
		Args []any  `json:"args"`
	}

	if err := decode(r, &body); err != nil {
		return nil, err
	}

	if body.SQL == "" {
		return nil, errNoSQL
	}

	// Route by statement kind rather than by trying one and falling
	// back: the read pool is opened query_only, so sending a write
	// there fails in a way that looks like a bug in the caller's SQL.
	if isQuery(body.SQL) {
		rows, err := d.DB.QueryContextWith(r.Context(), body.SQL, body.Args...)
		if err != nil {
			return nil, err
		}

		defer func() { _ = rows.Close() }()

		return scanAll(rows)
	}

	res, err := d.DB.ExecContext(body.SQL, body.Args...)
	if err != nil {
		return nil, err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}

	return map[string]any{"rowsAffected": affected}, nil
}

// isQuery reports whether a statement returns rows.
func isQuery(sql string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(sql), " ")

	switch strings.ToUpper(first) {
	case "SELECT", "WITH", "PRAGMA", "EXPLAIN":
		return true
	default:
		return false
	}
}

// dbPath reports where the SQLite file lives, mirroring database.NewDB.
func dbPath() string {
	dir, err := system.GetUserDataDirPath()
	if err != nil {
		return ""
	}

	return filepath.Join(dir, "yj.db")
}
