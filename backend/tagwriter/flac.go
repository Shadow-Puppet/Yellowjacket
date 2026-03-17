package tagwriter

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/go-flac/flacpicture/v2"
	"github.com/go-flac/flacvorbis/v2"
	flac "github.com/go-flac/go-flac/v2"

	"yellowjacket/backend/fileutil"
)

// writeFlacTags writes metadata tags to a FLAC file using Vorbis Comments
// and PICTURE metadata blocks, integrated with AtomicWrite for crash safety.
func writeFlacTags(logger *slog.Logger, filePath string, changes TagChanges) error {
	// Check file size and warn for very large files.
	if info, err := os.Stat(filePath); err == nil {
		const largeSizeThreshold = 500 * 1024 * 1024 // 500 MB
		if info.Size() > largeSizeThreshold {
			logger.Warn("large FLAC file may use significant memory",
				slog.String("path", filePath),
				slog.Int64("size", info.Size()),
			)
		}
	}

	f, err := flac.ParseFile(filePath)
	if err != nil {
		return fmt.Errorf("parse flac: %w", err)
	}

	// Find existing Vorbis Comment block.
	var cmt *flacvorbis.MetaDataBlockVorbisComment

	cmtIdx := -1

	for idx, meta := range f.Meta {
		if meta.Type == flac.VorbisComment {
			cmt, err = flacvorbis.ParseFromMetaDataBlock(*meta)
			if err != nil {
				return fmt.Errorf("parse vorbis comments: %w", err)
			}

			cmtIdx = idx

			break
		}
	}

	if cmt == nil {
		cmt = flacvorbis.New()
	}

	// Apply text changes from the diff map.
	if err := applyFlacTextChanges(cmt, changes); err != nil {
		return fmt.Errorf("apply flac text changes: %w", err)
	}

	// Marshal Vorbis Comment block back and update f.Meta.
	cmtMeta := cmt.Marshal()
	if cmtIdx >= 0 {
		f.Meta[cmtIdx] = &cmtMeta
	} else {
		f.Meta = append(f.Meta, &cmtMeta)
	}

	// Handle cover art — PICTURE metadata block.
	if err := applyFlacCoverArt(f, changes); err != nil {
		return fmt.Errorf("apply flac cover art: %w", err)
	}

	// Write atomically via AtomicWrite.
	return fileutil.AtomicWrite(logger, filePath, func(tmp *os.File) error {
		_, writeErr := f.WriteTo(tmp)
		if writeErr != nil {
			return fmt.Errorf("write flac: %w", writeErr)
		}

		return nil
	})
}

// applyFlacTextChanges applies text field changes to a Vorbis Comment block.
func applyFlacTextChanges(cmt *flacvorbis.MetaDataBlockVorbisComment, changes TagChanges) error {
	type fieldMapping struct {
		key      string
		vorbisID string
		isInt    bool
	}

	mappings := []fieldMapping{
		{FieldTitle, flacvorbis.FIELD_TITLE, false},
		{FieldArtist, flacvorbis.FIELD_ARTIST, false},
		{FieldAlbum, flacvorbis.FIELD_ALBUM, false},
		{FieldAlbumArtist, "ALBUMARTIST", false},
		{FieldGenre, flacvorbis.FIELD_GENRE, false},
		{FieldYear, flacvorbis.FIELD_DATE, true},
		{FieldTrackNumber, flacvorbis.FIELD_TRACKNUMBER, true},
		{FieldDiscNumber, "DISCNUMBER", true},
		{FieldComposer, "COMPOSER", false},
	}

	for _, m := range mappings {
		v, ok := changes[m.key]
		if !ok {
			continue
		}

		var val string
		if m.isInt {
			val = strconv.Itoa(v.(int))
		} else {
			val = v.(string)
		}

		replaceVorbisComment(cmt, m.vorbisID, val)
	}

	return nil
}

// replaceVorbisComment removes all existing entries for a field and adds a
// new value. Vorbis Comment field names are case-insensitive per spec.
func replaceVorbisComment(cmt *flacvorbis.MetaDataBlockVorbisComment, field string, value string) {
	// Remove all existing entries for this field (case-insensitive).
	prefix := strings.ToUpper(field) + "="
	filtered := make([]string, 0, len(cmt.Comments))

	for _, c := range cmt.Comments {
		if !strings.HasPrefix(strings.ToUpper(c), prefix) {
			filtered = append(filtered, c)
		}
	}

	cmt.Comments = filtered

	// Add the new value. Using the uppercase field name (Vorbis convention).
	_ = cmt.Add(strings.ToUpper(field), value)
}

// applyFlacCoverArt handles adding, replacing, or clearing PICTURE metadata
// blocks in a FLAC file.
func applyFlacCoverArt(f *flac.File, changes TagChanges) error {
	v, ok := changes[FieldCoverArt]
	if !ok {
		return nil
	}

	// Remove all existing PICTURE blocks.
	newMeta := make([]*flac.MetaDataBlock, 0, len(f.Meta))

	for _, meta := range f.Meta {
		if meta.Type != flac.Picture {
			newMeta = append(newMeta, meta)
		}
	}

	f.Meta = newMeta

	// If value is nil or empty, we've cleared the art — done.
	data, isBytes := v.([]byte)
	if !isBytes || len(data) == 0 {
		return nil
	}

	// Create new PICTURE block with the provided image data.
	pic, err := flacpicture.NewFromImageData(
		flacpicture.PictureTypeFrontCover,
		"Front cover",
		data,
		detectMIME(data),
	)
	if err != nil {
		return fmt.Errorf("create flac picture: %w", err)
	}

	picMeta := pic.Marshal()
	f.Meta = append(f.Meta, &picMeta)

	return nil
}
