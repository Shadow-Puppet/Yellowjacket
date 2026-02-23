package library

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
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
	TrackNumber int64
	DiscNumber  int64
	Album       string
	Genre       []string
	Year        int64
	Composer    string
	FileType    string
}

// genreDelimiter is the separator used by GROUP_CONCAT in the
// GetAllTracksWithFullMetadata query.
const genreDelimiter = "||"

// splitGenres splits a GROUP_CONCAT genre string into individual
// genre names.  An empty string returns nil.
func splitGenres(concatenated string) []string {
	if concatenated == "" {
		return nil
	}

	return strings.Split(concatenated, genreDelimiter)
}

// Artist represents an artist in the library.
type Artist struct {
	ID   int64
	Name string
}

// Album represents an album for the cover grid display.
type Album struct {
	ID             int64
	Name           string
	ArtistName     string
	CoverArtPath   string
	CoverArtSmall  string
	CoverArtMedium string
	CoverArtLarge  string
	Year           int64
}

// GetAllTracks returns an array of track structs of every file in the library.
func (l *Library) GetAllTracks() ([]Track, error) {
	rows, err := l.db.Queries.GetAllTracksWithFullMetadata(
		l.ctx,
	)
	if err != nil {
		l.logger.Error(
			"could not retrieve audio files",
			"error", err,
		)

		return nil, err
	}

	l.logger.Info("audio file list", "count", len(rows))

	if len(rows) == 0 {
		l.logger.Error("no tracks in library")

		return nil, errNoTracksInLibrary
	}

	tracks := make([]Track, 0, len(rows))

	for _, row := range rows {
		track := Track{
			TrackName:  row.Title,
			ArtistName: row.ArtistName,
			TrackLength: strconv.FormatInt(
				row.LengthMilliseconds, 10,
			),
			FilePath:    row.FilePath,
			TrackNumber: row.TrackNumber.Int64,
			DiscNumber:  row.DiscNumber.Int64,
			Album:       row.Album,
			Genre:       splitGenres(row.Genre),
			Year:        row.Year,
			Composer:    row.Composer,
			FileType:    row.FileType,
		}

		tracks = append(tracks, track)
	}

	l.logger.Info("formatted tracks", "count", len(tracks))

	return tracks, nil
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
			TrackName:  row.Title,
			ArtistName: row.ArtistName,
			TrackLength: strconv.FormatInt(
				row.LengthMilliseconds,
				10,
			),
			FilePath:    row.FilePath,
			TrackNumber: row.TrackNumber.Int64,
			DiscNumber:  row.DiscNumber.Int64,
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

		// Convert filesystem path to URL path for the asset handler.
		if row.CoverArtPath != "" {
			base := filepath.Base(row.CoverArtPath)
			album.CoverArtPath = "/covers/" + base
			album.CoverArtSmall = "/covers/" +
				SizedFilename(base, "_sm")
			album.CoverArtMedium = "/covers/" +
				SizedFilename(base, "_md")
			album.CoverArtLarge = "/covers/" +
				SizedFilename(base, "_lg")
		}

		albums = append(albums, album)
	}

	return albums, nil
}

// GetAllArtists returns artists that are credited as album artists, ordered by name.
func (l *Library) GetAllArtists() ([]Artist, error) {
	rows, err := l.db.Queries.GetAlbumArtists(l.ctx)
	if err != nil {
		l.logger.Error(
			"could not retrieve artists",
			"error", err,
		)

		return nil, fmt.Errorf(
			"could not get artists: %w",
			err,
		)
	}

	l.logger.Info("artist list", "count", len(rows))

	artists := make([]Artist, 0, len(rows))

	for _, row := range rows {
		artists = append(artists, Artist{
			ID:   row.ID,
			Name: row.Name,
		})
	}

	return artists, nil
}

// GetAlbumsByArtist returns all albums where the given artist is the album artist.
func (l *Library) GetAlbumsByArtist(
	artistID int64,
) ([]Album, error) {
	rows, err := l.db.Queries.GetAlbumsByArtist(
		l.ctx,
		artistID,
	)
	if err != nil {
		l.logger.Error(
			"could not retrieve albums for artist",
			"artistID", artistID,
			"error", err,
		)

		return nil, fmt.Errorf(
			"could not get albums for artist: %w",
			err,
		)
	}

	l.logger.Info(
		"albums for artist",
		"artistID", artistID,
		"count", len(rows),
	)

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

		// Convert filesystem path to URL path for the asset handler.
		if row.CoverArtPath != "" {
			base := filepath.Base(row.CoverArtPath)
			album.CoverArtPath = "/covers/" + base
			album.CoverArtSmall = "/covers/" +
				SizedFilename(base, "_sm")
			album.CoverArtMedium = "/covers/" +
				SizedFilename(base, "_md")
			album.CoverArtLarge = "/covers/" +
				SizedFilename(base, "_lg")
		}

		albums = append(albums, album)
	}

	return albums, nil
}
