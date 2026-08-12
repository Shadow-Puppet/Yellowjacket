// Command gentestdata generates the deterministic fixture library used
// by tests and by seeded development sandboxes.
//
// The fixtures are audio the app can actually decode, tagged by
// backend/tagwriter — the same writers the application uses — so the
// fixtures and the reader under test cannot drift apart.  Everything is
// derived from the spec in spec.go, so two machines running
// `make testdata` get libraries that agree on every logical property
// (paths, durations, tags, cover identity).  Encoded bytes may differ
// between ffmpeg builds; the manifest hash covers the spec, not bytes.
//
// Usage:
//
//	go run ./cmd/gentestdata            # generate if out of date
//	go run ./cmd/gentestdata -force     # regenerate unconditionally
//	go run ./cmd/gentestdata -bulk 50000  # the measurement library
//
// The -bulk library is a separate thing with a separate purpose; see
// bulk.go.  It is not committed, not a test dependency, and generating
// it does not regenerate the fixture library.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yellowjacket/backend/tagwriter"
)

// Fixed file modes and mtime.  The scanner keys incremental rescan off
// modified_at, so pinning mtime makes "changed since last scan"
// reproducible rather than a function of when generation ran.
const (
	filePerm = 0o644
	dirPerm  = 0o755
)

//nolint:gochecknoglobals // a package-level constant time value.
var fixedMTime = time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)

func main() {
	var (
		outDir      string
		brokenDir   string
		manifestOut string
		force       bool
		bulkTracks  int
		bulkOut     string
		bulkCover   int
	)

	flag.StringVar(
		&outDir, "out", "test_data/music_library_test",
		"library root to generate",
	)
	flag.StringVar(
		&brokenDir, "broken", "test_data/music_library_broken",
		"root for deliberately malformed files",
	)
	flag.StringVar(
		&manifestOut, "manifest", "test_data/music_library_test.manifest.json",
		"manifest path (kept outside the library root)",
	)
	flag.BoolVar(
		&force, "force", false,
		"regenerate even when the manifest is already up to date",
	)
	flag.IntVar(
		&bulkTracks, "bulk", 0,
		"generate a bulk measurement library of N tracks instead",
	)
	flag.StringVar(
		&bulkOut, "bulk-out", ".dev/music_library_bulk",
		"library root for -bulk (gitignored; not a test fixture)",
	)
	flag.IntVar(
		&bulkCover, "bulk-cover-px", bulkCoverPx,
		"edge length of the embedded cover art for -bulk",
	)
	flag.Parse()

	var err error

	if bulkTracks > 0 {
		err = generateBulk(bulkSpec{
			Out:     bulkOut,
			Tracks:  bulkTracks,
			CoverPx: bulkCover,
		}, force)
	} else {
		err = run(outDir, brokenDir, manifestOut, force)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "gentestdata:", err)
		os.Exit(1)
	}
}

func run(outDir, brokenDir, manifestOut string, force bool) error {
	want, err := buildManifest(outDir, brokenDir)
	if err != nil {
		return err
	}

	if !force && upToDate(manifestOut, want, outDir, brokenDir) {
		fmt.Printf(
			"up to date (%d tracks, hash %s)\n",
			len(want.Tracks), want.Hash[:12],
		)

		return nil
	}

	if err := requireFFmpeg(); err != nil {
		return err
	}

	for _, dir := range []string{outDir, brokenDir} {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("clean %s: %w", dir, err)
		}
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	for _, f := range libraryFixtures {
		if err := generateFixture(logger, outDir, f); err != nil {
			return err
		}
	}

	if err := writeAuxFiles(outDir, outDir, libraryExtras); err != nil {
		return err
	}

	if err := writeAuxFiles(outDir, brokenDir, brokenFiles); err != nil {
		return err
	}

	if err := writeManifest(manifestOut, want); err != nil {
		return err
	}

	fmt.Printf(
		"generated %d tracks + %d broken files in %s (hash %s)\n",
		len(want.Tracks), len(want.Broken), outDir, want.Hash[:12],
	)

	return nil
}

// upToDate reports whether the recorded manifest matches the spec and
// both roots still exist.  Cheap enough to run on every make invocation.
func upToDate(manifestOut string, want *manifest, roots ...string) bool {
	if readManifestHash(manifestOut) != want.Hash {
		return false
	}

	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			return false
		}
	}

	return true
}

// generateFixture synthesizes, encodes and tags a single fixture.
//
// Tags are written after encoding, by backend/tagwriter, rather than
// handed to ffmpeg: the fixtures must be tagged by the code the app
// reads back with, or a tag bug becomes invisible to every test.
func generateFixture(
	logger *slog.Logger,
	root string,
	f fixture,
) error {
	dst := filepath.Join(root, filepath.FromSlash(f.Rel))

	if err := ensureDir(dst); err != nil {
		return err
	}

	wav := dst
	if f.Format != tagwriter.FormatWAV {
		wav = dst + ".src.wav"
	}

	if err := synthesizeWAV(wav, f.Duration, f.FreqHz); err != nil {
		return err
	}

	if f.Format != tagwriter.FormatWAV {
		if err := transcode(wav, dst, f.Format); err != nil {
			return err
		}

		if err := os.Remove(wav); err != nil {
			return fmt.Errorf("remove scratch wav: %w", err)
		}
	}

	changes := f.Tags.changes()

	if f.Cover != "" {
		img, err := coverJPEG(f.Cover)
		if err != nil {
			return err
		}

		changes[tagwriter.FieldCoverArt] = img
	}

	if len(changes) > 0 {
		if err := tagwriter.WriteFileTags(logger, dst, changes); err != nil {
			return fmt.Errorf("tag %s: %w", f.Rel, err)
		}
	}

	return stampMTime(dst)
}

// writeAuxFiles writes non-audio and malformed files into dstRoot.
//
// Truncated fixtures are cut from an already-encoded file under
// libraryRoot, so this must run after the audio has been generated.
// The malformed set lands outside the library root on purpose: the
// clean library's track count has to stay deterministic, so a test
// that wants the scanner's error paths registers the broken root as a
// second library deliberately.
func writeAuxFiles(libraryRoot, dstRoot string, files []auxFile) error {
	for _, b := range files {
		dst := filepath.Join(dstRoot, filepath.FromSlash(b.Rel))

		if err := ensureDir(dst); err != nil {
			return err
		}

		var content []byte

		switch {
		case b.Source != "":
			src := filepath.Join(libraryRoot, filepath.FromSlash(b.Source))

			raw, err := os.ReadFile(src) //nolint:gosec // generated path.
			if err != nil {
				return fmt.Errorf("read source %s: %w", src, err)
			}

			content = raw[:min(b.Bytes, len(raw))]
		case strings.HasSuffix(b.Rel, ".jpg"):
			img, err := coverJPEG(b.Rel)
			if err != nil {
				return err
			}

			content = img
		default:
			content = []byte(b.Literal)
		}

		if err := os.WriteFile(dst, content, filePerm); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}

		if err := stampMTime(dst); err != nil {
			return err
		}
	}

	return nil
}
