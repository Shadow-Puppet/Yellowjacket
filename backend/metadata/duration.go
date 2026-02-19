package metadata

import (
	"fmt"
	"os"
	"path/filepath"
)

// getTrackDuration returns the duration of an audio file in
// milliseconds.  For MP3 files it uses a fast header-only parser
// (Xing/VBRI/CBR); for other formats it falls back to a full
// decode via beep which is already O(1) for FLAC, OGG, and WAV.
//
// The file position is undefined after this call.
func getTrackDuration(f *os.File) (int64, error) {
	ext := filepath.Ext(f.Name())

	if ext == ".mp3" {
		return getMP3Duration(f)
	}

	// FLAC, OGG, and WAV: beep's Decode() + Len() is already
	// cheap (reads headers/metadata only, no full audio decode).
	streamer, format, err := DecodeFile(f)
	if err != nil {
		return 0, fmt.Errorf("error decoding file: %w", err)
	}

	lengthMillis := int64(
		float64(streamer.Len()*1000) /
			float64(format.SampleRate),
	)
	_ = streamer.Close()

	return lengthMillis, nil
}
