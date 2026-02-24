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

// AudioProperties holds technical properties of an audio file that
// are extracted from its stream headers during scanning.
type AudioProperties struct {
	SampleRate int   // Sample rate in Hz (e.g. 44100, 96000).
	BitDepth   int   // Bits per sample (e.g. 16, 24).
	Channels   int   // Number of audio channels (1=mono, 2=stereo).
	Bitrate    int   // Bitrate in kbps.
	FileSize   int64 // File size in bytes.
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

// ExtractAllMetadata opens the file once and extracts tags, duration,
// and audio properties (sample rate, bit depth, channels, bitrate,
// file size).  If skipDuration is true, only tags are extracted and
// the remaining outputs are zero-valued.
// The returned ExtractionTiming records how long each sub-operation took.
func ExtractAllMetadata(
	path string,
	skipDuration bool,
) (*TrackMetadata, int64, *AudioProperties, *ExtractionTiming, error) {
	timing := &ExtractionTiming{}
	props := &AudioProperties{}

	f, err := os.Open(path)
	if err != nil {
		return nil, 0, props, timing, fmt.Errorf(
			"could not open file: %w", err,
		)
	}

	defer func() { _ = f.Close() }()

	// Capture file size.
	fi, err := f.Stat()
	if err != nil {
		return nil, 0, props, timing, fmt.Errorf(
			"could not stat file: %w", err,
		)
	}

	props.FileSize = fi.Size()

	// Extract tags first (reads only headers, fast).
	tagStart := time.Now()

	tags, err := ExtractTagsFromReader(f)

	timing.TagExtraction = time.Since(tagStart)

	if err != nil {
		return nil, 0, props, timing, fmt.Errorf(
			"could not extract tags from %s: %w", path, err,
		)
	}

	if skipDuration {
		return tags, 0, props, timing, nil
	}

	// Seek back to the beginning for duration extraction.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return tags, 0, props, timing, fmt.Errorf(
			"could not seek file for duration: %w", err,
		)
	}

	durStart := time.Now()

	lengthMillis, audioProps, err := getTrackDuration(f)

	timing.DurationExtraction = time.Since(durStart)

	if err != nil {
		return tags, 0, props, timing, fmt.Errorf(
			"error getting duration for %s: %w", path, err,
		)
	}

	// Merge stream properties into the result, keeping the
	// file size we already captured.
	audioProps.FileSize = props.FileSize

	// Compute bitrate from file size and duration when the
	// format parser did not provide one (lossless formats).
	if audioProps.Bitrate == 0 && lengthMillis > 0 {
		audioProps.Bitrate = int(
			props.FileSize * 8 / lengthMillis,
		)
	}

	return tags, lengthMillis, audioProps, timing, nil
}
