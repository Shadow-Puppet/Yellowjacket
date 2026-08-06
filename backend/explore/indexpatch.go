//go:build indexbuild

package explore

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"sync"

	"yellowjacket/backend/jobs"
)

// Helpers used only while building the catalog from the MetaBrainz
// dumps: the similar-artist patch pass and the per-stage error
// reporting that goes with it.  They live behind the `indexbuild` tag
// with the importer they serve, so the app binary carries neither.

const (
	// indexSimilarPerArtist is how many similar artists to store per
	// library artist in similar_artist_map.
	indexSimilarPerArtist = 20

	// similarArtistsBatchSize is the number of seed MBIDs processed in
	// one logging "batch".  The labs multi-seed POST form is broken, so
	// one GET per seed is issued (concurrency bounded by indexerRate);
	// batching here just keeps progress log output bounded.
	similarArtistsBatchSize = 50
)

// setTierError marks a build stage as errored.
func (si *SearchIndex) setTierError(name, errMsg string) {
	si.mu.Lock()

	for i := range si.buildStatus.Tiers {
		if si.buildStatus.Tiers[i].Name == name {
			si.buildStatus.Tiers[i].State = "error"
			si.buildStatus.Tiers[i].Error = errMsg
			si.mu.Unlock()

			si.logIndexJob(jobs.LevelError, name+": "+errMsg)
			si.emitStatus()

			return
		}
	}

	si.mu.Unlock()
}

// fetchSimilarArtistsBatch queries the labs similar-artists endpoint
// for multiple seed MBIDs.  Despite the name, this actually fans
// out one request per seed: the labs API's multi-seed mode is
// broken (results for different seeds get mis-labeled, and some
// seeds return zero), so batching with multiple artist_mbids is
// not viable.  Concurrency is bounded by indexerRate to respect
// the labs rate limit; each call goes through the provided LB
// client's rate limiter and cache.
func (si *SearchIndex) fetchSimilarArtistsBatch(
	ctx context.Context, lb *ListenBrainzClient, seedMBIDs []string,
) map[string][]lbSimilarArtistWire {
	if len(seedMBIDs) == 0 {
		return nil
	}

	var (
		mu      sync.Mutex
		grouped = make(map[string][]lbSimilarArtistWire, len(seedMBIDs))
		wg      sync.WaitGroup
	)

	sem := make(chan struct{}, indexerRate)

	for _, seedMBID := range seedMBIDs {
		if ctx.Err() != nil {
			break
		}

		sem <- struct{}{}

		wg.Add(1)

		go func(seed string) {
			defer func() {
				<-sem
				wg.Done()
			}()

			// Use the LB client's per-seed GET form — goes through
			// the shared rate limiter and cache.  The multi-seed
			// POST form is not viable (see function comment).
			similar, err := lb.SimilarArtists(ctx, seed)
			if err != nil || len(similar) == 0 {
				return
			}

			// Convert to the internal wire type used by the caller
			// and trim to indexSimilarPerArtist.
			if len(similar) > indexSimilarPerArtist {
				similar = similar[:indexSimilarPerArtist]
			}

			results := make([]lbSimilarArtistWire, len(similar))
			for i, s := range similar {
				results[i] = lbSimilarArtistWire{
					ArtistMBID:    s.ArtistMBID,
					Name:          s.Name,
					Score:         int(s.Score),
					ReferenceMBID: seed,
				}
			}

			mu.Lock()
			grouped[seed] = results
			mu.Unlock()
		}(seedMBID)
	}

	wg.Wait()

	return grouped
}

// chunkStrings splits a slice into chunks of at most size n.
func chunkStrings(s []string, n int) [][]string {
	var chunks [][]string

	for i := 0; i < len(s); i += n {
		end := i + n
		if end > len(s) {
			end = len(s)
		}

		chunks = append(chunks, s[i:end])
	}

	return chunks
}

// getLibraryArtistMBIDs returns MBIDs for all library artists that have one.
// Used when Tier 3 was skipped but Tier 4 needs the library MBID list.
func (si *SearchIndex) getLibraryArtistMBIDs() []string {
	rows, err := si.db.QueryContext(
		"SELECT DISTINCT mbid FROM artists WHERE mbid IS NOT NULL AND mbid != ''",
	)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	var mbids []string

	for rows.Next() {
		var mbid string
		if err := rows.Scan(&mbid); err == nil {
			mbids = append(mbids, mbid)
		}
	}

	return mbids
}

// discoverDumpFile walks a dump base directory, finds subdirectories
// matching dirRe (newest first, lexicographically — MetaBrainz dump
// directory names embed sortable timestamps), and returns the full URL
// of the first file inside matching fileRe.  Directories that don't
// contain a matching file (e.g. partial uploads) are skipped.
func discoverDumpFile(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	dirRe, fileRe *regexp.Regexp,
) (string, error) {
	hrefs, err := listHrefs(ctx, client, baseURL)
	if err != nil {
		return "", err
	}

	var dirs []string

	for _, h := range hrefs {
		trimmed := trimTrailingSlash(h)
		if dirRe.MatchString(trimmed) {
			dirs = append(dirs, trimmed)
		}
	}

	if len(dirs) == 0 {
		return "", fmt.Errorf("%w: no dump directories under %s", ErrDumpDiscovery, baseURL)
	}

	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))

	for _, dir := range dirs {
		dirURL := baseURL + dir + "/"

		files, err := listHrefs(ctx, client, dirURL)
		if err != nil {
			continue
		}

		for _, f := range files {
			if fileRe.MatchString(f) {
				return dirURL + f, nil
			}
		}
	}

	return "", fmt.Errorf("%w: no matching dump file under %s", ErrDumpDiscovery, baseURL)
}
