package tagwriter

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"yellowjacket/backend/fileutil"
)

// Sentinel errors for OGG operations.
var (
	errNotOgg           = errors.New("not an OGG file")
	errNotVorbis        = errors.New("not an OGG Vorbis file")
	errOggMultiStream   = errors.New("this OGG file contains multiple streams and cannot be edited")
	errOggTruncated     = errors.New("OGG file is truncated")
	errOggBadVersion    = errors.New("unsupported OGG version")
	errOggTooFewPackets = errors.New("ogg vorbis: expected at least 3 header packets")
	errOggNoPages       = errors.New("OGG file contains no pages")
	errOggMissingSetup  = errors.New("OGG file is missing the setup header packet")
)

// oggCRCTable is the pre-computed 256-entry CRC32 lookup table for OGG.
// OGG uses the standard CRC32 polynomial 0x04c11db7 with MSB-first
// (unreflected) bit ordering.  Go's hash/crc32 package uses reflected
// (LSB-first) ordering and CANNOT be used here.
var oggCRCTable [256]uint32 //nolint:gochecknoglobals // spec-mandated lookup table

func init() {
	const poly = 0x04c11db7 //nolint:mnd // OGG CRC32 polynomial

	for i := range 256 {
		crc := uint32(i) << 24 //nolint:mnd // MSB-first table generation

		for range 8 {
			if crc&(1<<31) != 0 { //nolint:mnd // check MSB
				crc = (crc << 1) ^ poly
			} else {
				crc <<= 1
			}
		}

		oggCRCTable[i] = crc
	}
}

// oggCRC computes the OGG CRC32 checksum over data using the MSB-first
// (unreflected) algorithm.  Initial value is 0; no final XOR.
func oggCRC(data []byte) uint32 {
	var crc uint32

	for _, b := range data {
		crc = (crc << 8) ^ oggCRCTable[(crc>>24)^uint32(b)] //nolint:mnd // MSB-first CRC update
	}

	return crc
}

// oggPage represents a single OGG page parsed from a file.
type oggPage struct {
	headerType   byte   // 0x01=continued, 0x02=bos, 0x04=eos
	granulePos   int64  // granule position (LE)
	serialNo     uint32 // stream serial number (LE)
	seqNo        uint32 // page sequence number (LE)
	segmentTable []byte // lacing values (each 0-255)
	data         []byte // page body (sum of lacing values bytes)
}

// parseOggPages reads the entire file and parses all OGG pages.
// CRC mismatches produce a warning but do not reject the file
// (lenient-read per project convention).  Truncated files are rejected.
// After parsing, the function validates that the stream is a single-stream
// OGG Vorbis file (no multi-stream, no chained streams, no non-Vorbis).
func parseOggPages(logger *slog.Logger, filePath string) ([]oggPage, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read ogg file: %w", err)
	}

	if len(raw) < 27 { //nolint:mnd // minimum OGG page header size
		return nil, errOggTruncated
	}

	var pages []oggPage

	offset := 0

	for offset < len(raw) {
		// Verify capture pattern "OggS".
		if offset+4 > len(raw) || string(raw[offset:offset+4]) != "OggS" {
			return nil, fmt.Errorf("%w: bad capture pattern at offset %d", errNotOgg, offset)
		}

		// Need at least 27 bytes for the fixed header.
		if offset+27 > len(raw) { //nolint:mnd // OGG page header size
			return nil, fmt.Errorf("%w: header truncated at offset %d", errOggTruncated, offset)
		}

		version := raw[offset+4]
		if version != 0 {
			return nil, fmt.Errorf("%w: version %d at offset %d", errOggBadVersion, version, offset)
		}

		headerType := raw[offset+5]
		granulePos := int64(binary.LittleEndian.Uint64(raw[offset+6 : offset+14]))
		serialNo := binary.LittleEndian.Uint32(raw[offset+14 : offset+18])
		seqNo := binary.LittleEndian.Uint32(raw[offset+18 : offset+22])
		storedCRC := binary.LittleEndian.Uint32(raw[offset+22 : offset+26])
		numSegments := int(raw[offset+26])

		// Read segment table.
		segStart := offset + 27 //nolint:mnd // fixed header size
		segEnd := segStart + numSegments

		if segEnd > len(raw) {
			return nil, fmt.Errorf(
				"%w: segment table truncated at offset %d",
				errOggTruncated,
				offset,
			)
		}

		segmentTable := make([]byte, numSegments)
		copy(segmentTable, raw[segStart:segEnd])

		// Compute total page body size from lacing values.
		bodySize := 0
		for _, s := range segmentTable {
			bodySize += int(s)
		}

		dataStart := segEnd
		dataEnd := dataStart + bodySize

		if dataEnd > len(raw) {
			return nil, fmt.Errorf("%w: page data truncated at offset %d", errOggTruncated, offset)
		}

		pageData := make([]byte, bodySize)
		copy(pageData, raw[dataStart:dataEnd])

		// CRC check: compute over entire page with CRC field zeroed.
		pageBytes := make([]byte, dataEnd-offset)
		copy(pageBytes, raw[offset:dataEnd])
		// Zero CRC field at offset 22-25 relative to page start.
		pageBytes[22] = 0
		pageBytes[23] = 0
		pageBytes[24] = 0
		pageBytes[25] = 0

		computedCRC := oggCRC(pageBytes)
		if computedCRC != storedCRC {
			logger.Warn("OGG page CRC mismatch (lenient read, continuing)",
				slog.Int("page", len(pages)),
				slog.Int("offset", offset),
				slog.String("stored", fmt.Sprintf("0x%08x", storedCRC)),
				slog.String("computed", fmt.Sprintf("0x%08x", computedCRC)),
			)
		}

		pages = append(pages, oggPage{
			headerType:   headerType,
			granulePos:   granulePos,
			serialNo:     serialNo,
			seqNo:        seqNo,
			segmentTable: segmentTable,
			data:         pageData,
		})

		offset = dataEnd
	}

	if len(pages) == 0 {
		return nil, errOggNoPages
	}

	// Validate: single stream (no multi-stream, no chained).
	serialNumbers := make(map[uint32]struct{})
	bosCount := 0

	for _, p := range pages {
		serialNumbers[p.serialNo] = struct{}{}

		if p.headerType&0x02 != 0 {
			bosCount++
		}
	}

	if len(serialNumbers) > 1 || bosCount > 1 {
		return nil, errOggMultiStream
	}

	// Validate: first page is Vorbis identification header.
	firstPacketData := pages[0].data
	if len(firstPacketData) < 7 ||
		firstPacketData[0] != 0x01 || //nolint:mnd // Vorbis ID header magic
		string(firstPacketData[1:7]) != "vorbis" {
		return nil, errNotVorbis
	}

	return pages, nil
}

// writeOggPage serializes a single OGG page to w with a correctly
// computed CRC32 checksum.
func writeOggPage(w io.Writer, page oggPage) error {
	numSegments := len(page.segmentTable)
	headerSize := 27 + numSegments //nolint:mnd // fixed OGG header + segment table

	buf := make([]byte, headerSize+len(page.data))

	// Write capture pattern.
	copy(buf[0:4], "OggS")
	// Version = 0.
	buf[4] = 0
	// Header type.
	buf[5] = page.headerType
	// Granule position (LE).
	binary.LittleEndian.PutUint64(buf[6:14], uint64(page.granulePos))
	// Serial number (LE).
	binary.LittleEndian.PutUint32(buf[14:18], page.serialNo)
	// Sequence number (LE).
	binary.LittleEndian.PutUint32(buf[18:22], page.seqNo)
	// CRC placeholder (zeroed for computation).
	buf[22] = 0
	buf[23] = 0
	buf[24] = 0
	buf[25] = 0
	// Number of segments.
	buf[26] = byte(numSegments)
	// Segment table.
	copy(buf[27:27+numSegments], page.segmentTable)
	// Page data.
	copy(buf[headerSize:], page.data)

	// Compute CRC over entire page (with CRC field = 0) and patch.
	crc := oggCRC(buf)
	binary.LittleEndian.PutUint32(buf[22:26], crc)

	_, err := w.Write(buf)
	if err != nil {
		return fmt.Errorf("write ogg page: %w", err)
	}

	return nil
}

// extractPackets reassembles logical packets from OGG pages using
// lacing values.  A segment with value 255 continues the packet;
// a value <255 terminates it.
func extractPackets(pages []oggPage) [][]byte {
	var packets [][]byte

	var current []byte

	for _, page := range pages {
		dataOffset := 0

		for _, lacing := range page.segmentTable {
			segSize := int(lacing)
			current = append(current, page.data[dataOffset:dataOffset+segSize]...)
			dataOffset += segSize

			// A lacing value <255 terminates the packet.
			if lacing < 255 { //nolint:mnd // OGG lacing value max
				packets = append(packets, current)
				current = nil
			}
		}
	}

	// If we still have data, it's a continuation that didn't terminate
	// (shouldn't happen in a well-formed file, but handle gracefully).
	if len(current) > 0 {
		packets = append(packets, current)
	}

	return packets
}

// splitPacketIntoSegments splits a packet into 255-byte segments
// plus a final shorter segment.  If the packet length is an exact
// multiple of 255, a 0-length terminating segment is appended.
func splitPacketIntoSegments(packet []byte) []byte {
	const segSize = 255

	var segments []byte

	for len(packet) >= segSize {
		segments = append(segments, segSize)
		packet = packet[segSize:]
	}

	// Final segment (0..254 bytes) — terminates the packet.
	segments = append(segments, byte(len(packet)))

	return segments
}

// buildPagesFromSegments builds OGG pages from serialized segment data,
// respecting the 255-segment-per-page limit.  The first page gets the
// headerType as-is; continuation pages get the continued flag (0x01)
// ORed in.  All pages share the same serialNo and granulePos.
func buildPagesFromSegments(
	segments []byte,
	packetData []byte,
	serialNo uint32,
	granulePos int64,
) []oggPage {
	const maxSegmentsPerPage = 255

	var pages []oggPage

	segIdx := 0
	dataIdx := 0

	for segIdx < len(segments) {
		// Determine how many segments fit on this page.
		end := segIdx + maxSegmentsPerPage
		if end > len(segments) {
			end = len(segments)
		}

		pageSegs := segments[segIdx:end]

		// Compute data for this page.
		pageDataSize := 0
		for _, s := range pageSegs {
			pageDataSize += int(s)
		}

		pageData := make([]byte, pageDataSize)
		copy(pageData, packetData[dataIdx:dataIdx+pageDataSize])

		headerType := byte(0x00)
		if segIdx > 0 {
			// Continuation page.
			headerType = 0x01
		}

		segTable := make([]byte, len(pageSegs))
		copy(segTable, pageSegs)

		pages = append(pages, oggPage{
			headerType:   headerType,
			granulePos:   granulePos,
			serialNo:     serialNo,
			segmentTable: segTable,
			data:         pageData,
		})

		segIdx = end
		dataIdx += pageDataSize
	}

	return pages
}

// buildHeaderPages builds OGG pages for the comment and setup packets
// combined.  Both packets go into shared pages (per Vorbis spec),
// with the setup packet ending on a page boundary.
func buildHeaderPages(commentPacket, setupPacket []byte, serialNo uint32) []oggPage {
	// Build segment lacing values for both packets.
	commentSegs := splitPacketIntoSegments(commentPacket)
	setupSegs := splitPacketIntoSegments(setupPacket)

	// Concatenate all segments from both packets.
	allSegs := append(commentSegs, setupSegs...) //nolint:gocritic // intentional append

	// Concatenate all packet data.
	allData := append(commentPacket, setupPacket...) //nolint:gocritic // intentional append

	// Build pages from the combined segments, respecting 255-per-page limit.
	// Granule position for header pages is 0 per Vorbis spec.
	pages := buildPagesFromSegments(allSegs, allData, serialNo, 0)

	return pages
}

// writeOggTags is the entry point for writing metadata to an OGG Vorbis file.
// It reads all pages, modifies the Vorbis Comment packet, and rewrites the
// entire file atomically via fileutil.AtomicWrite.
func writeOggTags(logger *slog.Logger, filePath string, changes TagChanges) error {
	// Warn for very large files (same threshold as FLAC/WAV writers).
	if info, err := os.Stat(filePath); err == nil {
		const largeSizeThreshold = 500 * 1024 * 1024 //nolint:mnd // 500 MB

		if info.Size() > largeSizeThreshold {
			logger.Warn("large OGG file may use significant memory",
				slog.String("path", filePath),
				slog.Int64("size", info.Size()),
			)
		}
	}

	// 1. Read and parse all OGG pages (lenient CRC).
	pages, err := parseOggPages(logger, filePath)
	if err != nil {
		return fmt.Errorf("parse ogg: %w", err)
	}

	// 2. Extract the 3 header packets from pages.
	// The first page (bos) contains the identification packet.
	// Subsequent header pages contain the comment and setup packets.
	packets := extractPackets(pages)

	const minHeaderPackets = 3

	if len(packets) < minHeaderPackets {
		return fmt.Errorf("%w: got %d", errOggTooFewPackets, len(packets))
	}

	_ = packets[0] // identification packet — preserved as page 0 unchanged
	commentPacket := packets[1]
	setupPacket := packets[2]

	// Verify setup header magic.
	if len(setupPacket) < 7 || setupPacket[0] != 0x05 || //nolint:mnd // Vorbis setup header type
		string(setupPacket[1:7]) != "vorbis" {
		return errOggMissingSetup
	}

	// 3. Parse Vorbis Comment from the comment packet.
	vc, err := parseVorbisCommentPacket(commentPacket)
	if err != nil {
		return fmt.Errorf("parse vorbis comment: %w", err)
	}

	// 4. Apply text changes.
	applyOggTextChanges(vc, changes)

	// 5. Apply cover art changes.
	applyOggCoverArt(vc, changes)

	// 6. Serialize modified Vorbis Comment back to packet bytes.
	newCommentPacket := serializeVorbisCommentPacket(vc)

	// 7. Get the serial number from the first page.
	serialNo := pages[0].serialNo

	// 8. Rebuild the page list.
	var rebuilt []oggPage

	// Page 0: identification header (copy original bos page unchanged).
	rebuilt = append(rebuilt, pages[0])

	// New header pages: comment + setup.
	headerPages := buildHeaderPages(newCommentPacket, setupPacket, serialNo)
	rebuilt = append(rebuilt, headerPages...)

	// Audio pages: find where audio starts in the original pages.
	// Audio pages are all pages after the header pages.
	// We need to find the first page that contains audio data.
	// The header packets (ident, comment, setup) span some number of pages.
	// We identify audio pages by skipping pages until we've consumed
	// all 3 header packets.
	audioStartIdx := findAudioPageStart(pages)
	rebuilt = append(rebuilt, pages[audioStartIdx:]...)

	// 9. Renumber ALL page sequence numbers sequentially from 0.
	for i := range rebuilt {
		rebuilt[i].seqNo = uint32(i)
	}

	// 10. Write via AtomicWrite.
	return fileutil.AtomicWrite(logger, filePath, func(tmp *os.File) error {
		w := io.Writer(tmp)
		for _, page := range rebuilt {
			if err := writeOggPage(w, page); err != nil {
				return err
			}
		}

		return nil
	})
}

// findAudioPageStart determines the index of the first audio page
// in the parsed page list.  It walks through pages consuming lacing
// values until 3 header packets have been fully read.
func findAudioPageStart(pages []oggPage) int {
	packetsCompleted := 0

	const headerPacketCount = 3

	for i, page := range pages {
		for _, lacing := range page.segmentTable {
			if lacing < 255 { //nolint:mnd // OGG lacing value terminates packet
				packetsCompleted++

				if packetsCompleted >= headerPacketCount {
					// All header packets consumed.  If this is the last
					// segment on this page, audio starts on the next page.
					// If there are more segments, they belong to the next
					// page's audio (but they're on this page, so audio
					// starts here too — but we've already included the
					// setup packet data, so the audio pages start at i+1).
					return i + 1
				}
			}
		}
	}

	// Fallback: shouldn't happen in a valid file.  Return after the
	// first page (identification header).
	if len(pages) > 1 {
		return 1
	}

	return len(pages)
}

// pageBytes serializes an OGG page to raw bytes (for CRC verification etc.).
func pageBytes(page oggPage) []byte {
	var buf bytes.Buffer

	_ = writeOggPage(&buf, page)

	return buf.Bytes()
}
