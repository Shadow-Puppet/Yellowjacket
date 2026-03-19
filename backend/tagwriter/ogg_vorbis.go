package tagwriter

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

// oggVorbisComment holds a parsed Vorbis Comment structure with raw
// byte preservation for non-edited fields (even if they contain
// invalid UTF-8).
type oggVorbisComment struct {
	vendor  []byte   // raw vendor string bytes (preserved)
	entries [][]byte // raw "FIELD=value" entries as byte slices
}

// parseVorbisCommentPacket parses a Vorbis Comment header packet.
// The packet must start with the 7-byte prefix "\x03vorbis".
// The trailing framing bit is consumed but not validated.
func parseVorbisCommentPacket(packet []byte) (*oggVorbisComment, error) {
	const prefixLen = 7 // \x03 + "vorbis"

	if len(packet) < prefixLen {
		return nil, fmt.Errorf("vorbis comment packet too short: %d bytes", len(packet))
	}

	if packet[0] != 0x03 || string(packet[1:7]) != "vorbis" { //nolint:mnd // Vorbis comment header magic
		return nil, fmt.Errorf("invalid vorbis comment header magic")
	}

	r := bytes.NewReader(packet[prefixLen:])

	// Read vendor string.
	var vendorLen uint32
	if err := binary.Read(r, binary.LittleEndian, &vendorLen); err != nil {
		return nil, fmt.Errorf("read vendor length: %w", err)
	}

	vendor := make([]byte, vendorLen)
	if _, err := r.Read(vendor); err != nil {
		return nil, fmt.Errorf("read vendor string: %w", err)
	}

	// Read comment count.
	var commentCount uint32
	if err := binary.Read(r, binary.LittleEndian, &commentCount); err != nil {
		return nil, fmt.Errorf("read comment count: %w", err)
	}

	entries := make([][]byte, 0, commentCount)

	for i := range commentCount {
		var entryLen uint32
		if err := binary.Read(r, binary.LittleEndian, &entryLen); err != nil {
			return nil, fmt.Errorf("read comment %d length: %w", i, err)
		}

		entry := make([]byte, entryLen)
		if _, err := r.Read(entry); err != nil {
			return nil, fmt.Errorf("read comment %d data: %w", i, err)
		}

		entries = append(entries, entry)
	}

	// Ignore the trailing framing bit (if present).

	return &oggVorbisComment{
		vendor:  vendor,
		entries: entries,
	}, nil
}

// serializeVorbisCommentPacket serializes a Vorbis Comment structure
// back to a packet including the \x03vorbis prefix and trailing
// framing bit 0x01.
func serializeVorbisCommentPacket(vc *oggVorbisComment) []byte {
	// Calculate total size.
	size := 7 + 4 + len(vc.vendor) + 4 //nolint:mnd // prefix + vendor_len + vendor + count

	for _, e := range vc.entries {
		size += 4 + len(e) //nolint:mnd // entry_len + entry
	}

	size++ // framing bit

	buf := make([]byte, 0, size)

	// Prefix: \x03 + "vorbis".
	buf = append(buf, 0x03)
	buf = append(buf, "vorbis"...)

	// Vendor length + vendor string.
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(vc.vendor)))
	buf = append(buf, vc.vendor...)

	// Comment count.
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(vc.entries)))

	// Each comment entry.
	for _, e := range vc.entries {
		buf = binary.LittleEndian.AppendUint32(buf, uint32(len(e)))
		buf = append(buf, e...)
	}

	// Framing bit: single byte 0x01.
	buf = append(buf, 0x01)

	return buf
}

// replaceField removes all existing entries for field (case-insensitive)
// and adds a new entry with the uppercase field name.
func (vc *oggVorbisComment) replaceField(field, value string) {
	prefix := []byte(strings.ToUpper(field) + "=")
	filtered := make([][]byte, 0, len(vc.entries))

	for _, entry := range vc.entries {
		if !bytes.HasPrefix(bytes.ToUpper(entry), prefix) {
			filtered = append(filtered, entry)
		}
	}

	filtered = append(filtered, []byte(strings.ToUpper(field)+"="+value))
	vc.entries = filtered
}

// removeField removes all entries for field (case-insensitive)
// without adding a replacement.  Used for stripping legacy cover art fields.
func (vc *oggVorbisComment) removeField(field string) {
	prefix := []byte(strings.ToUpper(field) + "=")
	filtered := make([][]byte, 0, len(vc.entries))

	for _, entry := range vc.entries {
		if !bytes.HasPrefix(bytes.ToUpper(entry), prefix) {
			filtered = append(filtered, entry)
		}
	}

	vc.entries = filtered
}

// oggFieldMappings maps TagChanges field names to Vorbis Comment field names.
// Same mappings as FLAC (Vorbis Comment field names are identical).
var oggFieldMappings = []struct { //nolint:gochecknoglobals // field mapping table
	key      string
	vorbisID string
	isInt    bool
}{
	{FieldTitle, "TITLE", false},
	{FieldArtist, "ARTIST", false},
	{FieldAlbum, "ALBUM", false},
	{FieldAlbumArtist, "ALBUMARTIST", false},
	{FieldGenre, "GENRE", false},
	{FieldYear, "DATE", true},
	{FieldTrackNumber, "TRACKNUMBER", true},
	{FieldDiscNumber, "DISCNUMBER", true},
	{FieldComposer, "COMPOSER", false},
}

// applyOggTextChanges applies text field changes to a Vorbis Comment.
func applyOggTextChanges(vc *oggVorbisComment, changes TagChanges) {
	for _, m := range oggFieldMappings {
		v, ok := changes[m.key]
		if !ok {
			continue
		}

		var val string

		if m.isInt {
			n, ok := asInt(v)
			if !ok {
				continue
			}

			val = strconv.Itoa(n)
		} else {
			s, ok := v.(string)
			if !ok {
				continue
			}

			val = s
		}

		vc.replaceField(m.vorbisID, val)
	}
}

// buildMetadataBlockPicture builds the binary FLAC PICTURE block for
// embedding in a Vorbis Comment METADATA_BLOCK_PICTURE field.
// All lengths are big-endian uint32 per the FLAC picture block spec.
func buildMetadataBlockPicture(imageData []byte) []byte {
	mime := detectMIME(imageData)
	desc := "Front cover"

	// Calculate size: type(4) + mimeLen(4) + mime + descLen(4) + desc
	//   + width(4) + height(4) + depth(4) + colors(4) + dataLen(4) + data
	size := 4 + 4 + len(mime) + 4 + len(desc) + 4*4 + 4 + len(imageData) //nolint:mnd // FLAC PICTURE block fields

	buf := make([]byte, 0, size)

	// Picture type: 3 = front cover (big-endian).
	buf = binary.BigEndian.AppendUint32(buf, 3) //nolint:mnd // PictureTypeFrontCover

	// MIME type.
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(mime)))
	buf = append(buf, mime...)

	// Description.
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(desc)))
	buf = append(buf, desc...)

	// Width, height, color depth, indexed colors — all 0 (unknown).
	buf = binary.BigEndian.AppendUint32(buf, 0)
	buf = binary.BigEndian.AppendUint32(buf, 0)
	buf = binary.BigEndian.AppendUint32(buf, 0)
	buf = binary.BigEndian.AppendUint32(buf, 0)

	// Picture data.
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(imageData)))
	buf = append(buf, imageData...)

	return buf
}

// applyOggCoverArt handles cover art changes for OGG Vorbis files.
// When cover art is present in changes:
//   - Always removes all METADATA_BLOCK_PICTURE, COVERART, and COVERARTMIME entries
//   - If value is non-nil []byte with len>0: builds FLAC PICTURE block,
//     base64-encodes it, adds as METADATA_BLOCK_PICTURE entry
//   - If value is nil or empty: just the removal (clear all art)
func applyOggCoverArt(vc *oggVorbisComment, changes TagChanges) {
	v, ok := changes[FieldCoverArt]
	if !ok {
		return
	}

	// Always strip all picture-related fields.
	vc.removeField("METADATA_BLOCK_PICTURE")
	vc.removeField("COVERART")
	vc.removeField("COVERARTMIME")

	// If value is nil or empty, we've cleared the art — done.
	data, isBytes := asBytes(v)
	if !isBytes || len(data) == 0 {
		return
	}

	// Build METADATA_BLOCK_PICTURE and base64-encode.
	pictureBlock := buildMetadataBlockPicture(data)
	encoded := base64.StdEncoding.EncodeToString(pictureBlock)

	vc.replaceField("METADATA_BLOCK_PICTURE", encoded)
}
