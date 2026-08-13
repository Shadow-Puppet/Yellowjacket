package explore

import (
	"context"
	"database/sql"
	"math/rand/v2"
)

// mixBatchSize is how many tracks one GenerateMix call returns.
const mixBatchSize = 30

// mixGenreBoost is added to a candidate's similarity weight for each
// genre it shares with the seed, biasing the pick toward tracks that
// match on both artist and tag rather than artist alone.
const mixGenreBoost = 0.5

// mixSimilarArtistsPerSeed caps how many similar artists are expanded
// per distinct seed artist, so a seed with an unusually long tail in
// similar_artist_map doesn't turn one fallback trigger into hundreds
// of queries.
const mixSimilarArtistsPerSeed = 15

// mixSession is a dynamic mix in progress: the seed it was built from
// (fixed for the life of the session, so successive batches don't
// drift away from what the session started as) and what it has
// already handed out, so a batch doesn't repeat a track that just
// played.
type mixSession struct {
	seedPaths []string
	played    map[string]bool
}

// GenerateMix returns the next batch of tracks for a dynamic-mix queue
// fallback, built by expanding the seed's artists to their similar
// artists (weighted by how often each appears in the seed, sharpened
// by shared genre tags) and restricting candidates to what is actually
// in the library — a queue can only play files that exist.
//
// continuing extends the current mix session — regenerating from its
// original seed rather than seedPaths — instead of starting a fresh
// one. Pass false whenever the queue that just exhausted was not
// itself a mix batch (a real selection just ran out); pass true when
// it was (the mix keeps going indefinitely). label names the batch
// after its most-represented seed artist, for the "Playing from" UI.
func (e *Service) GenerateMix(
	ctx context.Context,
	seedPaths []string,
	continuing bool,
) (paths []string, label string, err error) {
	e.mixMu.Lock()
	defer e.mixMu.Unlock()

	if !continuing || e.mix == nil {
		e.mix = &mixSession{seedPaths: seedPaths, played: map[string]bool{}}
	}

	seed := e.mix.seedPaths
	if len(seed) == 0 {
		return nil, "", nil
	}

	artistCounts, topArtistName, genres := e.mixSeedProfile(ctx, seed)
	if len(artistCounts) == 0 {
		return nil, "", nil
	}

	candidates := e.mixCandidates(ctx, artistCounts, genres, seed, e.mix.played)

	// The session has played through everything this seed can offer —
	// rather than dead-ending an "indefinite" mix, start handing out
	// repeats.
	if len(candidates) == 0 && len(e.mix.played) > 0 {
		candidates = e.mixCandidates(ctx, artistCounts, genres, seed, nil)
	}

	if len(candidates) == 0 {
		return nil, "", nil
	}

	picked := weightedSample(candidates, mixBatchSize)
	for _, p := range picked {
		e.mix.played[p] = true
	}

	if topArtistName != "" {
		label = "a mix inspired by " + topArtistName
	} else {
		label = "a dynamic mix"
	}

	return picked, label, nil
}

// mixSeedProfile tallies the seed's artists by frequency, its genre
// tags, and names the most-represented artist for the UI label.
func (e *Service) mixSeedProfile(
	ctx context.Context,
	seedPaths []string,
) (artistCounts map[string]int, topArtistName string, genres map[string]bool) {
	artistCounts = map[string]int{}
	artistNames := map[string]string{}
	genres = map[string]bool{}

	for _, p := range seedPaths {
		artist, err := e.db.ReadQueries.GetArtistByFilePath(ctx, p)
		if err == nil && artist.ArtistMbid != "" {
			artistCounts[artist.ArtistMbid]++
			artistNames[artist.ArtistMbid] = artist.ArtistName
		}

		names, err := e.db.ReadQueries.GetGenreNamesByFilePath(ctx, p)
		if err == nil {
			for _, g := range names {
				genres[g] = true
			}
		}
	}

	var topCount int

	for mbid, count := range artistCounts {
		if count > topCount {
			topCount = count
			topArtistName = artistNames[mbid]
		}
	}

	return artistCounts, topArtistName, genres
}

// mixCandidates builds the weighted pool of library tracks to draw a
// batch from: every owned track by a similar artist, weighted by that
// artist's similarity score times how often the seed artist it came
// from appears in the seed, boosted for a shared genre tag, excluding
// the seed itself and anything already excluded (typically what the
// mix has already played).
func (e *Service) mixCandidates(
	ctx context.Context,
	artistCounts map[string]int,
	seedGenres map[string]bool,
	seedPaths []string,
	exclude map[string]bool,
) map[string]float64 {
	excludeSeed := make(map[string]bool, len(seedPaths))
	for _, p := range seedPaths {
		excludeSeed[p] = true
	}

	candidates := map[string]float64{}

	for seedArtistMBID, count := range artistCounts {
		similar, err := e.SimilarArtists(seedArtistMBID)
		if err != nil {
			continue
		}

		if len(similar) > mixSimilarArtistsPerSeed {
			similar = similar[:mixSimilarArtistsPerSeed]
		}

		for _, s := range similar {
			if s.ArtistMBID == "" {
				continue
			}

			paths, err := e.db.ReadQueries.GetFilePathsByArtistMBID(
				ctx,
				sql.NullString{String: s.ArtistMBID, Valid: true},
			)
			if err != nil {
				continue
			}

			weight := s.Score * float64(count)

			for _, p := range paths {
				if excludeSeed[p] || exclude[p] {
					continue
				}

				if names, err := e.db.ReadQueries.GetGenreNamesByFilePath(ctx, p); err == nil {
					for _, g := range names {
						if seedGenres[g] {
							weight += mixGenreBoost

							break
						}
					}
				}

				candidates[p] += weight
			}
		}
	}

	return candidates
}

// weightedSample picks up to n distinct keys from weights without
// replacement, biased toward higher weight (roulette-wheel selection).
// A key with zero or negative weight is never picked.
func weightedSample(weights map[string]float64, n int) []string {
	type entry struct {
		key    string
		weight float64
	}

	pool := make([]entry, 0, len(weights))

	var total float64

	for k, w := range weights {
		if w <= 0 {
			continue
		}

		pool = append(pool, entry{k, w})
		total += w
	}

	picked := make([]string, 0, min(n, len(pool)))

	for len(picked) < n && len(pool) > 0 {
		r := rand.Float64() * total
		idx := 0

		for i, e := range pool {
			r -= e.weight
			if r <= 0 {
				idx = i

				break
			}
		}

		picked = append(picked, pool[idx].key)
		total -= pool[idx].weight
		pool = append(pool[:idx], pool[idx+1:]...)
	}

	return picked
}
