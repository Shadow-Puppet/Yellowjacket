package metadata

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

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

	// MusicBrainz IDs (from tags, may be empty)
	ArtistMBID       string
	AlbumArtistMBID  string
	ReleaseGroupMBID string
	ReleaseMBID      string
	RecordingMBID    string

	// Cover art (if present)
	Picture *PictureData

	// Format info
	TagFormat  string // "ID3v2.3", "VORBIS", etc.
	FileFormat string // "MP3", "FLAC", etc.

	// TagReadWarning is set when the tag could not be read cleanly:
	// ErrTagsRecovered if the lenient parser salvaged the fields above,
	// ErrTagsUnreadable if they are empty because nothing could read the
	// tag.  Nil on a clean read.  Either way the file is still usable —
	// callers should surface the warning, not discard the track.
	TagReadWarning error
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
	// The container decides, so this is asked before tag.ReadFrom and
	// not after its failure: a WAV's tags live in a RIFF chunk that
	// dhowden/tag cannot see, and its fallback -- an ID3v1 trailer --
	// would otherwise outrank them.
	if meta, ok := wavTags(r); ok {
		return meta, nil
	}

	m, err := tag.ReadFrom(r)
	if err != nil {
		// No tags found is not necessarily an error - return empty metadata
		if errors.Is(err, tag.ErrNoTagsFound) {
			return &TrackMetadata{}, nil
		}

		// A single malformed frame must not cost us the whole file: retry
		// with a more forgiving parser and, failing that, hand back empty
		// metadata carrying a warning.  The audio is still playable and
		// the caller can fall back to the filename.
		return recoverTags(r, err), nil
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

	// Extract MusicBrainz IDs from raw tags.
	extractMBIDs(m.Raw(), meta)

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

// mbidTagKeys maps TrackMetadata field names to the possible raw
// tag keys across formats (ID3v2 TXXX, Vorbis, MP4).  All keys
// are lowercased for case-insensitive matching.
var mbidTagKeys = map[string][]string{
	"ArtistMBID":       {"musicbrainz_artistid", "musicbrainz artist id"},
	"AlbumArtistMBID":  {"musicbrainz_albumartistid", "musicbrainz album artist id"},
	"ReleaseGroupMBID": {"musicbrainz_releasegroupid", "musicbrainz release group id"},
	"ReleaseMBID":      {"musicbrainz_albumid", "musicbrainz album id"},
	"RecordingMBID":    {"musicbrainz_trackid", "musicbrainz recording id"},
}

// extractMBIDs populates the MBID fields of meta from the raw tag
// map.  Handles both Vorbis comments (plain string values with
// lowercase keys) and ID3v2 TXXX frames (*tag.Comm values with
// TXXX_N keys and the tag name in the Description field).
func extractMBIDs(raw map[string]interface{}, meta *TrackMetadata) {
	if len(raw) == 0 {
		return
	}

	// Build a lowercased description → value map that works for
	// both formats:
	//   Vorbis: key="musicbrainz_artistid", value="uuid" (string)
	//   ID3v2:  key="TXXX_13", value=*tag.Comm{Description:"MusicBrainz Artist Id", Text:"uuid"}
	normalized := make(map[string]string, len(raw))

	for k, v := range raw {
		switch val := v.(type) {
		case string:
			// Vorbis comments — key is the tag name.
			normalized[strings.ToLower(k)] = val

		case *tag.Comm:
			// ID3v2 TXXX frames — Description is the tag name.
			if val != nil && val.Description != "" {
				text := strings.TrimRight(val.Text, "\x00 \t\n\r")
				normalized[strings.ToLower(val.Description)] = text
			}

		case *tag.UFID:
			// ID3v2 UFID frame — MusicBrainz recording ID.
			if val != nil && val.Provider == "http://musicbrainz.org" {
				meta.RecordingMBID = strings.TrimRight(string(val.Identifier), "\x00 \t\n\r")
			}
		}
	}

	for field, keys := range mbidTagKeys {
		for _, key := range keys {
			if val, ok := normalized[key]; ok && val != "" {
				switch field {
				case "ArtistMBID":
					meta.ArtistMBID = val
				case "AlbumArtistMBID":
					meta.AlbumArtistMBID = val
				case "ReleaseGroupMBID":
					meta.ReleaseGroupMBID = val
				case "ReleaseMBID":
					meta.ReleaseMBID = val
				case "RecordingMBID":
					meta.RecordingMBID = val
				}

				break
			}
		}
	}
}
