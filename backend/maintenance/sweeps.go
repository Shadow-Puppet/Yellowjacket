package maintenance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"yellowjacket/backend/database"
)

// Retention windows for cache data.  Cached artwork for artists the user
// actually owns is kept indefinitely; art fetched while browsing Explore
// is transient and ages out.
const (
	// browsedArtRetention is how long artwork for a non-library artist
	// survives after it was fetched.
	browsedArtRetention = 90 * 24 * time.Hour

	// proxyCacheRetention is how long an Explore cover-art thumbnail
	// survives after it was last written.
	proxyCacheRetention = 30 * 24 * time.Hour
)

// Default intervals.  These are minimums, not schedules — the runner
// skips a job that ran more recently.
const (
	frequentInterval = 6 * time.Hour
	dailyInterval    = 24 * time.Hour
)

// ExpiredHTTPCacheJob deletes HTTP cache rows past their TTL.
//
// Reads already filter on expires_at, so expired rows are inert — but
// nothing was deleting them, so the table grew without bound for the
// life of the install.
func ExpiredHTTPCacheJob(db *database.DB) Job {
	return Job{
		Name:        "http-cache-evict",
		MinInterval: frequentInterval,
		Run: func(_ context.Context) (Result, error) {
			res, err := db.ExecContext(
				"DELETE FROM http_cache WHERE expires_at < datetime('now')",
			)
			if err != nil {
				return Result{}, fmt.Errorf(
					"delete expired http_cache rows: %w", err,
				)
			}

			rows, _ := res.RowsAffected()

			return Result{RowsDeleted: rows}, nil
		},
	}
}

// OrphanedCoverFilesJob removes files from the covers directory that no
// cover_art row references.
//
// Cover art is derived data, so the live set is authoritative: every
// file that is not the original named by a cover_art row, or one of that
// original's derived size variants, is garbage.  This reclaims art left
// behind by earlier versions that deleted only the original and left its
// thumbnails.
func OrphanedCoverFilesJob(
	db *database.DB,
	coversDir string,
	expandVariants func(originalPath string) []string,
) Job {
	return Job{
		Name:        "covers-sweep",
		MinInterval: dailyInterval,
		Run: func(ctx context.Context) (Result, error) {
			live, err := liveCoverFiles(db, coversDir, expandVariants)
			if err != nil {
				return Result{}, err
			}

			// A covers directory with no live entries almost certainly
			// means the query failed to see the real table rather than
			// that every cover is garbage.  Refuse to empty the
			// directory on that basis.
			if len(live) == 0 {
				return Result{}, nil
			}

			return sweepDir(ctx, coversDir, func(name string) bool {
				return !live[name]
			})
		},
	}
}

// liveCoverFiles returns the basenames of every file the covers
// directory is supposed to contain.
func liveCoverFiles(
	db *database.DB,
	coversDir string,
	expandVariants func(string) []string,
) (map[string]bool, error) {
	rows, err := db.QueryContext("SELECT file_path FROM cover_art")
	if err != nil {
		return nil, fmt.Errorf("read cover_art paths: %w", err)
	}

	defer func() { _ = rows.Close() }()

	live := make(map[string]bool)

	for rows.Next() {
		var path string

		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("scan cover_art path: %w", err)
		}

		// Rows may store an absolute path from a previous install
		// location, so compare by basename within the covers directory.
		original := filepath.Join(coversDir, filepath.Base(path))

		for _, variant := range expandVariants(original) {
			live[filepath.Base(variant)] = true
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cover_art paths: %w", err)
	}

	return live, nil
}

// OrphanedArtistImagesJob evicts cached artist artwork.
//
// Artist images are cache data with no owner to compare against — they
// are fetched for any artist the user browses in Explore, most of whom
// are not in the library. The policy is therefore twofold: artwork for
// an artist the user owns is kept indefinitely, and everything else ages
// out. Rows whose file has vanished are dropped so the table matches
// what is actually on disk.
//
// dirFor maps an artist MBID to its directory.  It is a parameter, as
// OrphanedCoverFilesJob's expandVariants is, because that layout is the
// image provider's business and this job had been guessing it: artist
// directories are sharded under a two-character prefix, so joining the
// bare MBID named a path that has never existed.  RemoveAll succeeds on
// a missing path, so the job reported success, freed nothing, and
// deleted the rows that were the only record of the files it left
// behind.
func OrphanedArtistImagesJob(
	db *database.DB,
	artistImagesDir string,
	dirFor func(baseDir, mbid string) string,
) Job {
	return Job{
		Name:        "artist-images-sweep",
		MinInterval: dailyInterval,
		Run: func(ctx context.Context) (Result, error) {
			var result Result

			cutoff := time.Now().Add(-browsedArtRetention)

			// Collect the directories to remove before deleting rows, so
			// a failure partway leaves rows pointing at real files
			// rather than the reverse.
			stale, err := staleArtistMBIDs(db, cutoff)
			if err != nil {
				return Result{}, err
			}

			for _, mbid := range stale {
				if ctx.Err() != nil {
					return result, nil
				}

				dir := dirFor(artistImagesDir, mbid)

				freed, files := dirSize(dir)

				if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
					continue
				}

				result.FilesDeleted += files
				result.BytesFreed += freed
			}

			if len(stale) > 0 {
				rows, delErr := deleteArtistImageRows(db, stale)
				if delErr != nil {
					return result, delErr
				}

				result.RowsDeleted += rows
			}

			return result, nil
		},
	}
}

// StrayArtistImageFilesJob removes downloaded image candidates that
// were never the artist's portrait.
//
// The image provider used to download every candidate an upstream
// offered — up to ten, full size — and keep them all, while nothing in
// the app has ever read anything but primary.jpg and its three size
// tiers.  It now downloads candidates in priority order until one
// succeeds and records the rest as URLs, so nothing new lands here;
// this reclaims what earlier versions left, which on a real cache was
// about four fifths of it.
//
// keep is the set of names an artist directory is allowed to hold.  It
// is a parameter for the same reason dirFor above is: the provider owns
// the naming, and a sweep that guesses it deletes the wrong files.
func StrayArtistImageFilesJob(artistImagesDir string, keep map[string]bool) Job {
	return Job{
		Name:        "artist-images-strays",
		MinInterval: dailyInterval,
		Run: func(ctx context.Context) (Result, error) {
			// An empty keep set would mean "every file is garbage",
			// which is never the intent — refuse, as the covers sweep
			// refuses an empty live set.
			if len(keep) == 0 {
				return Result{}, nil
			}

			var result Result

			shards, err := os.ReadDir(artistImagesDir)
			if err != nil {
				if os.IsNotExist(err) {
					return Result{}, nil
				}

				return Result{}, fmt.Errorf("read %s: %w", artistImagesDir, err)
			}

			for _, shard := range shards {
				if !shard.IsDir() {
					continue
				}

				shardDir := filepath.Join(artistImagesDir, shard.Name())

				artists, err := os.ReadDir(shardDir)
				if err != nil {
					continue
				}

				for _, artist := range artists {
					if ctx.Err() != nil {
						return result, nil
					}

					if !artist.IsDir() {
						continue
					}

					swept, err := sweepDir(ctx,
						filepath.Join(shardDir, artist.Name()),
						func(name string) bool { return !keep[name] },
					)
					if err != nil {
						continue
					}

					result.FilesDeleted += swept.FilesDeleted
					result.BytesFreed += swept.BytesFreed
				}
			}

			return result, nil
		},
	}
}

// staleArtistMBIDs returns artist MBIDs whose cached artwork may be
// evicted: fetched before the cutoff and not an artist in the library.
func staleArtistMBIDs(
	db *database.DB,
	cutoff time.Time,
) ([]string, error) {
	rows, err := db.QueryContext(
		`SELECT DISTINCT artist_mbid FROM artist_images
		 WHERE created_at < ?
		   AND artist_mbid NOT IN (
		       SELECT mbid FROM artists
		       WHERE mbid IS NOT NULL AND mbid != ''
		   )`,
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("query stale artist images: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var mbids []string

	for rows.Next() {
		var mbid string

		if err := rows.Scan(&mbid); err != nil {
			return nil, fmt.Errorf("scan artist mbid: %w", err)
		}

		if mbid != "" {
			mbids = append(mbids, mbid)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stale artist images: %w", err)
	}

	return mbids, nil
}

// deleteArtistImageRows removes the rows for the given artist MBIDs.
func deleteArtistImageRows(
	db *database.DB,
	mbids []string,
) (int64, error) {
	var total int64

	for _, mbid := range mbids {
		res, err := db.ExecContext(
			"DELETE FROM artist_images WHERE artist_mbid = ?", mbid,
		)
		if err != nil {
			return total, fmt.Errorf(
				"delete artist_images rows for %s: %w", mbid, err,
			)
		}

		n, _ := res.RowsAffected()
		total += n
	}

	return total, nil
}

// ExpiredProxyCacheJob evicts Explore cover-art thumbnails that have not
// been rewritten within the retention window.
//
// This cache has no database table at all — it is keyed by release-group
// MBID on the filesystem — so age is the only signal available.
func ExpiredProxyCacheJob(proxyCacheDir string) Job {
	return Job{
		Name:        "cover-art-proxy-sweep",
		MinInterval: dailyInterval,
		Run: func(ctx context.Context) (Result, error) {
			cutoff := time.Now().Add(-proxyCacheRetention)

			return sweepDirFunc(ctx, proxyCacheDir,
				func(_ string, info os.FileInfo) bool {
					return info.ModTime().Before(cutoff)
				},
			)
		},
	}
}

// sweepDir removes every file in dir for which shouldDelete reports true.
func sweepDir(
	ctx context.Context,
	dir string,
	shouldDelete func(name string) bool,
) (Result, error) {
	return sweepDirFunc(ctx, dir, func(name string, _ os.FileInfo) bool {
		return shouldDelete(name)
	})
}

// sweepDirFunc removes files from a flat directory based on a predicate
// over the name and stat info.  Subdirectories are left alone; sweeps
// that own directory trees handle them explicitly.
func sweepDirFunc(
	ctx context.Context,
	dir string,
	shouldDelete func(name string, info os.FileInfo) bool,
) (Result, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{}, nil
		}

		return Result{}, fmt.Errorf("read %s: %w", dir, err)
	}

	var result Result

	for _, entry := range entries {
		if ctx.Err() != nil {
			return result, nil
		}

		if entry.IsDir() {
			continue
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}

		if !shouldDelete(entry.Name(), info) {
			continue
		}

		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			continue
		}

		result.FilesDeleted++
		result.BytesFreed += info.Size()
	}

	return result, nil
}

// dirSize totals the files in a directory tree.
func dirSize(dir string) (bytes, files int64) {
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // best-effort accounting
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}

		bytes += info.Size()
		files++

		return nil
	})

	return bytes, files
}
