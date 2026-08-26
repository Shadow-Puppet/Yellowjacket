// Package riff reads the chunk layout of a RIFF/WAVE container.
//
// It exists because both halves of WAV tagging need it and neither can
// import the other: backend/tagwriter writes a WAV's tags into a RIFF
// "id3 " chunk and already imports backend/metadata, which is what has
// to read them back out.  backend/tagtotals is the precedent.
//
// The two readers here are deliberately different.  Parse holds every
// chunk's data in memory, which is what rewriting a file needs; a WAV's
// audio *is* the "data" chunk, so doing that on the scan path would
// read every library file in full.  ID3Chunk seeks over what it is not
// looking for instead.  Both walk the same headers.
package riff

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Sentinel errors describing a container this package will not read.
var (
	ErrRF64NotSupported = errors.New("RF64 files are not yet supported")
	ErrNotRIFF          = errors.New("not a RIFF file")
	ErrNotWAVE          = errors.New("not a WAVE file")
	ErrNoID3Chunk       = errors.New("no ID3 chunk in RIFF file")
)

// Chunk holds a single RIFF sub-chunk (ID + raw data).
type Chunk struct {
	ID   [4]byte
	Data []byte
}

// IsID3 reports whether id is that of an ID3v2 RIFF chunk.  Both
// lowercase "id3 " and uppercase "ID3 " are accepted.
func IsID3(id [4]byte) bool {
	return strings.ToLower(string(id[:3])) == "id3"
}

// Parse reads every RIFF sub-chunk from r, in order, starting at the
// reader's current position.  It rejects RF64 files and non-WAVE
// containers with descriptive errors.  The parser is lenient: it
// tolerates a missing final padding byte and ignores the declared
// RIFF size.
func Parse(r io.Reader) ([]Chunk, error) {
	if err := readContainer(r); err != nil {
		return nil, err
	}

	var chunks []Chunk

	for {
		id, size, err := nextHeader(r)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, err
		}

		// Copied rather than allocated up front, as ID3Chunk does: the
		// size is four bytes off the file, so a truncated one is free to
		// declare a chunk larger than the whole of itself.
		var data bytes.Buffer
		if _, err := io.CopyN(&data, r, int64(size)); err != nil {
			return nil, fmt.Errorf("read chunk data for %q: %w", id, err)
		}

		chunks = append(chunks, Chunk{ID: id, Data: data.Bytes()})

		// Odd-length chunks have a padding byte.  Lenient: if the
		// read fails (e.g. EOF), just break rather than error.
		if size%2 != 0 {
			var pad [1]byte

			if _, err := r.Read(pad[:]); err != nil {
				break
			}
		}
	}

	return chunks, nil
}

// ID3Chunk returns the payload of the ID3v2 chunk of the RIFF/WAVE
// container at the reader's current position, seeking over every other
// chunk rather than reading it.  It returns ErrNoID3Chunk when the
// container carries no such chunk, and leaves the read position
// unspecified either way.
func ID3Chunk(r io.ReadSeeker) ([]byte, error) {
	if err := readContainer(r); err != nil {
		return nil, err
	}

	for {
		id, size, err := nextHeader(r)
		if errors.Is(err, io.EOF) {
			return nil, ErrNoID3Chunk
		}

		if err != nil {
			return nil, err
		}

		if !IsID3(id) {
			// Odd-length chunks carry a padding byte.  Seeking past
			// the end of the file is not an error; the next header
			// read is what reports the end.
			if _, err := r.Seek(int64(size)+int64(size%2), io.SeekCurrent); err != nil {
				return nil, fmt.Errorf("skip chunk %q: %w", id, err)
			}

			continue
		}

		// Copied rather than allocated up front: a truncated file is
		// free to declare a chunk larger than the whole of itself.
		var data bytes.Buffer
		if _, err := io.CopyN(&data, r, int64(size)); err != nil {
			return nil, fmt.Errorf("read chunk data for %q: %w", id, err)
		}

		return data.Bytes(), nil
	}
}

// readContainer consumes the 12-byte RIFF/WAVE header at the reader's
// current position.
func readContainer(r io.Reader) error {
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return fmt.Errorf("read RIFF magic: %w", err)
	}

	if string(magic[:]) == "RF64" {
		return ErrRF64NotSupported
	}

	if string(magic[:]) != "RIFF" {
		return fmt.Errorf("%w: got %q", ErrNotRIFF, magic)
	}

	// Read (and discard) RIFF size — lenient, do not enforce.
	var riffSize uint32
	if err := binary.Read(r, binary.LittleEndian, &riffSize); err != nil {
		return fmt.Errorf("read RIFF size: %w", err)
	}

	var form [4]byte
	if _, err := io.ReadFull(r, form[:]); err != nil {
		return fmt.Errorf("read WAVE form type: %w", err)
	}

	if string(form[:]) != "WAVE" {
		return fmt.Errorf("%w: got %q", ErrNotWAVE, form)
	}

	return nil
}

// nextHeader reads one sub-chunk header.  It returns io.EOF once the
// chunks are exhausted, including for a header cut short.
func nextHeader(r io.Reader) ([4]byte, uint32, error) {
	var id [4]byte

	_, err := io.ReadFull(r, id[:])
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return id, 0, io.EOF
	}

	if err != nil {
		return id, 0, fmt.Errorf("read chunk ID: %w", err)
	}

	var size uint32
	if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
		return id, 0, fmt.Errorf("read chunk size for %q: %w", id, err)
	}

	return id, size, nil
}
