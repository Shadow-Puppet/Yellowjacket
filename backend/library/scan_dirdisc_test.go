package library

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/tagwriter"
	"yellowjacket/internal/testfixtures"
)

// copyFile copies an untagged real MP3 fixture (decodable, so
// metadata extraction and duration decoding both work exactly as
// they would on a real library file) to path.
func copyFile(t *testing.T, src, dst string) {
	t.Helper()

	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open fixture %s: %v", src, err)
	}

	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}

	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy fixture to %s: %v", dst, err)
	}
}

// writeTestTrack copies a real, untagged MP3 fixture to path and,
// when discNumber is non-zero, stamps a disc-number tag onto it via
// the same tagwriter path the app itself uses to write tags — a
// discNumber of 0 leaves the file untagged, exactly like a track
// whose disc frame was never set.
func writeTestTrack(t *testing.T, path string, discNumber int) {
	t.Helper()

	m := testfixtures.Load(t)
	blank := m.Abs("unsorted/no-tags-at-all.mp3")

	copyFile(t, blank, path)

	if discNumber == 0 {
		return
	}

	if err := tagwriter.WriteFileTags(
		slog.Default(), path,
		tagwriter.TagChanges{tagwriter.FieldDiscNumber: discNumber},
	); err != nil {
		t.Fatalf("write disc tag on %s: %v", path, err)
	}
}

// scanTestGroupKeys creates a library row at root, runs a real
// synchronous scan of it, and returns the group_key each resulting
// audio_files row landed on, keyed by absolute file path.
func scanTestGroupKeys(t *testing.T, lib *Library, root string) map[string]string {
	t.Helper()

	library, err := lib.db.Queries.CreateLibrary(lib.ctx, sqlcgen.CreateLibraryParams{
		Name: root,
		Path: root,
	})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	metrics := lib.scanInternal(library.ID, library.Name, library.Path)
	if metrics == nil {
		t.Fatal("scanInternal returned nil metrics")
	}

	rows, err := lib.db.Queries.GetAudioFilesByLibrary(lib.ctx, library.ID)
	if err != nil {
		t.Fatalf("list audio files: %v", err)
	}

	got := make(map[string]string, len(rows))
	for _, r := range rows {
		got[r.FilePath] = r.GroupKey
	}

	return got
}

// TestScan_PartialDiscTaggingWithinOneFolderDoesNotFragment guards the
// fix for a real-world bug: a folder where only some tracks carry an
// explicit disc tag (common when files were ripped or re-tagged at
// different times) must not split into two tagging groups for what is
// really one single-disc album. Before directory-batched disc
// resolution, each file resolved its own group_key from only its own
// tag, so an untagged track always folded to disc 1 regardless of
// what its siblings said — fragmenting a real disc 2 whenever even one
// of its tracks lacked the tag.
func TestScan_PartialDiscTaggingWithinOneFolderDoesNotFragment(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)

	root := t.TempDir()
	dir := filepath.Join(root, "Artist", "Album")

	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	track1 := filepath.Join(dir, "01.mp3")
	track2 := filepath.Join(dir, "02.mp3")
	track3 := filepath.Join(dir, "03.mp3")

	writeTestTrack(t, track1, 2) // explicit disc 2
	writeTestTrack(t, track2, 0) // untagged
	writeTestTrack(t, track3, 2) // explicit disc 2

	keys := scanTestGroupKeys(t, lib, root)

	if len(keys) != 3 { //nolint:mnd
		t.Fatalf("expected 3 audio files, got %d: %+v", len(keys), keys)
	}

	if keys[track1] != keys[track2] || keys[track1] != keys[track3] {
		t.Errorf(
			"expected all three tracks to share one group_key, got %+v",
			keys,
		)
	}
}

// TestScan_GenuineMultiDiscFolderStillSplits is the flip side of the
// partial-tagging fix: a folder with no per-disc subfolders where the
// explicit disc tags genuinely disagree (a real two-disc release
// dumped flat) must still separate into two groups — directory-wide
// consensus must not paper over an actual multi-disc release just
// because it shares one directory.
func TestScan_GenuineMultiDiscFolderStillSplits(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)

	root := t.TempDir()
	dir := filepath.Join(root, "Artist", "Album")

	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	disc1TrackA := filepath.Join(dir, "1-01.mp3")
	disc1TrackB := filepath.Join(dir, "1-02.mp3")
	disc2TrackA := filepath.Join(dir, "2-01.mp3")
	disc2TrackB := filepath.Join(dir, "2-02.mp3")

	writeTestTrack(t, disc1TrackA, 1)
	writeTestTrack(t, disc1TrackB, 1)
	writeTestTrack(t, disc2TrackA, 2) //nolint:mnd
	writeTestTrack(t, disc2TrackB, 2) //nolint:mnd

	keys := scanTestGroupKeys(t, lib, root)

	if len(keys) != 4 { //nolint:mnd
		t.Fatalf("expected 4 audio files, got %d: %+v", len(keys), keys)
	}

	if keys[disc1TrackA] != keys[disc1TrackB] {
		t.Errorf("disc 1 tracks should share a group_key, got %+v", keys)
	}

	if keys[disc2TrackA] != keys[disc2TrackB] {
		t.Errorf("disc 2 tracks should share a group_key, got %+v", keys)
	}

	if keys[disc1TrackA] == keys[disc2TrackA] {
		t.Errorf("disc 1 and disc 2 must not share a group_key, got %+v", keys)
	}
}

// TestScan_MultipleDirectoriesDoNotCrossContaminate scans two
// unrelated folders — one partially disc-tagged, one fully untagged —
// in a single pass, guarding against the directory-batching buffer in
// the DB writer mixing up which files belong to which directory.
func TestScan_MultipleDirectoriesDoNotCrossContaminate(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)

	root := t.TempDir()
	albumA := filepath.Join(root, "Artist", "Album A")
	albumB := filepath.Join(root, "Artist", "Album B")

	for _, d := range []string{albumA, albumB} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	aTrack1 := filepath.Join(albumA, "01.mp3")
	aTrack2 := filepath.Join(albumA, "02.mp3")
	bTrack1 := filepath.Join(albumB, "01.mp3")
	bTrack2 := filepath.Join(albumB, "02.mp3")

	writeTestTrack(t, aTrack1, 2) //nolint:mnd
	writeTestTrack(t, aTrack2, 0)
	writeTestTrack(t, bTrack1, 0)
	writeTestTrack(t, bTrack2, 0)

	keys := scanTestGroupKeys(t, lib, root)

	if len(keys) != 4 { //nolint:mnd
		t.Fatalf("expected 4 audio files, got %d: %+v", len(keys), keys)
	}

	if keys[aTrack1] != keys[aTrack2] {
		t.Errorf("Album A's two tracks should share a group_key: %+v", keys)
	}

	if keys[bTrack1] != keys[bTrack2] {
		t.Errorf("Album B's two tracks should share a group_key: %+v", keys)
	}

	if keys[aTrack1] == keys[bTrack1] {
		t.Errorf("Album A and Album B must not share a group_key: %+v", keys)
	}
}
