// Package testfixtures gives tests typed access to the deterministic
// fixture library produced by cmd/gentestdata (`make testdata`).
//
// The library is gitignored and generated, so every accessor here
// skips the calling test when it is absent rather than failing: a
// clean clone must still be able to run `go test ./...`.  Tests select
// fixtures by case name — the behaviour they exercise — so fixture
// paths can be renamed without touching test code.
package testfixtures

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ManifestName is the manifest's filename, kept outside the library
// root so the scanner never sees it.
const ManifestName = "music_library_test.manifest.json"

// Case names, mirroring cmd/gentestdata's spec.
const (
	CaseCoverDedup    = "cover-dedup"
	CaseMultiDisc     = "multi-disc"
	CaseVariousArtist = "various-artists"
	CaseFLACAlbum     = "flac-album"
	CaseOGGAlbum      = "ogg-album"
	CaseWAVTracks     = "wav-tracks"
	CasePartialTags   = "partial-tags"
	CaseUnicode       = "unicode"
	CaseDuplicates    = "duplicates"
	CaseEdgeLengths   = "edge-lengths"
	CaseBroken        = "broken"
)

// Track is one generated fixture, as specified rather than as encoded.
type Track struct {
	Path       string         `json:"path"`
	Case       string         `json:"case"`
	Format     string         `json:"format"`
	DurationMS int64          `json:"durationMs"`
	FreqHz     float64        `json:"freqHz"`
	Cover      string         `json:"cover,omitempty"`
	CoverSHA   string         `json:"coverSha,omitempty"`
	Tags       map[string]any `json:"tags"`
}

// Manifest describes a generated fixture library.
type Manifest struct {
	Version     int                 `json:"version"`
	Generator   string              `json:"generator"`
	Hash        string              `json:"hash"`
	LibraryRoot string              `json:"libraryRoot"`
	BrokenRoot  string              `json:"brokenRoot"`
	Cases       map[string][]string `json:"cases"`
	Tracks      []Track             `json:"tracks"`
	Extras      []string            `json:"extras"`
	Broken      []string            `json:"broken"`

	repoRoot string
}

// Root returns the absolute path of the fixture library root.
func (m *Manifest) Root() string {
	return filepath.Join(m.repoRoot, filepath.FromSlash(m.LibraryRoot))
}

// BrokenPath returns the absolute path of the malformed-file root,
// which is deliberately a sibling of the library rather than part of
// it: the clean library's track count has to stay deterministic.
func (m *Manifest) BrokenPath() string {
	return filepath.Join(m.repoRoot, filepath.FromSlash(m.BrokenRoot))
}

// Abs resolves a manifest-relative track path to an absolute one.
func (m *Manifest) Abs(rel string) string {
	return filepath.Join(m.Root(), filepath.FromSlash(rel))
}

// Case returns the absolute paths belonging to a case, failing the
// test when the case is unknown — a typo should not silently pass as
// an empty set.
func (m *Manifest) Case(t *testing.T, name string) []string {
	t.Helper()

	rels, ok := m.Cases[name]
	if !ok {
		t.Fatalf("testfixtures: unknown case %q", name)
	}

	paths := make([]string, 0, len(rels))
	for _, rel := range rels {
		paths = append(paths, m.Abs(rel))
	}

	return paths
}

// Track looks up a fixture by its manifest-relative path.
func (m *Manifest) Track(t *testing.T, rel string) Track {
	t.Helper()

	for _, track := range m.Tracks {
		if track.Path == rel {
			return track
		}
	}

	t.Fatalf("testfixtures: no fixture at %q", rel)

	return Track{}
}

//nolint:gochecknoglobals // memoised manifest load, keyed to the process.
var (
	loadOnce sync.Once
	loaded   *Manifest
)

// Load returns the fixture manifest, skipping the test when the
// library has not been generated (`make testdata`).
func Load(t *testing.T) *Manifest {
	t.Helper()

	loadOnce.Do(func() {
		loaded = load()
	})

	if loaded == nil {
		t.Skip(
			"testfixtures: fixture library not generated; " +
				"run `make testdata`",
		)
	}

	return loaded
}

// load reads and validates the manifest, returning nil when the
// fixtures are missing or stale.
func load() *Manifest {
	repo, err := repoRoot()
	if err != nil {
		return nil
	}

	raw, err := os.ReadFile(filepath.Join(repo, "test_data", ManifestName))
	if err != nil {
		return nil
	}

	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}

	m.repoRoot = repo

	// A manifest without its library is worse than no manifest: it
	// would point every test at paths that do not exist.
	if _, err := os.Stat(m.Root()); err != nil {
		return nil
	}

	return &m
}

// repoRoot walks up from the working directory to the module root, so
// fixtures resolve identically from any package's test.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}

		dir = parent
	}
}
