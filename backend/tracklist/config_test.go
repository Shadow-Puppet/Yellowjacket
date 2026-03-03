package tracklist

import (
	"testing"
)

func TestTrackListConfig_Validate_ValidColumns(t *testing.T) {
	t.Parallel()

	c := &Config{
		Columns: []Column{
			{ID: ColTrackName},
			{ID: ColArtistName},
			{ID: ColAlbum},
			{ID: ColTrackLength},
			{ID: ColGenre},
		},
	}

	if err := c.Validate(); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}
}

func TestTrackListConfig_Validate_UnknownColumnID(t *testing.T) {
	t.Parallel()

	c := &Config{
		Columns: []Column{
			{ID: ColTrackName},
			{ID: "nonexistent"},
		},
	}

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for unknown column ID, got nil")
	}
}

func TestTrackListConfig_Validate_DuplicateColumn(t *testing.T) {
	t.Parallel()

	c := &Config{
		Columns: []Column{
			{ID: ColTrackName},
			{ID: ColArtistName},
			{ID: ColTrackName},
		},
	}

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for duplicate column ID, got nil")
	}
}

func TestTrackListConfig_ApplyDefaults(t *testing.T) {
	t.Parallel()

	c := &Config{}
	c.ApplyDefaults()

	if len(c.Columns) != len(DefaultColumns) {
		t.Fatalf("Columns length = %d, want %d", len(c.Columns), len(DefaultColumns))
	}

	for i, col := range c.Columns {
		if col.ID != DefaultColumns[i].ID {
			t.Errorf("Columns[%d].ID = %q, want %q", i, col.ID, DefaultColumns[i].ID)
		}
	}
}
