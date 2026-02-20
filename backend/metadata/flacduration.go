package metadata

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// errInvalidFLACSignature is returned when the file does not contain
// a valid FLAC stream signature ("fLaC") at the expected position.
var errInvalidFLACSignature = errors.New(
	"invalid FLAC signature",
)

// errInvalidStreamInfo is returned when the first metadata block is
// not a StreamInfo block or has an unexpected length.
var errInvalidStreamInfo = errors.New(
	"invalid StreamInfo metadata block",
)

// errZeroSampleRate is returned when the StreamInfo block reports a
// sample rate of zero, which would cause a division by zero.
var errZeroSampleRate = errors.New(
	"FLAC StreamInfo sample rate is zero",
)

// flacSignatureBytes is the four-byte marker that begins every FLAC
// stream.
var flacSignatureBytes = [4]byte{'f', 'L', 'a', 'C'}

// streamInfoLength is the fixed size of a FLAC StreamInfo body in
// bytes.
const streamInfoLength = 34

// streamInfoBlockType is the metadata block type for StreamInfo.
const streamInfoBlockType = 0

// getFlacDuration computes the duration of a FLAC file in
// milliseconds by reading only the StreamInfo metadata block header.
// It handles an optional prepended ID3v2 tag by seeking past it.
//
// This replaces the previous beep/mewkiz-flac decode path which has
// a bug in its ID3v2 skip logic (bufio over bufseekio causes a
// position overshoot).
//
// The file position is undefined after this call.
//
//nolint:mnd // byte offsets and bit shifts from the FLAC spec.
func getFlacDuration(f *os.File) (int64, error) {
	audioStart, err := skipID3v2(f)
	if err != nil {
		return 0, fmt.Errorf("skipping ID3v2: %w", err)
	}

	// Read the 4-byte FLAC signature.
	var sig [4]byte

	if _, err := f.ReadAt(sig[:], audioStart); err != nil {
		return 0, fmt.Errorf(
			"reading FLAC signature: %w", err,
		)
	}

	if sig != flacSignatureBytes {
		return 0, fmt.Errorf(
			"%w: expected %q, got %q",
			errInvalidFLACSignature, flacSignatureBytes, sig,
		)
	}

	// Read the metadata block header (4 bytes) immediately after
	// the signature.
	var mbh [4]byte

	if _, err := f.ReadAt(
		mbh[:], audioStart+4,
	); err != nil {
		return 0, fmt.Errorf(
			"reading metadata block header: %w", err,
		)
	}

	blockType := mbh[0] & 0x7F

	blockLen := int64(mbh[1])<<16 |
		int64(mbh[2])<<8 |
		int64(mbh[3])

	if blockType != streamInfoBlockType ||
		blockLen != streamInfoLength {
		return 0, fmt.Errorf(
			"%w: type=%d, length=%d",
			errInvalidStreamInfo, blockType, blockLen,
		)
	}

	// Read the 34-byte StreamInfo body.
	var si [streamInfoLength]byte

	if _, err := f.ReadAt(
		si[:], audioStart+8,
	); err != nil {
		return 0, fmt.Errorf(
			"reading StreamInfo block: %w", err,
		)
	}

	sampleRate, totalSamples := parseFlacStreamInfo(si)

	if sampleRate == 0 {
		return 0, errZeroSampleRate
	}

	durationMS := int64(totalSamples) * 1000 /
		int64(sampleRate)

	return durationMS, nil
}

// parseFlacStreamInfo extracts the sample rate (20 bits) and total
// sample count (36 bits) from a 34-byte FLAC StreamInfo body.
//
// StreamInfo layout (bytes 10-17 contain the fields we need):
//
//	bits  0-19:  sample rate in Hz     (20 bits)
//	bits 20-22:  number of channels -1 (3 bits, unused here)
//	bits 23-27:  bits per sample -1    (5 bits, unused here)
//	bits 28-63:  total samples         (36 bits)
//
//nolint:mnd // bit offsets from the FLAC spec.
func parseFlacStreamInfo(
	si [streamInfoLength]byte,
) (sampleRate uint32, totalSamples uint64) {
	// Bytes 10-13 packed as big-endian uint32 contain sample rate
	// in the upper 20 bits.
	packed := binary.BigEndian.Uint32(si[10:14])
	sampleRate = packed >> 12

	// Total samples: 4 low bits of byte 13, then bytes 14-17.
	totalSamples = uint64(si[13]&0x0F)<<32 |
		uint64(si[14])<<24 |
		uint64(si[15])<<16 |
		uint64(si[16])<<8 |
		uint64(si[17])

	return sampleRate, totalSamples
}
