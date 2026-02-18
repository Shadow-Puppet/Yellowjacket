// Package playlist provides playlist management functionality.
package playlist

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/library"
)

var (
	errEmptyName     = errors.New("playlist name cannot be empty")
	errEmptyFilePath = errors.New("file path cannot be empty")
	errNoFilePaths   = errors.New("no file paths provided")
)

// Summary is a lightweight representation of a playlist for the picker UI.
type Summary struct {
	ID   int64  `json:"ID"`
	Name string `json:"Name"`
}

// Track represents a track within a playlist, including its metadata.
type Track struct {
	ID                    int64  `json:"ID"`
	Position              int64  `json:"Position"`
	FilePath              string `json:"FilePath"`
	Title                 string `json:"Title"`
	Artist                string `json:"Artist"`
	Album                 string `json:"Album"`
	CoverArtPath          string `json:"CoverArtPath"`
	CoverArtThumbnailPath string `json:"CoverArtThumbnailPath"`
	Duration              string `json:"Duration"`
}

// WithTracks contains a playlist summary and all its tracks.
type WithTracks struct {
	Summary Summary `json:"Summary"`
	Tracks  []Track `json:"Tracks"`
}

// Service manages playlist operations.
type Service struct {
	ctx    context.Context
	logger *slog.Logger
	db     *database.DB
}

// NewService creates a new playlist service.
func NewService(
	logger *slog.Logger,
	db *database.DB,
) *Service {
	return &Service{
		logger: logger.WithGroup("playlist"),
		db:     db,
	}
}

// SetContext sets the Wails runtime context.
func (s *Service) SetContext(ctx context.Context) {
	s.ctx = ctx
}

// GetAllPlaylists returns all playlists ordered by most recently updated.
func (s *Service) GetAllPlaylists() ([]Summary, error) {
	playlists, err := s.db.Queries.GetAllPlaylists(s.db.Ctx)
	if err != nil {
		s.logger.Error("Failed to get playlists", "err", err)

		return nil, fmt.Errorf("failed to get playlists: %w", err)
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

// GetAllPlaylistsWithTracks returns all playlists with their tracks in a single call.
func (s *Service) GetAllPlaylistsWithTracks() (
	[]WithTracks,
	error,
) {
	playlists, err := s.db.Queries.GetAllPlaylists(s.db.Ctx)
	if err != nil {
		s.logger.Error("Failed to get playlists", "err", err)

		return nil, fmt.Errorf("failed to get playlists: %w", err)
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

	// Group tracks by playlist ID.
	tracksByPlaylist := make(map[int64][]Track)

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

		tracksByPlaylist[row.PlaylistID] = append(
			tracksByPlaylist[row.PlaylistID],
			track,
		)
	}

	result := make([]WithTracks, 0, len(playlists))

	for _, p := range playlists {
		tracks := tracksByPlaylist[p.ID]
		if tracks == nil {
			tracks = []Track{}
		}

		result = append(result, WithTracks{
			Summary: Summary{ID: p.ID, Name: p.Name},
			Tracks:  tracks,
		})
	}

	return result, nil
}

// GetPlaylistTracks returns all tracks in a playlist with full metadata.
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

	tracks := make([]Track, 0, len(rows))

	for _, row := range rows {
		tracks = append(tracks, trackFromRow(
			row.ID,
			row.Position,
			row.FilePath,
			row.Title,
			row.Artist,
			row.Album,
			row.LengthMilliseconds,
			row.CoverArtPath,
		))
	}

	return tracks, nil
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
		track.CoverArtThumbnailPath = "/covers/" +
			library.ThumbnailFilename(base)
	}

	return track
}

// CreatePlaylist creates a new empty playlist with the given name.
func (s *Service) CreatePlaylist(name string) (Summary, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return Summary{}, errEmptyName
	}

	created, err := s.db.Queries.CreatePlaylist(s.db.Ctx, trimmed)
	if err != nil {
		s.logger.Error("Failed to create playlist", "name", trimmed, "err", err)

		return Summary{}, fmt.Errorf("failed to create playlist: %w", err)
	}

	s.logger.Info("Playlist created", "id", created.ID, "name", created.Name)

	return Summary{ID: created.ID, Name: created.Name}, nil
}

// AddTracksToPlaylist adds one or more tracks to an existing playlist.
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

		return fmt.Errorf("failed to get next track position: %w", err)
	}

	for i, fp := range filePaths {
		if err := s.addSingleTrack(playlistID, fp, nextPos+int64(i)); err != nil {
			return err
		}
	}

	s.logger.Info(
		"Tracks added to playlist",
		"playlistId", playlistID,
		"count", len(filePaths),
	)

	return nil
}

// CreatePlaylistWithTracks creates a new playlist and populates it with tracks.
func (s *Service) CreatePlaylistWithTracks(
	name string,
	filePaths []string,
) (Summary, error) {
	summary, err := s.CreatePlaylist(name)
	if err != nil {
		return Summary{}, err
	}

	if len(filePaths) > 0 {
		if err := s.AddTracksToPlaylist(summary.ID, filePaths); err != nil {
			return Summary{}, fmt.Errorf(
				"playlist created but failed to add tracks: %w",
				err,
			)
		}
	}

	return summary, nil
}

// RemoveTracksFromPlaylist removes multiple tracks from a playlist by their
// playlist_track IDs. Each track is removed individually using the existing
// RemovePlaylistTrack query.
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

	return nil
}

// addSingleTrack looks up the audio file by path and inserts it into the playlist.
func (s *Service) addSingleTrack(
	playlistID int64,
	filePath string,
	position int64,
) error {
	if strings.TrimSpace(filePath) == "" {
		return errEmptyFilePath
	}

	audioFile, err := s.db.Queries.GetAudioFileByPath(s.db.Ctx, filePath)
	if err != nil {
		s.logger.Error(
			"Failed to find audio file",
			"filePath", filePath,
			"err", err,
		)

		return fmt.Errorf("failed to find audio file %q: %w", filePath, err)
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

		return fmt.Errorf("failed to add track to playlist: %w", err)
	}

	return nil
}
