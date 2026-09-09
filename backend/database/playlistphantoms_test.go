package database

import (
	"context"
	"database/sql"
	"testing"
)

// TestPreservePlaylistPhantomsForFilesScopesToTheRequestedIDs is the
// scoping half of the scoped variant: a run over one file's id must
// fill that file's playlist entries and leave every other entry alone,
// because the incremental scan calls this once per orphan batch and a
// pass that rewrote the whole table would touch every playlist row on
// every scan.
func TestPreservePlaylistPhantomsForFilesScopesToTheRequestedIDs(t *testing.T) {
	ctx := context.Background()
	db := openRaw(t, t.TempDir())

	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("pragma: %v", err)
	}

	if err := applySchema(ctx, db); err != nil {
		t.Fatalf("applySchema: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO playlists (id, name) VALUES (1, 'keepme');
		INSERT INTO libraries (id, name, path) VALUES (0, 'test', '/music');
		INSERT INTO artists (id, name) VALUES (3, 'Aurora Fields');
		INSERT INTO cover_art (id, file_path, mime_type)
			VALUES (9, 'covers/7.jpg', 'image/jpeg');
		INSERT INTO albums (id, name, artist_id, cover_art_id)
			VALUES (4, 'Tideline', 3, 9);
		INSERT INTO audio_files
			(id, file_path, file_type_id, length_milliseconds,
			 title, artist_credit, artist_id, album_id)
			VALUES
				(7, '/music/a.flac', 1, 1000,
				 'Slack Water', 'Aurora Fields', 3, 4),
				(8, '/music/b.flac', 1, 2000,
				 'Second Tide', 'Aurora Fields', 3, 4);
		INSERT INTO playlist_tracks (playlist_id, audio_file_id, position)
			VALUES (1, 7, 0), (1, 8, 1);
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	defer func() { _ = tx.Rollback() }()

	if err := PreservePlaylistPhantomsForFiles(
		ctx, tx, []int64{7}, testLogger(),
	); err != nil {
		t.Fatalf("preserve: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var filled, untouched sql.NullString

	if err := db.QueryRowContext(ctx,
		"SELECT phantom_file_path FROM playlist_tracks WHERE audio_file_id = 7",
	).Scan(&filled); err != nil {
		t.Fatalf("read the requested entry: %v", err)
	}

	if filled.String != "/music/a.flac" {
		t.Errorf(
			"requested entry phantom_file_path = %q, want %q",
			filled.String, "/music/a.flac",
		)
	}

	if err := db.QueryRowContext(ctx,
		"SELECT phantom_file_path FROM playlist_tracks WHERE audio_file_id = 8",
	).Scan(&untouched); err != nil {
		t.Fatalf("read the untouched entry: %v", err)
	}

	if untouched.Valid {
		t.Errorf(
			"untouched entry got phantom_file_path = %q, want NULL "+
				"(a scoped run must not rewrite the whole table)",
			untouched.String,
		)
	}
}
