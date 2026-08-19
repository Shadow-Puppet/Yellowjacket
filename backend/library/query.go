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

// searchTrackLimit bounds an FTS search's result set.
const searchTrackLimit = 500

var errNoTracksInLibrary = errors.New("no tracks in library")

// Track is one audio file with everything a list needs to draw it.
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
// Year is the album's preferred display year - MusicBrainz's
// first-release-date when known, falling back to the file-tag year.
// ReleaseYear is the file-tag year of the specific copy in the library;
// for a 2010 remaster of a 1973 album, Year=1973 and ReleaseYear=2010.
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

// genreDelimiter is the separator GROUP_CONCAT uses in track_metadata.
const genreDelimiter = "||"

// splitGenres splits a GROUP_CONCAT genre string into genre names.
func splitGenres(concatenated string) []string {
	if concatenated == "" {
		return nil
	}

	return strings.Split(concatenated, genreDelimiter)
}

// trackFromRow converts one track_metadata row into a Track.
//
// There is one of these because there is one query shape.  It used to
// be a twenty-two argument function called from nine places, one per
// hand-rolled copy of the same projection - each with its own generated
// row struct, which is why the arguments were positional and why two of
// the call sites passed the wrong year.
func trackFromRow(row sqlcgen.TrackMetadatum) Track {
	var lastPlayed string
	if row.LastPlayed.Valid {
		lastPlayed = row.LastPlayed.Time.Format(time.DateTime)
	}

	t := Track{
		TrackName:        row.Title,
		ArtistName:       row.ArtistName,
		TrackLength:      strconv.FormatInt(row.LengthMilliseconds, 10),
		FilePath:         row.FilePath,
		TrackNumber:      row.TrackNumber.Int64,
		DiscNumber:       row.DiscNumber.Int64,
		Album:            row.Album,
		Genre:            splitGenres(row.Genre),
		Year:             row.Year,
		Composer:         row.Composer,
		FileType:         row.FileType,
		SampleRate:       row.SampleRate,
		BitDepth:         row.BitDepth,
		Channels:         row.Channels,
		Bitrate:          row.Bitrate,
		FileSize:         row.FileSize,
		PlayCount:        row.PlayCount,
		LastPlayed:       lastPlayed,
		ArtistMBID:       row.ArtistMbid,
		ReleaseGroupMBID: row.ReleaseGroupMbid,
		RecordingMBID:    row.RecordingMbid,
	}

	if row.CoverArtPath != "" {
		urls := coverart.ResolveURLs(row.CoverArtPath)
		t.CoverArtPath = urls.Original
		t.CoverArtSmall = urls.Small
		t.CoverArtMedium = urls.Medium
		t.CoverArtLarge = urls.Large
	}

	return t
}

func tracksFromRows(rows []sqlcgen.TrackMetadatum) []Track {
	tracks := make([]Track, 0, len(rows))
	for _, row := range rows {
		tracks = append(tracks, trackFromRow(row))
	}

	return tracks
}

// TrackMBIDs are the MusicBrainz ids a file's tags carry.
type TrackMBIDs struct {
	RecordingMBID    string `json:"recordingMbid"`
	ReleaseGroupMBID string `json:"releaseGroupMbid"`
	ArtistMBID       string `json:"artistMbid"`
}

// GetTrackMBIDs returns the MusicBrainz ids for one file.
func (l *Library) GetTrackMBIDs(filePath string) TrackMBIDs {
	row, err := l.db.ReadQueries.GetTrackByPath(l.ctx, filePath)
	if err != nil {
		return TrackMBIDs{}
	}

	return TrackMBIDs{
		RecordingMBID:    row.RecordingMbid,
		ReleaseGroupMBID: row.ReleaseGroupMbid,
		ArtistMBID:       row.ArtistMbid,
	}
}

// GetTracks returns every track in a library, or in all of them when
// libraryID is 0.
//
// The library id is a parameter rather than a second method because the
// two used to be separate queries, separate bindings and a branch at
// every call site - and the scoped form costs nothing (measured: 23 ms
// against 21 ms over 26k rows).
func (l *Library) GetTracks(libraryID int64) ([]Track, error) {
	rows, err := l.db.ReadQueries.GetTracks(l.ctx, libraryID)
	if err != nil {
		l.logger.Error("could not retrieve audio files", "error", err)

		return nil, fmt.Errorf("could not get tracks: %w", err)
	}

	l.logger.Info("audio file list", "count", len(rows), "libraryID", libraryID)

	if len(rows) == 0 {
		return nil, errNoTracksInLibrary
	}

	return tracksFromRows(rows), nil
}

// SearchTracks runs the library's FTS index and returns whole tracks.
func (l *Library) SearchTracks(query string, libraryID int64) ([]Track, error) {
	rows, err := l.db.SearchFTSTracks(query, libraryID, searchTrackLimit)
	if err != nil {
		l.logger.Error("FTS track search failed", "query", query, "error", err)

		return nil, fmt.Errorf("search tracks failed: %w", err)
	}

	return tracksFromRows(rows), nil
}

// AlbumCompleteness says how much of an album is present, as the files
// themselves claim.
//
// Known is the part that matters: a tag that never declared a total is
// not the same as a total that is unmet, and rendering the two alike
// would put an "incomplete" mark on most of an untagged library.  When
// Known is false, Expected means nothing and the caller must say
// nothing.
type AlbumCompleteness struct {
	Owned    int  `json:"owned"`
	Expected int  `json:"expected"`
	Known    bool `json:"known"`
	Complete bool `json:"complete"`
}

// GetAlbumCompleteness answers "do I have all of this album" from the
// tags read at scan time, with no network.
//
// Complete is deliberately >= rather than ==: bonus and hidden tracks
// routinely put a folder over its declared total, and that is a
// complete album, not a broken one.
func (l *Library) GetAlbumCompleteness(albumID int64) (AlbumCompleteness, error) {
	row, err := l.db.ReadQueries.GetAlbumCompleteness(
		l.ctx, sql.NullInt64{Int64: albumID, Valid: true},
	)
	if err != nil {
		return AlbumCompleteness{}, fmt.Errorf("could not get album completeness: %w", err)
	}

	known := row.Known != 0 && row.Expected > 0

	return AlbumCompleteness{
		Owned:    int(row.Owned),
		Expected: int(row.Expected),
		Known:    known,
		Complete: known && row.Owned >= row.Expected,
	}, nil
}

// GetAlbumsCompleteness answers the same question for a screenful of
// albums in one query, keyed by album id.
//
// A card grid asks this about every card that has a local album behind
// it, and one query per card is how a grid of fifty albums becomes
// fifty round trips. The answer matters there for the reason it
// matters on the album page: an album held 9 tracks of 12 has to show
// the count, and a bare tick saying "in your library" is the complaint
// this whole rule came from.
//
// An album with no row in the result is one with no files, and it is
// absent rather than zeroed — "I have none of this" and "I have no
// idea" are the same third state `Known` exists to keep apart, and a
// caller reading a missing key gets nothing rather than a confident 0.
func (l *Library) GetAlbumsCompleteness(
	albumIDs []int64,
) (map[int64]AlbumCompleteness, error) {
	out := make(map[int64]AlbumCompleteness, len(albumIDs))

	if len(albumIDs) == 0 {
		return out, nil
	}

	keys := make([]sql.NullInt64, 0, len(albumIDs))

	for _, id := range albumIDs {
		if id <= 0 {
			continue
		}

		keys = append(keys, sql.NullInt64{Int64: id, Valid: true})
	}

	if len(keys) == 0 {
		return out, nil
	}

	rows, err := l.db.ReadQueries.GetAlbumsCompleteness(l.ctx, keys)
	if err != nil {
		l.logger.Error("could not get album completeness in batch",
			"albums", len(keys), "error", err)

		return nil, fmt.Errorf("could not get album completeness: %w", err)
	}

	for _, row := range rows {
		known := row.Known != 0 && row.Expected > 0

		out[row.AlbumID] = AlbumCompleteness{
			Owned:    int(row.Owned),
			Expected: int(row.Expected),
			Known:    known,
			Complete: known && row.Owned >= row.Expected,
		}
	}

	return out, nil
}

// GetAlbumTracks returns one album's tracks in disc/track order.
func (l *Library) GetAlbumTracks(albumID, libraryID int64) ([]Track, error) {
	rows, err := l.db.ReadQueries.GetTracksByAlbum(
		l.ctx, sqlcgen.GetTracksByAlbumParams{
			AlbumID:   sql.NullInt64{Int64: albumID, Valid: true},
			LibraryID: libraryID,
		},
	)
	if err != nil {
		l.logger.Error("could not retrieve album tracks", "albumID", albumID, "error", err)

		return nil, fmt.Errorf("could not get album tracks: %w", err)
	}

	return tracksFromRows(rows), nil
}

// GetTracksByGenre returns every track carrying a genre.
func (l *Library) GetTracksByGenre(genre string, libraryID int64) ([]Track, error) {
	rows, err := l.db.ReadQueries.GetTracksByGenre(
		l.ctx, sqlcgen.GetTracksByGenreParams{Genre: genre, LibraryID: libraryID},
	)
	if err != nil {
		l.logger.Error("could not retrieve genre tracks", "genre", genre, "error", err)

		return nil, fmt.Errorf("could not get genre tracks: %w", err)
	}

	return tracksFromRows(rows), nil
}

// albumFromRow builds an Album from either album query's row.  Both
// select the same columns, so this takes them one by one rather than
// tying itself to whichever generated struct it was handed.
func albumFromRow(
	id int64, name, artistName, artistMBID string,
	mbid sql.NullString, coverArtPath string,
	year sql.NullInt64, releaseYear int64,
) Album {
	album := Album{
		ID:          id,
		Name:        name,
		ArtistName:  artistName,
		ArtistMBID:  artistMBID,
		ReleaseYear: releaseYear,
	}

	if mbid.Valid {
		album.MBID = mbid.String
	}

	if year.Valid {
		album.Year = year.Int64
	}

	if coverArtPath != "" {
		urls := coverart.ResolveURLs(coverArtPath)
		album.CoverArtPath = urls.Original
		album.CoverArtSmall = urls.Small
		album.CoverArtMedium = urls.Medium
		album.CoverArtLarge = urls.Large
	}

	return album
}

// GetAlbums returns every album, or those with a file in one library.
func (l *Library) GetAlbums(libraryID int64) ([]Album, error) {
	rows, err := l.db.ReadQueries.GetAlbums(l.ctx, libraryID)
	if err != nil {
		l.logger.Error("could not retrieve albums", "error", err)

		return nil, fmt.Errorf("could not get albums: %w", err)
	}

	l.logger.Info("album list", "count", len(rows))

	albums := make([]Album, 0, len(rows))
	for _, row := range rows {
		albums = append(albums, albumFromRow(
			row.ID, row.Name, row.ArtistName, row.ArtistMbid,
			row.Mbid, row.CoverArtPath, row.Year, row.ReleaseYear,
		))
	}

	return albums, nil
}

// GetAlbumsByArtist returns the albums credited to an artist by name.
func (l *Library) GetAlbumsByArtist(artist string, libraryID int64) ([]Album, error) {
	rows, err := l.db.ReadQueries.GetAlbumsByArtistName(
		l.ctx, sqlcgen.GetAlbumsByArtistNameParams{Artist: artist, LibraryID: libraryID},
	)
	if err != nil {
		l.logger.Error("could not retrieve artist albums", "artist", artist, "error", err)

		return nil, fmt.Errorf("could not get artist albums: %w", err)
	}

	albums := make([]Album, 0, len(rows))
	for _, row := range rows {
		albums = append(albums, albumFromRow(
			row.ID, row.Name, row.ArtistName, row.ArtistMbid,
			row.Mbid, row.CoverArtPath, row.Year, row.ReleaseYear,
		))
	}

	return albums, nil
}

// GetArtists returns the album artists in a library.
func (l *Library) GetArtists(libraryID int64) ([]Artist, error) {
	rows, err := l.db.ReadQueries.GetAlbumArtists(l.ctx, libraryID)
	if err != nil {
		l.logger.Error("could not retrieve artists", "error", err)

		return nil, fmt.Errorf("could not get artists: %w", err)
	}

	artists := make([]Artist, 0, len(rows))
	for _, row := range rows {
		artist := Artist{ID: row.ID, Name: row.Name}
		if row.Mbid.Valid {
			artist.MBID = row.Mbid.String
		}

		artists = append(artists, artist)
	}

	l.resolveArtistImages(artists)

	return artists, nil
}

// GenreWithCount is a genre and how many tracks carry it.
type GenreWithCount struct {
	Name       string
	TrackCount int64
}

// GetGenres returns every genre with its track count.
func (l *Library) GetGenres(libraryID int64) ([]GenreWithCount, error) {
	rows, err := l.db.ReadQueries.GetAllGenresWithCounts(l.ctx, libraryID)
	if err != nil {
		l.logger.Error("could not retrieve genres", "error", err)

		return nil, fmt.Errorf("could not get genres: %w", err)
	}

	genres := make([]GenreWithCount, 0, len(rows))
	for _, row := range rows {
		genres = append(genres, GenreWithCount{Name: row.Name, TrackCount: row.TrackCount})
	}

	return genres, nil
}

// resolveArtistImages fills in the on-disk portrait tiers for artists
// that have one.  The directory layout is sharded by the MBID's first
// two characters - explore.ArtistImageDir is its one definition, and a
// caller that reinvents it names a path that has never existed.
func (l *Library) resolveArtistImages(artists []Artist) {
	if len(artists) == 0 {
		return
	}

	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		return
	}

	baseDir := filepath.Join(dataDir, "artist-images")

	for i := range artists {
		mbid := artists[i].MBID
		if len(mbid) < 2 {
			continue
		}

		dir := filepath.Join(baseDir, mbid[:2], mbid)
		prefix := "/artist-images/" + mbid[:2] + "/" + mbid + "/"

		for name, dst := range map[string]*string{
			"primary_sm.jpg": &artists[i].ImageSmall,
			"primary_md.jpg": &artists[i].ImageMedium,
			"primary_lg.jpg": &artists[i].ImageLarge,
		} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				*dst = prefix + name
			}
		}
	}
}

// Info is one library and how many files are in it.
type Info struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	TrackCount int64  `json:"trackCount"`
}

// GetAllLibrariesWithTrackCounts lists the libraries and their sizes.
func (l *Library) GetAllLibrariesWithTrackCounts() ([]Info, error) {
	libs, err := l.db.ReadQueries.GetAllLibraries(l.ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get libraries: %w", err)
	}

	result := make([]Info, 0, len(libs))

	for _, lib := range libs {
		count, countErr := l.db.ReadQueries.CountAudioFiles(l.ctx, lib.ID)
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

// inLibrary reports whether a row belongs to the requested library.
// A wanted id of 0 means "every library".
//
// The three lookups below filter here rather than in SQL because they
// also take a slice: sqlc expands a slice into N placeholders but
// numbers a named parameter independently, so the two together bind the
// wrong values.  See the comment on GetFilePathsByAlbums.
func inLibrary(rowLibraryID, wanted int64) bool {
	return wanted == 0 || rowLibraryID == wanted
}

// GetFilePathsByAlbums returns the file paths of every track in the
// given albums, grouped by album id.
//
// "Play this artist", "play these albums" and the album drag cache each
// resolved paths with one binding call per album, sequentially, and each
// asked for whole track rows to read one field off them (perf.m2).  This
// is that question asked once.  The result is grouped rather than
// flattened because the caller owns the ordering - an album list is
// sorted by name, not by id - and because the drag cache stores it per
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

	ids := make([]sql.NullInt64, 0, len(albumIDs))
	for _, id := range albumIDs {
		ids = append(ids, sql.NullInt64{Int64: id, Valid: true})
	}

	rows, err := l.db.ReadQueries.GetFilePathsByAlbums(l.ctx, ids)
	if err != nil {
		l.logger.Error("could not retrieve album file paths",
			"albums", len(albumIDs), "libraryID", libraryID, "error", err)

		return nil, fmt.Errorf("could not get album file paths: %w", err)
	}

	for _, row := range rows {
		if row.AlbumID.Valid && inLibrary(row.LibraryID, libraryID) {
			paths[row.AlbumID.Int64] = append(paths[row.AlbumID.Int64], row.FilePath)
		}
	}

	return paths, nil
}

// GetFilePathsByGenres returns file paths grouped by genre name.
func (l *Library) GetFilePathsByGenres(
	genreNames []string, libraryID int64,
) (map[string][]string, error) {
	paths := make(map[string][]string, len(genreNames))

	if len(genreNames) == 0 {
		return paths, nil
	}

	rows, err := l.db.ReadQueries.GetFilePathsByGenres(l.ctx, genreNames)
	if err != nil {
		l.logger.Error("could not retrieve genre file paths",
			"genres", len(genreNames), "libraryID", libraryID, "error", err)

		return nil, fmt.Errorf("could not get genre file paths: %w", err)
	}

	for _, row := range rows {
		if inLibrary(row.LibraryID, libraryID) {
			paths[row.Genre] = append(paths[row.Genre], row.FilePath)
		}
	}

	return paths, nil
}

// GetFilePathsByRecordingMBIDs answers "which of these catalog
// recordings do I actually have a file for", grouped by MBID.
//
// It asks audio_files, which is the only table whose rows are files.
// The version of this question that asked the metadata tables said yes
// for 129 tracks in a real library that had no file at all - a
// retagged file left its old recording row behind, the catalog matched
// it, and every action on the row then failed.
func (l *Library) GetFilePathsByRecordingMBIDs(
	mbids []string, libraryID int64,
) (map[string][]string, error) {
	paths := make(map[string][]string, len(mbids))

	if len(mbids) == 0 {
		return paths, nil
	}

	// An empty MBID would match every untagged file, which is the
	// opposite of the question.
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

	rows, err := l.db.ReadQueries.GetFilePathsByRecordingMBIDs(l.ctx, keys)
	if err != nil {
		l.logger.Error("could not retrieve recording file paths",
			"recordings", len(keys), "libraryID", libraryID, "error", err)

		return nil, fmt.Errorf("could not get recording file paths: %w", err)
	}

	for _, row := range rows {
		if row.RecordingMbid.Valid && inLibrary(row.LibraryID, libraryID) {
			paths[row.RecordingMbid.String] = append(paths[row.RecordingMbid.String], row.FilePath)
		}
	}

	return paths, nil
}
