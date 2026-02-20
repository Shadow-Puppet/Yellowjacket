// Package playlist provides playlist management functionality.
package playlist

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/events"
	"yellowjacket/backend/library"
	"yellowjacket/backend/system"
)

var (
	errEmptyName           = errors.New("playlist name cannot be empty")
	errEmptyFilePath       = errors.New("file path cannot be empty")
	errNoFilePaths         = errors.New("no file paths provided")
	errUnsupportedFileType = errors.New("unsupported file type")
)

// playlistsDirName is the subdirectory within the user data
// directory where M3U8 playlist files are stored.
const playlistsDirName = "playlists"

// LibraryDirProvider is a narrow interface for obtaining the
// configured library directory path.
type LibraryDirProvider interface {
	GetLibraryDirectory() string
}

// Summary is a lightweight representation of a playlist for the
// picker UI.
type Summary struct {
	ID   int64  `json:"ID"`
	Name string `json:"Name"`
}

// Track represents a track within a playlist, including its
// metadata.
type Track struct {
	ID             int64  `json:"ID"`
	Position       int64  `json:"Position"`
	FilePath       string `json:"FilePath"`
	Title          string `json:"Title"`
	Artist         string `json:"Artist"`
	Album          string `json:"Album"`
	CoverArtPath   string `json:"CoverArtPath"`
	CoverArtSmall  string `json:"CoverArtSmall"`
	CoverArtMedium string `json:"CoverArtMedium"`
	CoverArtLarge  string `json:"CoverArtLarge"`
	Duration       string `json:"Duration"`
	Phantom        bool   `json:"Phantom"`
}

// WithTracks contains a playlist summary and all its tracks.
type WithTracks struct {
	Summary Summary `json:"Summary"`
	Tracks  []Track `json:"Tracks"`
}

// Service manages playlist operations.
type Service struct {
	ctx        context.Context
	logger     *slog.Logger
	db         *database.DB
	libraryDir LibraryDirProvider
}

// NewService creates a new playlist service.
func NewService(
	logger *slog.Logger,
	db *database.DB,
	libraryDir LibraryDirProvider,
) *Service {
	return &Service{
		logger:     logger.WithGroup("playlist"),
		db:         db,
		libraryDir: libraryDir,
	}
}

// SetContext sets the Wails runtime context and runs the
// one-time startup migration to bootstrap M3U8 files for
// existing playlists.
func (s *Service) SetContext(ctx context.Context) {
	s.ctx = ctx
	s.migrateExistingPlaylists()
}

// GetAllPlaylists returns all playlists ordered by most recently
// updated.
func (s *Service) GetAllPlaylists() ([]Summary, error) {
	playlists, err := s.db.Queries.GetAllPlaylists(s.db.Ctx)
	if err != nil {
		s.logger.Error(
			"Failed to get playlists", "err", err,
		)

		return nil, fmt.Errorf(
			"failed to get playlists: %w", err,
		)
	}

	summaries := make([]Summary, 0, len(playlists))

	for _, p := range playlists {
		summaries = append(summaries, Summary{
			ID:   p.ID,
			Name: p.Name,
		})
	}

	return summaries, nil
}

// GetAllPlaylistsWithTracks returns all playlists with their
// tracks in a single call, merging phantom tracks from M3U8 files.
func (s *Service) GetAllPlaylistsWithTracks() (
	[]WithTracks,
	error,
) {
	playlists, err := s.db.Queries.GetAllPlaylists(s.db.Ctx)
	if err != nil {
		s.logger.Error(
			"Failed to get playlists", "err", err,
		)

		return nil, fmt.Errorf(
			"failed to get playlists: %w", err,
		)
	}

	rows, err := s.db.Queries.GetAllPlaylistTracksWithMetadata(
		s.db.Ctx,
	)
	if err != nil {
		s.logger.Error(
			"Failed to get all playlist tracks",
			"err", err,
		)

		return nil, fmt.Errorf(
			"failed to get all playlist tracks: %w",
			err,
		)
	}

	// Group DB tracks by playlist ID, keyed by absolute file path.
	dbTracksByPlaylist := make(
		map[int64]map[string]Track,
	)

	for _, row := range rows {
		track := trackFromRow(
			row.ID,
			row.Position,
			row.FilePath,
			row.Title,
			row.Artist,
			row.Album,
			row.LengthMilliseconds,
			row.CoverArtPath,
		)

		if dbTracksByPlaylist[row.PlaylistID] == nil {
			dbTracksByPlaylist[row.PlaylistID] = make(
				map[string]Track,
			)
		}

		dbTracksByPlaylist[row.PlaylistID][row.FilePath] = track
	}

	result := make([]WithTracks, 0, len(playlists))

	for _, p := range playlists {
		tracks := s.mergeTracksForPlaylist(
			p.ID,
			p.Name,
			dbTracksByPlaylist[p.ID],
		)

		result = append(result, WithTracks{
			Summary: Summary{ID: p.ID, Name: p.Name},
			Tracks:  tracks,
		})
	}

	return result, nil
}

// GetPlaylistTracks returns all tracks in a playlist with full
// metadata, merging phantom tracks from the M3U8 file.
func (s *Service) GetPlaylistTracks(
	playlistID int64,
) ([]Track, error) {
	rows, err := s.db.Queries.GetPlaylistTracksWithMetadata(
		s.db.Ctx,
		playlistID,
	)
	if err != nil {
		s.logger.Error(
			"Failed to get playlist tracks",
			"playlistId", playlistID,
			"err", err,
		)

		return nil, fmt.Errorf(
			"failed to get playlist tracks: %w",
			err,
		)
	}

	// Build a map of DB tracks keyed by absolute file path.
	dbTracks := make(map[string]Track, len(rows))

	for _, row := range rows {
		track := trackFromRow(
			row.ID,
			row.Position,
			row.FilePath,
			row.Title,
			row.Artist,
			row.Album,
			row.LengthMilliseconds,
			row.CoverArtPath,
		)

		dbTracks[row.FilePath] = track
	}

	// Get playlist name for M3U file lookup.
	playlist, err := s.db.Queries.GetPlaylist(
		s.db.Ctx, playlistID,
	)
	if err != nil {
		s.logger.Error(
			"Failed to get playlist",
			"playlistId", playlistID,
			"err", err,
		)

		return nil, fmt.Errorf(
			"failed to get playlist: %w", err,
		)
	}

	return s.mergeTracksForPlaylist(
		playlistID, playlist.Name, dbTracks,
	), nil
}

// mergeTracksForPlaylist merges DB tracks with M3U8 entries,
// producing phantom tracks for unresolved paths.
func (s *Service) mergeTracksForPlaylist(
	playlistID int64,
	_ string,
	dbTracks map[string]Track,
) []Track {
	dir, err := s.playlistsDir()
	if err != nil {
		s.logger.Warn(
			"Could not get playlists dir for merge",
			"err", err,
		)

		return dbTracksToSlice(dbTracks)
	}

	m3uPath, err := findPlaylistFile(dir, playlistID)
	if err != nil || m3uPath == "" {
		return dbTracksToSlice(dbTracks)
	}

	parsed, err := parseM3U8(m3uPath)
	if err != nil {
		s.logger.Warn(
			"Could not parse M3U8 for merge",
			"playlistId", playlistID,
			"path", m3uPath,
			"err", err,
		)

		return dbTracksToSlice(dbTracks)
	}

	libraryRoot := s.getLibraryRoot()
	tracks := make([]Track, 0, len(parsed.Entries))

	for i, entry := range parsed.Entries {
		absPath := toAbsolutePath(
			entry.RelativePath, libraryRoot,
		)

		if dbTrack, ok := dbTracks[absPath]; ok {
			dbTrack.Position = int64(i)
			tracks = append(tracks, dbTrack)

			continue
		}

		// Phantom track — file not resolved in DB.
		tracks = append(tracks, Track{
			Position: int64(i),
			FilePath: absPath,
			Title:    entry.DisplayTitle,
			Phantom:  true,
		})
	}

	return tracks
}

// dbTracksToSlice converts a map of tracks to an ordered slice.
func dbTracksToSlice(m map[string]Track) []Track {
	if len(m) == 0 {
		return []Track{}
	}

	tracks := make([]Track, 0, len(m))

	for _, t := range m {
		tracks = append(tracks, t)
	}

	return tracks
}

// trackFromRow converts raw query row fields into a Track.
func trackFromRow(
	id, position int64,
	filePath, title, artist, album string,
	lengthMilliseconds int64,
	coverArtPath string,
) Track {
	track := Track{
		ID:       id,
		Position: position,
		FilePath: filePath,
		Title:    title,
		Artist:   artist,
		Album:    album,
		Duration: strconv.FormatInt(lengthMilliseconds, 10),
	}

	if coverArtPath != "" {
		base := filepath.Base(coverArtPath)
		track.CoverArtPath = "/covers/" + base
		track.CoverArtSmall = "/covers/" +
			library.SizedFilename(base, "_sm")
		track.CoverArtMedium = "/covers/" +
			library.SizedFilename(base, "_md")
		track.CoverArtLarge = "/covers/" +
			library.SizedFilename(base, "_lg")
	}

	return track
}

// CreatePlaylist creates a new empty playlist with the given name.
func (s *Service) CreatePlaylist(
	name string,
) (Summary, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return Summary{}, errEmptyName
	}

	created, err := s.db.Queries.CreatePlaylist(
		s.db.Ctx, trimmed,
	)
	if err != nil {
		s.logger.Error(
			"Failed to create playlist",
			"name", trimmed, "err", err,
		)

		return Summary{}, fmt.Errorf(
			"failed to create playlist: %w", err,
		)
	}

	s.logger.Info(
		"Playlist created",
		"id", created.ID, "name", created.Name,
	)

	s.savePlaylistFile(created.ID, created.Name)
	s.emitEvent(events.PlaylistCreated, Summary{
		ID: created.ID, Name: created.Name,
	})

	return Summary{
		ID: created.ID, Name: created.Name,
	}, nil
}

// AddTracksToPlaylist adds one or more tracks to an existing
// playlist.
func (s *Service) AddTracksToPlaylist(
	playlistID int64,
	filePaths []string,
) error {
	if len(filePaths) == 0 {
		return errNoFilePaths
	}

	nextPos, err := s.db.Queries.GetNextPlaylistTrackPosition(
		s.db.Ctx,
		playlistID,
	)
	if err != nil {
		s.logger.Error(
			"Failed to get next position",
			"playlistId", playlistID,
			"err", err,
		)

		return fmt.Errorf(
			"failed to get next track position: %w", err,
		)
	}

	for i, fp := range filePaths {
		if err := s.addSingleTrack(
			playlistID, fp, nextPos+int64(i),
		); err != nil {
			return err
		}
	}

	s.logger.Info(
		"Tracks added to playlist",
		"playlistId", playlistID,
		"count", len(filePaths),
	)

	s.savePlaylistFileByID(playlistID)
	s.emitEvent(events.PlaylistTracksChanged, playlistID)

	return nil
}

// CreatePlaylistWithTracks creates a new playlist and populates
// it with tracks.
func (s *Service) CreatePlaylistWithTracks(
	name string,
	filePaths []string,
) (Summary, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return Summary{}, errEmptyName
	}

	created, err := s.db.Queries.CreatePlaylist(
		s.db.Ctx, trimmed,
	)
	if err != nil {
		s.logger.Error(
			"Failed to create playlist",
			"name", trimmed, "err", err,
		)

		return Summary{}, fmt.Errorf(
			"failed to create playlist: %w", err,
		)
	}

	if len(filePaths) > 0 {
		nextPos, posErr := s.db.Queries.GetNextPlaylistTrackPosition(
			s.db.Ctx,
			created.ID,
		)
		if posErr != nil {
			return Summary{}, fmt.Errorf(
				"failed to get next track position: %w",
				posErr,
			)
		}

		for i, fp := range filePaths {
			if err := s.addSingleTrack(
				created.ID, fp, nextPos+int64(i),
			); err != nil {
				return Summary{}, fmt.Errorf(
					"playlist created but failed to add tracks: %w",
					err,
				)
			}
		}
	}

	summary := Summary{
		ID: created.ID, Name: created.Name,
	}

	s.logger.Info(
		"Playlist created with tracks",
		"id", created.ID,
		"name", created.Name,
		"trackCount", len(filePaths),
	)

	s.savePlaylistFile(created.ID, created.Name)
	s.emitEvent(events.PlaylistCreated, summary)

	return summary, nil
}

// RemoveTracksFromPlaylist removes multiple tracks from a playlist
// by their playlist_track IDs.
func (s *Service) RemoveTracksFromPlaylist(
	playlistID int64,
	trackIDs []int64,
) error {
	if len(trackIDs) == 0 {
		return nil
	}

	for _, id := range trackIDs {
		if err := s.db.Queries.RemovePlaylistTrack(
			s.db.Ctx,
			id,
		); err != nil {
			s.logger.Error(
				"Failed to remove playlist track",
				"playlistId", playlistID,
				"trackId", id,
				"err", err,
			)

			return fmt.Errorf(
				"failed to remove track %d from playlist: %w",
				id,
				err,
			)
		}
	}

	s.logger.Info(
		"Tracks removed from playlist",
		"playlistId", playlistID,
		"count", len(trackIDs),
	)

	s.savePlaylistFileByID(playlistID)
	s.emitEvent(events.PlaylistTracksChanged, playlistID)

	return nil
}

// DeletePlaylist deletes a playlist and its M3U8 file.
func (s *Service) DeletePlaylist(playlistID int64) error {
	if err := s.db.Queries.DeletePlaylist(
		s.db.Ctx, playlistID,
	); err != nil {
		s.logger.Error(
			"Failed to delete playlist",
			"playlistId", playlistID,
			"err", err,
		)

		return fmt.Errorf(
			"failed to delete playlist: %w", err,
		)
	}

	s.deletePlaylistFile(playlistID)

	s.logger.Info(
		"Playlist deleted", "playlistId", playlistID,
	)

	s.emitEvent(events.PlaylistDeleted, playlistID)

	return nil
}

// RenamePlaylist renames a playlist and updates its M3U8 file.
func (s *Service) RenamePlaylist(
	playlistID int64,
	newName string,
) error {
	trimmed := strings.TrimSpace(newName)
	if trimmed == "" {
		return errEmptyName
	}

	if err := s.db.Queries.UpdatePlaylistName(
		s.db.Ctx,
		sqlcgen.UpdatePlaylistNameParams{
			Name: trimmed,
			ID:   playlistID,
		},
	); err != nil {
		s.logger.Error(
			"Failed to rename playlist",
			"playlistId", playlistID,
			"newName", trimmed,
			"err", err,
		)

		return fmt.Errorf(
			"failed to rename playlist: %w", err,
		)
	}

	// Re-save the M3U8 file with the new name (handles rename
	// of the file on disk).
	s.savePlaylistFile(playlistID, trimmed)

	s.logger.Info(
		"Playlist renamed",
		"playlistId", playlistID,
		"newName", trimmed,
	)

	s.emitEvent(events.PlaylistRenamed, Summary{
		ID: playlistID, Name: trimmed,
	})

	return nil
}

// ImportPlaylist imports a playlist from an external M3U/M3U8
// file. It creates a new playlist in the DB, resolves tracks
// against the library, and saves an M3U8 file.
func (s *Service) ImportPlaylist(
	filePath string,
) (Summary, error) {
	if strings.TrimSpace(filePath) == "" {
		return Summary{}, errEmptyFilePath
	}

	ext := filepath.Ext(filePath)
	if !isValidM3UExtension(ext) {
		return Summary{}, fmt.Errorf(
			"%w: %q, expected .m3u or .m3u8",
			errUnsupportedFileType, ext,
		)
	}

	parsed, err := parseM3U8(filePath)
	if err != nil {
		return Summary{}, fmt.Errorf(
			"could not parse playlist file: %w", err,
		)
	}

	playlistName := parsed.Name
	if playlistName == "" {
		base := filepath.Base(filePath)
		playlistName = strings.TrimSuffix(
			base, filepath.Ext(base),
		)
	}

	// Create playlist in DB.
	created, err := s.db.Queries.CreatePlaylist(
		s.db.Ctx, playlistName,
	)
	if err != nil {
		return Summary{}, fmt.Errorf(
			"could not create playlist for import: %w", err,
		)
	}

	libraryRoot := s.getLibraryRoot()

	var (
		resolved   int
		unresolved int
	)

	for i, entry := range parsed.Entries {
		absPath := toAbsolutePath(
			entry.RelativePath, libraryRoot,
		)

		audioFile, lookupErr := s.db.Queries.GetAudioFileByPath(
			s.db.Ctx, absPath,
		)
		if lookupErr != nil {
			// Track not in library — will appear as phantom.
			unresolved++

			continue
		}

		_, addErr := s.db.Queries.AddPlaylistTrack(
			s.db.Ctx,
			sqlcgen.AddPlaylistTrackParams{
				PlaylistID:  created.ID,
				AudioFileID: audioFile.ID,
				Position:    int64(i),
			},
		)
		if addErr != nil {
			s.logger.Warn(
				"Could not add imported track",
				"playlistId", created.ID,
				"path", absPath,
				"err", addErr,
			)

			continue
		}

		resolved++
	}

	// Save the M3U8 file with entries (preserves unresolved
	// paths for phantom display).
	s.saveImportedPlaylistFile(
		created.ID, playlistName, parsed.Entries, libraryRoot,
	)

	s.logger.Info(
		"Playlist imported",
		"id", created.ID,
		"name", playlistName,
		"resolved", resolved,
		"unresolved", unresolved,
	)

	summary := Summary{
		ID: created.ID, Name: playlistName,
	}

	s.emitEvent(events.PlaylistCreated, summary)

	return summary, nil
}

// RestoreAllPlaylists restores playlist tracks from M3U8 files.
// This is called after a full library rescan to repopulate
// playlist_tracks from the surviving M3U8 files.
func (s *Service) RestoreAllPlaylists() {
	dir, err := s.playlistsDir()
	if err != nil {
		s.logger.Warn(
			"Could not get playlists dir for restore",
			"err", err,
		)

		return
	}

	files, err := listPlaylistFiles(dir)
	if err != nil {
		s.logger.Warn(
			"Could not list playlist files",
			"err", err,
		)

		return
	}

	if len(files) == 0 {
		return
	}

	libraryRoot := s.getLibraryRoot()

	var totalRestored, totalUnresolved int

	for _, file := range files {
		playlistID := extractPlaylistID(file)
		if playlistID == 0 {
			s.logger.Warn(
				"Could not extract playlist ID from filename",
				"file", file,
			)

			continue
		}

		restored, unresolved := s.restoreSinglePlaylist(
			playlistID, file, libraryRoot,
		)

		totalRestored += restored
		totalUnresolved += unresolved
	}

	s.logger.Info(
		"All playlists restored from M3U8 files",
		"totalRestored", totalRestored,
		"totalUnresolved", totalUnresolved,
	)

	s.emitEvent(events.PlaylistsRestored, nil)
}

// restoreSinglePlaylist restores tracks for a single playlist
// from its M3U8 file.
func (s *Service) restoreSinglePlaylist(
	playlistID int64,
	m3uPath string,
	libraryRoot string,
) (restored, unresolved int) {
	parsed, err := parseM3U8(m3uPath)
	if err != nil {
		s.logger.Warn(
			"Could not parse M3U8 for restore",
			"playlistId", playlistID,
			"path", m3uPath,
			"err", err,
		)

		return 0, 0
	}

	// Verify the playlist exists in the DB.
	_, err = s.db.Queries.GetPlaylist(
		s.db.Ctx, playlistID,
	)
	if err != nil {
		s.logger.Warn(
			"Playlist not found in DB during restore",
			"playlistId", playlistID,
			"err", err,
		)

		return 0, 0
	}

	for i, entry := range parsed.Entries {
		absPath := toAbsolutePath(
			entry.RelativePath, libraryRoot,
		)

		audioFile, lookupErr := s.db.Queries.GetAudioFileByPath(
			s.db.Ctx, absPath,
		)
		if lookupErr != nil {
			unresolved++

			continue
		}

		_, addErr := s.db.Queries.AddPlaylistTrack(
			s.db.Ctx,
			sqlcgen.AddPlaylistTrackParams{
				PlaylistID:  playlistID,
				AudioFileID: audioFile.ID,
				Position:    int64(i),
			},
		)
		if addErr != nil {
			s.logger.Warn(
				"Could not restore track",
				"playlistId", playlistID,
				"path", absPath,
				"err", addErr,
			)

			continue
		}

		restored++
	}

	s.logger.Info(
		"Playlist restored",
		"playlistId", playlistID,
		"restored", restored,
		"unresolved", unresolved,
	)

	return restored, unresolved
}

// addSingleTrack looks up the audio file by path and inserts it
// into the playlist.
func (s *Service) addSingleTrack(
	playlistID int64,
	filePath string,
	position int64,
) error {
	if strings.TrimSpace(filePath) == "" {
		return errEmptyFilePath
	}

	audioFile, err := s.db.Queries.GetAudioFileByPath(
		s.db.Ctx, filePath,
	)
	if err != nil {
		s.logger.Error(
			"Failed to find audio file",
			"filePath", filePath,
			"err", err,
		)

		return fmt.Errorf(
			"failed to find audio file %q: %w",
			filePath, err,
		)
	}

	_, err = s.db.Queries.AddPlaylistTrack(
		s.db.Ctx,
		sqlcgen.AddPlaylistTrackParams{
			PlaylistID:  playlistID,
			AudioFileID: audioFile.ID,
			Position:    position,
		},
	)
	if err != nil {
		s.logger.Error(
			"Failed to add track to playlist",
			"playlistId", playlistID,
			"audioFileId", audioFile.ID,
			"err", err,
		)

		return fmt.Errorf(
			"failed to add track to playlist: %w", err,
		)
	}

	return nil
}

// --- M3U8 file management helpers ---

// playlistsDir returns the path to the playlists directory,
// creating it if needed.
func (s *Service) playlistsDir() (string, error) {
	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		return "", fmt.Errorf(
			"could not get user data directory: %w", err,
		)
	}

	dir := filepath.Join(dataDir, playlistsDirName)

	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return "", fmt.Errorf(
			"could not create playlists directory: %w", err,
		)
	}

	return dir, nil
}

// getLibraryRoot returns the configured library directory path.
func (s *Service) getLibraryRoot() string {
	if s.libraryDir == nil {
		return ""
	}

	return s.libraryDir.GetLibraryDirectory()
}

// savePlaylistFile saves the current state of a playlist to its
// M3U8 file.
func (s *Service) savePlaylistFile(
	playlistID int64,
	name string,
) {
	dir, err := s.playlistsDir()
	if err != nil {
		s.logger.Warn(
			"Could not get playlists dir for save",
			"err", err,
		)

		return
	}

	entries := s.buildM3UEntries(playlistID)

	if err := writeM3U8(
		dir, playlistID, name, entries,
	); err != nil {
		s.logger.Warn(
			"Could not save playlist M3U8 file",
			"playlistId", playlistID,
			"err", err,
		)
	}
}

// savePlaylistFileByID looks up the playlist name and saves.
func (s *Service) savePlaylistFileByID(playlistID int64) {
	playlist, err := s.db.Queries.GetPlaylist(
		s.db.Ctx, playlistID,
	)
	if err != nil {
		s.logger.Warn(
			"Could not get playlist for save",
			"playlistId", playlistID,
			"err", err,
		)

		return
	}

	s.savePlaylistFile(playlistID, playlist.Name)
}

// saveImportedPlaylistFile saves an M3U8 file for an imported
// playlist, preserving the original entries (including
// unresolved paths).
func (s *Service) saveImportedPlaylistFile(
	playlistID int64,
	name string,
	entries []m3uEntry,
	libraryRoot string,
) {
	dir, err := s.playlistsDir()
	if err != nil {
		s.logger.Warn(
			"Could not get playlists dir for import save",
			"err", err,
		)

		return
	}

	// Convert any absolute paths in entries to relative.
	converted := make([]m3uEntry, len(entries))

	for i, entry := range entries {
		converted[i] = m3uEntry{
			RelativePath: toRelativePath(
				toAbsolutePath(
					entry.RelativePath, libraryRoot,
				),
				libraryRoot,
			),
			DurationSec:  entry.DurationSec,
			DisplayTitle: entry.DisplayTitle,
		}
	}

	if err := writeM3U8(
		dir, playlistID, name, converted,
	); err != nil {
		s.logger.Warn(
			"Could not save imported playlist M3U8 file",
			"playlistId", playlistID,
			"err", err,
		)
	}
}

// buildM3UEntries builds M3U entries from the current DB state
// of a playlist.
func (s *Service) buildM3UEntries(
	playlistID int64,
) []m3uEntry {
	rows, err := s.db.Queries.GetPlaylistTracksWithMetadata(
		s.db.Ctx,
		playlistID,
	)
	if err != nil {
		s.logger.Warn(
			"Could not get tracks for M3U build",
			"playlistId", playlistID,
			"err", err,
		)

		return nil
	}

	libraryRoot := s.getLibraryRoot()
	entries := make([]m3uEntry, 0, len(rows))

	for _, row := range rows {
		durationSec := int(
			row.LengthMilliseconds / 1000,
		)

		entries = append(entries, m3uEntry{
			RelativePath: toRelativePath(
				row.FilePath, libraryRoot,
			),
			DurationSec: durationSec,
			DisplayTitle: displayTitle(
				row.Artist, row.Title,
			),
		})
	}

	return entries
}

// deletePlaylistFile removes the M3U8 file for a playlist.
func (s *Service) deletePlaylistFile(playlistID int64) {
	dir, err := s.playlistsDir()
	if err != nil {
		return
	}

	existing, err := findPlaylistFile(dir, playlistID)
	if err != nil || existing == "" {
		return
	}

	if err := os.Remove(existing); err != nil &&
		!os.IsNotExist(err) {
		s.logger.Warn(
			"Could not delete playlist file",
			"playlistId", playlistID,
			"path", existing,
			"err", err,
		)
	}
}

// emitEvent emits a Wails event if the context is available.
func (s *Service) emitEvent(
	eventName string,
	data any,
) {
	if s.ctx == nil {
		return
	}

	runtime.EventsEmit(s.ctx, eventName, data)
}

// migrateExistingPlaylists generates M3U8 files for any
// existing DB playlists that don't already have one. This runs
// once at startup to bootstrap the file-based backup for users
// who already have playlists.
func (s *Service) migrateExistingPlaylists() {
	dir, err := s.playlistsDir()
	if err != nil {
		s.logger.Warn(
			"Could not get playlists dir for migration",
			"err", err,
		)

		return
	}

	existingFiles, err := listPlaylistFiles(dir)
	if err != nil {
		s.logger.Warn(
			"Could not list existing playlist files",
			"err", err,
		)

		return
	}

	// Build a set of IDs that already have files.
	existingIDs := make(map[int64]struct{})

	for _, file := range existingFiles {
		id := extractPlaylistID(file)
		if id > 0 {
			existingIDs[id] = struct{}{}
		}
	}

	playlists, err := s.db.Queries.GetAllPlaylists(s.db.Ctx)
	if err != nil {
		s.logger.Warn(
			"Could not get playlists for migration",
			"err", err,
		)

		return
	}

	var migrated int

	for _, p := range playlists {
		if _, exists := existingIDs[p.ID]; exists {
			continue
		}

		s.savePlaylistFile(p.ID, p.Name)

		migrated++
	}

	if migrated > 0 {
		s.logger.Info(
			"Migrated existing playlists to M3U8 files",
			"count", migrated,
		)
	}
}
