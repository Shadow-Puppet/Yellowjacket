package playlist

import (
	"path/filepath"
	"testing"
)

// setupPhantomPlaylist builds a playlist whose M3U8 holds two entries —
// one file the library has and one it does not — with the matching
// playlist_tracks rows: a linked row at position 0 and a phantom at
// position 1.  It returns the playlist id, the phantom's absolute path
// and the library path the user would resolve it to.
func setupPhantomPlaylist(
	t *testing.T,
) (svc *Service, playlistID int64, phantomAbs, targetAbs string) {
	t.Helper()

	svc, db, _ := setupRecordedService(t)
	paths := seedPlaylistTracks(t, db, 2)

	libDir := svc.libraryDir.(stubLibraryDir).dir
	// seedPlaylistTracks writes absolute paths outside the library root,
	// which is fine: an M3U8 entry may be absolute, and resolveM3UPath
	// returns an absolute entry unchanged.
	targetAbs = paths[1]
	phantomAbs = filepath.Join(libDir, "gone", "missing.mp3")

	created, err := svc.CreatePlaylist("Imported")
	if err != nil {
		t.Fatalf("CreatePlaylist: %v", err)
	}

	playlistID = created.ID

	dir, err := svc.playlistsDir()
	if err != nil {
		t.Fatalf("playlistsDir: %v", err)
	}

	if err := writeM3U8(dir, playlistID, "Imported", []m3uEntry{
		{RelativePath: paths[0], DisplayTitle: "Track 1", DurationSec: 180},
		{
			RelativePath: phantomAbs,
			DisplayTitle: "Some Artist - Missing",
			DurationSec:  200,
		},
	}); err != nil {
		t.Fatalf("writeM3U8: %v", err)
	}

	if _, err := db.ExecContext(
		`INSERT INTO playlist_tracks (playlist_id, audio_file_id, position)
		 VALUES (?, 1, 0)`,
		playlistID,
	); err != nil {
		t.Fatalf("insert linked track: %v", err)
	}

	if _, err := db.ExecContext(
		`INSERT INTO playlist_tracks
		   (playlist_id, position, phantom_title, phantom_file_path)
		 VALUES (?, 1, 'Some Artist - Missing', ?)`,
		playlistID, phantomAbs,
	); err != nil {
		t.Fatalf("insert phantom track: %v", err)
	}

	return svc, playlistID, phantomAbs, targetAbs
}

// countPlaylistRows reports how many playlist_tracks rows the playlist
// has, and how many of them are still phantoms.
func countPlaylistRows(
	t *testing.T, svc *Service, playlistID int64,
) (total, phantoms int) {
	t.Helper()

	rows, err := svc.db.QueryContext(
		`SELECT COUNT(*),
		        COALESCE(SUM(audio_file_id IS NULL), 0)
		 FROM playlist_tracks WHERE playlist_id = ?`,
		playlistID,
	)
	if err != nil {
		t.Fatalf("count playlist_tracks: %v", err)
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		t.Fatal("count playlist_tracks: no row")
	}

	if err := rows.Scan(&total, &phantoms); err != nil {
		t.Fatalf("scan count: %v", err)
	}

	return total, phantoms
}

// TestResolvePhantomTracksFillsTheRowItAlreadyHas is the regression for
// a manually resolved phantom appearing twice: the resolution used to
// append a *new* playlist_tracks row and leave the phantom row behind,
// so the playlist held two rows for one M3U8 line — and the resolved
// one sat at the end of the playlist rather than where the track was.
func TestResolvePhantomTracksFillsTheRowItAlreadyHas(t *testing.T) {
	t.Parallel()

	svc, playlistID, phantomAbs, targetAbs := setupPhantomPlaylist(t)

	if err := svc.ResolvePhantomTracks(
		playlistID, map[string]string{phantomAbs: targetAbs},
	); err != nil {
		t.Fatalf("ResolvePhantomTracks: %v", err)
	}

	total, phantoms := countPlaylistRows(t, svc, playlistID)
	if total != 2 {
		t.Errorf("playlist_tracks rows = %d, want 2", total)
	}

	if phantoms != 0 {
		t.Errorf("phantom rows left = %d, want 0", phantoms)
	}

	// The track stays where it was in the playlist.
	tracks, err := svc.GetPlaylistTracks(playlistID)
	if err != nil {
		t.Fatalf("GetPlaylistTracks: %v", err)
	}

	if len(tracks) != 2 {
		t.Fatalf("tracks = %d, want 2", len(tracks))
	}

	if tracks[1].FilePath != targetAbs {
		t.Errorf(
			"resolved track at position 1 = %q, want %q",
			tracks[1].FilePath, targetAbs,
		)
	}

	if tracks[1].Phantom {
		t.Error("resolved track is still marked phantom")
	}
}

// TestResolvePhantomTracksRefusesTwoPhantomsForOneFile pins the rule
// FindPhantomMatches already applies on the auto-match path: one library
// file cannot stand in for two phantom tracks, or resolving adds it to
// the playlist twice.
func TestResolvePhantomTracksRefusesTwoPhantomsForOneFile(t *testing.T) {
	t.Parallel()

	svc, playlistID, phantomAbs, targetAbs := setupPhantomPlaylist(t)

	secondPhantom := filepath.Join(
		svc.libraryDir.(stubLibraryDir).dir, "gone", "missing-2.mp3",
	)

	if _, err := svc.db.ExecContext(
		`INSERT INTO playlist_tracks
		   (playlist_id, position, phantom_title, phantom_file_path)
		 VALUES (?, 2, 'Some Artist - Missing 2', ?)`,
		playlistID, secondPhantom,
	); err != nil {
		t.Fatalf("insert second phantom: %v", err)
	}

	if err := svc.ResolvePhantomTracks(playlistID, map[string]string{
		phantomAbs:    targetAbs,
		secondPhantom: targetAbs,
	}); err != nil {
		t.Fatalf("ResolvePhantomTracks: %v", err)
	}

	total, phantoms := countPlaylistRows(t, svc, playlistID)
	if total != 3 {
		t.Errorf("playlist_tracks rows = %d, want 3", total)
	}

	// One of the two phantoms is resolved; the other is left for the
	// user to point somewhere else.
	if phantoms != 1 {
		t.Errorf("phantom rows left = %d, want 1", phantoms)
	}
}
