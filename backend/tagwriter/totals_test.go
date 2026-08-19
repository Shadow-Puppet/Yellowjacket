package tagwriter

import (
	"path/filepath"
	"testing"

	"yellowjacket/backend/metadata"
)

// The totals are the evidence GetAlbumCompleteness reads, and every way
// of getting them wrong is silent: a tag written under a name the
// reader does not look at reads back as no total at all, which is
// indistinguishable from never having written one.  So these assert the
// round trip through the *reader the scan uses*, not the bytes.
//
// WAV is the exception and it is not this change's: dhowden/tag has no
// RIFF reader at all, so metadata.ExtractTags cannot see a WAV's ID3
// chunk -- which is why every other test here reads that chunk itself.
func TestWriteTotals_RoundTripsInEveryFormat(t *testing.T) {
	t.Parallel()

	changes := TagChanges{
		FieldTitle:       "Some Song",
		FieldTrackNumber: 2,
		FieldTotalTracks: 10,
		FieldDiscNumber:  1,
		FieldTotalDiscs:  2,
	}

	viaScanner := func(t *testing.T, path string) *metadata.TrackMetadata {
		t.Helper()

		meta, err := metadata.ExtractTags(path)
		if err != nil {
			t.Fatalf("ExtractTags: %v", err)
		}

		return meta
	}

	tests := []struct {
		name  string
		write func(t *testing.T, dir string) string
		read  func(t *testing.T, path string) *metadata.TrackMetadata
	}{
		{
			name: "mp3",
			read: viaScanner,
			write: func(t *testing.T, dir string) string {
				t.Helper()

				path := createTestMP3(t, dir, "totals.mp3", nil)
				if err := writeMp3Tags(testLogger(), path, changes); err != nil {
					t.Fatalf("writeMp3Tags: %v", err)
				}

				return path
			},
		},
		{
			name: "flac",
			read: viaScanner,
			write: func(t *testing.T, dir string) string {
				t.Helper()

				path := filepath.Join(dir, "totals.flac")
				makeMinimalFLAC(t, path)

				if err := writeFlacTags(testLogger(), path, changes); err != nil {
					t.Fatalf("writeFlacTags: %v", err)
				}

				return path
			},
		},
		{
			name: "ogg",
			read: viaScanner,
			write: func(t *testing.T, dir string) string {
				t.Helper()

				path := filepath.Join(dir, "totals.ogg")
				createTestOGG(t, path)

				if err := writeOggTags(testLogger(), path, changes); err != nil {
					t.Fatalf("writeOggTags: %v", err)
				}

				return path
			},
		},
		{
			name: "wav",
			read: readWavID3Tags,
			write: func(t *testing.T, dir string) string {
				t.Helper()

				path := createTestWAV(t, dir, "totals.wav", nil)

				if err := writeWavTags(testLogger(), path, changes); err != nil {
					t.Fatalf("writeWavTags: %v", err)
				}

				return path
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			meta := tc.read(t, tc.write(t, t.TempDir()))

			assertIntField(t, "TrackNumber", meta.TrackNumber, 2)
			assertIntField(t, "TotalTracks", meta.TotalTracks, 10)
			assertIntField(t, "DiscNumber", meta.DiscNumber, 1)
			assertIntField(t, "TotalDiscs", meta.TotalDiscs, 2)
		})
	}
}

// A number and a total are separate diff entries, so writing one must
// not discard the other.  For ID3v2 they share a single "n/N" frame,
// which is the only place this can go wrong -- and it goes wrong by
// silently zeroing a total the file already declared.
func TestWriteMp3Totals_PartialUpdateKeepsTheOtherHalf(t *testing.T) {
	t.Parallel()

	t.Run("writing the number keeps the total", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := createTestMP3(t, dir, "seeded.mp3", TagChanges{
			FieldTrackNumber: 2,
			FieldTotalTracks: 10,
		})

		if err := writeMp3Tags(testLogger(), path, TagChanges{
			FieldTrackNumber: 4,
		}); err != nil {
			t.Fatalf("writeMp3Tags: %v", err)
		}

		meta, err := metadata.ExtractTags(path)
		if err != nil {
			t.Fatalf("ExtractTags: %v", err)
		}

		assertIntField(t, "TrackNumber", meta.TrackNumber, 4)
		assertIntField(t, "TotalTracks", meta.TotalTracks, 10)
	})

	t.Run("writing the total keeps the number", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := createTestMP3(t, dir, "seeded.mp3", TagChanges{
			FieldTrackNumber: 7,
		})

		if err := writeMp3Tags(testLogger(), path, TagChanges{
			FieldTotalTracks: 12,
		}); err != nil {
			t.Fatalf("writeMp3Tags: %v", err)
		}

		meta, err := metadata.ExtractTags(path)
		if err != nil {
			t.Fatalf("ExtractTags: %v", err)
		}

		assertIntField(t, "TrackNumber", meta.TrackNumber, 7)
		assertIntField(t, "TotalTracks", meta.TotalTracks, 12)
	})

	// "/12" says nothing a reader can use, and dhowden/tag reads it as
	// track 0 -- which the scan would store as a real track number.
	t.Run("a total with no number writes nothing", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := createTestMP3(t, dir, "bare.mp3", nil)

		if err := writeMp3Tags(testLogger(), path, TagChanges{
			FieldTotalTracks: 12,
		}); err != nil {
			t.Fatalf("writeMp3Tags: %v", err)
		}

		meta, err := metadata.ExtractTags(path)
		if err != nil {
			t.Fatalf("ExtractTags: %v", err)
		}

		assertIntField(t, "TrackNumber", meta.TrackNumber, 0)
		assertIntField(t, "TotalTracks", meta.TotalTracks, 0)
	})
}
