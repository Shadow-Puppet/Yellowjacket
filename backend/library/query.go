package library

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
)

// Sentinel errors for library queries.
var (
	errNoTracksInLibrary = errors.New("no tracks in library")
	errNoTracksForAlbum  = errors.New("no tracks found for album")
)

// Track represents a playable audio file in the library.
type Track struct {
	TrackName   string
	ArtistName  string
	TrackLength string
	FilePath    string
}

// Album represents an album for the cover grid display.
type Album struct {
	ID                    int64
	Name                  string
	ArtistName            string
	CoverArtPath          string
	CoverArtThumbnailPath string
	Year                  int64
}

// GetAllTracks returns an array of track structs of every file in the library.
func (l *Library) GetAllTracks() ([]Track, error) {
	audioFiles, err := l.db.Queries.GetAllAudioFilesWithArtist(l.ctx)
	if err != nil {
		l.logger.Error("could not retrieve audio files", "error", err)

		return nil, err
	}

	l.logger.Info("audio file list", "count", len(audioFiles))

	if len(audioFiles) == 0 {
		l.logger.Error("no tracks in library")

		return nil, errNoTracksInLibrary
	}

	var formattedTracks []Track

	for _, file := range audioFiles {
		track := Track{
			TrackName:   file.Title,
			ArtistName:  file.ArtistName,
			TrackLength: strconv.FormatInt(file.LengthMilliseconds, 10),
			FilePath:    file.FilePath,
		}
		formattedTracks = append(formattedTracks, track)
	}

	l.logger.Info("formatted tracks", "count", len(formattedTracks))

	return formattedTracks, nil
}

// GetAlbumTracks returns all tracks for a given album (release group), ordered by disc and track number.
func (l *Library) GetAlbumTracks(albumID int64) ([]Track, error) {
	rows, err := l.db.Queries.GetAudioFilesByReleaseGroup(l.ctx, albumID)
	if err != nil {
		l.logger.Error("could not retrieve album tracks", "albumID", albumID, "error", err)

		return nil, fmt.Errorf("could not get album tracks: %w", err)
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("%w %d", errNoTracksForAlbum, albumID)
	}

	tracks := make([]Track, 0, len(rows))

	for _, row := range rows {
		tracks = append(tracks, Track{
			TrackName:   row.Title,
			ArtistName:  row.ArtistName,
			TrackLength: strconv.FormatInt(row.LengthMilliseconds, 10),
			FilePath:    row.FilePath,
		})
	}

	return tracks, nil
}

// GetAllAlbums returns all albums with cover art and artist info for the cover grid.
func (l *Library) GetAllAlbums() ([]Album, error) {
	rows, err := l.db.Queries.GetAllAlbumsWithDetails(l.ctx)
	if err != nil {
		l.logger.Error("could not retrieve albums", "error", err)

		return nil, fmt.Errorf("could not get albums: %w", err)
	}

	l.logger.Info("album list", "count", len(rows))

	albums := make([]Album, 0, len(rows))

	for _, row := range rows {
		album := Album{
			ID:         row.ID,
			Name:       row.Name,
			ArtistName: row.ArtistName,
		}

		if row.Year.Valid {
			album.Year = row.Year.Int64
		}

		// Convert filesystem path to URL path for the asset handler
		if row.CoverArtPath != "" {
			base := filepath.Base(row.CoverArtPath)
			album.CoverArtPath = "/covers/" + base
			album.CoverArtThumbnailPath = "/covers/" + ThumbnailFilename(base)
		}

		albums = append(albums, album)
	}

	return albums, nil
}
