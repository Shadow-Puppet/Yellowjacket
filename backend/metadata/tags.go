package metadata

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/dhowden/tag"
)

// TrackMetadata holds all extracted tag data for an audio file.
type TrackMetadata struct {
	// Basic info
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Composer    string
	Genre       string
	Year        int

	// Track position
	TrackNumber int
	TotalTracks int
	DiscNumber  int
	TotalDiscs  int

	// Extended
	Lyrics  string
	Comment string

	// Cover art (if present)
	Picture *PictureData

	// Format info
	TagFormat  string // "ID3v2.3", "VORBIS", etc.
	FileFormat string // "MP3", "FLAC", etc.
}

// PictureData holds embedded artwork.
type PictureData struct {
	Data     []byte
	MIMEType string
	Ext      string // "jpg", "png", etc.
}

// ExtractTags reads metadata tags from an audio file.
func ExtractTags(path string) (*TrackMetadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not open file for tag extraction: %w", err)
	}

	defer func() { _ = f.Close() }()

	return ExtractTagsFromReader(f)
}

// ExtractTagsFromReader reads metadata from an io.ReadSeeker.
func ExtractTagsFromReader(r io.ReadSeeker) (*TrackMetadata, error) {
	m, err := tag.ReadFrom(r)
	if err != nil {
		// No tags found is not necessarily an error - return empty metadata
		if errors.Is(err, tag.ErrNoTagsFound) {
			return &TrackMetadata{}, nil
		}

		return nil, fmt.Errorf("could not read tags: %w", err)
	}

	trackNum, totalTracks := m.Track()
	discNum, totalDiscs := m.Disc()

	meta := &TrackMetadata{
		Title:       m.Title(),
		Artist:      m.Artist(),
		Album:       m.Album(),
		AlbumArtist: m.AlbumArtist(),
		Composer:    m.Composer(),
		Genre:       m.Genre(),
		Year:        m.Year(),
		TrackNumber: trackNum,
		TotalTracks: totalTracks,
		DiscNumber:  discNum,
		TotalDiscs:  totalDiscs,
		Lyrics:      m.Lyrics(),
		Comment:     m.Comment(),
		TagFormat:   string(m.Format()),
		FileFormat:  string(m.FileType()),
	}

	// Extract picture if present
	if pic := m.Picture(); pic != nil {
		meta.Picture = &PictureData{
			Data:     pic.Data,
			MIMEType: pic.MIMEType,
			Ext:      pic.Ext,
		}
	}

	return meta, nil
}
