// Package tagwriter writes metadata tags to audio files.
package tagwriter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TagChanges is a diff map of field name → new value. Only changed
// fields are present. Callers specify changed fields; unchanged
// fields are left as-is in the file.
type TagChanges map[string]any

// Field name constants for the diff map.
const (
	FieldTitle       = "title"
	FieldArtist      = "artist"
	FieldAlbum       = "album"
	FieldAlbumArtist = "album_artist"
	FieldGenre       = "genre"
	FieldYear        = "year"
	FieldTrackNumber = "track_number"
	FieldDiscNumber  = "disc_number"
	FieldComposer    = "composer"
	FieldCoverArt    = "cover_art" // []byte for set, nil for clear

	// FieldTotalTracks is how many tracks are on *this file's disc*, not
	// in the whole release.  That is what the "5/12" form declares and
	// what GetAlbumCompleteness sums per disc; a release total written
	// here would multiply the expectation by the number of discs.
	FieldTotalTracks = "total_tracks"

	// FieldTotalDiscs is how many discs the release has.
	FieldTotalDiscs = "total_discs"
)

// AudioFormat represents a supported audio file format.
type AudioFormat string

const (
	// FormatMP3 is the MP3 audio format.
	FormatMP3 AudioFormat = "mp3"
	// FormatFLAC is the FLAC audio format.
	FormatFLAC AudioFormat = "flac"
	// FormatWAV is the WAV audio format.
	FormatWAV AudioFormat = "wav"
	// FormatOGG is the OGG Vorbis audio format.
	FormatOGG AudioFormat = "ogg"
)

// errUnsupportedFormat is returned when the audio format is not supported.
var errUnsupportedFormat = errors.New("tagwriter: unsupported audio format")

// DetectFormat determines the audio format of a file from its extension.
func DetectFormat(filePath string) (AudioFormat, error) {
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".mp3":
		return FormatMP3, nil
	case ".flac":
		return FormatFLAC, nil
	case ".wav":
		return FormatWAV, nil
	case ".ogg":
		return FormatOGG, nil
	default:
		return "", fmt.Errorf("%w: %s", errUnsupportedFormat, ext)
	}
}

// asInt extracts an integer from a TagChanges value.  JSON numbers from
// Wails arrive as float64; Go callers may pass int.  Returns (value, true)
// on success or (0, false) if the value is not a recognised numeric type.
func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	case int64:
		return int(n), true
	case float32:
		return int(n), true
	default:
		return 0, false
	}
}

// asBytes extracts a byte slice from a TagChanges value.  JSON arrays
// from Wails arrive as []interface{} of float64; Go callers may pass
// []byte directly.  Returns (data, true) on success or (nil, false).
func asBytes(v any) ([]byte, bool) {
	if v == nil {
		return nil, false
	}

	if b, ok := v.([]byte); ok {
		return b, true
	}

	arr, ok := v.([]interface{})
	if !ok {
		return nil, false
	}

	out := make([]byte, len(arr))

	for i, elem := range arr {
		f, ok := elem.(float64)
		if !ok {
			return nil, false
		}

		out[i] = byte(f)
	}

	return out, true
}

// detectMIME returns the MIME type of image data by checking magic bytes.
func detectMIME(data []byte) string {
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8 {
		return "image/jpeg"
	}

	if len(data) >= 4 && data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}

	return "application/octet-stream"
}

// id3v2OriginalTagSize reads an MP3 file's ID3v2 header and returns the
// total number of bytes occupied by the tag (10-byte header + body).
// If the file does not start with an ID3v2 header it returns 0.
func id3v2OriginalTagSize(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open for tag size: %w", err)
	}

	defer func() { _ = f.Close() }()

	// ID3v2 header: "ID3" (3 B) + version (2 B) + flags (1 B) + size (4 B synchsafe).
	var hdr [10]byte
	if _, err := f.Read(hdr[:]); err != nil {
		return 0, fmt.Errorf("read id3v2 header: %w", err)
	}

	if string(hdr[:3]) != "ID3" {
		return 0, nil
	}

	size := decodeSynchSafe(hdr[6:10])

	const headerBytes = 10

	return headerBytes + int64(size), nil
}

// decodeSynchSafe decodes a 4-byte synchsafe integer (7 bits per byte).
func decodeSynchSafe(b []byte) uint32 {
	_ = b[3] // bounds check hint

	return uint32(b[0])<<21 |
		uint32(b[1])<<14 |
		uint32(b[2])<<7 |
		uint32(b[3])
}
