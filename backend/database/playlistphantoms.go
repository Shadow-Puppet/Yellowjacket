package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

// Preserving a playlist entry across the loss of its track is two
// statements, not one, and the split is not tidiness -- it is what
// makes the important half work in the situation that needs it most.
//
// `playlist_tracks.audio_file_id` is ON DELETE SET NULL, so an entry
// outlives its file as an id-less row that says nothing about what the
// user put in the playlist.  The phantom_* columns carry the answer
// across and ResolvePhantomTracksAfterScan re-links them afterwards --
// but only if something fills them *before* the rows go.
//
// The two halves are not equally important and are not equally
// available:
//
//   - **phantom_file_path is the one that matters.**
//     ResolvePhantomTracksAfterScan matches it against
//     `audio_files.file_path`, so without it an entry can never be
//     re-linked and the playlist is empty for good.  It comes straight
//     off `audio_files`, whose `file_path` is the table's natural key
//     and has been present in every shape it has ever had -- including
//     the pre-013 stub of `(id, file_path, recording_id)`.
//   - The rest is *display* for a phantom entry before a rescan
//     re-links it, and it comes from the `track_metadata` view, which
//     is the one definition of a track row and not worth restating.
//
// Reading the view is what cannot be relied on here, and that is the
// whole reason for the split.  This runs *before* applySchema, which is
// precisely the moment the schema is inconsistent: the view is whatever
// the last launch's schema declared, while `audio_files` is whatever
// the launch before that left behind.  A view over columns the table no
// longer has is not merely empty -- `pragma_table_info` on it *errors*,
// and so does selecting from it.  `cmd/indexbuild`'s fixture is exactly
// that shape and is what caught this.
//
// COALESCE keeps an existing phantom value in both halves: an entry
// already phantom is one whose file went missing in an earlier pass,
// and its recorded metadata is the only copy left.  Overwriting that
// from a NULL join erases the rows this exists to protect.
const (
	preservePhantomPathSQL = `
		UPDATE playlist_tracks
		SET phantom_file_path = COALESCE(phantom_file_path, (
			SELECT af.file_path FROM audio_files af
			WHERE af.id = playlist_tracks.audio_file_id
		))
		WHERE audio_file_id IS NOT NULL
	`

	preservePhantomDisplaySQL = `
		UPDATE playlist_tracks
		SET
			phantom_title = COALESCE(phantom_title, (
				SELECT tm.title FROM track_metadata tm
				WHERE tm.id = playlist_tracks.audio_file_id
			)),
			phantom_artist = COALESCE(phantom_artist, (
				SELECT tm.artist_name FROM track_metadata tm
				WHERE tm.id = playlist_tracks.audio_file_id
			)),
			phantom_album = COALESCE(phantom_album, (
				SELECT tm.album FROM track_metadata tm
				WHERE tm.id = playlist_tracks.audio_file_id
			)),
			phantom_duration_ms = COALESCE(phantom_duration_ms, (
				SELECT af.length_milliseconds FROM audio_files af
				WHERE af.id = playlist_tracks.audio_file_id
			)),
			phantom_genre = COALESCE(phantom_genre, (
				SELECT tm.genre FROM track_metadata tm
				WHERE tm.id = playlist_tracks.audio_file_id
			)),
			phantom_cover_art_path = COALESCE(phantom_cover_art_path, (
				SELECT tm.cover_art_path FROM track_metadata tm
				WHERE tm.id = playlist_tracks.audio_file_id
			))
		WHERE audio_file_id IS NOT NULL
	`
)

// PreservePlaylistPhantoms records every linked playlist entry's track
// metadata on the entry itself, so the entry survives the rows being
// deleted underneath it.
//
// Every path that empties `audio_files` must call this first, inside
// the same transaction as the delete.  There are two such paths and
// they had drifted: the full rescan in backend/library did this and the
// stale-shape retire in this package did not, so the *documented*
// repair ("delete and rescan") preserved playlists while the automatic
// one that exists to spare the user that work silently emptied them.
//
// The display half is skipped, with a warning, when `track_metadata`
// cannot answer -- see the note above.  Skipping it costs a phantom
// entry its title until a rescan re-links it; skipping the path half
// would cost the entry outright, so that one is an error.
func PreservePlaylistPhantoms(
	ctx context.Context, tx *sql.Tx, logger *slog.Logger,
) error {
	if _, err := tx.ExecContext(ctx, preservePhantomPathSQL); err != nil {
		return fmt.Errorf(
			"could not preserve playlist track file paths: %w", err,
		)
	}

	if _, err := tx.ExecContext(ctx, preservePhantomDisplaySQL); err != nil {
		// A failed statement does not roll back a SQLite transaction,
		// so the path half above stands and the entries remain
		// re-linkable.
		logger.Warn(
			"could not record display metadata for playlist entries; "+
				"they will be re-linked by the next scan but read as "+
				"unknown until then",
			"err", err,
		)
	}

	return nil
}
