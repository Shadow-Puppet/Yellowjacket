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
	errInvalidM3U     = errors.New("invalid M3U file: missing #EXTM3U header")
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

		// Check header.
		if !headerSeen {
			if line == m3uHeader {
				headerSeen = true

				continue
			}

			return parsedPlaylist{}, errInvalidM3U
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

	if len(matches) == 0 {
		return "", nil
	}

	return matches[0], nil
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
