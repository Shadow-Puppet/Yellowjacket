package metadata

import (
	"fmt"
	"io"
	"os"
	"time"
)

// ExtractionTiming holds sub-operation durations from a single
// ExtractAllMetadata call so callers can build per-format aggregates.
type ExtractionTiming struct {
	TagExtraction      time.Duration
	DurationExtraction time.Duration
}

// AudioFileExtension represents a supported audio file extension.
type AudioFileExtension string

// Supported audio file extensions.
const (
	MP3  AudioFileExtension = ".mp3"
	FLAC AudioFileExtension = ".flac"
	OGG  AudioFileExtension = ".ogg"
	WAV  AudioFileExtension = ".wav"
)

// SupportedFileExtensions lists all supported audio formats.
var SupportedFileExtensions = []AudioFileExtension{MP3, FLAC, OGG, WAV}

// GetSupportedFileType checks if a file extension is supported.
func GetSupportedFileType(ext string) (AudioFileExtension, bool) {
	for _, supported := range SupportedFileExtensions {
		if string(supported) == ext {
			return supported, true
		}
	}

	return "", false
}

// GetTrackLengthMillis returns the duration of an audio file in milliseconds.
func GetTrackLengthMillis(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("could not open file: %w", err)
	}

	streamer, format, err := DecodeFile(f)
	if err != nil {
		_ = f.Close()

		return 0, fmt.Errorf("error decoding file: %w", err)
	}

	lengthMillis := int64(float64(streamer.Len()*1000) / float64(format.SampleRate))
	_ = streamer.Close()
	_ = f.Close()

	return lengthMillis, nil
}

// ExtractAllMetadata opens the file once and extracts both tags and duration.
// This avoids the overhead of opening the file twice when both are needed.
// If skipDuration is true, only tags are extracted and lengthMillis is 0.
// The returned ExtractionTiming records how long each sub-operation took.
func ExtractAllMetadata(
	path string,
	skipDuration bool,
) (*TrackMetadata, int64, *ExtractionTiming, error) {
	timing := &ExtractionTiming{}

	f, err := os.Open(path)
	if err != nil {
		return nil, 0, timing, fmt.Errorf(
			"could not open file: %w", err,
		)
	}

	defer func() { _ = f.Close() }()

	// Extract tags first (reads only headers, fast).
	tagStart := time.Now()

	tags, err := ExtractTagsFromReader(f)

	timing.TagExtraction = time.Since(tagStart)

	if err != nil {
		return nil, 0, timing, fmt.Errorf(
			"could not extract tags from %s: %w", path, err,
		)
	}

	if skipDuration {
		return tags, 0, timing, nil
	}

	// Seek back to the beginning for duration extraction.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return tags, 0, timing, fmt.Errorf(
			"could not seek file for duration: %w", err,
		)
	}

	durStart := time.Now()

	lengthMillis, err := getTrackDuration(f)

	timing.DurationExtraction = time.Since(durStart)

	if err != nil {
		return tags, 0, timing, fmt.Errorf(
			"error getting duration for %s: %w", path, err,
		)
	}

	return tags, lengthMillis, timing, nil
}
