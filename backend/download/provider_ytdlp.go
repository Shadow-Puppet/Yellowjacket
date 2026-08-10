package download

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// yt-dlp is the local-subprocess shape: no server for the user to run,
// no credentials, but everything comes back as text from a binary whose
// output format is not a stable contract.  The defences are: pin a
// minimum version and check it before use, ask for JSON rather than
// parsing human output, and never build a shell command — every
// invocation is an argv slice, so a track title containing `; rm -rf`
// is an argument and not a command.

// yt-dlp provider errors.
var (
	// ErrYtDlpMissing means the binary was not found.
	ErrYtDlpMissing = errors.New("yt-dlp was not found")

	// ErrYtDlpTooOld means the installed version predates the output
	// format this adapter relies on.
	ErrYtDlpTooOld = errors.New("yt-dlp is too old")

	// ErrYtDlpFailed wraps a non-zero exit.
	ErrYtDlpFailed = errors.New("yt-dlp failed")

	// ErrUnsafeURL rejects a URL that is not plain http(s).  yt-dlp
	// accepts things like file:// that must never come from a search
	// result.
	ErrUnsafeURL = errors.New("refusing to fetch a non-http URL")
)

// minYtDlpVersion is the oldest release known to support the
// --progress-template and --dump-json output this adapter parses.
// yt-dlp versions are date-stamped, so this compares lexically.
const minYtDlpVersion = "2023.01.01"

// ytSearchCount is how many results to ask for per query.
const ytSearchCount = 5

// ytTrackConcurrency bounds parallel per-track searches when assembling
// an album.  YouTube throttles aggressively; three is fast enough to
// finish inside the search timeout without tripping it.
const ytTrackConcurrency = 3

func init() {
	Register(
		Descriptor{
			Kind:    KindYtDlp,
			Name:    "yt-dlp",
			Summary: "Download audio from YouTube, SoundCloud, Bandcamp and other sites yt-dlp supports.",
			Caps: Caps{
				CanSearch:    true,
				CanTransport: true,
				CanCancel:    true,
				ReportsSize:  true,
			},
			Fields: []Field{
				{
					Key:         "binary",
					Label:       "yt-dlp path",
					Placeholder: "yt-dlp",
					Help:        "Leave blank to find yt-dlp on your PATH.",
					Default:     "yt-dlp",
				},
				{
					Key:     "audioFormat",
					Label:   "Audio format",
					Help:    "flac, mp3, opus, m4a, or 'best' to keep the source format.",
					Default: "flac",
				},
				{
					Key:   "searchPrefix",
					Label: "Search source",
					Help: "ytsearch for YouTube, ytmsearch for YouTube Music. " +
						"Defaults to ytsearch.",
					Default: "ytsearch",
				},
			},
		},
		newYtDlp,
	)
}

// ytDlp is the yt-dlp provider.
type ytDlp struct {
	info   ProviderInfo
	logger *slog.Logger

	binary       string
	audioFormat  string
	searchPrefix string
}

// newYtDlp builds a yt-dlp provider from config.
func newYtDlp(
	cfg Config,
	_ SecretLookup,
	logger *slog.Logger,
) (Provider, error) {
	binary := cfg.Setting("binary", "yt-dlp")

	resolved, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrYtDlpMissing, binary)
	}

	return &ytDlp{
		info: ProviderInfo{
			ID:       cfg.ID,
			Kind:     KindYtDlp,
			Name:     cfg.Name,
			Enabled:  cfg.Enabled,
			Priority: cfg.Priority,
			Caps: Caps{
				CanSearch:    true,
				CanTransport: true,
				CanCancel:    true,
				ReportsSize:  true,
			},
		},
		logger:       logger.With("provider", "yt-dlp"),
		binary:       resolved,
		audioFormat:  cfg.Setting("audioFormat", "flac"),
		searchPrefix: cfg.Setting("searchPrefix", "ytsearch"),
	}, nil
}

// Info returns the provider's identity.
func (y *ytDlp) Info() ProviderInfo {
	return y.info
}

// Close is a no-op; each invocation is its own process.
func (y *ytDlp) Close() error {
	return nil
}

// Check verifies the binary runs and is new enough.  Version drift is
// yt-dlp's defining trait, so this is the difference between a clear
// error at configuration time and garbled output at download time.
func (y *ytDlp) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, y.binary, "--version").Output()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrYtDlpFailed, err)
	}

	version := strings.TrimSpace(string(out))
	if version < minYtDlpVersion {
		return fmt.Errorf(
			"%w: found %s, need %s or newer",
			ErrYtDlpTooOld, version, minYtDlpVersion,
		)
	}

	return nil
}

// ytEntry is the subset of yt-dlp's --dump-json output this adapter
// uses.  yt-dlp emits far more; naming only what is needed means a
// field being added or reordered upstream cannot break parsing.
type ytEntry struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	URL      string  `json:"url"`
	WebURL   string  `json:"webpage_url"`
	Uploader string  `json:"uploader"`
	Duration float64 `json:"duration"`
	Filesize int64   `json:"filesize_approx"`
}

// link returns the entry's best usable URL.
func (e ytEntry) link() string {
	if e.WebURL != "" {
		return e.WebURL
	}

	return e.URL
}

// Search assembles candidates.  With an expected tracklist it searches
// per track and offers the assembled album as one candidate, which is
// how yt-dlp is actually useful for albums — a single "full album"
// video is one file and cannot be imported as tracks.  Without a
// tracklist it falls back to returning the top individual results.
func (y *ytDlp) Search(ctx context.Context, dl Download) ([]Candidate, error) {
	if len(dl.Expected) > 0 {
		c, err := y.assembleAlbum(ctx, dl)
		if err != nil {
			return nil, err
		}

		if len(c.Files) > 0 {
			return []Candidate{c}, nil
		}
	}

	entries, err := y.search(ctx, dl.SearchText(), ytSearchCount)
	if err != nil {
		return nil, err
	}

	out := make([]Candidate, 0, len(entries))

	for _, e := range entries {
		link := e.link()
		if link == "" {
			continue
		}

		name := sanitizePathPart(e.Title) + "." + y.extension()

		out = append(out, Candidate{
			ID:       "ytdlp:" + e.ID,
			Kind:     KindYtDlp,
			Protocol: ProtocolDirect,
			Title:    e.Title,
			Artist:   e.Uploader,
			Origin:   "yt-dlp",
			Files: []CandidateFile{{
				Path:    name,
				Size:    e.Filesize,
				IsAudio: true,
			}},
			TotalSize: e.Filesize,
			// yt-dlp results are always available; there is no peer to
			// be offline, so health carries no information here.
			Health:  0.75,
			Payload: map[string]string{name: link},
		})
	}

	return out, nil
}

// assembleAlbum searches once per expected track and builds a single
// multi-file candidate.  Tracks that find no result are left out; the
// completeness score then reflects the gap, and the importer's
// threshold decides whether what arrived is enough.
func (y *ytDlp) assembleAlbum(
	ctx context.Context,
	dl Download,
) (Candidate, error) {
	type hit struct {
		index int
		entry ytEntry
	}

	var (
		mu   sync.Mutex
		hits []hit
	)

	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(ytTrackConcurrency)

	for i, track := range dl.Expected {
		group.Go(func() error {
			query := strings.TrimSpace(
				dl.Artist + " " + track.Title,
			)

			entries, err := y.search(gctx, query, 1)
			if err != nil || len(entries) == 0 {
				// One missing track is not a failed search.  Recording
				// nothing lets completeness scoring speak for it.
				return nil //nolint:nilerr // partial results are expected
			}

			mu.Lock()

			hits = append(hits, hit{index: i, entry: entries[0]})

			mu.Unlock()

			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return Candidate{}, fmt.Errorf("assemble album: %w", err)
	}

	c := Candidate{
		ID:       "ytdlp:album:" + dl.ID,
		Kind:     KindYtDlp,
		Protocol: ProtocolDirect,
		Title:    dl.Album,
		Artist:   dl.Artist,
		Origin:   "yt-dlp (assembled per track)",
		Health:   0.75,
		Payload:  map[string]string{},
		Files:    make([]CandidateFile, 0, len(hits)),
	}

	for _, h := range hits {
		track := dl.Expected[h.index]

		link := h.entry.link()
		if link == "" {
			continue
		}

		// Name the staged file after the expected track, not the video
		// title: the video is called whatever the uploader felt like,
		// and the import step matches on filename.
		name := trackToken(track.Position) + " - " +
			sanitizePathPart(track.Title) + "." + y.extension()

		c.Files = append(c.Files, CandidateFile{
			Path:      name,
			Size:      h.entry.Filesize,
			IsAudio:   true,
			MatchedTo: track.Position,
		})

		c.TotalSize += h.entry.Filesize
		c.Payload[name] = link
	}

	return c, nil
}

// search runs one yt-dlp search and decodes its JSON lines.
func (y *ytDlp) search(
	ctx context.Context,
	query string,
	count int,
) ([]ytEntry, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	// The search term is one argv element; yt-dlp parses the
	// "ytsearchN:" prefix itself.  No shell is involved at any point.
	target := y.searchPrefix + strconv.Itoa(count) + ":" + query

	args := []string{
		"--dump-json",
		"--flat-playlist",
		"--no-warnings",
		"--no-playlist",
		"--ignore-config",
		"--socket-timeout", "15",
		target,
	}

	cmd := exec.CommandContext(ctx, y.binary, args...)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: search: %w", ErrYtDlpFailed, err)
	}

	return decodeYtEntries(strings.NewReader(string(out))), nil
}

// decodeYtEntries reads newline-delimited JSON, skipping lines that do
// not parse.  yt-dlp mixes warnings into stdout in some versions, and
// one bad line must not discard the rest of the results.
func decodeYtEntries(r io.Reader) []ytEntry {
	var out []ytEntry

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}

		var e ytEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}

		if e.ID == "" {
			continue
		}

		out = append(out, e)
	}

	return out
}

// Grab downloads each of the candidate's files into dst.
func (y *ytDlp) Grab(
	ctx context.Context,
	c Candidate,
	dst string,
	onProgress ProgressFunc,
) (Result, error) {
	result := Result{Dir: dst, Files: make([]string, 0, len(c.Files))}

	for i, f := range c.Files {
		link, ok := c.Payload[f.Path]
		if !ok {
			continue
		}

		if err := validateHTTPURL(link); err != nil {
			return Result{}, err
		}

		if onProgress != nil {
			onProgress(Progress{
				Current: int64(i),
				Total:   int64(len(c.Files)),
				Phase: fmt.Sprintf(
					"Downloading %d of %d", i+1, len(c.Files),
				),
			})
		}

		path, err := y.fetchOne(ctx, link, dst, f.Path)
		if err != nil {
			return Result{}, err
		}

		result.Files = append(result.Files, path)
	}

	return result, nil
}

// fetchOne downloads a single URL to a known filename inside dst.
func (y *ytDlp) fetchOne(
	ctx context.Context,
	link, dst, name string,
) (string, error) {
	// Strip the extension from the output template: yt-dlp appends the
	// real one after extraction, and forcing it here produces
	// double-extensioned files.
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	template := filepath.Join(dst, stem) + ".%(ext)s"

	args := []string{
		"--extract-audio",
		"--no-playlist",
		"--no-warnings",
		"--ignore-config",
		"--newline",
		"--no-part",
		"--socket-timeout", "30",
		"--output", template,
	}

	if y.audioFormat != "" && y.audioFormat != "best" {
		args = append(args, "--audio-format", y.audioFormat)
	}

	args = append(args, "--", link)

	cmd := exec.CommandContext(ctx, y.binary, args...)

	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf(
			"%w: download: %w: %s",
			ErrYtDlpFailed, err, lastLine(string(out)),
		)
	}

	// The extracted extension is whatever yt-dlp produced, so find the
	// file by stem rather than assuming.
	matches, err := filepath.Glob(filepath.Join(dst, stem) + ".*")
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf(
			"%w: no output file for %s", ErrYtDlpFailed, stem,
		)
	}

	return matches[0], nil
}

// extension returns the file extension downloads will have.
func (y *ytDlp) extension() string {
	if y.audioFormat == "" || y.audioFormat == "best" {
		return "opus"
	}

	return y.audioFormat
}

// validateHTTPURL rejects anything that is not plain http(s).  yt-dlp
// happily accepts file:// and other schemes, and a search result is
// untrusted input.
func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrUnsafeURL, raw)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: %s", ErrUnsafeURL, raw)
	}

	return nil
}

// lastLine returns the final non-empty line of output, which is where
// yt-dlp puts its error message.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")

	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}

	return ""
}
