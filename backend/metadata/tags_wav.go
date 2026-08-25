package metadata

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"yellowjacket/backend/riff"
)

// wavTags reads the ID3v2 tag a WAV carries in its RIFF "id3 " chunk,
// which is where backend/tagwriter puts it and where dhowden/tag --
// having no RIFF reader at all -- cannot look.  Without this a WAV
// scans as an untagged file however carefully it was tagged.
//
// ok is false when r is not a RIFF/WAVE container, and the read
// position is restored either way so the caller can carry on.
func wavTags(r io.ReadSeeker) (*TrackMetadata, bool) {
	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, false
	}

	id3Data, chunkErr := riff.ID3Chunk(r)

	if _, err := r.Seek(start, io.SeekStart); err != nil {
		return nil, false
	}

	switch {
	case chunkErr == nil:
		return wavTagsFrom(id3Data), true

	// Not ours to read: let the ordinary dispatch have the file.
	case errors.Is(chunkErr, riff.ErrNotRIFF), errors.Is(chunkErr, riff.ErrNotWAVE):
		return nil, false

	// A RIFF container we cannot get a tag out of -- no chunk, an RF64
	// file, a truncated header.  That is a file with no readable tags,
	// which is what the scanner's filename fallback is for.
	default:
		return &TrackMetadata{}, true
	}
}

// wavTagsFrom parses the bytes of a WAV's ID3v2 chunk.
func wavTagsFrom(id3Data []byte) *TrackMetadata {
	meta, err := extractID3v2Lenient(bytes.NewReader(id3Data))
	if err != nil {
		// A tag holding no frames is not a damaged tag: writing every
		// field back out empty leaves one, and warning about it would
		// put a fault on a file that has none.
		if errors.Is(err, ErrTagsUnreadable) {
			return &TrackMetadata{}
		}

		return &TrackMetadata{
			TagReadWarning: fmt.Errorf("%w: %w", ErrTagsUnreadable, err),
		}
	}

	// extractID3v2Lenient names MP3, being the recovery path for one.
	meta.FileFormat = strings.ToUpper(strings.TrimPrefix(string(WAV), "."))

	return meta
}
