package metadata

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/bogem/id3v2/v2"
)

// Sentinel errors describing a degraded tag read.  Both travel on
// TrackMetadata.TagReadWarning rather than being returned, so that one
// malformed frame never costs the caller the entire file.
var (
	// ErrTagsRecovered means the strict parser rejected the tag but the
	// lenient ID3v2 fallback read it.  The metadata is populated.
	ErrTagsRecovered = errors.New("tags recovered by lenient parser")

	// ErrTagsUnreadable means no parser could read the tag.  The
	// metadata is empty and callers should fall back to the filename.
	ErrTagsUnreadable = errors.New("tags could not be parsed")
)

// recoverTags retries a failed tag read with bogem/id3v2, which
// tolerates frames that dhowden/tag rejects outright — a stray NUL
// inside a UTF-16 TXXX frame, for example.  It always returns usable
// metadata: when nothing can be salvaged the metadata is empty and only
// TagReadWarning is set.
func recoverTags(r io.ReadSeeker, cause error) *TrackMetadata {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return &TrackMetadata{
			TagReadWarning: fmt.Errorf("%w: %w", ErrTagsUnreadable, cause),
		}
	}

	meta, err := extractID3v2Lenient(r)
	if err != nil {
		return &TrackMetadata{
			TagReadWarning: fmt.Errorf("%w: %w", ErrTagsUnreadable, cause),
		}
	}

	meta.TagReadWarning = fmt.Errorf("%w: %w", ErrTagsRecovered, cause)

	return meta
}

// extractID3v2Lenient reads an ID3v2 tag with bogem/id3v2.  Only MP3
// (and other ID3v2-carrying containers) can be recovered this way;
// for anything else the tag has no frames and the read fails.
func extractID3v2Lenient(r io.Reader) (*TrackMetadata, error) {
	// The tag is not Closed here: it wraps a reader the caller owns.
	t, err := id3v2.ParseReader(r, id3v2.Options{Parse: true})
	if err != nil {
		return nil, fmt.Errorf("lenient id3v2 parse: %w", err)
	}

	if !t.HasFrames() {
		return nil, ErrTagsUnreadable
	}

	meta := &TrackMetadata{
		Title:       t.Title(),
		Artist:      t.Artist(),
		Album:       t.Album(),
		AlbumArtist: id3Text(t, "Band/Orchestra/Accompaniment"),
		Composer:    id3Text(t, "Composer"),
		Genre:       t.Genre(),
		Year:        parseLeadingInt(t.Year()),
		Lyrics:      id3Lyrics(t),
		Comment:     id3Comment(t),
		TagFormat:   fmt.Sprintf("ID3v2.%d", t.Version()),
		FileFormat:  strings.ToUpper(strings.TrimPrefix(string(MP3), ".")),
	}

	meta.TrackNumber, meta.TotalTracks = parsePosition(
		id3Text(t, "Track number/Position in set"),
	)
	meta.DiscNumber, meta.TotalDiscs = parsePosition(
		id3Text(t, "Part of a set"),
	)

	extractMBIDsID3v2(t, meta)

	meta.Picture = id3Picture(t)

	return meta, nil
}

// id3Text returns the text of the frame registered under the given
// common description, empty if the frame is absent.
func id3Text(t *id3v2.Tag, description string) string {
	return strings.TrimRight(
		t.GetTextFrame(t.CommonID(description)).Text, "\x00 \t\n\r",
	)
}

// id3Lyrics returns the first non-empty USLT frame.
func id3Lyrics(t *id3v2.Tag) string {
	for _, f := range t.GetFrames(t.CommonID("Unsynchronised lyrics/text transcription")) {
		if uslf, ok := f.(id3v2.UnsynchronisedLyricsFrame); ok && uslf.Lyrics != "" {
			return uslf.Lyrics
		}
	}

	return ""
}

// id3Comment returns the first non-empty COMM frame, skipping the
// machine-written iTunes frames that carry no user comment.
func id3Comment(t *id3v2.Tag) string {
	for _, f := range t.GetFrames(t.CommonID("Comments")) {
		cf, ok := f.(id3v2.CommentFrame)
		if !ok || cf.Text == "" {
			continue
		}

		if strings.HasPrefix(cf.Description, "iTun") {
			continue
		}

		return cf.Text
	}

	return ""
}

// id3Picture returns the front cover if one is attached, otherwise the
// first attached picture of any type.
func id3Picture(t *id3v2.Tag) *PictureData {
	var first *PictureData

	for _, f := range t.GetFrames(t.CommonID("Attached picture")) {
		pf, ok := f.(id3v2.PictureFrame)
		if !ok || len(pf.Picture) == 0 {
			continue
		}

		pic := &PictureData{
			Data:     pf.Picture,
			MIMEType: pf.MimeType,
			Ext:      imageExtFromMIME(pf.MimeType),
		}

		if pf.PictureType == id3v2.PTFrontCover {
			return pic
		}

		if first == nil {
			first = pic
		}
	}

	return first
}

// extractMBIDsID3v2 populates the MBID fields of meta from TXXX and
// UFID frames, mirroring extractMBIDs for the lenient parser.
func extractMBIDsID3v2(t *id3v2.Tag, meta *TrackMetadata) {
	normalized := make(map[string]string)

	for _, f := range t.GetFrames(t.CommonID("User defined text information frame")) {
		if udtf, ok := f.(id3v2.UserDefinedTextFrame); ok && udtf.Description != "" {
			normalized[strings.ToLower(udtf.Description)] = strings.TrimRight(
				udtf.Value, "\x00 \t\n\r",
			)
		}
	}

	for _, f := range t.GetFrames(t.CommonID("Unique file identifier")) {
		if ufid, ok := f.(id3v2.UFIDFrame); ok &&
			ufid.OwnerIdentifier == "http://musicbrainz.org" {
			meta.RecordingMBID = strings.TrimRight(
				string(ufid.Identifier), "\x00 \t\n\r",
			)
		}
	}

	targets := map[string]*string{
		"ArtistMBID":       &meta.ArtistMBID,
		"AlbumArtistMBID":  &meta.AlbumArtistMBID,
		"ReleaseGroupMBID": &meta.ReleaseGroupMBID,
		"ReleaseMBID":      &meta.ReleaseMBID,
		"RecordingMBID":    &meta.RecordingMBID,
	}

	for field, keys := range mbidTagKeys {
		for _, key := range keys {
			if val, ok := normalized[key]; ok && val != "" {
				*targets[field] = val

				break
			}
		}
	}
}

// parsePosition splits an ID3 "n/total" position string such as the
// TRCK or TPOS payload.  Missing parts come back as zero.
func parsePosition(raw string) (int, int) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, 0
	}

	number, total, found := strings.Cut(raw, "/")
	if !found {
		return parseLeadingInt(number), 0
	}

	return parseLeadingInt(number), parseLeadingInt(total)
}

// parseLeadingInt reads the leading run of digits from s, returning
// zero when there is none.  Tolerates values like "2021-06-11" (a
// TDRC date) and "3 " (a padded track number).
func parseLeadingInt(s string) int {
	s = strings.TrimSpace(s)

	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}

	if end == 0 {
		return 0
	}

	n, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0
	}

	return n
}

// imageExtFromMIME returns a file extension for common image MIME types.
func imageExtFromMIME(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "image/bmp":
		return "bmp"
	default:
		return "jpg"
	}
}
