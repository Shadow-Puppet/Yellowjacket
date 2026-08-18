package tagwriter

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

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

	applyPositionFrame(tag, "Track number/Position in set", changes,
		FieldTrackNumber, FieldTotalTracks)
	applyPositionFrame(tag, "Part of a set", changes,
		FieldDiscNumber, FieldTotalDiscs)

	if v, ok := changes[FieldComposer].(string); ok {
		tag.DeleteFrames("TCOM")
		tag.AddTextFrame("TCOM", id3v2.EncodingUTF8, v)
	}

	if v, ok := changes[FieldAlbumArtist].(string); ok {
		tpe2ID := tag.CommonID("Band/Orchestra/Accompaniment")
		tag.DeleteFrames(tpe2ID)
		tag.AddTextFrame(tpe2ID, id3v2.EncodingUTF8, v)
	}
}

// applyPositionFrame writes an ID3v2 position frame (TRCK or TPOS) in
// the "n/N" form the readers parse.
//
// The number and the total are separate diff entries and either may be
// absent, so the frame's *existing* value is the base: writing a total
// alone must not discard the number that is already there, and writing
// a number alone must not discard a total the file already declared.
// A total with no number at all is not written, since "/12" says
// nothing a reader can use.
func applyPositionFrame(
	tag *id3v2.Tag, description string, changes TagChanges, numKey, totalKey string,
) {
	_, hasNum := changes[numKey]
	_, hasTotal := changes[totalKey]

	if !hasNum && !hasTotal {
		return
	}

	frameID := tag.CommonID(description)

	num, total := parseXofN(
		strings.TrimRight(tag.GetTextFrame(frameID).Text, "\x00 \t\n\r"),
	)

	if v, ok := asInt(changes[numKey]); ok {
		num = v
	}

	if v, ok := asInt(changes[totalKey]); ok {
		total = v
	}

	if num <= 0 {
		return
	}

	value := strconv.Itoa(num)
	if total > 0 {
		value += "/" + strconv.Itoa(total)
	}

	tag.DeleteFrames(frameID)
	tag.AddTextFrame(frameID, id3v2.EncodingUTF8, value)
}

// parseXofN splits an ID3v2 "n/N" position value.  A bare "n" yields a
// zero total, and anything unparseable yields zeros — the same reading
// dhowden/tag gives the frame.
func parseXofN(s string) (int, int) {
	numText, totalText, _ := strings.Cut(s, "/")

	num, _ := strconv.Atoi(strings.TrimSpace(numText))
	total, _ := strconv.Atoi(strings.TrimSpace(totalText))

	return num, total
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

	data, isBytes := asBytes(val)
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
