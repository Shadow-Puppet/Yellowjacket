// Package tracklist manages track-list display configuration.
package tracklist

import (
	"errors"
	"fmt"
	"slices"
)

var (
	errUnknownColumnID = errors.New("unknown track-list column ID")
	errDuplicateColumn = errors.New("duplicate column ID")
)

// ColumnID identifies a displayable column in the track list.
type ColumnID string

// Valid column identifiers.
const (
	ColTrackName   ColumnID = "trackName"
	ColArtistName  ColumnID = "artistName"
	ColTrackLength ColumnID = "trackLength"
	ColAlbum       ColumnID = "album"
	ColGenre       ColumnID = "genre"
	ColYear        ColumnID = "year"
	ColComposer    ColumnID = "composer"
	ColTrackNumber ColumnID = "trackNumber"
	ColDiscNumber  ColumnID = "discNumber"
	ColFilePath    ColumnID = "filePath"
	ColFileType    ColumnID = "fileType"
)

// AllColumnIDs lists every recognised column in default display
// order.
var AllColumnIDs = []ColumnID{
	ColTrackName,
	ColArtistName,
	ColTrackLength,
	ColAlbum,
	ColGenre,
	ColYear,
	ColComposer,
	ColTrackNumber,
	ColDiscNumber,
	ColFilePath,
	ColFileType,
}

// DefaultColumns is the initial column configuration matching the
// original hardcoded layout.
var DefaultColumns = []Column{
	{ID: ColTrackName},
	{ID: ColArtistName},
	{ID: ColTrackLength},
}

// Column represents a visible column in the track list.
type Column struct {
	ID ColumnID `json:"id" toml:"ID"`
}

// Config holds track-list display preferences.
type Config struct {
	Columns []Column `json:"columns" toml:"Columns"`
}

// ApplyDefaults fills zero-value fields with sensible defaults.
func (c *Config) ApplyDefaults() {
	if len(c.Columns) == 0 {
		c.Columns = make([]Column, len(DefaultColumns))
		copy(c.Columns, DefaultColumns)
	}
}

// Validate checks that every column ID is recognised and that
// there are no duplicates.
func (c *Config) Validate() error {
	c.ApplyDefaults()

	seen := make(map[ColumnID]bool, len(c.Columns))

	for _, col := range c.Columns {
		if !isValidColumnID(col.ID) {
			return fmt.Errorf(
				"%w: %q", errUnknownColumnID, col.ID,
			)
		}

		if seen[col.ID] {
			return fmt.Errorf(
				"%w: %q", errDuplicateColumn, col.ID,
			)
		}

		seen[col.ID] = true
	}

	return nil
}

// isValidColumnID returns true when id matches a known column.
func isValidColumnID(id ColumnID) bool {
	return slices.Contains(AllColumnIDs, id)
}
