// Package metadata handles audio file decoding and metadata extraction.
package metadata

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"
)

// ErrUnsupportedFileType is returned when the audio file type is not supported.
var ErrUnsupportedFileType = errors.New("unsupported file type")

// DecodeFile decodes an audio file into a stream seeker and format.
func DecodeFile(f *os.File) (beep.StreamSeekCloser, beep.Format, error) {
	ext := filepath.Ext(f.Name())

	switch ext {
	case ".mp3":
		return mp3.Decode(f)
	case ".flac":
		return flac.Decode(f)
	case ".ogg":
		return vorbis.Decode(f)
	case ".wav":
		return wav.Decode(f)
	default:
		return nil, beep.Format{}, fmt.Errorf("%w: %s", ErrUnsupportedFileType, ext)
	}
}
