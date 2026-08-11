package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// manifestVersion is bumped when the manifest's shape changes in a way
// that older readers cannot handle.
const manifestVersion = 1

// manifestTrack records what a fixture is supposed to be, so a test can
// assert against the spec rather than against whatever happens to be on
// disk.
type manifestTrack struct {
	Path       string         `json:"path"`
	Case       string         `json:"case"`
	Format     string         `json:"format"`
	DurationMS int64          `json:"durationMs"`
	FreqHz     float64        `json:"freqHz"`
	Cover      string         `json:"cover,omitempty"`
	CoverSHA   string         `json:"coverSha,omitempty"`
	Tags       map[string]any `json:"tags"`
}

// manifest describes a generated fixture library.
//
// Hash covers the *logical* spec — paths, formats, durations, tags,
// cover identity — and deliberately not the encoded bytes: ffmpeg
// stamps its own encoder strings, so byte hashes differ between ffmpeg
// builds while the fixtures they describe are identical.
type manifest struct {
	Version     int                 `json:"version"`
	Generator   string              `json:"generator"`
	Hash        string              `json:"hash"`
	LibraryRoot string              `json:"libraryRoot"`
	BrokenRoot  string              `json:"brokenRoot"`
	Cases       map[string][]string `json:"cases"`
	Tracks      []manifestTrack     `json:"tracks"`
	Extras      []string            `json:"extras"`
	Broken      []string            `json:"broken"`
}

// buildManifest derives the manifest from the spec alone.  It runs
// before any file is written, which is what lets generation be skipped
// when the on-disk manifest already matches.
func buildManifest(libraryRoot, brokenRoot string) (*manifest, error) {
	m := &manifest{
		Version:     manifestVersion,
		Generator:   "gentestdata",
		LibraryRoot: filepath.ToSlash(libraryRoot),
		BrokenRoot:  filepath.ToSlash(brokenRoot),
		Cases:       map[string][]string{},
		Tracks:      make([]manifestTrack, 0, len(libraryFixtures)),
	}

	for _, f := range libraryFixtures {
		track := manifestTrack{
			Path:       f.Rel,
			Case:       f.Case,
			Format:     string(f.Format),
			DurationMS: f.Duration.Milliseconds(),
			FreqHz:     f.FreqHz,
			Cover:      f.Cover,
			Tags:       f.Tags.changes(),
		}

		if f.Cover != "" {
			img, err := coverJPEG(f.Cover)
			if err != nil {
				return nil, err
			}

			sum := sha256.Sum256(img)
			track.CoverSHA = hex.EncodeToString(sum[:])
		}

		m.Tracks = append(m.Tracks, track)
		m.Cases[f.Case] = append(m.Cases[f.Case], f.Rel)
	}

	for _, e := range libraryExtras {
		m.Extras = append(m.Extras, e.Rel)
	}

	for _, b := range brokenFiles {
		m.Broken = append(m.Broken, b.Rel)
		m.Cases[caseBroken] = append(m.Cases[caseBroken], b.Rel)
	}

	hash, err := hashManifest(m)
	if err != nil {
		return nil, err
	}

	m.Hash = hash

	return m, nil
}

// hashManifest hashes everything except the hash field itself.
func hashManifest(m *manifest) (string, error) {
	clone := *m
	clone.Hash = ""

	raw, err := json.Marshal(clone)
	if err != nil {
		return "", fmt.Errorf("marshal manifest for hashing: %w", err)
	}

	sum := sha256.Sum256(raw)

	return hex.EncodeToString(sum[:]), nil
}

// writeManifest persists the manifest next to (not inside) the library
// root, so the scanner never sees it as a stray file.
func writeManifest(path string, m *manifest) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, append(raw, '\n'), filePerm); err != nil {
		return fmt.Errorf("write manifest %s: %w", path, err)
	}

	return nil
}

// readManifestHash returns the hash recorded in an existing manifest,
// or "" when there is no readable manifest at path.
func readManifestHash(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}

	return m.Hash
}
