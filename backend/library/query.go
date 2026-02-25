package library

import (
	"database/sql"
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
	SampleRate  int64
	BitDepth    int64
	Channels    int64
	Bitrate     int64
	FileSize    int64
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

// mapTrackRow converts raw database column values into a Track.
// This is shared by GetAllTracks, SearchTracks, and GetTracksByGenre
// to avoid tripling the row-mapping code.
func mapTrackRow(
	filePath string,
	lengthMs int64,
	title, artistName string,
	trackNumber, discNumber sql.NullInt64,
	album, genre string,
	year int64,
	composer, fileType string,
	sampleRate, bitDepth, channels, bitrate, fileSize int64,
) Track {
	return Track{
		TrackName:   title,
		ArtistName:  artistName,
		TrackLength: strconv.FormatInt(lengthMs, 10),
		FilePath:    filePath,
		TrackNumber: trackNumber.Int64,
		DiscNumber:  discNumber.Int64,
		Album:       album,
		Genre:       splitGenres(genre),
		Year:        year,
		Composer:    composer,
		FileType:    fileType,
		SampleRate:  sampleRate,
		BitDepth:    bitDepth,
		Channels:    channels,
		Bitrate:     bitrate,
		FileSize:    fileSize,
	}
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
		tracks = append(tracks, mapTrackRow(
			row.FilePath,
			row.LengthMilliseconds,
			row.Title,
			row.ArtistName,
			row.TrackNumber,
			row.DiscNumber,
			row.Album,
			row.Genre,
			row.Year,
			row.Composer,
			row.FileType,
			row.SampleRate,
			row.BitDepth,
			row.Channels,
			row.Bitrate,
			row.FileSize,
		))
	}

	l.logger.Info("formatted tracks", "count", len(tracks))

	return tracks, nil
}

// searchTrackLimit is the maximum number of results returned by
// a full-text search.
const searchTrackLimit = 200

// SearchTracks performs an FTS5 full-text search and returns
// matching tracks with full metadata.
func (l *Library) SearchTracks(
	query string,
) ([]Track, error) {
	rows, err := l.db.SearchFTSTracks(
		query, searchTrackLimit,
	)
	if err != nil {
		l.logger.Error(
			"FTS track search failed",
			"query", query,
			"error", err,
		)

		return nil, fmt.Errorf(
			"search tracks failed: %w", err,
		)
	}

	tracks := make([]Track, 0, len(rows))

	for _, row := range rows {
		tracks = append(tracks, mapTrackRow(
			row.FilePath,
			row.LengthMilliseconds,
			row.Title,
			row.ArtistName,
			row.TrackNumber,
			row.DiscNumber,
			row.Album,
			row.Genre,
			row.Year,
			row.Composer,
			row.FileType,
			row.SampleRate,
			row.BitDepth,
			row.Channels,
			row.Bitrate,
			row.FileSize,
		))
	}

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

// GenreWithCount holds a genre name and its associated track count.
type GenreWithCount struct {
	Name       string `json:"Name"`
	TrackCount int64  `json:"TrackCount"`
}

// GetTracksByGenre returns all tracks tagged with the given genre.
func (l *Library) GetTracksByGenre(
	genreName string,
) ([]Track, error) {
	rows, err := l.db.Queries.GetTracksByGenre(
		l.ctx, genreName,
	)
	if err != nil {
		l.logger.Error(
			"could not retrieve tracks for genre",
			"genre", genreName,
			"error", err,
		)

		return nil, fmt.Errorf(
			"could not get tracks for genre: %w", err,
		)
	}

	tracks := make([]Track, 0, len(rows))

	for _, row := range rows {
		tracks = append(tracks, mapTrackRow(
			row.FilePath,
			row.LengthMilliseconds,
			row.Title,
			row.ArtistName,
			row.TrackNumber,
			row.DiscNumber,
			row.Album,
			row.Genre,
			row.Year,
			row.Composer,
			row.FileType,
			row.SampleRate,
			row.BitDepth,
			row.Channels,
			row.Bitrate,
			row.FileSize,
		))
	}

	return tracks, nil
}

// GetAllGenresWithCounts returns all genres with their track counts.
func (l *Library) GetAllGenresWithCounts() (
	[]GenreWithCount, error,
) {
	rows, err := l.db.Queries.GetAllGenresWithCounts(
		l.ctx,
	)
	if err != nil {
		l.logger.Error(
			"could not retrieve genres with counts",
			"error", err,
		)

		return nil, fmt.Errorf(
			"could not get genres: %w", err,
		)
	}

	genres := make([]GenreWithCount, 0, len(rows))

	for _, row := range rows {
		genres = append(genres, GenreWithCount{
			Name:       row.Name,
			TrackCount: row.TrackCount,
		})
	}

	return genres, nil
}
