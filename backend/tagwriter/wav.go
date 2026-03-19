package tagwriter

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	id3v2 "github.com/bogem/id3v2/v2"

	"yellowjacket/backend/fileutil"
)

// Sentinel errors for WAV RIFF operations.
var (
	errRF64NotSupported   = errors.New("RF64 files are not yet supported")
	errNotRIFF            = errors.New("not a RIFF file")
	errNotWAVE            = errors.New("not a WAVE file")
	errFileTooLargeForWAV = errors.New("file too large for WAV format (>4GB)")
)

// riffChunk holds a single RIFF sub-chunk (ID + raw data).
type riffChunk struct {
	id   [4]byte
	data []byte
}

// parseRIFF reads all RIFF sub-chunks from r.  It rejects RF64 files
// and non-WAVE containers with descriptive errors.  The parser is
// lenient on read: it tolerates missing padding bytes and ignores
// the declared RIFF size.
func parseRIFF(r io.ReadSeeker) ([]riffChunk, error) {
	// Read 4-byte container magic.
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return nil, fmt.Errorf("read RIFF magic: %w", err)
	}

	if string(magic[:]) == "RF64" {
		return nil, errRF64NotSupported
	}

	if string(magic[:]) != "RIFF" {
		return nil, fmt.Errorf("%w: got %q", errNotRIFF, magic)
	}

	// Read (and discard) RIFF size — lenient, do not enforce.
	var riffSize uint32
	if err := binary.Read(r, binary.LittleEndian, &riffSize); err != nil {
		return nil, fmt.Errorf("read RIFF size: %w", err)
	}

	// Read 4-byte form type.
	var form [4]byte
	if _, err := io.ReadFull(r, form[:]); err != nil {
		return nil, fmt.Errorf("read WAVE form type: %w", err)
	}

	if string(form[:]) != "WAVE" {
		return nil, fmt.Errorf("%w: got %q", errNotWAVE, form)
	}

	// Read sub-chunks until EOF.
	var chunks []riffChunk

	for {
		var chunkID [4]byte

		_, err := io.ReadFull(r, chunkID[:])
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("read chunk ID: %w", err)
		}

		var chunkSize uint32
		if err := binary.Read(r, binary.LittleEndian, &chunkSize); err != nil {
			return nil, fmt.Errorf("read chunk size for %q: %w", chunkID, err)
		}

		data := make([]byte, chunkSize)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, fmt.Errorf("read chunk data for %q: %w", chunkID, err)
		}

		chunks = append(chunks, riffChunk{id: chunkID, data: data})

		// Odd-length chunks have a padding byte.  Lenient: if the
		// read fails (e.g. EOF), just break rather than error.
		if chunkSize%2 != 0 {
			var pad [1]byte

			if _, err := r.Read(pad[:]); err != nil {
				break
			}
		}
	}

	return chunks, nil
}

// isID3ChunkID returns true if id represents an ID3v2 RIFF chunk.
// Both lowercase "id3 " and uppercase "ID3 " are accepted.
func isID3ChunkID(id [4]byte) bool {
	s := strings.ToLower(string(id[:3]))

	return s == "id3"
}

// writeRIFF writes a complete RIFF/WAVE container to w, preserving
// the given chunks in order and appending the id3Data as the final
// "id3 " chunk.  Returns errFileTooLargeForWAV if the result would
// exceed the 4 GB RIFF limit.
func writeRIFF(w io.Writer, chunks []riffChunk, id3Data []byte) error {
	// Calculate total RIFF payload size:
	//   4 bytes (WAVE form type)
	// + for each preserved chunk: 8 (header) + len(data) + padding
	// + id3 chunk: 8 + len(id3Data) + padding
	riffPayload := uint64(4)

	for _, c := range chunks {
		sz := uint64(len(c.data))
		riffPayload += 8 + sz

		if sz%2 != 0 {
			riffPayload++
		}
	}

	id3Len := uint64(len(id3Data))
	riffPayload += 8 + id3Len

	if id3Len%2 != 0 {
		riffPayload++
	}

	// The RIFF header itself is 8 bytes (magic + size), so the
	// total file size is riffPayload + 8.
	const maxRIFFSize = 0xFFFFFFFF
	if riffPayload > maxRIFFSize {
		return errFileTooLargeForWAV
	}

	// Write RIFF header: magic + uint32 LE size + WAVE.
	if _, err := w.Write([]byte("RIFF")); err != nil {
		return fmt.Errorf("write RIFF magic: %w", err)
	}

	if err := binary.Write(w, binary.LittleEndian, uint32(riffPayload)); err != nil {
		return fmt.Errorf("write RIFF size: %w", err)
	}

	if _, err := w.Write([]byte("WAVE")); err != nil {
		return fmt.Errorf("write WAVE form: %w", err)
	}

	// Write each preserved chunk.
	for _, c := range chunks {
		if err := writeChunk(w, c.id, c.data); err != nil {
			return err
		}
	}

	// Write id3 chunk last.
	var id3ID [4]byte
	copy(id3ID[:], "id3 ")

	return writeChunk(w, id3ID, id3Data)
}

// writeChunk writes a single RIFF sub-chunk (id + size + data + padding).
func writeChunk(w io.Writer, id [4]byte, data []byte) error {
	if _, err := w.Write(id[:]); err != nil {
		return fmt.Errorf("write chunk ID %q: %w", id, err)
	}

	if err := binary.Write(w, binary.LittleEndian, uint32(len(data))); err != nil {
		return fmt.Errorf("write chunk size %q: %w", id, err)
	}

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write chunk data %q: %w", id, err)
	}

	// Pad odd-length chunks with a zero byte.
	if len(data)%2 != 0 {
		if _, err := w.Write([]byte{0}); err != nil {
			return fmt.Errorf("write chunk padding %q: %w", id, err)
		}
	}

	return nil
}

// writeWavTags applies the given TagChanges to a WAV file's ID3v2 tag
// embedded in a RIFF "id3 " chunk.  All non-ID3v2 chunks are
// preserved byte-for-byte in their original order.  The result is
// written atomically via fileutil.AtomicWrite.
func writeWavTags(
	logger *slog.Logger,
	filePath string,
	changes TagChanges,
) error {
	// Warn for very large files (same threshold as FLAC writer).
	if info, err := os.Stat(filePath); err == nil {
		const largeSizeThreshold = 500 * 1024 * 1024 // 500 MB

		if info.Size() > largeSizeThreshold {
			logger.Warn("large WAV file may use significant memory",
				slog.String("path", filePath),
				slog.Int64("size", info.Size()),
			)
		}
	}

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open wav for reading: %w", err)
	}

	allChunks, err := parseRIFF(f)

	// Close immediately — we need the handle released before
	// AtomicWrite creates the replacement file.
	_ = f.Close()

	if err != nil {
		return fmt.Errorf("parse wav RIFF: %w", err)
	}

	// Separate preserved chunks from existing ID3 data.
	var (
		preserved   []riffChunk
		existingID3 []byte
	)

	for _, c := range allChunks {
		if isID3ChunkID(c.id) {
			existingID3 = c.data
		} else {
			preserved = append(preserved, c)
		}
	}

	// Build ID3v2 tag — merge with existing if present.
	var tag *id3v2.Tag

	if len(existingID3) > 0 {
		parsed, parseErr := id3v2.ParseReader(
			bytes.NewReader(existingID3),
			id3v2.Options{Parse: true},
		)
		if parseErr != nil {
			return fmt.Errorf("parse existing ID3v2 in WAV: %w", parseErr)
		}

		tag = parsed
	} else {
		tag = id3v2.NewEmptyTag()
		tag.SetDefaultEncoding(id3v2.EncodingUTF8)
	}

	applyTextChanges(tag, changes)
	applyCoverArtChanges(tag, changes)

	// Serialize tag to bytes.
	var id3Buf bytes.Buffer
	if _, err := tag.WriteTo(&id3Buf); err != nil {
		return fmt.Errorf("serialize ID3v2 tag: %w", err)
	}

	return fileutil.AtomicWrite(logger, filePath, func(tmp *os.File) error {
		return writeRIFF(tmp, preserved, id3Buf.Bytes())
	})
}
