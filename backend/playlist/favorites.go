package playlist

import (
	"errors"
	"fmt"

	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/events"
	"yellowjacket/backend/favorites"
)

var errNoDefaultPlaylist = errors.New(
	"no default playlist configured",
)

// FavoritesConfigProvider is a narrow interface for reading and
// writing the default-playlist configuration.
type FavoritesConfigProvider interface {
	GetFavoritesPlaylistID() int64
	SetFavoritesPlaylistID(id int64) error
	GetFavoritesIconStyle() string
}

// EnsureDefaultPlaylist verifies the configured default playlist
// exists in the database.  If the playlist is missing or no ID
// has been configured yet, a new playlist named "Favorites" is
// created and the config is updated.
func (s *Service) EnsureDefaultPlaylist() {
	if s.favoritesConf == nil {
		s.logger.Warn(
			"No favorites config provider, skipping",
		)

		return
	}

	id := s.favoritesConf.GetFavoritesPlaylistID()

	// Check whether the playlist still exists.
	if id > 0 {
		_, err := s.db.Queries.GetPlaylist(
			s.db.Ctx, id,
		)
		if err == nil {
			return // Playlist exists, nothing to do.
		}

		s.logger.Warn(
			"Default playlist not found, recreating",
			"configuredId", id,
		)
	}

	// Create a fresh default playlist.
	created, err := s.db.Queries.CreatePlaylist(
		s.db.Ctx, favorites.DefaultPlaylistName,
	)
	if err != nil {
		s.logger.Error(
			"Failed to create default playlist",
			"err", err,
		)

		return
	}

	s.savePlaylistFile(created.ID, created.Name)

	if setErr := s.favoritesConf.SetFavoritesPlaylistID(
		created.ID,
	); setErr != nil {
		s.logger.Error(
			"Failed to save default playlist ID",
			"err", setErr,
		)
	}

	s.logger.Info(
		"Default playlist created",
		"id", created.ID,
		"name", created.Name,
	)

	s.emitEvent(events.PlaylistCreated, Summary{
		ID: created.ID, Name: created.Name,
	})
}

// GetDefaultPlaylistTrackPaths returns the file paths of all
// tracks in the default playlist.
func (s *Service) GetDefaultPlaylistTrackPaths() (
	[]string,
	error,
) {
	id := s.defaultPlaylistID()
	if id == 0 {
		return []string{}, nil
	}

	paths, err := s.db.Queries.GetPlaylistTrackFilePaths(
		s.db.Ctx, id,
	)
	if err != nil {
		s.logger.Error(
			"Failed to get default playlist paths",
			"playlistId", id,
			"err", err,
		)

		return nil, fmt.Errorf(
			"failed to get default playlist paths: %w",
			err,
		)
	}

	if paths == nil {
		paths = []string{}
	}

	return paths, nil
}

// GetDefaultPlaylistInfo returns the ID and name of the default
// playlist for display in the frontend.
func (s *Service) GetDefaultPlaylistInfo() (
	Summary,
	error,
) {
	id := s.defaultPlaylistID()
	if id == 0 {
		return Summary{}, nil
	}

	pl, err := s.db.Queries.GetPlaylist(s.db.Ctx, id)
	if err != nil {
		return Summary{}, fmt.Errorf(
			"failed to get default playlist: %w", err,
		)
	}

	return Summary{ID: pl.ID, Name: pl.Name}, nil
}

// ToggleDefaultPlaylistTrack adds or removes a single track
// from the default playlist.  Returns true if the track is now
// in the playlist (was added), false if it was removed.
func (s *Service) ToggleDefaultPlaylistTrack(
	filePath string,
) (bool, error) {
	id := s.defaultPlaylistID()
	if id == 0 {
		return false, errNoDefaultPlaylist
	}

	inPlaylist, err := s.db.Queries.IsTrackInPlaylist(
		s.db.Ctx,
		sqlcgen.IsTrackInPlaylistParams{
			PlaylistID: id,
			FilePath:   filePath,
		},
	)
	if err != nil {
		return false, fmt.Errorf(
			"failed to check playlist membership: %w",
			err,
		)
	}

	if inPlaylist != 0 {
		// Remove.
		if rmErr := s.db.Queries.RemovePlaylistTrackByPath(
			s.db.Ctx,
			sqlcgen.RemovePlaylistTrackByPathParams{
				PlaylistID: id,
				FilePath:   filePath,
			},
		); rmErr != nil {
			return false, fmt.Errorf(
				"failed to remove track: %w", rmErr,
			)
		}

		s.savePlaylistFileByID(id)
		s.emitEvent(
			events.DefaultPlaylistChanged,
			map[string]any{
				"filePath": filePath,
				"added":    false,
			},
		)
		s.emitEvent(events.PlaylistTracksChanged, id)

		return false, nil
	}

	// Add.
	nextPos, posErr := s.db.Queries.GetNextPlaylistTrackPosition(
		s.db.Ctx, id,
	)
	if posErr != nil {
		return false, fmt.Errorf(
			"failed to get next position: %w", posErr,
		)
	}

	if addErr := s.addSingleTrack(
		id, filePath, nextPos,
	); addErr != nil {
		return false, fmt.Errorf(
			"failed to add track: %w", addErr,
		)
	}

	s.savePlaylistFileByID(id)
	s.emitEvent(
		events.DefaultPlaylistChanged,
		map[string]any{
			"filePath": filePath,
			"added":    true,
		},
	)
	s.emitEvent(events.PlaylistTracksChanged, id)

	return true, nil
}

// AddToDefaultPlaylist adds multiple tracks to the default
// playlist, skipping any that are already present.
func (s *Service) AddToDefaultPlaylist(
	filePaths []string,
) error {
	id := s.defaultPlaylistID()
	if id == 0 {
		return errNoDefaultPlaylist
	}

	nextPos, err := s.db.Queries.GetNextPlaylistTrackPosition(
		s.db.Ctx, id,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to get next position: %w", err,
		)
	}

	var added int

	for _, fp := range filePaths {
		inPlaylist, chkErr := s.db.Queries.IsTrackInPlaylist(
			s.db.Ctx,
			sqlcgen.IsTrackInPlaylistParams{
				PlaylistID: id,
				FilePath:   fp,
			},
		)
		if chkErr != nil {
			s.logger.Warn(
				"Could not check playlist membership",
				"filePath", fp,
				"err", chkErr,
			)

			continue
		}

		if inPlaylist != 0 {
			continue
		}

		if addErr := s.addSingleTrack(
			id, fp, nextPos+int64(added),
		); addErr != nil {
			s.logger.Warn(
				"Could not add track to default playlist",
				"filePath", fp,
				"err", addErr,
			)

			continue
		}

		added++
	}

	if added > 0 {
		s.savePlaylistFileByID(id)
		s.emitEvent(
			events.DefaultPlaylistChanged, nil,
		)
		s.emitEvent(events.PlaylistTracksChanged, id)
	}

	return nil
}

// RemoveFromDefaultPlaylist removes multiple tracks from the
// default playlist.
func (s *Service) RemoveFromDefaultPlaylist(
	filePaths []string,
) error {
	id := s.defaultPlaylistID()
	if id == 0 {
		return errNoDefaultPlaylist
	}

	var removed int

	for _, fp := range filePaths {
		rmErr := s.db.Queries.RemovePlaylistTrackByPath(
			s.db.Ctx,
			sqlcgen.RemovePlaylistTrackByPathParams{
				PlaylistID: id,
				FilePath:   fp,
			},
		)
		if rmErr != nil {
			s.logger.Warn(
				"Could not remove track from default playlist",
				"filePath", fp,
				"err", rmErr,
			)

			continue
		}

		removed++
	}

	if removed > 0 {
		s.savePlaylistFileByID(id)
		s.emitEvent(
			events.DefaultPlaylistChanged, nil,
		)
		s.emitEvent(events.PlaylistTracksChanged, id)
	}

	return nil
}

// defaultPlaylistID returns the configured default playlist ID,
// or 0 if not configured.
func (s *Service) defaultPlaylistID() int64 {
	if s.favoritesConf == nil {
		return 0
	}

	return s.favoritesConf.GetFavoritesPlaylistID()
}
