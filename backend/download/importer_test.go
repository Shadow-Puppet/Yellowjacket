package download

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"yellowjacket/backend/tagwriter"
)

// recordingTagWriter captures tag writes instead of touching files, so
// importer tests do not need real audio.
type recordingTagWriter struct {
	mu      sync.Mutex
	writes  map[string]tagwriter.TagChanges
	failFor string
}

func newRecordingTagWriter() *recordingTagWriter {
	return &recordingTagWriter{writes: map[string]tagwriter.TagChanges{}}
}

var errTagWriteFailed = errors.New("tag write failed")

func (r *recordingTagWriter) WriteUntrackedFileTags(
	path string,
	changes tagwriter.TagChanges,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.failFor != "" && strings.Contains(path, r.failFor) {
		return errTagWriteFailed
	}

	r.writes[filepath.Base(path)] = changes

	return nil
}

// stubLibrary records scan requests.
type stubLibrary struct {
	mu      sync.Mutex
	scanned []int64
	path    string
}

func (s *stubLibrary) ScanLibrary(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.scanned = append(s.scanned, id)

	return nil
}

func (s *stubLibrary) LibraryPath(int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.path, nil
}

// importFixture stages a set of files and returns the pieces an import
// needs.
type importFixture struct {
	staging  *Staging
	importer *Importer
	tags     *recordingTagWriter
	lib      *stubLibrary
	dir      string
	root     string
	files    []string
}

func newImportFixture(t *testing.T, names ...string) importFixture {
	t.Helper()

	staging := newTestStaging(t)
	tags := newRecordingTagWriter()
	lib := &stubLibrary{}
	imp := NewImporter(slogDiscard(), staging, tags, lib)

	dir, err := staging.Reserve("item-1")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	files := make([]string, 0, len(names))

	for _, n := range names {
		p := filepath.Join(dir, n)

		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		if err := os.WriteFile(p, []byte("audio-data"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		files = append(files, p)
	}

	return importFixture{
		staging:  staging,
		importer: imp,
		tags:     tags,
		lib:      lib,
		dir:      dir,
		root:     t.TempDir(),
		files:    files,
	}
}

func fourTrackRequest() Request {
	return Request{
		ID:          "req-1",
		LibraryID:   1,
		ReleaseMBID: "mbid-1",
		Artist:      "Radiohead",
		Album:       "OK Computer",
		Expected: []ExpectedTrack{
			{Position: 1, Title: "Airbag"},
			{Position: 2, Title: "Paranoid Android"},
			{Position: 3, Title: "Subterranean Homesick Alien"},
			{Position: 4, Title: "Exit Music (For a Film)"},
		},
	}
}

func TestImportPlacesAndTagsFiles(t *testing.T) {
	t.Parallel()

	f := newImportFixture(t,
		"01 - Airbag.flac",
		"02 - Paranoid Android.flac",
		"03 - Subterranean Homesick Alien.flac",
		"04 - Exit Music (For a Film).flac",
	)

	got, err := f.importer.Import(
		context.Background(),
		fourTrackRequest(),
		Result{Dir: f.dir, Files: f.files},
		ImportOptions{LibraryRoot: f.root, WriteTags: true},
	)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if len(got.Paths) != 4 {
		t.Fatalf("imported %d files, want 4", len(got.Paths))
	}

	if got.Tagged != 4 {
		t.Errorf("tagged %d files, want 4", got.Tagged)
	}

	want := filepath.Join(
		f.root, "Radiohead", "OK Computer", "01 Airbag.flac",
	)

	if got.Paths[0] != want {
		t.Errorf("first path = %s, want %s", got.Paths[0], want)
	}

	for _, p := range got.Paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("imported file missing: %v", err)
		}
	}
}

// Tags must be written while the file is still staged: if the library
// ever sees an untagged file, the scanner ingests it and the user
// watches it correct itself.
func TestImportTagsBeforeMoving(t *testing.T) {
	t.Parallel()

	f := newImportFixture(t, "01 - Airbag.flac")

	req := fourTrackRequest()
	req.Expected = req.Expected[:1]

	if _, err := f.importer.Import(
		context.Background(),
		req,
		Result{Dir: f.dir, Files: f.files},
		ImportOptions{LibraryRoot: f.root, WriteTags: true},
	); err != nil {
		t.Fatalf("Import: %v", err)
	}

	changes, ok := f.tags.writes["01 - Airbag.flac"]
	if !ok {
		t.Fatalf(
			"tags were not written to the staged filename; got writes for %v",
			keysOf(f.tags.writes),
		)
	}

	if changes[tagwriter.FieldTitle] != "Airbag" {
		t.Errorf("title = %v, want Airbag", changes[tagwriter.FieldTitle])
	}

	if changes[tagwriter.FieldAlbum] != "OK Computer" {
		t.Errorf("album = %v, want OK Computer", changes[tagwriter.FieldAlbum])
	}
}

// One unwritable file should not cost the whole album.
func TestImportContinuesWhenTaggingFails(t *testing.T) {
	t.Parallel()

	f := newImportFixture(t,
		"01 - Airbag.flac",
		"02 - Paranoid Android.flac",
		"03 - Subterranean Homesick Alien.flac",
		"04 - Exit Music (For a Film).flac",
	)
	f.tags.failFor = "Paranoid"

	got, err := f.importer.Import(
		context.Background(),
		fourTrackRequest(),
		Result{Dir: f.dir, Files: f.files},
		ImportOptions{LibraryRoot: f.root, WriteTags: true},
	)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if len(got.Paths) != 4 {
		t.Errorf("imported %d files, want all 4", len(got.Paths))
	}

	if got.Tagged != 3 {
		t.Errorf("tagged %d, want 3 (one failure)", got.Tagged)
	}
}

func TestImportRejectsTooIncomplete(t *testing.T) {
	t.Parallel()

	f := newImportFixture(t, "01 - Airbag.flac")

	_, err := f.importer.Import(
		context.Background(),
		fourTrackRequest(),
		Result{Dir: f.dir, Files: f.files},
		ImportOptions{LibraryRoot: f.root, WriteTags: true},
	)

	if !errors.Is(err, ErrTooIncomplete) {
		t.Fatalf("error = %v, want ErrTooIncomplete", err)
	}

	entries, _ := os.ReadDir(f.root)
	if len(entries) != 0 {
		t.Error("failed import wrote into the library root")
	}
}

func TestImportRejectsNoAudio(t *testing.T) {
	t.Parallel()

	f := newImportFixture(t, "rip.log", "cover.jpg")

	_, err := f.importer.Import(
		context.Background(),
		fourTrackRequest(),
		Result{Dir: f.dir, Files: f.files},
		ImportOptions{LibraryRoot: f.root, WriteTags: true},
	)

	if !errors.Is(err, ErrNoAudio) {
		t.Fatalf("error = %v, want ErrNoAudio", err)
	}
}

func TestImportSkipsNonAudioFiles(t *testing.T) {
	t.Parallel()

	f := newImportFixture(t,
		"01 - Airbag.flac",
		"02 - Paranoid Android.flac",
		"03 - Subterranean Homesick Alien.flac",
		"04 - Exit Music (For a Film).flac",
		"rip.log",
		"cover.jpg",
	)

	got, err := f.importer.Import(
		context.Background(),
		fourTrackRequest(),
		Result{Dir: f.dir, Files: f.files},
		ImportOptions{LibraryRoot: f.root, WriteTags: true},
	)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if got.Skipped != 2 {
		t.Errorf("skipped = %d, want 2", got.Skipped)
	}

	if len(got.Paths) != 4 {
		t.Errorf("imported %d, want 4 audio files only", len(got.Paths))
	}
}

// An existing file is never overwritten: it may be a better copy the
// user already owns, and arriving later does not make a download
// authoritative.
func TestImportNeverOverwrites(t *testing.T) {
	t.Parallel()

	f := newImportFixture(t, "01 - Airbag.flac")

	req := fourTrackRequest()
	req.Expected = req.Expected[:1]

	existing := filepath.Join(
		f.root, "Radiohead", "OK Computer", "01 Airbag.flac",
	)

	if err := os.MkdirAll(filepath.Dir(existing), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(existing, []byte("original"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := f.importer.Import(
		context.Background(),
		req,
		Result{Dir: f.dir, Files: f.files},
		ImportOptions{LibraryRoot: f.root, WriteTags: true},
	)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	data, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if string(data) != "original" {
		t.Error("import overwrote an existing library file")
	}

	if got.Paths[0] == existing {
		t.Errorf("imported to the occupied path %s", existing)
	}
}

func TestImportCustomPathTemplate(t *testing.T) {
	t.Parallel()

	f := newImportFixture(t, "01 - Airbag.flac")

	req := fourTrackRequest()
	req.Expected = req.Expected[:1]

	got, err := f.importer.Import(
		context.Background(),
		req,
		Result{Dir: f.dir, Files: f.files},
		ImportOptions{
			LibraryRoot:  f.root,
			PathTemplate: "{albumartist} - {album}/{track}. {title}",
			WriteTags:    true,
		},
	)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	want := filepath.Join(
		f.root, "Radiohead - OK Computer", "01. Airbag.flac",
	)

	if got.Paths[0] != want {
		t.Errorf("path = %s, want %s", got.Paths[0], want)
	}
}

func TestSanitizePathPart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"AC/DC", "AC_DC"},
		{"Where Are We Now?", "Where Are We Now_"},
		{`Bad: Title*`, "Bad_ Title_"},
		{"Vol. 2 ", "Vol. 2"},
		{"trailing dots...", "trailing dots"},
		{"", "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			if got := sanitizePathPart(tt.in); got != tt.want {
				t.Errorf("sanitizePathPart(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestImportRequiresLibraryRoot(t *testing.T) {
	t.Parallel()

	f := newImportFixture(t, "01 - Airbag.flac")

	req := fourTrackRequest()
	req.Expected = req.Expected[:1]

	_, err := f.importer.Import(
		context.Background(),
		req,
		Result{Dir: f.dir, Files: f.files},
		ImportOptions{WriteTags: true},
	)

	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("error = %v, want ErrNotConfigured", err)
	}
}

func keysOf(m map[string]tagwriter.TagChanges) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}
