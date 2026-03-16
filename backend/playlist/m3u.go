package playlist

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	m3uHeader    = "#EXTM3U"
	m3uPlaylist  = "#PLAYLIST:"
	m3uExtInf    = "#EXTINF:"
	m3uExtension = ".m3u8"
)

var (
	errEmptyM3UFile   = errors.New("M3U file is empty")
	errPlaylistDirNil = errors.New("playlists directory path is empty")
)

// unsafeChars matches characters that are not safe for filenames.
// Uses Unicode letter/digit classes so accented characters are kept.
var unsafeChars = regexp.MustCompile(`[^\p{L}\p{N}\-. ]+`)

// m3uEntry represents a single track entry parsed from an M3U8 file.
type m3uEntry struct {
	// RelativePath is the path relative to the library root.
	RelativePath string
	// DurationSec is the track duration in seconds (from #EXTINF).
	DurationSec int
	// DisplayTitle is the display title (from #EXTINF).
	DisplayTitle string
}

// parsedPlaylist is the result of parsing an M3U8 file.
type parsedPlaylist struct {
	Name    string
	Entries []m3uEntry
}

// writeM3U8 writes a playlist to an M3U8 file at the given directory.
// The file is named "{id}-{sanitized-name}.m3u8".
func writeM3U8(
	dirPath string,
	playlistID int64,
	name string,
	entries []m3uEntry,
) error {
	if dirPath == "" {
		return errPlaylistDirNil
	}

	filePath := playlistFilePath(dirPath, playlistID, name)

	// Remove any old file for this ID with a different name.
	if err := removeOldPlaylistFile(
		dirPath, playlistID, filePath,
	); err != nil {
		return fmt.Errorf(
			"could not remove old playlist file: %w", err,
		)
	}

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf(
			"could not create M3U8 file %q: %w",
			filePath, err,
		)
	}

	defer func() { _ = file.Close() }()

	w := bufio.NewWriter(file)

	// Write header.
	_, _ = fmt.Fprintln(w, m3uHeader)
	_, _ = fmt.Fprintf(
		w, "%s%s\n", m3uPlaylist, name,
	)

	// Write entries.
	for _, entry := range entries {
		_, _ = fmt.Fprintf(
			w, "%s%d,%s\n",
			m3uExtInf,
			entry.DurationSec,
			entry.DisplayTitle,
		)
		_, _ = fmt.Fprintln(w, entry.RelativePath)
	}

	if err := w.Flush(); err != nil {
		return fmt.Errorf(
			"could not flush M3U8 file %q: %w",
			filePath, err,
		)
	}

	return nil
}

// parseM3U8 reads and parses an M3U8 (or M3U) file.
func parseM3U8(filePath string) (parsedPlaylist, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return parsedPlaylist{}, fmt.Errorf(
			"could not open M3U file %q: %w", filePath, err,
		)
	}

	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)

	var result parsedPlaylist

	headerSeen := false
	pendingDuration := 0
	pendingTitle := ""
	hasPendingExtInf := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Check header. If the first non-empty line is not
		// #EXTM3U, treat the file as a simple M3U (just
		// path lines) and fall through to process normally.
		if !headerSeen {
			headerSeen = true

			if line == m3uHeader {
				continue
			}
		}

		// Playlist name directive.
		if strings.HasPrefix(line, m3uPlaylist) {
			result.Name = strings.TrimPrefix(line, m3uPlaylist)

			continue
		}

		// EXTINF line.
		if strings.HasPrefix(line, m3uExtInf) {
			dur, title := parseExtInf(line)
			pendingDuration = dur
			pendingTitle = title
			hasPendingExtInf = true

			continue
		}

		// Skip other comment lines.
		if strings.HasPrefix(line, "#") {
			continue
		}

		// This is a track path line.
		entry := m3uEntry{
			RelativePath: line,
		}

		if hasPendingExtInf {
			entry.DurationSec = pendingDuration
			entry.DisplayTitle = pendingTitle
			hasPendingExtInf = false
			pendingDuration = 0
			pendingTitle = ""
		}

		result.Entries = append(result.Entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return parsedPlaylist{}, fmt.Errorf(
			"error reading M3U file %q: %w", filePath, err,
		)
	}

	if !headerSeen {
		return parsedPlaylist{}, errEmptyM3UFile
	}

	// Derive name from filename if not set via #PLAYLIST directive.
	if result.Name == "" {
		base := filepath.Base(filePath)
		result.Name = strings.TrimSuffix(
			base, filepath.Ext(base),
		)

		// Strip ID prefix if present (e.g., "1-my-playlist").
		if idx := strings.Index(result.Name, "-"); idx > 0 {
			prefix := result.Name[:idx]
			if _, err := strconv.ParseInt(
				prefix, 10, 64,
			); err == nil {
				result.Name = result.Name[idx+1:]
			}
		}
	}

	return result, nil
}

// parseExtInf parses an #EXTINF line and returns duration and title.
// Format: #EXTINF:duration,display title.
func parseExtInf(line string) (int, string) {
	data := strings.TrimPrefix(line, m3uExtInf)

	commaIdx := strings.Index(data, ",")
	if commaIdx < 0 {
		dur, _ := strconv.Atoi(strings.TrimSpace(data))

		return dur, ""
	}

	durStr := strings.TrimSpace(data[:commaIdx])
	title := strings.TrimSpace(data[commaIdx+1:])

	dur, _ := strconv.Atoi(durStr)

	return dur, title
}

// playlistFilePath returns the full path for a playlist M3U8 file.
func playlistFilePath(
	dirPath string,
	id int64,
	name string,
) string {
	sanitized := sanitizeFilename(name)

	return filepath.Join(
		dirPath,
		fmt.Sprintf("%d-%s%s", id, sanitized, m3uExtension),
	)
}

// sanitizeFilename converts a playlist name to a safe filename.
func sanitizeFilename(name string) string {
	// Lowercase.
	s := strings.ToLower(name)

	// Replace spaces and underscores with hyphens.
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")

	// Remove unsafe characters.
	s = unsafeChars.ReplaceAllString(s, "")

	// Collapse multiple hyphens.
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}

	// Trim leading/trailing hyphens and dots.
	s = strings.Trim(s, "-.")

	// Ensure non-empty.
	if s == "" {
		s = "playlist"
	}

	// Truncate to a reasonable length.
	const maxLen = 100

	if runeCount := len([]rune(s)); runeCount > maxLen {
		runes := []rune(s)
		s = string(runes[:maxLen])
	}

	return s
}

// findPlaylistFile finds the existing M3U8 file for a given playlist
// ID by globbing for "{id}-*.m3u8".
func findPlaylistFile(
	dirPath string,
	id int64,
) (string, error) {
	pattern := filepath.Join(
		dirPath,
		fmt.Sprintf("%d-*%s", id, m3uExtension),
	)

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf(
			"could not glob for playlist file: %w", err,
		)
	}

	// Filter matches to ensure the extracted ID matches the
	// target. The glob pattern "1-*.m3u8" also matches
	// "10-foo.m3u8", "11-bar.m3u8", etc.
	for _, m := range matches {
		if extractPlaylistID(m) == id {
			return m, nil
		}
	}

	return "", nil
}

// removeOldPlaylistFile removes an old playlist file for the given
// ID if it exists and differs from the expected path.
func removeOldPlaylistFile(
	dirPath string,
	id int64,
	expectedPath string,
) error {
	existing, err := findPlaylistFile(dirPath, id)
	if err != nil {
		return err
	}

	if existing == "" || existing == expectedPath {
		return nil
	}

	if err := os.Remove(existing); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf(
			"could not remove old playlist file %q: %w",
			existing, err,
		)
	}

	return nil
}

// toAbsolutePath converts a relative path to an absolute path using
// the library root. If the path is already absolute, it is returned
// as-is.
func toAbsolutePath(relativePath, libraryRoot string) string {
	if filepath.IsAbs(relativePath) {
		return relativePath
	}

	return filepath.Join(libraryRoot, relativePath)
}

// toRelativePath converts an absolute path to a relative path based
// on the library root. If the path cannot be made relative, it is
// returned as-is.
func toRelativePath(absolutePath, libraryRoot string) string {
	if libraryRoot == "" {
		return absolutePath
	}

	rel, err := filepath.Rel(libraryRoot, absolutePath)
	if err != nil {
		return absolutePath
	}

	// If the relative path escapes the library root (starts with
	// ".."), keep the absolute path.
	if strings.HasPrefix(rel, "..") {
		return absolutePath
	}

	return rel
}

// resolveM3UPath resolves a relative M3U path against multiple
// library roots, returning the first absolute path that exists in
// the knownPaths set. If the path is already absolute and known,
// it is returned as-is. Falls back to the first root if no match
// is found, preserving current behavior for phantom tracks.
func resolveM3UPath(
	relativePath string,
	libraryRoots []string,
	knownPaths map[string]struct{},
) string {
	if filepath.IsAbs(relativePath) {
		if _, ok := knownPaths[relativePath]; ok {
			return relativePath
		}

		return relativePath
	}

	for _, root := range libraryRoots {
		absPath := filepath.Join(root, relativePath)
		if _, ok := knownPaths[absPath]; ok {
			return absPath
		}
	}

	// Fallback: use first root (preserves current behavior
	// for phantom tracks).
	if len(libraryRoots) > 0 {
		return filepath.Join(libraryRoots[0], relativePath)
	}

	return relativePath
}

// toRelativePathMultiRoot converts an absolute path to a relative
// path using the first library root that contains the path.
// If no root matches, the absolute path is returned unchanged.
func toRelativePathMultiRoot(
	absolutePath string,
	libraryRoots []string,
) string {
	for _, root := range libraryRoots {
		if root == "" {
			continue
		}

		rel, err := filepath.Rel(root, absolutePath)
		if err != nil {
			continue
		}

		if !strings.HasPrefix(rel, "..") {
			return rel
		}
	}

	return absolutePath
}

// isValidM3UExtension checks whether a file extension is a
// recognized M3U variant.
func isValidM3UExtension(ext string) bool {
	lower := strings.ToLower(ext)

	return lower == ".m3u" || lower == ".m3u8"
}

// listPlaylistFiles returns all M3U8 files in the playlists
// directory.
func listPlaylistFiles(dirPath string) ([]string, error) {
	pattern := filepath.Join(dirPath, "*"+m3uExtension)

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf(
			"could not list playlist files: %w", err,
		)
	}

	return matches, nil
}

// extractPlaylistID extracts the playlist DB ID from an M3U8
// filename. The expected format is "{id}-{name}.m3u8". Returns 0 if
// the ID cannot be extracted.
func extractPlaylistID(filePath string) int64 {
	base := filepath.Base(filePath)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	idx := strings.Index(name, "-")
	if idx <= 0 {
		return 0
	}

	id, err := strconv.ParseInt(name[:idx], 10, 64)
	if err != nil {
		return 0
	}

	return id
}

// removeM3UEntries removes entries from a slice whose resolved
// absolute paths appear in the target set. Each entry is resolved
// against all library roots.
func removeM3UEntries(
	entries []m3uEntry,
	targetAbsPaths map[string]struct{},
	libraryRoots []string,
) []m3uEntry {
	result := make([]m3uEntry, 0, len(entries))

	for _, e := range entries {
		absPath := resolveM3UPath(
			e.RelativePath, libraryRoots, targetAbsPaths,
		)
		if _, remove := targetAbsPaths[absPath]; remove {
			continue
		}

		result = append(result, e)
	}

	return result
}

// replaceM3UEntryPaths replaces the relative paths of entries
// whose resolved absolute paths match keys in the replacements
// map. Values are new relative paths. Each entry is resolved
// against all library roots.
func replaceM3UEntryPaths(
	entries []m3uEntry,
	replacements map[string]string,
	libraryRoots []string,
) []m3uEntry {
	// Build a set of replacement keys for resolveM3UPath lookup.
	keySet := make(map[string]struct{}, len(replacements))
	for k := range replacements {
		keySet[k] = struct{}{}
	}

	result := make([]m3uEntry, len(entries))

	for i, e := range entries {
		result[i] = e

		absPath := resolveM3UPath(
			e.RelativePath, libraryRoots, keySet,
		)

		if newRel, ok := replacements[absPath]; ok {
			result[i].RelativePath = newRel
		}
	}

	return result
}

// findM3UEntry finds the M3U entry whose resolved absolute path
// matches the given target path. Each entry is resolved against
// all library roots. Returns the entry and its index, or -1 if
// not found.
func findM3UEntry(
	entries []m3uEntry,
	targetAbsPath string,
	libraryRoots []string,
) (m3uEntry, int) {
	targetSet := map[string]struct{}{
		targetAbsPath: {},
	}

	for i, e := range entries {
		absPath := resolveM3UPath(
			e.RelativePath, libraryRoots, targetSet,
		)
		if absPath == targetAbsPath {
			return e, i
		}
	}

	return m3uEntry{}, -1
}

// displayTitle builds an EXTINF display title from artist and title.
func displayTitle(artist, title string) string {
	artist = strings.TrimSpace(artist)
	title = strings.TrimSpace(title)

	if artist == "" && title == "" {
		return "Unknown"
	}

	if artist == "" {
		return title
	}

	if title == "" {
		return artist
	}

	return artist + " - " + title
}
