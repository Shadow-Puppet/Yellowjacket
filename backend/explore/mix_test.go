package explore

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"yellowjacket/backend/database"
)

// seedMixTrack inserts one owned track by the given artist (creating
// the artist/artist_credit/recording/audio_file chain as needed),
// tagged with the given genres.
func seedMixTrack(
	t *testing.T,
	db *database.DB,
	id int,
	artistName, artistMBID string,
	genreNames ...string,
) string {
	t.Helper()

	fp := fmt.Sprintf("/music/%s/track%d.mp3", artistName, id)

	_, err := db.ExecContext(
		"INSERT INTO artists (id, name, mbid) VALUES (?, ?, ?) "+
			"ON CONFLICT(name) DO NOTHING",
		id, artistName, artistMBID,
	)
	if err != nil {
		t.Fatalf("insert artist: %v", err)
	}

	_, err = db.ExecContext(
		"INSERT INTO artist_credit (id, text) VALUES (?, ?) "+
			"ON CONFLICT(text) DO NOTHING",
		id, artistName,
	)
	if err != nil {
		t.Fatalf("insert artist_credit: %v", err)
	}

	_, err = db.ExecContext(
		"INSERT OR IGNORE INTO artist_credit_artist (artist_id, credit_id) VALUES (?, ?)",
		id, id,
	)
	if err != nil {
		t.Fatalf("insert artist_credit_artist: %v", err)
	}

	_, err = db.ExecContext(
		"INSERT INTO recordings (id, name, artist_credit_id) VALUES (?, ?, ?)",
		id, fmt.Sprintf("Track %d", id), id,
	)
	if err != nil {
		t.Fatalf("insert recording: %v", err)
	}

	_, err = db.ExecContext(
		"INSERT INTO audio_files (id, file_path, length_milliseconds, file_type_id, recording_id) "+
			"VALUES (?, ?, 180000, 0, ?)",
		id, fp, id,
	)
	if err != nil {
		t.Fatalf("insert audio_file: %v", err)
	}

	for _, g := range genreNames {
		var genreID int64

		row := db.QueryRowWriter(
			"INSERT INTO genres (name) VALUES (?) "+
				"ON CONFLICT(name) DO UPDATE SET name = name RETURNING id",
			g,
		)
		if err := row.Scan(&genreID); err != nil {
			t.Fatalf("upsert genre %q: %v", g, err)
		}

		_, err = db.ExecContext(
			"INSERT OR IGNORE INTO recording_genres (recording_id, genre_id) VALUES (?, ?)",
			id, genreID,
		)
		if err != nil {
			t.Fatalf("insert recording_genre: %v", err)
		}
	}

	return fp
}

// seedSimilarArtist records a pre-computed similarity row, as the
// Tier 4 index build / lazy LB fetch would.
func seedSimilarArtist(
	t *testing.T,
	db *database.DB,
	sourceMBID, similarMBID, similarName string,
	score int,
) {
	t.Helper()

	_, err := db.ExecContext(
		"INSERT INTO similar_artist_map "+
			"(source_artist_mbid, similar_artist_mbid, similar_artist_name, score) "+
			"VALUES (?, ?, ?, ?)",
		sourceMBID, similarMBID, similarName, score,
	)
	if err != nil {
		t.Fatalf("insert similar_artist_map row: %v", err)
	}
}

func newMixTestService(db *database.DB) *Service {
	return &Service{db: db, logger: slog.Default()}
}

func TestGenerateMix_ExpandsToSimilarLibraryArtists(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	e := newMixTestService(db)

	seedPath := seedMixTrack(t, db, 1, "Seed Artist", "mbid-seed")
	similarPath := seedMixTrack(t, db, 2, "Similar Artist", "mbid-similar")
	unrelatedPath := seedMixTrack(t, db, 3, "Unrelated Artist", "mbid-unrelated")

	seedSimilarArtist(t, db, "mbid-seed", "mbid-similar", "Similar Artist", 90)

	paths, label, err := e.GenerateMix(context.Background(), []string{seedPath}, false)
	if err != nil {
		t.Fatalf("GenerateMix: %v", err)
	}

	if len(paths) != 1 || paths[0] != similarPath {
		t.Errorf("paths: got %v, want [%q]", paths, similarPath)
	}

	for _, p := range paths {
		if p == unrelatedPath {
			t.Error("mix included a track by an artist with no recorded similarity")
		}
	}

	if label == "" {
		t.Error("label: got empty string, want a seed-derived label")
	}
}

func TestGenerateMix_BoostsSharedGenre(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	e := newMixTestService(db)

	seedPath := seedMixTrack(t, db, 1, "Seed Artist", "mbid-seed", "Shoegaze")
	matchingGenrePath := seedMixTrack(t, db, 2, "Similar A", "mbid-similar-a", "Shoegaze")
	differentGenrePath := seedMixTrack(t, db, 3, "Similar B", "mbid-similar-b", "Ambient")

	// Same base similarity score for both, so genre is what breaks the tie.
	seedSimilarArtist(t, db, "mbid-seed", "mbid-similar-a", "Similar A", 50)
	seedSimilarArtist(t, db, "mbid-seed", "mbid-similar-b", "Similar B", 50)

	candidates := e.mixCandidates(
		context.Background(),
		map[string]int{"mbid-seed": 1},
		map[string]bool{"Shoegaze": true},
		[]string{seedPath},
		nil,
	)

	if candidates[matchingGenrePath] <= candidates[differentGenrePath] {
		t.Errorf(
			"weight: shared-genre candidate (%v) should outweigh the other (%v)",
			candidates[matchingGenrePath], candidates[differentGenrePath],
		)
	}
}

func TestGenerateMix_ExcludesAlreadyPlayed(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	e := newMixTestService(db)

	seedPath := seedMixTrack(t, db, 1, "Seed Artist", "mbid-seed")
	similarPath := seedMixTrack(t, db, 2, "Similar Artist", "mbid-similar")

	seedSimilarArtist(t, db, "mbid-seed", "mbid-similar", "Similar Artist", 90)

	ctx := context.Background()

	first, _, err := e.GenerateMix(ctx, []string{seedPath}, false)
	if err != nil {
		t.Fatalf("first GenerateMix: %v", err)
	}

	if len(first) != 1 || first[0] != similarPath {
		t.Fatalf("first batch: got %v, want [%q]", first, similarPath)
	}

	// Continuing the same session, with nothing new to offer: rather
	// than dead-ending, it should replay from the pool instead of
	// returning nothing.
	second, _, err := e.GenerateMix(ctx, nil, true)
	if err != nil {
		t.Fatalf("second GenerateMix: %v", err)
	}

	if len(second) != 1 || second[0] != similarPath {
		t.Errorf(
			"second batch: got %v, want [%q] (replayed after exhausting the pool)",
			second, similarPath,
		)
	}
}

func TestGenerateMix_ContinuingIgnoresNewSeed(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	e := newMixTestService(db)

	originalSeed := seedMixTrack(t, db, 1, "Seed Artist", "mbid-seed")
	_ = seedMixTrack(t, db, 2, "Similar Artist", "mbid-similar")
	unrelatedSeed := seedMixTrack(t, db, 3, "Other Artist", "mbid-other")
	similarToUnrelated := seedMixTrack(t, db, 4, "Other Similar", "mbid-other-similar")

	seedSimilarArtist(t, db, "mbid-seed", "mbid-similar", "Similar Artist", 90)
	seedSimilarArtist(t, db, "mbid-other", "mbid-other-similar", "Other Similar", 90)

	ctx := context.Background()

	if _, _, err := e.GenerateMix(ctx, []string{originalSeed}, false); err != nil {
		t.Fatalf("GenerateMix: %v", err)
	}

	// A second, unrelated seed passed while "continuing" is ignored —
	// the mix stays anchored to what it started with.
	paths, _, err := e.GenerateMix(ctx, []string{unrelatedSeed}, true)
	if err != nil {
		t.Fatalf("GenerateMix (continuing): %v", err)
	}

	for _, p := range paths {
		if p == similarToUnrelated {
			t.Error("continuing mix drifted to the newly passed seed instead of the original")
		}
	}
}

func TestGenerateMix_NoSeedArtistDataReturnsNothing(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	e := newMixTestService(db)

	// A file path with no matching audio_files row at all.
	paths, label, err := e.GenerateMix(context.Background(), []string{"/nowhere.mp3"}, false)
	if err != nil {
		t.Fatalf("GenerateMix: %v", err)
	}

	if len(paths) != 0 || label != "" {
		t.Errorf("got (%v, %q), want (nil, \"\")", paths, label)
	}
}
