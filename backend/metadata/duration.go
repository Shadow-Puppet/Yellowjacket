package metadata

import (
	"fmt"
	"os"
	"path/filepath"
)

// getTrackDuration returns the duration of an audio file in
// milliseconds together with its audio stream properties.  For MP3
// files it uses a fast header-only parser (Xing/VBRI/CBR); for FLAC
// it reads the StreamInfo block; for other formats it falls back to
// beep which is already O(1) for OGG and WAV.
//
// The file position is undefined after this call.
func getTrackDuration(
	f *os.File,
) (int64, *AudioProperties, error) {
	ext := filepath.Ext(f.Name())

	switch ext {
	case ".mp3":
		return getMP3Duration(f)
	case ".flac":
		return getFlacDuration(f)
	}

	// OGG and WAV: beep's Decode() + Len() is already cheap
	// (reads headers/metadata only, no full audio decode).
	streamer, format, err := DecodeFile(f)
	if err != nil {
		return 0, nil, fmt.Errorf(
			"error decoding file: %w", err,
		)
	}

	lengthMillis := int64(
		float64(streamer.Len()*1000) /
			float64(format.SampleRate),
	)
	_ = streamer.Close()

	props := &AudioProperties{
		SampleRate: int(format.SampleRate),
		BitDepth:   format.Precision * 8,
		Channels:   format.NumChannels,
	}

	return lengthMillis, props, nil
}
