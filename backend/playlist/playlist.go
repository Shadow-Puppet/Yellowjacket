// Package playlist provides playlist management functionality.
package playlist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"yellowjacket/backend/coverart"
	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/events"
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
	ID        int64  `json:"ID"`
	Name      string `json:"Name"`
	CreatedAt string `json:"CreatedAt"`
	UpdatedAt string `json:"UpdatedAt"`
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

// CandidateTrack represents a potential library match for a
// phantom track.
type CandidateTrack struct {
	FilePath string  `json:"FilePath"`
	Title    string  `json:"Title"`
	Artist   string  `json:"Artist"`
	Album    string  `json:"Album"`
	Duration string  `json:"Duration"`
	Score    float64 `json:"Score"`
}

// PhantomMatch represents a high-confidence pairing of a phantom
// track to a library track.
type PhantomMatch struct {
	PhantomPath  string         `json:"PhantomPath"`
	PhantomTitle string         `json:"PhantomTitle"`
	Candidate    CandidateTrack `json:"Candidate"`
}

// PhantomSearchResult contains auto-matched pairs and remaining
// unmatched phantom paths for a batch search operation.
type PhantomSearchResult struct {
	AutoMatched []PhantomMatch `json:"AutoMatched"`
	Unmatched   []string       `json:"Unmatched"`
}

// DuplicateTrackInfo holds metadata for a track that already
// exists in a playlist.
type DuplicateTrackInfo struct {
	FilePath string `json:"FilePath"`
	Title    string `json:"Title"`
	Artist   string `json:"Artist"`
	Album    string `json:"Album"`
	Duration string `json:"Duration"`
}

// DuplicateCheckResult contains the outcome of checking for
// duplicate tracks in a playlist.
type DuplicateCheckResult struct {
	Duplicates []DuplicateTrackInfo `json:"Duplicates"`
	Unique     []string             `json:"Unique"`
}

// Service manages playlist operations.
type Service struct {
	// mu protects ctx and favoritesConf from concurrent access
	// during initialization.
	mu            sync.Mutex
	ctx           context.Context
	logger        *slog.Logger
	db            *database.DB
	libraryDir    LibraryDirProvider
	favoritesConf FavoritesConfigProvider
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

// SetFavoritesConfig sets the provider used to read and write
// the default-playlist configuration.
func (s *Service) SetFavoritesConfig(
	provider FavoritesConfigProvider,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.favoritesConf = provider
}

// SetContext sets the Wails runtime context and runs the
// one-time startup migration to bootstrap M3U8 files for
// existing playlists.
func (s *Service) SetContext(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()

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
			ID:        p.ID,
			Name:      p.Name,
			CreatedAt: p.CreatedAt.Format(time.RFC3339),
			UpdatedAt: p.UpdatedAt.Format(time.RFC3339),
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
			Summary: Summary{
				ID:        p.ID,
				Name:      p.Name,
				CreatedAt: p.CreatedAt.Format(time.RFC3339),
				UpdatedAt: p.UpdatedAt.Format(time.RFC3339),
			},
			Tracks: tracks,
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
		urls := coverart.ResolveURLs(coverArtPath)
		track.CoverArtPath = urls.Original
		track.CoverArtSmall = urls.Small
		track.CoverArtMedium = urls.Medium
		track.CoverArtLarge = urls.Large
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
		ID:        created.ID,
		Name:      created.Name,
		CreatedAt: created.CreatedAt.Format(time.RFC3339),
		UpdatedAt: created.UpdatedAt.Format(time.RFC3339),
	})

	return Summary{
		ID:        created.ID,
		Name:      created.Name,
		CreatedAt: created.CreatedAt.Format(time.RFC3339),
		UpdatedAt: created.UpdatedAt.Format(time.RFC3339),
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

// FindDuplicateTracksInPlaylist checks which of the given file
// paths already exist in the specified playlist. Returns metadata
// for each duplicate and a list of non-duplicate file paths.
func (s *Service) FindDuplicateTracksInPlaylist(
	playlistID int64,
	filePaths []string,
) (DuplicateCheckResult, error) {
	rows, err := s.db.Queries.GetPlaylistTracksWithMetadata(
		s.db.Ctx,
		playlistID,
	)
	if err != nil {
		s.logger.Error(
			"Failed to get playlist tracks for duplicate check",
			"playlistId", playlistID,
			"err", err,
		)

		return DuplicateCheckResult{}, fmt.Errorf(
			"failed to get playlist tracks: %w", err,
		)
	}

	existingPaths := make(
		map[string]sqlcgen.GetPlaylistTracksWithMetadataRow,
		len(rows),
	)

	for _, row := range rows {
		existingPaths[row.FilePath] = row
	}

	var duplicates []DuplicateTrackInfo

	var unique []string

	for _, fp := range filePaths {
		if row, exists := existingPaths[fp]; exists {
			duplicates = append(duplicates, DuplicateTrackInfo{
				FilePath: fp,
				Title:    row.Title,
				Artist:   row.Artist,
				Album:    row.Album,
				Duration: strconv.FormatInt(
					row.LengthMilliseconds, 10,
				),
			})
		} else {
			unique = append(unique, fp)
		}
	}

	return DuplicateCheckResult{
		Duplicates: duplicates,
		Unique:     unique,
	}, nil
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
		ID:        created.ID,
		Name:      created.Name,
		CreatedAt: created.CreatedAt.Format(time.RFC3339),
		UpdatedAt: created.UpdatedAt.Format(time.RFC3339),
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
// If the deleted playlist was the default, a new default
// playlist is automatically created.
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

	// Recreate the default playlist if we just deleted it.
	if s.defaultPlaylistID() == playlistID {
		s.EnsureDefaultPlaylist()
	}

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
		ID:   playlistID,
		Name: trimmed,
	})

	return nil
}

// uniquePlaylistName returns a name that doesn't collide with existing
// playlists. If "Chill Vibes" exists, returns "Chill Vibes (1)".
// If that also exists, returns "Chill Vibes (2)", etc.
func (s *Service) uniquePlaylistName(name string) string {
	count, err := s.db.Queries.CountPlaylistsByName(s.db.Ctx, name)
	if err != nil || count == 0 {
		return name
	}

	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s (%d)", name, i)

		c, err := s.db.Queries.CountPlaylistsByName(s.db.Ctx, candidate)
		if err != nil || c == 0 {
			return candidate
		}
	}
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

	playlistName = s.uniquePlaylistName(playlistName)

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
		position   int
	)

	for _, entry := range parsed.Entries {
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
				AudioFileID: sql.NullInt64{Int64: audioFile.ID, Valid: true},
				Position:    int64(position),
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

		position++
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
		ID:        created.ID,
		Name:      playlistName,
		CreatedAt: created.CreatedAt.Format(time.RFC3339),
		UpdatedAt: created.UpdatedAt.Format(time.RFC3339),
	}

	s.emitEvent(events.PlaylistCreated, summary)

	return summary, nil
}

// ImportPlaylists imports multiple playlists from external M3U/M3U8
// files. Each file is imported sequentially using ImportPlaylist.
// Errors from individual imports are collected; partial success is
// possible. Returns the summaries of successfully imported playlists
// and the first error encountered (if any).
func (s *Service) ImportPlaylists(
	filePaths []string,
) ([]Summary, error) {
	if len(filePaths) == 0 {
		return nil, errNoFilePaths
	}

	summaries := make([]Summary, 0, len(filePaths))

	var firstErr error

	for _, fp := range filePaths {
		summary, err := s.ImportPlaylist(fp)
		if err != nil {
			s.logger.Warn(
				"Failed to import playlist file",
				"path", fp,
				"err", err,
			)

			if firstErr == nil {
				firstErr = fmt.Errorf(
					"import %q failed: %w", fp, err,
				)
			}

			continue
		}

		summaries = append(summaries, summary)
	}

	return summaries, firstErr
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

	var position int

	for _, entry := range parsed.Entries {
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
				AudioFileID: sql.NullInt64{Int64: audioFile.ID, Valid: true},
				Position:    int64(position),
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

		position++
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
			AudioFileID: sql.NullInt64{Int64: audioFile.ID, Valid: true},
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

// getLibraryRoot returns the library root directory path.
// It first checks the legacy config DirectoryPath; if that is
// empty (removed during multi-library migration) it falls back
// to the first library's path from the database.
func (s *Service) getLibraryRoot() string {
	if s.libraryDir != nil {
		if dir := s.libraryDir.GetLibraryDirectory(); dir != "" {
			return dir
		}
	}

	// Fallback: query the first library from the database.
	libs, err := s.db.Queries.GetAllLibraries(s.db.Ctx)
	if err != nil || len(libs) == 0 {
		return ""
	}

	return libs[0].Path
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

// =================================================================
// Phantom track resolution
// =================================================================

// FindPhantomMatches searches the library for matches for the
// given phantom file paths. High-confidence matches are returned
// as auto-matched pairs; the rest remain in the unmatched list.
func (s *Service) FindPhantomMatches(
	playlistID int64,
	phantomPaths []string,
) (PhantomSearchResult, error) {
	if len(phantomPaths) == 0 {
		return PhantomSearchResult{}, nil
	}

	dir, err := s.playlistsDir()
	if err != nil {
		return PhantomSearchResult{}, fmt.Errorf(
			"could not get playlists dir: %w", err,
		)
	}

	libraryRoot := s.getLibraryRoot()

	// Load M3U8 entries for display title / duration data.
	m3uPath, err := findPlaylistFile(dir, playlistID)
	if err != nil {
		return PhantomSearchResult{}, fmt.Errorf(
			"could not find playlist file: %w", err,
		)
	}

	var entries []m3uEntry

	if m3uPath != "" {
		parsed, parseErr := parseM3U8(m3uPath)
		if parseErr == nil {
			entries = parsed.Entries
		}
	}

	// Build a lookup from absolute path to M3U entry.
	entryByPath := make(map[string]m3uEntry, len(entries))

	for _, e := range entries {
		absPath := toAbsolutePath(
			e.RelativePath, libraryRoot,
		)
		entryByPath[absPath] = e
	}

	// Track which candidates have been claimed by auto-match
	// so we don't assign the same candidate to two phantoms.
	claimed := make(map[string]struct{})

	var result PhantomSearchResult

	for _, phantomPath := range phantomPaths {
		entry := entryByPath[phantomPath]
		candidates := s.searchCandidates(
			phantomPath, entry,
		)

		matched := false

		for _, c := range candidates {
			if _, taken := claimed[c.FilePath]; taken {
				continue
			}

			if c.Score >= autoMatchMinimum {
				result.AutoMatched = append(
					result.AutoMatched,
					PhantomMatch{
						PhantomPath:  phantomPath,
						PhantomTitle: entry.DisplayTitle,
						Candidate:    c,
					},
				)

				claimed[c.FilePath] = struct{}{}
				matched = true

				break
			}
		}

		if !matched {
			result.Unmatched = append(
				result.Unmatched, phantomPath,
			)
		}
	}

	return result, nil
}

// GetPhantomCandidates returns scored candidate matches for a
// single phantom track.
func (s *Service) GetPhantomCandidates(
	playlistID int64,
	phantomPath string,
) ([]CandidateTrack, error) {
	dir, err := s.playlistsDir()
	if err != nil {
		return nil, fmt.Errorf(
			"could not get playlists dir: %w", err,
		)
	}

	libraryRoot := s.getLibraryRoot()

	// Find the M3U entry for this phantom.
	m3uPath, err := findPlaylistFile(dir, playlistID)
	if err != nil {
		return nil, fmt.Errorf(
			"could not find playlist file: %w", err,
		)
	}

	var entry m3uEntry

	if m3uPath != "" {
		parsed, parseErr := parseM3U8(m3uPath)
		if parseErr == nil {
			entry, _ = findM3UEntry(
				parsed.Entries, phantomPath, libraryRoot,
			)
		}
	}

	return s.searchCandidates(
		phantomPath, entry,
	), nil
}

// SearchLibrary searches the entire library by a free-text query
// for manual phantom resolution.
func (s *Service) SearchLibrary(
	query string,
) ([]CandidateTrack, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return []CandidateTrack{}, nil
	}

	rows, err := s.db.SearchFTS(
		trimmed, maxLibrarySearchResults,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"library search failed: %w", err,
		)
	}

	candidates := make([]CandidateTrack, 0, len(rows))

	for _, row := range rows {
		candidates = append(candidates, CandidateTrack{
			FilePath: row.FilePath,
			Title:    row.Title,
			Artist:   row.Artist,
			Album:    row.Album,
			Duration: strconv.FormatInt(
				row.LengthMilliseconds, 10,
			),
		})
	}

	return candidates, nil
}

// ResolvePhantomTracks replaces phantom entries in a playlist
// with real library tracks. The matches map keys are phantom
// absolute paths and values are resolved absolute paths.
func (s *Service) ResolvePhantomTracks(
	playlistID int64,
	matches map[string]string,
) error {
	if len(matches) == 0 {
		return nil
	}

	dir, err := s.playlistsDir()
	if err != nil {
		return fmt.Errorf(
			"could not get playlists dir: %w", err,
		)
	}

	libraryRoot := s.getLibraryRoot()

	m3uPath, err := findPlaylistFile(dir, playlistID)
	if err != nil || m3uPath == "" {
		return fmt.Errorf(
			"could not find M3U8 file for playlist %d: %w",
			playlistID, err,
		)
	}

	parsed, err := parseM3U8(m3uPath)
	if err != nil {
		return fmt.Errorf(
			"could not parse M3U8: %w", err,
		)
	}

	// Get next available DB position.
	nextPos, err := s.db.Queries.GetNextPlaylistTrackPosition(
		s.db.Ctx, playlistID,
	)
	if err != nil {
		return fmt.Errorf(
			"could not get next position: %w", err,
		)
	}

	// Build M3U path replacements and insert DB rows.
	pathReplacements := make(
		map[string]string, len(matches),
	)

	var resolved int

	for phantomAbs, resolvedAbs := range matches {
		audioFile, lookupErr := s.db.Queries.GetAudioFileByPath(
			s.db.Ctx, resolvedAbs,
		)
		if lookupErr != nil {
			s.logger.Warn(
				"Resolved path not found in library",
				"phantomPath", phantomAbs,
				"resolvedPath", resolvedAbs,
				"err", lookupErr,
			)

			continue
		}

		_, addErr := s.db.Queries.AddPlaylistTrack(
			s.db.Ctx,
			sqlcgen.AddPlaylistTrackParams{
				PlaylistID:  playlistID,
				AudioFileID: sql.NullInt64{Int64: audioFile.ID, Valid: true},
				Position:    nextPos + int64(resolved),
			},
		)
		if addErr != nil {
			s.logger.Warn(
				"Could not add resolved track",
				"playlistId", playlistID,
				"path", resolvedAbs,
				"err", addErr,
			)

			continue
		}

		newRel := toRelativePath(resolvedAbs, libraryRoot)
		pathReplacements[phantomAbs] = newRel
		resolved++
	}

	// Rewrite the M3U8 with updated paths.
	if resolved > 0 {
		updated := replaceM3UEntryPaths(
			parsed.Entries, pathReplacements, libraryRoot,
		)

		playlist, nameErr := s.db.Queries.GetPlaylist(
			s.db.Ctx, playlistID,
		)
		if nameErr != nil {
			return fmt.Errorf(
				"could not get playlist name: %w", nameErr,
			)
		}

		if writeErr := writeM3U8(
			dir, playlistID, playlist.Name, updated,
		); writeErr != nil {
			return fmt.Errorf(
				"could not rewrite M3U8: %w", writeErr,
			)
		}
	}

	s.logger.Info(
		"Phantom tracks resolved",
		"playlistId", playlistID,
		"resolved", resolved,
		"requested", len(matches),
	)

	s.emitEvent(events.PlaylistTracksChanged, playlistID)

	return nil
}

// RemovePhantomTracks removes phantom entries from a playlist's
// M3U8 file. Since phantom tracks have no DB rows, only the
// M3U8 file is modified.
func (s *Service) RemovePhantomTracks(
	playlistID int64,
	phantomPaths []string,
) error {
	if len(phantomPaths) == 0 {
		return nil
	}

	dir, err := s.playlistsDir()
	if err != nil {
		return fmt.Errorf(
			"could not get playlists dir: %w", err,
		)
	}

	libraryRoot := s.getLibraryRoot()

	m3uPath, err := findPlaylistFile(dir, playlistID)
	if err != nil || m3uPath == "" {
		return fmt.Errorf(
			"could not find M3U8 file for playlist %d: %w",
			playlistID, err,
		)
	}

	parsed, err := parseM3U8(m3uPath)
	if err != nil {
		return fmt.Errorf(
			"could not parse M3U8: %w", err,
		)
	}

	targetSet := make(
		map[string]struct{}, len(phantomPaths),
	)

	for _, p := range phantomPaths {
		targetSet[p] = struct{}{}
	}

	updated := removeM3UEntries(
		parsed.Entries, targetSet, libraryRoot,
	)

	playlist, err := s.db.Queries.GetPlaylist(
		s.db.Ctx, playlistID,
	)
	if err != nil {
		return fmt.Errorf(
			"could not get playlist name: %w", err,
		)
	}

	if err := writeM3U8(
		dir, playlistID, playlist.Name, updated,
	); err != nil {
		return fmt.Errorf(
			"could not rewrite M3U8: %w", err,
		)
	}

	s.logger.Info(
		"Phantom tracks removed",
		"playlistId", playlistID,
		"removed", len(phantomPaths),
	)

	s.emitEvent(events.PlaylistTracksChanged, playlistID)

	return nil
}

// searchCandidates finds and scores candidate library tracks
// for a single phantom track.
func (s *Service) searchCandidates(
	phantomPath string,
	entry m3uEntry,
) []CandidateTrack {
	basename := filepath.Base(phantomPath)
	seen := make(map[string]struct{})

	var combined []database.SearchRow

	// 1. Exact basename match via indexed column.
	bnRows, err := s.db.Queries.SearchAudioFilesByBasename(
		s.db.Ctx,
		sqlcgen.SearchAudioFilesByBasenameParams{
			Basename: basename,
			Limit:    int64(maxCandidates),
		},
	)
	if err != nil {
		s.logger.Warn(
			"Basename search failed",
			"basename", basename,
			"err", err,
		)
	}

	for _, r := range bnRows {
		if _, ok := seen[r.FilePath]; ok {
			continue
		}

		seen[r.FilePath] = struct{}{}

		combined = append(combined, database.SearchRow{
			FilePath:           r.FilePath,
			LengthMilliseconds: r.LengthMilliseconds,
			Title:              r.Title,
			Artist:             r.Artist,
			Album:              r.Album,
		})
	}

	// 2. FTS5 filename-token search for fuzzy basename
	// matches (e.g. different extension).
	ftsFileRows, err := s.db.SearchFTSByFilename(
		basename, maxCandidates,
	)
	if err != nil {
		s.logger.Warn(
			"FTS filename search failed",
			"basename", basename,
			"err", err,
		)
	}

	for _, r := range ftsFileRows {
		if _, ok := seen[r.FilePath]; ok {
			continue
		}

		seen[r.FilePath] = struct{}{}

		combined = append(combined, r)
	}

	// 3. FTS5 keyword search from path + display title.
	keywords := extractKeywords(phantomPath)

	if entry.DisplayTitle != "" {
		titleKeywords := extractKeywords(
			entry.DisplayTitle,
		)
		keywords = append(keywords, titleKeywords...)
		keywords = dedupStrings(keywords)
	}

	if len(keywords) > 0 {
		kwQuery := strings.Join(keywords, " ")

		kwRows, kwErr := s.db.SearchFTS(
			kwQuery, maxCandidates,
		)
		if kwErr != nil {
			s.logger.Warn(
				"FTS keyword search failed",
				"keywords", keywords,
				"err", kwErr,
			)
		}

		for _, r := range kwRows {
			if _, ok := seen[r.FilePath]; ok {
				continue
			}

			seen[r.FilePath] = struct{}{}

			combined = append(combined, r)
		}
	}

	// Score each candidate.
	pp := newPhantomProfile(
		phantomPath, entry.DisplayTitle,
		entry.DurationSec,
	)

	candidates := make(
		[]CandidateTrack, 0, len(combined),
	)

	for _, row := range combined {
		score := scoreCandidate(
			pp,
			row.FilePath,
			row.Title,
			row.Artist,
			row.LengthMilliseconds,
		)

		candidates = append(candidates, CandidateTrack{
			FilePath: row.FilePath,
			Title:    row.Title,
			Artist:   row.Artist,
			Album:    row.Album,
			Duration: strconv.FormatInt(
				row.LengthMilliseconds, 10,
			),
			Score: score,
		})
	}

	// Sort by score descending.
	sortCandidatesByScore(candidates)

	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	return candidates
}

// sortCandidatesByScore sorts candidates by score descending.
func sortCandidatesByScore(candidates []CandidateTrack) {
	slices.SortFunc(
		candidates,
		func(a, b CandidateTrack) int {
			if a.Score > b.Score {
				return -1
			}

			if a.Score < b.Score {
				return 1
			}

			return 0
		},
	)
}
