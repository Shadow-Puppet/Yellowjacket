package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"yellowjacket/backend/tagwriter"
)

// The import step is the only writer into library paths.  Everything
// before it happens in staging, where a bad download is a directory to
// delete rather than a row to un-ingest.
//
// Order matters: tags are written while the files are still staged, so
// the scanner's first sight of a file is already correct.  Tagging
// after the move would mean a window where the library holds a track
// titled "01 - Track01.flac", and the user would watch it fix itself.

// Import errors.
var (
	// ErrNoAudio means the grab produced no playable audio files.
	ErrNoAudio = errors.New("download contained no audio files")

	// ErrTooIncomplete means too few of the expected tracks arrived to
	// call the download successful.
	ErrTooIncomplete = errors.New("download is missing too many tracks")

	// ErrDestinationExists means the computed library path is already
	// occupied by a different file.
	ErrDestinationExists = errors.New("destination file already exists")
)

// minCompleteness is the fraction of the expected tracklist that must
// arrive for an anchored import to proceed.  Below this the download is
// a different thing than what was asked for — a single, a sampler, a
// partial transfer — and quietly importing it would corrupt the
// library's idea of the album.
const minCompleteness = 0.8

// TagWriterPort is the tag-writing capability the importer needs.
// Narrow interface rather than *tagwriter.TagWriter so importer tests
// do not need a database.
type TagWriterPort interface {
	WriteUntrackedFileTags(filePath string, changes tagwriter.TagChanges) error
}

// LibraryPort is the library-side capability the importer needs.
type LibraryPort interface {
	// ScanLibrary triggers a rescan so imported files are ingested.
	ScanLibrary(id int64) error
}

// ImportOptions configures how imported files are laid out.
type ImportOptions struct {
	// LibraryRoot is the directory imported files are placed under.
	LibraryRoot string

	// PathTemplate lays out the destination path.  Supported tokens:
	// {albumartist} {artist} {album} {year} {track} {disc} {title}.
	// Empty means flat: everything into LibraryRoot/{albumartist}/{album}.
	PathTemplate string

	// WriteTags controls whether the importer tags files before moving
	// them.  Off for delegate providers, which have already imported
	// and tagged the files themselves.
	WriteTags bool
}

// DefaultPathTemplate is the layout used when none is configured.
const DefaultPathTemplate = "{albumartist}/{album}/{track} {title}"

// Importer moves verified downloads into the library.
type Importer struct {
	logger  *slog.Logger
	staging *Staging
	tags    TagWriterPort
	library LibraryPort
}

// NewImporter builds an importer.
func NewImporter(
	logger *slog.Logger,
	staging *Staging,
	tags TagWriterPort,
	library LibraryPort,
) *Importer {
	return &Importer{
		logger:  logger,
		staging: staging,
		tags:    tags,
		library: library,
	}
}

// ImportResult reports what an import placed where.
type ImportResult struct {
	// Paths are the library paths files ended up at.
	Paths []string

	// Tagged counts files whose tags were rewritten.
	Tagged int

	// Skipped counts non-audio files left in staging (logs, cue sheets,
	// scene .nfo files) — deliberately not imported.
	Skipped int
}

// Import verifies, tags and moves a completed grab into the library.
//
// On any failure the staging directory is left intact so the user can
// retry or inspect it; only a fully successful import releases staging.
func (i *Importer) Import(
	ctx context.Context,
	req Request,
	result Result,
	opts ImportOptions,
) (ImportResult, error) {
	files, err := i.staging.Verify(result.Dir, result.Files)
	if err != nil {
		return ImportResult{}, err
	}

	audio, skipped := splitAudio(files)
	if len(audio) == 0 {
		return ImportResult{}, ErrNoAudio
	}

	if err := checkCompleteness(len(audio), req); err != nil {
		return ImportResult{}, err
	}

	// Align staged files to the expected tracklist so tags and
	// filenames reflect the release, not the uploader's naming.
	plan := i.planFiles(audio, req)

	out := ImportResult{
		Paths:   make([]string, 0, len(plan)),
		Skipped: skipped,
	}

	for _, p := range plan {
		if err := ctx.Err(); err != nil {
			return out, fmt.Errorf("import cancelled: %w", err)
		}

		if opts.WriteTags {
			if err := i.tagFile(p, req); err != nil {
				// A file that cannot be tagged is still worth importing
				// — the scanner will read whatever tags it has, and the
				// autotag queue can pick it up later.  Losing the whole
				// album over one unwritable file would be worse.
				i.logger.Warn(
					"could not tag downloaded file before import",
					"path", p.Source,
					"error", err,
				)
			} else {
				out.Tagged++
			}
		}

		dest, err := i.destinationFor(p, req, opts)
		if err != nil {
			return out, err
		}

		if err := movePath(p.Source, dest); err != nil {
			return out, err
		}

		out.Paths = append(out.Paths, dest)
	}

	return out, nil
}

// plannedFile pairs a staged file with the expected track it matched.
type plannedFile struct {
	Source string

	// Track is the matched expected track, or the zero value when the
	// file could not be aligned (free-text requests, bonus tracks).
	Track   ExpectedTrack
	Matched bool
}

// planFiles aligns staged files to the expected tracklist.
func (i *Importer) planFiles(audio []string, req Request) []plannedFile {
	files := make([]CandidateFile, 0, len(audio))

	for _, a := range audio {
		format, isAudio := FormatForPath(a)
		files = append(files, CandidateFile{
			Path:    a,
			Format:  format,
			IsAudio: isAudio,
		})
	}

	matched, _ := matchFiles(files, req.Expected)

	byPosition := make(map[int]ExpectedTrack, len(req.Expected))
	for _, e := range req.Expected {
		byPosition[e.Position] = e
	}

	out := make([]plannedFile, 0, len(matched))

	for _, m := range matched {
		p := plannedFile{Source: m.Path}

		if t, ok := byPosition[m.MatchedTo]; ok && m.MatchedTo != 0 {
			p.Track = t
			p.Matched = true
		}

		out = append(out, p)
	}

	// Stable order: matched tracks by position, then unmatched by path,
	// so a partial import is reproducible.
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Matched != out[b].Matched {
			return out[a].Matched
		}

		if out[a].Matched {
			if out[a].Track.DiscNumber != out[b].Track.DiscNumber {
				return out[a].Track.DiscNumber < out[b].Track.DiscNumber
			}

			return out[a].Track.Position < out[b].Track.Position
		}

		return out[a].Source < out[b].Source
	})

	return out
}

// tagFile writes the release's metadata onto a staged file.
func (i *Importer) tagFile(p plannedFile, req Request) error {
	if i.tags == nil || !p.Matched {
		return nil
	}

	changes := tagwriter.TagChanges{
		tagwriter.FieldAlbum:       req.Album,
		tagwriter.FieldAlbumArtist: req.Artist,
		tagwriter.FieldTitle:       p.Track.Title,
		tagwriter.FieldTrackNumber: p.Track.Position,
	}

	if p.Track.Artist != "" {
		changes[tagwriter.FieldArtist] = p.Track.Artist
	} else {
		changes[tagwriter.FieldArtist] = req.Artist
	}

	if p.Track.DiscNumber > 0 {
		changes[tagwriter.FieldDiscNumber] = p.Track.DiscNumber
	}

	if err := i.tags.WriteUntrackedFileTags(p.Source, changes); err != nil {
		return fmt.Errorf("write tags: %w", err)
	}

	return nil
}

// destinationFor computes a file's library path from the template.
func (i *Importer) destinationFor(
	p plannedFile,
	req Request,
	opts ImportOptions,
) (string, error) {
	if opts.LibraryRoot == "" {
		return "", fmt.Errorf(
			"%w: no library root configured", ErrNotConfigured,
		)
	}

	tmpl := opts.PathTemplate
	if tmpl == "" {
		tmpl = DefaultPathTemplate
	}

	ext := filepath.Ext(p.Source)

	title := p.Track.Title
	if title == "" {
		// Unmatched file: keep the uploader's name rather than
		// inventing one, so nothing is silently renamed to a track it
		// may not be.
		title = strings.TrimSuffix(filepath.Base(p.Source), ext)
	}

	artist := p.Track.Artist
	if artist == "" {
		artist = req.Artist
	}

	repl := strings.NewReplacer(
		"{albumartist}", sanitizePathPart(fallback(req.Artist, "Unknown Artist")),
		"{artist}", sanitizePathPart(fallback(artist, "Unknown Artist")),
		"{album}", sanitizePathPart(fallback(req.Album, "Unknown Album")),
		"{title}", sanitizePathPart(title),
		"{track}", trackToken(p.Track.Position),
		"{disc}", strconv.Itoa(p.Track.DiscNumber),
		"{year}", "",
	)

	rel := repl.Replace(tmpl)

	// Clean up any empty segments left by unset tokens.
	parts := make([]string, 0, 4)

	for _, seg := range strings.Split(rel, "/") {
		seg = strings.TrimSpace(seg)
		if seg != "" {
			parts = append(parts, seg)
		}
	}

	if len(parts) == 0 {
		return "", fmt.Errorf(
			"%w: path template produced an empty path", ErrNotConfigured,
		)
	}

	dest := filepath.Join(opts.LibraryRoot, filepath.Join(parts...)) + ext

	return uniqueDestination(dest)
}

// uniqueDestination returns dest, or a numbered variant when dest is
// taken.  Overwriting is never right here: the existing file may be a
// better copy the user already owns, and the download is not
// authoritative just because it arrived later.
func uniqueDestination(dest string) (string, error) {
	const maxAttempts = 50

	ext := filepath.Ext(dest)
	base := strings.TrimSuffix(dest, ext)

	for n := range maxAttempts {
		candidate := dest
		if n > 0 {
			candidate = base + " (" + strconv.Itoa(n+1) + ")" + ext
		}

		_, err := os.Stat(candidate)
		if os.IsNotExist(err) {
			return candidate, nil
		}

		if err != nil {
			return "", fmt.Errorf("stat destination: %w", err)
		}
	}

	return "", fmt.Errorf("%w: %s", ErrDestinationExists, dest)
}

// movePath moves a file, falling back to copy+remove when the staging
// area and the library are on different filesystems — which is the
// normal case, since staging lives in the user data directory.
func movePath(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	if err := os.Rename(src, dest); err == nil {
		return nil
	}

	if err := copyFile(src, dest); err != nil {
		return err
	}

	if err := os.Remove(src); err != nil {
		// The copy succeeded, so the import is good; a leftover staged
		// file is swept later.
		return nil //nolint:nilerr // staging sweep handles the leftover
	}

	return nil
}

// copyFile copies src to dest, writing to a temporary file first so an
// interrupted copy never leaves a partial file at a library path where
// the scanner would find it.
func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open downloaded file: %w", err)
	}

	defer func() { _ = in.Close() }()

	tmp := dest + ".part"

	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return fmt.Errorf("create library file: %w", err)
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)

		return fmt.Errorf("copy into library: %w", err)
	}

	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("close library file: %w", err)
	}

	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("finalize library file: %w", err)
	}

	return nil
}

// checkCompleteness rejects an anchored download that is missing too
// much of its tracklist.
func checkCompleteness(got int, req Request) error {
	if len(req.Expected) == 0 {
		return nil
	}

	ratio := float64(got) / float64(len(req.Expected))
	if ratio < minCompleteness {
		return fmt.Errorf(
			"%w: got %d of %d tracks",
			ErrTooIncomplete, got, len(req.Expected),
		)
	}

	return nil
}

// splitAudio partitions verified files into audio and a count of the
// rest.
func splitAudio(files []string) (audio []string, skipped int) {
	audio = make([]string, 0, len(files))

	for _, f := range files {
		if _, ok := FormatForPath(f); ok {
			audio = append(audio, f)

			continue
		}

		skipped++
	}

	return audio, skipped
}

// trackToken formats a track number as a zero-padded two-digit string,
// or empty when unknown.
func trackToken(n int) string {
	if n <= 0 {
		return ""
	}

	if n < 10 {
		return "0" + strconv.Itoa(n)
	}

	return strconv.Itoa(n)
}

// fallback returns s, or alt when s is blank.
func fallback(s, alt string) string {
	if strings.TrimSpace(s) == "" {
		return alt
	}

	return s
}

// sanitizePathPart makes a string safe as a single path segment on
// every supported platform: Windows reserves characters that are legal
// on Linux, and a library synced between the two must not produce
// unopenable files.
func sanitizePathPart(s string) string {
	const maxSegment = 120

	var b strings.Builder

	b.Grow(len(s))

	for _, r := range s {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteByte('_')
		default:
			if r < 0x20 {
				continue
			}

			b.WriteRune(r)
		}
	}

	out := strings.TrimSpace(b.String())

	// Trailing dots and spaces are silently stripped by Windows, which
	// turns "Vol. 2 " into a name that no longer round-trips.
	out = strings.TrimRight(out, ". ")

	if len(out) > maxSegment {
		out = strings.TrimSpace(out[:maxSegment])
	}

	if out == "" {
		return "Unknown"
	}

	return out
}
