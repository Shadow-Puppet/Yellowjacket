package metadata

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// errNoSyncWord is returned when no valid MP3 frame sync word
// is found within the search window.
var errNoSyncWord = errors.New("could not find MP3 sync word")

// maxSyncSearchBytes limits how far we scan for the first sync word
// after skipping any ID3v2 tags.  512 KB accommodates files with
// large embedded artwork or multiple prepended ID3v2 tags.
const maxSyncSearchBytes = 512 * 1024

// maxID3v2Tags limits how many consecutive ID3v2 tags we skip.
// Some files contain multiple prepended tags from different tagging
// tools.
const maxID3v2Tags = 5

// MPEG version constants.
const (
	mpegVersion1   = 3 // 0b11
	mpegVersion2   = 2 // 0b10
	mpegVersion2_5 = 0 // 0b00  (unofficial extension)
)

// bitrateTable maps [versionIndex][bitrateIndex] to kbps.
// versionIndex 0 = MPEG1, 1 = MPEG2/2.5.
// bitrateIndex 0 and 15 are invalid.
//
//nolint:mnd // lookup table values are from the MPEG spec.
var bitrateTable = [2][16]int{
	// MPEG1 Layer 3
	{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
	// MPEG2/2.5 Layer 3
	{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
}

// sampleRateTable maps [versionIndex][sampleRateIndex] to Hz.
// versionIndex: 0 = MPEG1, 1 = MPEG2, 2 = MPEG2.5.
//
//nolint:mnd // lookup table values are from the MPEG spec.
var sampleRateTable = [3][4]int{
	{44100, 48000, 32000, 0}, // MPEG1
	{22050, 24000, 16000, 0}, // MPEG2
	{11025, 12000, 8000, 0},  // MPEG2.5
}

// samplesPerFrame returns the number of PCM samples per MP3 frame
// for the given MPEG version (Layer 3 only).
//
//nolint:mnd // constants from the MPEG spec.
func samplesPerFrame(version int) int {
	if version == mpegVersion1 {
		return 1152
	}

	return 576 // MPEG2 / MPEG2.5
}

// mp3BitDepth is the effective bit depth for decoded MP3 audio.
// The MPEG standard decodes to 16-bit PCM.
const mp3BitDepth = 16

// getMP3Duration computes the duration of an MP3 file in
// milliseconds by reading only the first frame's header and any
// Xing/VBRI VBR header it contains.  For CBR files (no VBR header)
// it falls back to fileSize / bitrate.  It also returns audio
// properties extracted from the frame header.
//
// The file position is undefined after this call.
func getMP3Duration(
	f *os.File,
) (int64, *AudioProperties, error) {
	// 1. Skip all leading ID3v2 tags.  Some files have multiple
	//    consecutive tags from different tagging tools.
	audioStart, err := skipID3v2(f)
	if err != nil {
		return 0, nil, fmt.Errorf(
			"skipping ID3v2: %w", err,
		)
	}

	audioStart, err = skipAdditionalID3v2(f, audioStart)
	if err != nil {
		return 0, nil, fmt.Errorf(
			"skipping additional ID3v2 tags: %w", err,
		)
	}

	// 2. Find and parse the first MP3 frame header.
	hdr, frameOffset, err := findFrameHeader(f, audioStart)
	if err != nil {
		return 0, nil, err
	}

	// Build audio properties from the frame header.
	channels := 2
	if hdr.channelMode == 3 { //nolint:mnd // 3 = mono
		channels = 1
	}

	props := &AudioProperties{
		SampleRate: hdr.sampleRate,
		BitDepth:   mp3BitDepth,
		Channels:   channels,
		Bitrate:    hdr.bitrateKbps,
	}

	// 3. Attempt to read a VBR header (Xing/Info or VBRI) from
	//    inside the first frame.
	vbrFrames, found, err := readVBRHeader(
		f, hdr, frameOffset,
	)
	if err != nil {
		return 0, nil, err
	}

	if found && vbrFrames > 0 {
		spf := samplesPerFrame(hdr.version)
		durationMS := int64(vbrFrames) *
			int64(spf) * 1000 / int64(hdr.sampleRate)

		return durationMS, props, nil
	}

	// 4. CBR fallback: duration = audioBytes * 8 / bitrate.
	fi, err := f.Stat()
	if err != nil {
		return 0, nil, fmt.Errorf(
			"stat file for CBR duration: %w", err,
		)
	}

	audioBytes := fi.Size() - audioStart
	durationMS := audioBytes * 8 * 1000 /
		(int64(hdr.bitrateKbps) * 1000)

	return durationMS, props, nil
}

// mpegFrameHeader holds the parsed fields of a 4-byte MPEG audio
// frame header.
type mpegFrameHeader struct {
	version     int // mpegVersion1, mpegVersion2, mpegVersion2_5
	bitrateKbps int
	sampleRate  int
	channelMode int // 0-3; 3 = mono
	padding     int // 0 or 1
}

// skipID3v2 checks for an ID3v2 tag at the start of f and returns
// the byte offset where audio data begins.
//
//nolint:mnd // byte offsets from the ID3v2 spec.
func skipID3v2(f *os.File) (int64, error) {
	var buf [10]byte

	if _, err := f.ReadAt(buf[:], 0); err != nil {
		return 0, fmt.Errorf("reading ID3v2 header: %w", err)
	}

	if string(buf[:3]) != "ID3" {
		return 0, nil // no ID3v2 tag
	}

	// Syncsafe integer: 4 bytes, each using 7 bits.
	size := int64(buf[6])<<21 |
		int64(buf[7])<<14 |
		int64(buf[8])<<7 |
		int64(buf[9])

	return 10 + size, nil
}

// skipAdditionalID3v2 looks for further ID3v2 tags starting at
// offset and advances past each one found.  This handles files
// where multiple tagging tools have each prepended their own ID3v2
// header.
//
//nolint:mnd // byte offsets from the ID3v2 spec.
func skipAdditionalID3v2(
	f *os.File,
	offset int64,
) (int64, error) {
	var buf [10]byte

	for range maxID3v2Tags {
		if _, err := f.ReadAt(buf[:], offset); err != nil {
			// EOF or short read means no more tags.
			return offset, nil //nolint:nilerr
		}

		if string(buf[:3]) != "ID3" {
			return offset, nil
		}

		size := int64(buf[6])<<21 |
			int64(buf[7])<<14 |
			int64(buf[8])<<7 |
			int64(buf[9])

		offset += 10 + size
	}

	return offset, nil
}

// findFrameHeader scans from startOffset for the first valid MP3
// sync word and returns the parsed header plus the file offset
// where the frame begins.
//
//nolint:mnd,cyclop // bit manipulation from the MPEG spec.
func findFrameHeader(
	f *os.File,
	startOffset int64,
) (mpegFrameHeader, int64, error) {
	if _, err := f.Seek(startOffset, io.SeekStart); err != nil {
		return mpegFrameHeader{}, 0, fmt.Errorf(
			"seeking to audio start: %w", err,
		)
	}

	// Read a chunk large enough to contain the first frame.
	buf := make([]byte, maxSyncSearchBytes)

	n, err := io.ReadAtLeast(f, buf, 4)
	if err != nil {
		return mpegFrameHeader{}, 0, fmt.Errorf(
			"reading audio data: %w", err,
		)
	}

	buf = buf[:n]

	for i := 0; i <= len(buf)-4; i++ {
		// Sync word: 11 set bits (0xFF followed by 0xE0 mask).
		if buf[i] != 0xFF || buf[i+1]&0xE0 != 0xE0 {
			continue
		}

		hdr, ok := parseFrameHeader(buf[i : i+4])
		if !ok {
			continue
		}

		return hdr, startOffset + int64(i), nil
	}

	return mpegFrameHeader{}, 0, errNoSyncWord
}

// parseFrameHeader decodes a 4-byte MPEG audio frame header.
// Returns false if the header contains invalid field combinations.
//
//nolint:mnd,cyclop // bit manipulation from the MPEG spec.
func parseFrameHeader(b []byte) (mpegFrameHeader, bool) {
	version := int((b[1] >> 3) & 0x03)
	layer := int((b[1] >> 1) & 0x03)

	// We only handle Layer 3.
	if layer != 1 { // Layer encoding: 1 = Layer 3
		return mpegFrameHeader{}, false
	}

	// Determine version index for the bitrate table.
	var bitrateIdx int

	switch version {
	case mpegVersion1:
		bitrateIdx = 0
	case mpegVersion2, mpegVersion2_5:
		bitrateIdx = 1
	default:
		return mpegFrameHeader{}, false // reserved
	}

	brIndex := int((b[2] >> 4) & 0x0F)
	bitrate := bitrateTable[bitrateIdx][brIndex]

	if bitrate == 0 {
		return mpegFrameHeader{}, false
	}

	// Sample rate.
	var srVersionIdx int

	switch version {
	case mpegVersion1:
		srVersionIdx = 0
	case mpegVersion2:
		srVersionIdx = 1
	case mpegVersion2_5:
		srVersionIdx = 2
	}

	srIndex := int((b[2] >> 2) & 0x03)
	sampleRate := sampleRateTable[srVersionIdx][srIndex]

	if sampleRate == 0 {
		return mpegFrameHeader{}, false
	}

	padding := int((b[2] >> 1) & 0x01)
	channelMode := int((b[3] >> 6) & 0x03)

	return mpegFrameHeader{
		version:     version,
		bitrateKbps: bitrate,
		sampleRate:  sampleRate,
		channelMode: channelMode,
		padding:     padding,
	}, true
}

// readVBRHeader tries to read a Xing/Info or VBRI header from the
// first frame at frameOffset.  Returns the total frame count and
// whether a VBR header was found.
//
//nolint:mnd // byte offsets from Xing/VBRI specs.
func readVBRHeader(
	f *os.File,
	hdr mpegFrameHeader,
	frameOffset int64,
) (uint32, bool, error) {
	// Xing/Info header offset depends on version and channel mode.
	var sideInfoSize int

	switch {
	case hdr.version == mpegVersion1 && hdr.channelMode != 3:
		sideInfoSize = 32
	case hdr.version == mpegVersion1 && hdr.channelMode == 3:
		sideInfoSize = 17
	case hdr.channelMode != 3:
		sideInfoSize = 17
	default:
		sideInfoSize = 9
	}

	// The Xing header sits right after the 4-byte frame header +
	// side information.
	xingOffset := frameOffset + 4 + int64(sideInfoSize)

	// Read enough bytes for Xing header (magic + flags + frames).
	var xingBuf [12]byte

	if _, err := f.ReadAt(xingBuf[:], xingOffset); err != nil {
		if errors.Is(err, io.EOF) {
			return 0, false, nil
		}

		return 0, false, fmt.Errorf(
			"reading Xing header: %w", err,
		)
	}

	magic := string(xingBuf[:4])
	if magic == "Xing" || magic == "Info" {
		flags := binary.BigEndian.Uint32(xingBuf[4:8])

		// Bit 0 of flags indicates the frames field is present.
		if flags&0x01 != 0 {
			frames := binary.BigEndian.Uint32(xingBuf[8:12])

			return frames, true, nil
		}

		// Xing header present but no frame count — fall through
		// to CBR fallback.
		return 0, true, nil
	}

	// VBRI header is always at a fixed offset of 36 bytes from
	// the frame start (regardless of version/channel mode).
	vbriOffset := frameOffset + 36

	var vbriBuf [26]byte

	if _, err := f.ReadAt(vbriBuf[:], vbriOffset); err != nil {
		if errors.Is(err, io.EOF) {
			return 0, false, nil
		}

		return 0, false, fmt.Errorf(
			"reading VBRI header: %w", err,
		)
	}

	if string(vbriBuf[:4]) == "VBRI" {
		// Total frames at offset 14 from VBRI magic.
		frames := binary.BigEndian.Uint32(vbriBuf[14:18])

		return frames, true, nil
	}

	return 0, false, nil
}
