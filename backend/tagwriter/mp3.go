package tagwriter

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"

	id3v2 "github.com/bogem/id3v2/v2"

	"yellowjacket/backend/fileutil"
)

// writeMp3Tags applies the given TagChanges to an MP3 file's ID3v2 tag
// and writes the result atomically via fileutil.AtomicWrite.
func writeMp3Tags(logger *slog.Logger, filePath string, changes TagChanges) error {
	// Snapshot the original tag size before opening the tag for editing.
	// We need this later to locate the start of the audio data in the
	// original file so we can copy it into the new temp file.
	originalTagSize, err := id3v2OriginalTagSize(filePath)
	if err != nil {
		return fmt.Errorf("read original tag size: %w", err)
	}

	tag, err := id3v2.Open(filePath, id3v2.Options{Parse: true})
	if err != nil {
		return fmt.Errorf("open mp3 for tag writing: %w", err)
	}

	defer func() { _ = tag.Close() }()

	applyTextChanges(tag, changes)
	applyCoverArtChanges(tag, changes)

	return fileutil.AtomicWrite(logger, filePath, func(tmp *os.File) error {
		// Write the new ID3v2 tag to the temp file.
		if _, wErr := tag.WriteTo(tmp); wErr != nil {
			return fmt.Errorf("write id3v2 tag: %w", wErr)
		}

		// Copy the audio data from the original file.
		return copyAudioData(filePath, originalTagSize, tmp)
	})
}

// applyTextChanges maps diff-map fields to ID3v2 setter calls.
func applyTextChanges(tag *id3v2.Tag, changes TagChanges) {
	if v, ok := changes[FieldTitle].(string); ok {
		tag.SetTitle(v)
	}

	if v, ok := changes[FieldArtist].(string); ok {
		tag.SetArtist(v)
	}

	if v, ok := changes[FieldAlbum].(string); ok {
		tag.SetAlbum(v)
	}

	if v, ok := changes[FieldGenre].(string); ok {
		tag.SetGenre(v)
	}

	if v, ok := asInt(changes[FieldYear]); ok {
		tag.SetYear(strconv.Itoa(v))
	}

	if v, ok := asInt(changes[FieldTrackNumber]); ok {
		trckID := tag.CommonID("Track number/Position in set")
		tag.DeleteFrames(trckID)
		tag.AddTextFrame(trckID, id3v2.EncodingUTF8, strconv.Itoa(v))
	}

	if v, ok := asInt(changes[FieldDiscNumber]); ok {
		tposID := tag.CommonID("Part of a set")
		tag.DeleteFrames(tposID)
		tag.AddTextFrame(tposID, id3v2.EncodingUTF8, strconv.Itoa(v))
	}

	if v, ok := changes[FieldComposer].(string); ok {
		tag.DeleteFrames("TCOM")
		tag.AddTextFrame("TCOM", id3v2.EncodingUTF8, v)
	}
}

// applyCoverArtChanges handles the FieldCoverArt entry in the diff map.
//
//   - []byte with len > 0: embed the given image as front cover.
//   - nil (key present): clear all attached pictures.
func applyCoverArtChanges(tag *id3v2.Tag, changes TagChanges) {
	val, present := changes[FieldCoverArt]
	if !present {
		return
	}

	apicID := tag.CommonID("Attached picture")

	data, isBytes := val.([]byte)
	if isBytes && len(data) > 0 {
		tag.DeleteFrames(apicID)
		tag.AddAttachedPicture(id3v2.PictureFrame{
			Encoding:    id3v2.EncodingUTF8,
			MimeType:    detectMIME(data),
			PictureType: id3v2.PTFrontCover,
			Description: "Front cover",
			Picture:     data,
		})

		return
	}

	// Key is present with nil or empty slice — clear art.
	tag.DeleteFrames(apicID)
}

// copyAudioData opens the original MP3, seeks past the ID3v2 tag, and
// copies the remaining audio data into dst.
func copyAudioData(originalPath string, tagSize int64, dst *os.File) error {
	src, err := os.Open(originalPath)
	if err != nil {
		return fmt.Errorf("open original for audio copy: %w", err)
	}

	defer func() { _ = src.Close() }()

	if tagSize > 0 {
		if _, err := src.Seek(tagSize, io.SeekStart); err != nil {
			return fmt.Errorf("seek past original tag: %w", err)
		}
	}

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy audio data: %w", err)
	}

	return nil
}
