package metadata

import (
	"fmt"
	"os"
)

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
