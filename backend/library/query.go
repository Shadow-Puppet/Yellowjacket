package library

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"yellowjacket/backend/coverart"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/system"
)

// Sentinel errors for library queries.
var (
	errNoTracksInLibrary = errors.New("no tracks in library")
	errNoTracksForAlbum  = errors.New("no tracks found for album")
)

// Track represents a playable audio file in the library.
type Track struct {
	TrackName        string
	ArtistName       string
	TrackLength      string
	FilePath         string
	TrackNumber      int64
	DiscNumber       int64
	Album            string
	Genre            []string
	Year             int64
	Composer         string
	FileType         string
	SampleRate       int64
	BitDepth         int64
	Channels         int64
	Bitrate          int64
	FileSize         int64
	PlayCount        int64
	LastPlayed       string
	RecordingMBID    string
	ArtistMBID       string
	ReleaseGroupMBID string
	CoverArtPath     string
	CoverArtSmall    string
	CoverArtMedium   string
	CoverArtLarge    string
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
	playCount int64,
	lastPlayed sql.NullTime,
	coverArtPath string,
	artistMBID, releaseGroupMBID, recordingMBID string,
) Track {
	var lastPlayedStr string
	if lastPlayed.Valid {
		lastPlayedStr = lastPlayed.Time.Format(time.DateTime)
	}

	t := Track{
		TrackName:        title,
		ArtistName:       artistName,
		TrackLength:      strconv.FormatInt(lengthMs, 10),
		FilePath:         filePath,
		TrackNumber:      trackNumber.Int64,
		DiscNumber:       discNumber.Int64,
		Album:            album,
		Genre:            splitGenres(genre),
		Year:             year,
		Composer:         composer,
		FileType:         fileType,
		SampleRate:       sampleRate,
		BitDepth:         bitDepth,
		Channels:         channels,
		Bitrate:          bitrate,
		FileSize:         fileSize,
		PlayCount:        playCount,
		LastPlayed:       lastPlayedStr,
		ArtistMBID:       artistMBID,
		ReleaseGroupMBID: releaseGroupMBID,
		RecordingMBID:    recordingMBID,
	}

	if coverArtPath != "" {
		urls := coverart.ResolveURLs(coverArtPath)
		t.CoverArtPath = urls.Original
		t.CoverArtSmall = urls.Small
		t.CoverArtMedium = urls.Medium
		t.CoverArtLarge = urls.Large
	}

	return t
}

// TrackMBIDs holds MusicBrainz identifiers for a track, resolved
// from the recording, release group, and artist tables.
type TrackMBIDs struct {
	RecordingMBID    string `json:"recordingMbid"`
	ReleaseGroupMBID string `json:"releaseGroupMbid"`
	ArtistMBID       string `json:"artistMbid"`
}

// GetTrackMBIDs returns the MusicBrainz IDs for the track at the
// given file path.  Returns empty strings for entities without MBIDs.
func (l *Library) GetTrackMBIDs(filePath string) TrackMBIDs {
	rows, err := l.db.QueryContext(`
		SELECT
			COALESCE(r.mbid, '') AS recording_mbid,
			COALESCE(rg.mbid, '') AS release_group_mbid,
			COALESCE(a.mbid, '') AS artist_mbid
		FROM audio_files af
		JOIN recordings r ON af.recording_id = r.id
		JOIN artist_credit ac ON r.artist_credit_id = ac.id
		JOIN artist_credit_artist aca ON aca.credit_id = ac.id
		JOIN artists a ON a.id = aca.artist_id
		LEFT JOIN release_group_recordings rgr ON r.id = rgr.recording_id
		LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
		WHERE af.file_path = ?
		LIMIT 1
	`, filePath)
	if err != nil {
		return TrackMBIDs{}
	}

	defer func() { _ = rows.Close() }()

	var result TrackMBIDs

	if rows.Next() {
		_ = rows.Scan(&result.RecordingMBID, &result.ReleaseGroupMBID, &result.ArtistMBID)
	}

	return result
}

// Artist represents an artist in the library.
type Artist struct {
	ID          int64
	Name        string
	MBID        string
	ImageSmall  string
	ImageMedium string
	ImageLarge  string
}

// Album represents an album for the cover grid display.
//
// Year is the album's preferred display year — the release-group's
// original-release-date (MusicBrainz first-release-date) when known,
// falling back to the file-tag year.  ReleaseYear is the file-tag
// year of the specific release in the library; for a 2010 remaster
// of a 1973 album, Year=1973 and ReleaseYear=2010.
type Album struct {
	ID             int64
	Name           string
	ArtistName     string
	ArtistMBID     string
	MBID           string
	CoverArtPath   string
	CoverArtSmall  string
	CoverArtMedium string
	CoverArtLarge  string
	Year           int64
	ReleaseYear    int64
}

// GetAllTracks returns an array of track structs of every file in the library.
func (l *Library) GetAllTracks() ([]Track, error) {
	rows, err := l.db.ReadQueries.GetAllTracksWithFullMetadata(
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
			row.PlayCount,
			row.LastPlayed,
			row.CoverArtPath,
			row.ArtistMbid,
			row.ReleaseGroupMbid,
			row.RecordingMbid,
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
			0, sql.NullTime{},
			"",
			"", "", "",
		))
	}

	return tracks, nil
}

// GetAlbumTracks returns all tracks for a given album (release group), ordered by disc and track number.
func (l *Library) GetAlbumTracks(albumID int64) ([]Track, error) {
	rows, err := l.db.ReadQueries.GetAudioFilesByReleaseGroup(l.ctx, albumID)
	if err != nil {
		l.logger.Error("could not retrieve album tracks", "albumID", albumID, "error", err)

		return nil, fmt.Errorf("could not get album tracks: %w", err)
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("%w %d", errNoTracksForAlbum, albumID)
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
			0, sql.NullTime{},
			"",
			row.ArtistMbid,
			row.ReleaseGroupMbid,
			row.RecordingMbid,
		))
	}

	return tracks, nil
}

// GetAllAlbums returns all albums with cover art and artist info for the cover grid.
func (l *Library) GetAllAlbums() ([]Album, error) {
	rows, err := l.db.ReadQueries.GetAllAlbumsWithDetails(l.ctx)
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
			ArtistMBID: row.ArtistMbid,
		}

		if row.Year.Valid {
			album.Year = row.Year.Int64
		}

		album.ReleaseYear = row.ReleaseYear

		if row.Mbid.Valid {
			album.MBID = row.Mbid.String
		}

		// Convert filesystem path to URL path for the asset handler.
		if row.CoverArtPath != "" {
			urls := coverart.ResolveURLs(row.CoverArtPath)
			album.CoverArtPath = urls.Original
			album.CoverArtSmall = urls.Small
			album.CoverArtMedium = urls.Medium
			album.CoverArtLarge = urls.Large
		}

		albums = append(albums, album)
	}

	return albums, nil
}

// GetAllArtists returns artists that are credited as album artists, ordered by name.
func (l *Library) GetAllArtists() ([]Artist, error) {
	rows, err := l.db.ReadQueries.GetAlbumArtists(l.ctx)
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
		a := Artist{
			ID:   row.ID,
			Name: row.Name,
		}

		if row.Mbid.Valid {
			a.MBID = row.Mbid.String
		}

		artists = append(artists, a)
	}

	// Resolve artist image URLs from the disk cache.
	l.resolveArtistImages(artists)

	return artists, nil
}

// resolveArtistImages populates ImageSmall/Medium/Large for artists
// that have cached images on disk.  Does a bulk MBID lookup from the
// artists table, then checks the artist-images directory for each.
func (l *Library) resolveArtistImages(artists []Artist) {
	if len(artists) == 0 {
		return
	}

	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		return
	}

	baseDir := filepath.Join(dataDir, "artist-images")

	// Bulk load name→mbid from the artists table.
	rows, err := l.db.QueryContext(
		"SELECT name, mbid FROM artists WHERE mbid IS NOT NULL AND mbid != ''",
	)
	if err != nil {
		return
	}

	defer func() { _ = rows.Close() }()

	mbidMap := make(map[string]string)

	for rows.Next() {
		var name, mbid string
		if err := rows.Scan(&name, &mbid); err == nil {
			mbidMap[name] = mbid
		}
	}

	for i := range artists {
		mbid, ok := mbidMap[artists[i].Name]
		if !ok || len(mbid) < 2 {
			continue
		}

		dir := filepath.Join(baseDir, mbid[:2], mbid)
		prefix := "/artist-images/" + mbid[:2] + "/" + mbid + "/"

		if _, err := os.Stat(filepath.Join(dir, "primary_sm.jpg")); err == nil {
			artists[i].ImageSmall = prefix + "primary_sm.jpg"
		}

		if _, err := os.Stat(filepath.Join(dir, "primary_md.jpg")); err == nil {
			artists[i].ImageMedium = prefix + "primary_md.jpg"
		}

		if _, err := os.Stat(filepath.Join(dir, "primary_lg.jpg")); err == nil {
			artists[i].ImageLarge = prefix + "primary_lg.jpg"
		}
	}
}

// GetAlbumsByArtist returns all albums where the given artist is the album artist.
func (l *Library) GetAlbumsByArtist(
	artistID int64,
) ([]Album, error) {
	rows, err := l.db.ReadQueries.GetAlbumsByArtist(
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
			ArtistMBID: row.ArtistMbid,
		}

		if row.Year.Valid {
			album.Year = row.Year.Int64
		}

		album.ReleaseYear = row.ReleaseYear

		// Convert filesystem path to URL path for the asset handler.
		if row.CoverArtPath != "" {
			urls := coverart.ResolveURLs(row.CoverArtPath)
			album.CoverArtPath = urls.Original
			album.CoverArtSmall = urls.Small
			album.CoverArtMedium = urls.Medium
			album.CoverArtLarge = urls.Large
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
	rows, err := l.db.ReadQueries.GetTracksByGenre(
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
			0, sql.NullTime{},
			"",
			"", "", "",
		))
	}

	return tracks, nil
}

// GetAllGenresWithCounts returns all genres with their track counts.
func (l *Library) GetAllGenresWithCounts() (
	[]GenreWithCount, error,
) {
	rows, err := l.db.ReadQueries.GetAllGenresWithCounts(
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

// GetAllTracksByLibrary returns tracks scoped to a specific library.
func (l *Library) GetAllTracksByLibrary(
	libraryID int64,
) ([]Track, error) {
	rows, err := l.db.ReadQueries.GetAllTracksWithFullMetadataByLibrary(
		l.ctx, libraryID,
	)
	if err != nil {
		l.logger.Error(
			"could not retrieve tracks for library",
			"libraryID", libraryID,
			"error", err,
		)

		return nil, fmt.Errorf(
			"could not get tracks for library: %w", err,
		)
	}

	l.logger.Info(
		"tracks for library",
		"libraryID", libraryID,
		"count", len(rows),
	)

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
			row.PlayCount,
			row.LastPlayed,
			row.CoverArtPath,
			row.ArtistMbid,
			row.ReleaseGroupMbid,
			row.RecordingMbid,
		))
	}

	return tracks, nil
}

// GetAllAlbumsByLibrary returns albums that have tracks in the given library.
func (l *Library) GetAllAlbumsByLibrary(
	libraryID int64,
) ([]Album, error) {
	rows, err := l.db.ReadQueries.GetAllAlbumsWithDetailsByLibrary(
		l.ctx, libraryID,
	)
	if err != nil {
		l.logger.Error(
			"could not retrieve albums for library",
			"libraryID", libraryID,
			"error", err,
		)

		return nil, fmt.Errorf(
			"could not get albums for library: %w", err,
		)
	}

	l.logger.Info(
		"albums for library",
		"libraryID", libraryID,
		"count", len(rows),
	)

	albums := make([]Album, 0, len(rows))

	for _, row := range rows {
		album := Album{
			ID:         row.ID,
			Name:       row.Name,
			ArtistName: row.ArtistName,
			ArtistMBID: row.ArtistMbid,
		}

		if row.Year.Valid {
			album.Year = row.Year.Int64
		}

		album.ReleaseYear = row.ReleaseYear

		if row.Mbid.Valid {
			album.MBID = row.Mbid.String
		}

		if row.CoverArtPath != "" {
			urls := coverart.ResolveURLs(row.CoverArtPath)
			album.CoverArtPath = urls.Original
			album.CoverArtSmall = urls.Small
			album.CoverArtMedium = urls.Medium
			album.CoverArtLarge = urls.Large
		}

		albums = append(albums, album)
	}

	return albums, nil
}

// GetAllArtistsByLibrary returns artists that have albums with tracks
// in the given library.
func (l *Library) GetAllArtistsByLibrary(
	libraryID int64,
) ([]Artist, error) {
	rows, err := l.db.ReadQueries.GetAlbumArtistsByLibrary(
		l.ctx, libraryID,
	)
	if err != nil {
		l.logger.Error(
			"could not retrieve artists for library",
			"libraryID", libraryID,
			"error", err,
		)

		return nil, fmt.Errorf(
			"could not get artists for library: %w", err,
		)
	}

	l.logger.Info(
		"artists for library",
		"libraryID", libraryID,
		"count", len(rows),
	)

	artists := make([]Artist, 0, len(rows))

	for _, row := range rows {
		a := Artist{
			ID:   row.ID,
			Name: row.Name,
		}

		if row.Mbid.Valid {
			a.MBID = row.Mbid.String
		}

		artists = append(artists, a)
	}

	l.resolveArtistImages(artists)

	return artists, nil
}

// GetAlbumsByArtistByLibrary returns albums for the given artist
// that have tracks in the given library.
func (l *Library) GetAlbumsByArtistByLibrary(
	artistID, libraryID int64,
) ([]Album, error) {
	rows, err := l.db.ReadQueries.GetAlbumsByArtistByLibrary(
		l.ctx, sqlcgen.GetAlbumsByArtistByLibraryParams{
			ArtistID:  artistID,
			LibraryID: libraryID,
		},
	)
	if err != nil {
		l.logger.Error(
			"could not retrieve albums for artist in library",
			"artistID", artistID,
			"libraryID", libraryID,
			"error", err,
		)

		return nil, fmt.Errorf(
			"could not get albums for artist in library: %w",
			err,
		)
	}

	l.logger.Info(
		"albums for artist in library",
		"artistID", artistID,
		"libraryID", libraryID,
		"count", len(rows),
	)

	albums := make([]Album, 0, len(rows))

	for _, row := range rows {
		album := Album{
			ID:         row.ID,
			Name:       row.Name,
			ArtistName: row.ArtistName,
			ArtistMBID: row.ArtistMbid,
		}

		if row.Year.Valid {
			album.Year = row.Year.Int64
		}

		album.ReleaseYear = row.ReleaseYear

		if row.CoverArtPath != "" {
			urls := coverart.ResolveURLs(row.CoverArtPath)
			album.CoverArtPath = urls.Original
			album.CoverArtSmall = urls.Small
			album.CoverArtMedium = urls.Medium
			album.CoverArtLarge = urls.Large
		}

		albums = append(albums, album)
	}

	return albums, nil
}

// GetAllGenresWithCountsByLibrary returns genres with track counts
// scoped to the given library.
func (l *Library) GetAllGenresWithCountsByLibrary(
	libraryID int64,
) ([]GenreWithCount, error) {
	rows, err := l.db.ReadQueries.GetAllGenresWithCountsByLibrary(
		l.ctx, libraryID,
	)
	if err != nil {
		l.logger.Error(
			"could not retrieve genres for library",
			"libraryID", libraryID,
			"error", err,
		)

		return nil, fmt.Errorf(
			"could not get genres for library: %w", err,
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

// GetTracksByGenreByLibrary returns tracks tagged with the given
// genre, scoped to the given library.
func (l *Library) GetTracksByGenreByLibrary(
	genreName string, libraryID int64,
) ([]Track, error) {
	rows, err := l.db.ReadQueries.GetTracksByGenreByLibrary(
		l.ctx, sqlcgen.GetTracksByGenreByLibraryParams{
			Name:      genreName,
			LibraryID: libraryID,
		},
	)
	if err != nil {
		l.logger.Error(
			"could not retrieve tracks for genre in library",
			"genre", genreName,
			"libraryID", libraryID,
			"error", err,
		)

		return nil, fmt.Errorf(
			"could not get tracks for genre in library: %w",
			err,
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
			0, sql.NullTime{},
			"",
			"", "", "",
		))
	}

	return tracks, nil
}

// GetAlbumTracksByLibrary returns tracks for the given album,
// scoped to the given library.
func (l *Library) GetAlbumTracksByLibrary(
	albumID, libraryID int64,
) ([]Track, error) {
	rows, err := l.db.ReadQueries.GetAudioFilesByReleaseGroupByLibrary(
		l.ctx, sqlcgen.GetAudioFilesByReleaseGroupByLibraryParams{
			ReleaseGroupID: albumID,
			LibraryID:      libraryID,
		},
	)
	if err != nil {
		l.logger.Error(
			"could not retrieve album tracks for library",
			"albumID", albumID,
			"libraryID", libraryID,
			"error", err,
		)

		return nil, fmt.Errorf(
			"could not get album tracks for library: %w",
			err,
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
			0, sql.NullTime{},
			"",
			row.ArtistMbid,
			row.ReleaseGroupMbid,
			row.RecordingMbid,
		))
	}

	return tracks, nil
}

// SearchTracksByLibrary performs an FTS5 search scoped to a specific
// library and returns matching tracks with full metadata.
func (l *Library) SearchTracksByLibrary(
	query string, libraryID int64,
) ([]Track, error) {
	rows, err := l.db.SearchFTSTracksByLibrary(
		query, searchTrackLimit, libraryID,
	)
	if err != nil {
		l.logger.Error(
			"FTS library track search failed",
			"query", query,
			"libraryID", libraryID,
			"error", err,
		)

		return nil, fmt.Errorf(
			"search tracks by library failed: %w", err,
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
			0, sql.NullTime{},
			"",
			"", "", "",
		))
	}

	return tracks, nil
}

// Info contains library metadata enriched with track count
// for the frontend settings UI.
type Info struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	TrackCount int64  `json:"trackCount"`
}

// GetAllLibrariesWithTrackCounts returns all libraries with their
// audio file counts. Typically 1-5 libraries so the loop is trivial.
func (l *Library) GetAllLibrariesWithTrackCounts() ([]Info, error) {
	libs, err := l.db.ReadQueries.GetAllLibraries(l.ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get libraries: %w", err)
	}

	result := make([]Info, 0, len(libs))

	for _, lib := range libs {
		count, countErr := l.db.ReadQueries.CountAudioFilesByLibrary(l.ctx, lib.ID)
		if countErr != nil {
			l.logger.Error("could not count tracks for library",
				"libraryID", lib.ID, "error", countErr)

			count = 0
		}

		result = append(result, Info{
			ID:         lib.ID,
			Name:       lib.Name,
			Path:       lib.Path,
			TrackCount: count,
		})
	}

	return result, nil
}

// GetFilePathsByAlbums returns the file paths of every track in the
// given albums, grouped by album id.
//
// "Play this artist", "play these albums" and the album drag cache each
// resolved paths with one binding call per album, sequentially, and each
// asked for whole track rows to read one field off them (perf.m2).  This
// is that question asked once.  The result is grouped rather than
// flattened because the caller owns the ordering — an album list is
// sorted by name, not by id — and because the drag cache stores it per
// album.
//
// A library id of 0 means "every library", matching the caller's
// selected-library filter being unset.
func (l *Library) GetFilePathsByAlbums(
	albumIDs []int64, libraryID int64,
) (map[int64][]string, error) {
	paths := make(map[int64][]string, len(albumIDs))

	if len(albumIDs) == 0 {
		return paths, nil
	}

	if libraryID > 0 {
		rows, err := l.db.ReadQueries.GetFilePathsByReleaseGroupsByLibrary(
			l.ctx, sqlcgen.GetFilePathsByReleaseGroupsByLibraryParams{
				ReleaseGroupIds: albumIDs,
				LibraryID:       libraryID,
			},
		)
		if err != nil {
			l.logger.Error(
				"could not retrieve album file paths for library",
				"albums", len(albumIDs),
				"libraryID", libraryID,
				"error", err,
			)

			return nil, fmt.Errorf("could not get album file paths: %w", err)
		}

		for _, row := range rows {
			paths[row.ReleaseGroupID] = append(paths[row.ReleaseGroupID], row.FilePath)
		}

		return paths, nil
	}

	rows, err := l.db.ReadQueries.GetFilePathsByReleaseGroups(l.ctx, albumIDs)
	if err != nil {
		l.logger.Error(
			"could not retrieve album file paths",
			"albums", len(albumIDs),
			"error", err,
		)

		return nil, fmt.Errorf("could not get album file paths: %w", err)
	}

	for _, row := range rows {
		paths[row.ReleaseGroupID] = append(paths[row.ReleaseGroupID], row.FilePath)
	}

	return paths, nil
}

// GetFilePathsByGenres returns the file paths of every track tagged with
// the given genres, grouped by genre name.  See GetFilePathsByAlbums —
// same finding, same shape, and the caller still owns the de-duplication
// across genres because it owns the order.
func (l *Library) GetFilePathsByGenres(
	genreNames []string, libraryID int64,
) (map[string][]string, error) {
	paths := make(map[string][]string, len(genreNames))

	if len(genreNames) == 0 {
		return paths, nil
	}

	if libraryID > 0 {
		rows, err := l.db.ReadQueries.GetFilePathsByGenresByLibrary(
			l.ctx, sqlcgen.GetFilePathsByGenresByLibraryParams{
				GenreNames: genreNames,
				LibraryID:  libraryID,
			},
		)
		if err != nil {
			l.logger.Error(
				"could not retrieve genre file paths for library",
				"genres", len(genreNames),
				"libraryID", libraryID,
				"error", err,
			)

			return nil, fmt.Errorf("could not get genre file paths: %w", err)
		}

		for _, row := range rows {
			paths[row.GenreName] = append(paths[row.GenreName], row.FilePath)
		}

		return paths, nil
	}

	rows, err := l.db.ReadQueries.GetFilePathsByGenres(l.ctx, genreNames)
	if err != nil {
		l.logger.Error(
			"could not retrieve genre file paths",
			"genres", len(genreNames),
			"error", err,
		)

		return nil, fmt.Errorf("could not get genre file paths: %w", err)
	}

	for _, row := range rows {
		paths[row.GenreName] = append(paths[row.GenreName], row.FilePath)
	}

	return paths, nil
}

// GetFilePathsByRecordingMBIDs returns the file paths of every track
// whose recording MBID is in mbids, grouped by MBID.
//
// This is the catalog side of GetFilePathsByAlbums.  An Explore album
// page knows what the user owns as a set of recording MBIDs and nothing
// else: that is exactly how the backend decides a track's InLibrary
// flag (markReleasesInLibrary → CheckMBIDs), and MBTrack.LocalID is a
// declared field that nothing writes, so there is no id to ask by.
//
// Grouped rather than flattened for the same two reasons as its
// siblings — the caller owns the order (the tracklist's, not the
// database's), and one recording can have more than one file, which is
// what this app's duplicate detection exists for.
//
// A library id of 0 means "every library".
func (l *Library) GetFilePathsByRecordingMBIDs(
	mbids []string, libraryID int64,
) (map[string][]string, error) {
	paths := make(map[string][]string, len(mbids))

	if len(mbids) == 0 {
		return paths, nil
	}

	// recordings.mbid is nullable, so sqlc asks for NullStrings.  An
	// empty MBID would match every untagged recording in the library,
	// which is the opposite of the question, so those are dropped here
	// rather than passed through as NULL.
	keys := make([]sql.NullString, 0, len(mbids))

	for _, mbid := range mbids {
		if mbid == "" {
			continue
		}

		keys = append(keys, sql.NullString{String: mbid, Valid: true})
	}

	if len(keys) == 0 {
		return paths, nil
	}

	rows, err := l.filePathRowsByMBID(keys, libraryID)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		if !row.mbid.Valid {
			continue
		}

		paths[row.mbid.String] = append(paths[row.mbid.String], row.path)
	}

	return paths, nil
}

// filePathRowsByMBID runs the scoped or unscoped query behind
// GetFilePathsByRecordingMBIDs and flattens the two row types into one.
func (l *Library) filePathRowsByMBID(
	keys []sql.NullString, libraryID int64,
) ([]mbidFilePath, error) {
	if libraryID > 0 {
		rows, err := l.db.ReadQueries.GetFilePathsByRecordingMBIDsByLibrary(
			l.ctx, sqlcgen.GetFilePathsByRecordingMBIDsByLibraryParams{
				Mbids:     keys,
				LibraryID: libraryID,
			},
		)
		if err != nil {
			l.logger.Error(
				"could not retrieve recording file paths for library",
				"recordings", len(keys),
				"libraryID", libraryID,
				"error", err,
			)

			return nil, fmt.Errorf("could not get recording file paths: %w", err)
		}

		out := make([]mbidFilePath, 0, len(rows))
		for _, row := range rows {
			out = append(out, mbidFilePath{mbid: row.RecordingMbid, path: row.FilePath})
		}

		return out, nil
	}

	rows, err := l.db.ReadQueries.GetFilePathsByRecordingMBIDs(l.ctx, keys)
	if err != nil {
		l.logger.Error(
			"could not retrieve recording file paths",
			"recordings", len(keys),
			"error", err,
		)

		return nil, fmt.Errorf("could not get recording file paths: %w", err)
	}

	out := make([]mbidFilePath, 0, len(rows))
	for _, row := range rows {
		out = append(out, mbidFilePath{mbid: row.RecordingMbid, path: row.FilePath})
	}

	return out, nil
}

// mbidFilePath is one row of either GetFilePathsByRecordingMBIDs query.
type mbidFilePath struct {
	mbid sql.NullString
	path string
}
