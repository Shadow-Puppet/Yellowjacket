package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"yellowjacket/backend/tagwriter"
)

// Synthesis parameters.  Mono 22.05 kHz keeps the whole fixture
// library in the single-digit megabytes while staying a format every
// decoder in the app handles.
const (
	sampleRate    = 22050
	amplitude     = 0.3
	fadeSeconds   = 0.02
	jpegQuality   = 80
	coverSizePx   = 64
	ffmpegTimeout = 2 * time.Minute
)

var errFFmpegMissing = errors.New(
	"ffmpeg not found in PATH; install it to generate fixtures",
)

// synthesizeWAV writes a mono 16-bit WAV holding a sine wave at freqHz
// for the given duration, with a short fade at each end so lossy
// encoders do not introduce a click that shifts the reported length.
//
// The waveform is a pure function of (duration, freqHz), which is what
// makes a fixture reproducible: the same spec always yields the same
// PCM, and a decoded sample identifies which track is playing.
func synthesizeWAV(path string, dur time.Duration, freqHz float64) error {
	total := int(float64(sampleRate) * dur.Seconds())
	fade := int(sampleRate * fadeSeconds)

	pcm := make([]byte, total*2)

	for i := range total {
		t := float64(i) / sampleRate
		v := math.Sin(2*math.Pi*freqHz*t) * amplitude

		switch {
		case i < fade:
			v *= float64(i) / float64(fade)
		case i >= total-fade:
			v *= float64(total-i) / float64(fade)
		}

		binary.LittleEndian.PutUint16(
			pcm[i*2:], uint16(int16(v*math.MaxInt16)),
		)
	}

	return writeWAVContainer(path, pcm)
}

// writeWAVContainer wraps raw PCM in a canonical 44-byte RIFF header.
func writeWAVContainer(path string, pcm []byte) error {
	const (
		headerSize    = 44
		fmtChunkSize  = 16
		pcmFormat     = 1
		channels      = 1
		bitsPerSample = 16
	)

	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8

	buf := make([]byte, 0, headerSize+len(pcm))
	buf = append(buf, "RIFF"...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(36+len(pcm)))
	buf = append(buf, "WAVEfmt "...)
	buf = binary.LittleEndian.AppendUint32(buf, fmtChunkSize)
	buf = binary.LittleEndian.AppendUint16(buf, pcmFormat)
	buf = binary.LittleEndian.AppendUint16(buf, channels)
	buf = binary.LittleEndian.AppendUint32(buf, sampleRate)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(byteRate))
	buf = binary.LittleEndian.AppendUint16(buf, uint16(blockAlign))
	buf = binary.LittleEndian.AppendUint16(buf, bitsPerSample)
	buf = append(buf, "data"...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(pcm)))
	buf = append(buf, pcm...)

	if err := os.WriteFile(path, buf, filePerm); err != nil {
		return fmt.Errorf("write wav %s: %w", path, err)
	}

	return nil
}

// encodeArgs returns the ffmpeg codec arguments for a target format.
//
// Metadata is stripped (-map_metadata -1): every tag this library
// carries is written afterwards by backend/tagwriter, so the fixtures
// and the app's reader cannot drift apart.
func encodeArgs(format tagwriter.AudioFormat) ([]string, error) {
	switch format {
	case tagwriter.FormatMP3:
		return []string{"-c:a", "libmp3lame", "-q:a", "5"}, nil
	case tagwriter.FormatFLAC:
		return []string{"-c:a", "flac", "-compression_level", "5"}, nil
	case tagwriter.FormatOGG:
		return []string{"-c:a", "libvorbis", "-q:a", "2"}, nil
	case tagwriter.FormatWAV:
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: %s", errUnknownFormat, format)
	}
}

// transcode converts the synthesized WAV at src into dst's format.
func transcode(src, dst string, format tagwriter.AudioFormat) error {
	args, err := encodeArgs(format)
	if err != nil {
		return err
	}

	full := append([]string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-i", src, "-map_metadata", "-1",
	}, args...)
	full = append(full, dst)

	ctx, cancel := context.WithTimeout(context.Background(), ffmpegTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "ffmpeg", full...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg %s: %w: %s", dst, err, out)
	}

	return nil
}

// requireFFmpeg fails early with an actionable message rather than
// letting the first transcode blow up halfway through generation.
func requireFFmpeg() error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return errFFmpegMissing
	}

	return nil
}

// coverJPEG renders a small, deterministic cover image for a key.
//
// Identical keys produce byte-identical JPEGs, which is exactly what
// the library's cover-art deduplication is supposed to collapse into a
// single stored blob.
func coverJPEG(key string) ([]byte, error) {
	return coverJPEGSized(key, coverSizePx)
}

// coverJPEGSized is coverJPEG at an explicit edge length.  The bulk
// library uses a larger one, because a 64 px cover cannot show the
// difference between rendering the original artwork and rendering the
// thumbnail tier that exists for the purpose.
// coverJPEGSized is coverJPEG at an explicit edge length.  The bulk
// library uses a larger one, because a 64 px cover cannot show the
// difference between rendering the original artwork and rendering the
// thumbnail tier that exists for the purpose.
func coverJPEGSized(key string, px int) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, px, px))

	// A per-key hue derived from the key's bytes, plus a diagonal
	// band, so covers are distinguishable by eye in a screenshot.
	var seed uint32
	for _, b := range []byte(key) {
		seed = seed*31 + uint32(b)
	}

	base := color.RGBA{
		R: uint8(seed >> 16),
		G: uint8(seed >> 8),
		B: uint8(seed),
		A: 255,
	}

	for y := range px {
		for x := range px {
			c := base
			if (x+y)%16 < 8 {
				c.R /= 2
				c.G /= 2
				c.B /= 2
			}

			img.Set(x, y, c)
		}
	}

	var buf bytes.Buffer

	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, fmt.Errorf("encode cover %q: %w", key, err)
	}

	return buf.Bytes(), nil
}

// stampMTime pins a fixture's modification time.  The library scanner
// keys incremental rescans off audio_files.modified_at, so a fixed
// mtime makes "has this changed since the last scan" reproducible.
func stampMTime(path string) error {
	if err := os.Chtimes(path, fixedMTime, fixedMTime); err != nil {
		return fmt.Errorf("chtimes %s: %w", path, err)
	}

	return nil
}

// ensureDir creates a fixture's parent directory.
func ensureDir(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}

	return nil
}
