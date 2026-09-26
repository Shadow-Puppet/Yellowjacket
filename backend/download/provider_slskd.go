package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Soulseek is reached through a user-run slskd daemon rather than the
// wire protocol.  That trades a setup step for not having to implement
// peer connections, distributed search, and upload obligations — and
// keeps the user's Soulseek credentials in their daemon instead of in
// this process.
//
// One wrinkle shapes this adapter: slskd downloads into its own
// configured directory, not one we hand it.  There is no API to stream
// a finished file back.  So the user tells us where that directory is,
// and Grab waits for the transfer, then moves the files into staging.
// When slskd runs on another machine, that path has to be a mount —
// which is why Check verifies it exists rather than discovering the
// problem after a two-hour transfer.

// slskd provider errors.
var (
	// ErrSlskdUnreachable means the daemon did not answer.
	ErrSlskdUnreachable = errors.New("slskd is unreachable")

	// ErrSlskdAuth means the API key was rejected.
	ErrSlskdAuth = errors.New("slskd rejected the API key")

	// ErrSlskdDownloadsPath means the configured downloads directory is
	// missing or unreadable from this machine.
	ErrSlskdDownloadsPath = errors.New(
		"slskd downloads directory is not readable from here",
	)

	// ErrSlskdTransferFailed means a peer transfer ended badly.
	ErrSlskdTransferFailed = errors.New("slskd transfer failed")

	// ErrSlskdTimeout means a search or transfer outlived its budget.
	ErrSlskdTimeout = errors.New("slskd timed out")
)

// slskd tuning.
const (
	// slskdSearchPoll is how often an in-flight search is polled.
	slskdSearchPoll = 1 * time.Second

	// slskdSearchWait bounds a single search.  Soulseek searches return
	// results progressively; waiting the full budget gets noticeably
	// more peers than bailing at the first response.  12s was measured
	// to miss real, available peers on real-world queries (roughly 4 of
	// 5 attempts for a live search came back empty before this many
	// responses had a chance to arrive), so this is generous rather
	// than tight.  Kept a few seconds under Manager's per-provider
	// searchTimeout (25s) so the request/cleanup round-trips around it
	// do not get cut off by the context deadline.
	slskdSearchWait = 20 * time.Second

	// slskdTransferPoll is how often transfer state is polled.
	slskdTransferPoll = 3 * time.Second

	// slskdMinFiles is the fewest audio files a folder needs before it
	// is offered as a candidate for an album.  Soulseek returns a lot of
	// one-file noise for common queries.  A single-track request takes
	// one (see minFilesFor).
	slskdMinFiles = 2

	// slskdHTTPTimeout bounds one API call.
	slskdHTTPTimeout = 20 * time.Second

	// millisPerSecond converts slskd's whole-second file lengths.
	millisPerSecond = 1000

	// slskdStallAfter is how long a grab may go without a byte arriving
	// before the peer is given up on.  It is measured from enqueue, so
	// it covers a peer that queues us and never starts as well as one
	// that starts and stops.  Ten minutes is long enough for a short
	// queue ahead of us to clear and short enough that one unresponsive
	// peer does not hold slskd's single transfer slot for an evening.
	slskdStallAfter = 10 * time.Minute

	// slskdAbsentGrace is how long a requested file may be missing from
	// slskd's transfer list before it is counted as failed.  slskd lists
	// a transfer as soon as it accepts it, so a file still absent after
	// a few polls was refused.
	slskdAbsentGrace = 30 * time.Second

	// slskdCancelTimeout bounds the cleanup that cancels abandoned
	// transfers.
	slskdCancelTimeout = 15 * time.Second
)

func init() {
	Register(
		Descriptor{
			Kind: KindSlskd,
			Name: "Soulseek (slskd)",
			Summary: "Search and download from the Soulseek network " +
				"through your own slskd daemon.",
			RequiresExternal: "slskd",
			Caps: Caps{
				CanSearch:    true,
				CanTransport: true,
				CanCancel:    true,
				ReportsSize:  true,
			},
			Fields: []Field{
				{
					Key:         "url",
					Label:       "slskd URL",
					Placeholder: "http://localhost:5030",
					Required:    true,
					Default:     "http://localhost:5030",
				},
				{
					Key:      "apiKey",
					Label:    "API key",
					Secret:   true,
					Required: true,
					Help:     "From your slskd configuration under web.authentication.",
				},
				{
					Key:         "downloadsPath",
					Label:       "slskd downloads folder",
					Placeholder: "/var/lib/slskd/downloads",
					Path:        true,
					Required:    true,
					Help: "The folder slskd saves to, as this machine sees it. " +
						"If slskd runs elsewhere, this must be a mounted share.",
				},
			},
		},
		newSlskd,
	)
}

// slskd is the Soulseek provider.
type slskd struct {
	info   ProviderInfo
	logger *slog.Logger
	client *apiClient

	downloadsPath string

	// Poll intervals are fields rather than constants so tests can run
	// the full search-and-transfer flow without sleeping through it.
	searchPoll   time.Duration
	searchWait   time.Duration
	transferPoll time.Duration
	stallAfter   time.Duration
	absentGrace  time.Duration
}

// newSlskd builds the provider from config.
func newSlskd(
	cfg Config,
	secrets SecretLookup,
	logger *slog.Logger,
) (Provider, error) {
	base := strings.TrimRight(cfg.Setting("url", ""), "/")
	if base == "" {
		return nil, fmt.Errorf("%w: slskd URL is required", ErrNotConfigured)
	}

	downloads := cfg.Setting("downloadsPath", "")
	if downloads == "" {
		return nil, fmt.Errorf(
			"%w: slskd downloads folder is required", ErrNotConfigured,
		)
	}

	apiKey := ""

	if secrets != nil {
		key, err := secrets("apiKey")
		if err != nil {
			return nil, fmt.Errorf("%w: no API key stored", ErrNotConfigured)
		}

		apiKey = key
	}

	return &slskd{
		info: ProviderInfo{
			ID:       cfg.ID,
			Kind:     KindSlskd,
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
		logger: logger.With("provider", "slskd"),
		client: newAPIClient(
			base, "X-Api-Key", apiKey, slskdHTTPTimeout,
			ErrSlskdUnreachable, ErrSlskdAuth,
		),
		downloadsPath: downloads,
		searchPoll:    slskdSearchPoll,
		searchWait:    slskdSearchWait,
		transferPoll:  slskdTransferPoll,
		stallAfter:    slskdStallAfter,
		absentGrace:   slskdAbsentGrace,
	}, nil
}

// Info returns the provider's identity.
func (s *slskd) Info() ProviderInfo {
	return s.info
}

// Close is a no-op; the HTTP client holds no session.
func (s *slskd) Close() error {
	return nil
}

// Check verifies the daemon answers, the key is accepted, and the
// downloads directory is readable from this machine.
func (s *slskd) Check(ctx context.Context) error {
	var app map[string]any

	if err := s.client.get(ctx, "/api/v0/application", &app); err != nil {
		return err
	}

	info, err := os.Stat(s.downloadsPath)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w: %s", ErrSlskdDownloadsPath, s.downloadsPath)
	}

	return nil
}

// ---------------------------------------------------------------------------
// API types
// ---------------------------------------------------------------------------

// slskdSearch is a search as slskd reports it.
type slskdSearch struct {
	ID         string          `json:"id"`
	IsComplete bool            `json:"isComplete"`
	Responses  []slskdResponse `json:"responses"`
}

// slskdResponse is one peer's answer to a search.
type slskdResponse struct {
	Username          string      `json:"username"`
	HasFreeUploadSlot bool        `json:"hasFreeUploadSlot"`
	QueueLength       int         `json:"queueLength"`
	UploadSpeed       int64       `json:"uploadSpeed"`
	Files             []slskdFile `json:"files"`
	LockedFileCount   int         `json:"lockedFileCount"`
	FileCount         int         `json:"fileCount"`
}

// slskdFile is one file a peer is offering.
type slskdFile struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	BitRate  int    `json:"bitRate"`

	// Length is the duration in whole seconds.
	Length int `json:"length"`
}

// slskdTransfer is one download's state.
type slskdTransfer struct {
	ID               string `json:"id"`
	Username         string `json:"username"`
	Filename         string `json:"filename"`
	State            string `json:"state"`
	Size             int64  `json:"size"`
	BytesTransferred int64  `json:"bytesTransferred"`
}

// done reports whether the transfer reached a terminal state, and
// whether it succeeded.  slskd reports compound states such as
// "Completed, Succeeded" and "Completed, Errored".
func (t slskdTransfer) done() (finished, ok bool) {
	if !strings.Contains(t.State, "Completed") {
		return false, false
	}

	return true, strings.Contains(t.State, "Succeeded")
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

// Search runs a Soulseek search and groups the results into per-peer,
// per-folder candidates.  A folder from one peer is the unit a user
// actually wants: Soulseek has no album concept, but people organise
// their shares by album directory.
//
// Up to two queries run at once — the request as written and a
// normalised form of it (see slskdQueries) — and their candidates are
// merged.  They run concurrently rather than as a fallback because the
// manager gives a provider one search budget, and a Soulseek search
// spends most of it waiting for peers to answer; a second query after
// the first would not fit.
func (s *slskd) Search(ctx context.Context, dl Download) ([]Candidate, error) {
	queries := slskdQueries(dl)
	if len(queries) == 0 {
		return nil, nil
	}

	type found struct {
		candidates []Candidate
		err        error
	}

	results := make(chan found, len(queries))

	for _, q := range queries {
		go func(q string) {
			c, err := s.searchOnce(ctx, q, minFilesFor(dl))
			results <- found{candidates: c, err: err}
		}(q)
	}

	var (
		out      []Candidate
		seen     = map[string]bool{}
		firstErr error
		answered int
	)

	for range queries {
		r := <-results
		if r.err != nil {
			s.logger.Debug("slskd search failed", "error", r.err)

			if firstErr == nil {
				firstErr = r.err
			}

			continue
		}

		answered++

		// The same peer's folder turns up under both queries; the ID is
		// peer and folder, so it is the same candidate.
		for _, c := range r.candidates {
			if seen[c.ID] {
				continue
			}

			seen[c.ID] = true

			out = append(out, c)
		}
	}

	if answered == 0 {
		return nil, firstErr
	}

	return out, nil
}

// searchOnce runs one query to completion and returns its candidates.
func (s *slskd) searchOnce(
	ctx context.Context,
	text string,
	minFiles int,
) ([]Candidate, error) {
	// slskd's search endpoint deserializes id as a .NET Guid server-side,
	// so it must be a dashed UUID — the app's own newID() (a plain hex
	// string, used for request/item IDs elsewhere) is rejected with an
	// HTTP 400 before any search happens.
	searchID := uuid.NewString()

	if err := s.client.post(
		ctx, "/api/v0/searches", s.searchRequest(searchID, text, minFiles), nil,
	); err != nil {
		return nil, err
	}

	// Best effort cleanup; a left-behind search is harmless but clutters
	// the slskd UI.
	defer func() {
		_ = s.client.delete(
			context.WithoutCancel(ctx), "/api/v0/searches/"+searchID,
		)
	}()

	if err := s.awaitSearch(ctx, searchID); err != nil {
		return nil, err
	}

	responses, err := s.searchResponses(ctx, searchID)
	if err != nil {
		return nil, err
	}

	return s.candidatesFrom(responses, minFiles), nil
}

// searchRequest is the body that starts a search.
//
// Every option is stated rather than left to the daemon, because
// slskd's defaults are its own and not ours.  Its search timeout in
// particular has to finish inside our wait: a search that slskd is still
// running when we stop polling is results we asked for and discarded.
// The response and file limits are raised well above what a popular
// album produces, and the peer filters let slskd drop answers this
// provider would only score down to nothing — a folder too small to be
// a candidate, a peer with a queue it will not reach today.
func (s *slskd) searchRequest(id, text string, minFiles int) map[string]any {
	const (
		responseLimit          = 500
		fileLimit              = 20_000
		maximumPeerQueueLength = 100
	)

	// A tenth of the wait is left for the last poll and the responses
	// fetch.
	timeout := s.searchWait - s.searchWait/10

	return map[string]any{
		"id":                       id,
		"searchText":               text,
		"searchTimeout":            timeout.Milliseconds(),
		"responseLimit":            responseLimit,
		"fileLimit":                fileLimit,
		"filterResponses":          true,
		"minimumResponseFileCount": minFiles,
		"maximumPeerQueueLength":   maximumPeerQueueLength,
	}
}

// slskdQueries is what is searched for a request: the request's own
// search text, and a normalised form of it when that differs.
//
// Soulseek matches every term against the file's full path, so each
// extra word is a filter, and some words filter wrongly:
//
//   - edition qualifiers — "(Deluxe Edition)", "[2011 Remaster]" — are
//     in the catalog's title and rarely in anyone's folder name;
//   - punctuation splits a term oddly, and a term that starts with "-"
//     is an *exclusion*, so an album called "-ism" searches for
//     everything without it;
//   - "Various Artists" is in no one's path for a compilation.
//
// A query the user typed is theirs and is searched exactly as written.
func slskdQueries(dl Download) []string {
	primary := strings.TrimSpace(dl.SearchText())
	if primary == "" {
		return nil
	}

	out := []string{primary}

	if dl.Query != "" {
		return out
	}

	artist := dl.Artist
	if isVariousArtists(artist) {
		artist = ""
	}

	normal := Download{
		Artist: normalizeSearchTerms(artist),
		Album:  normalizeSearchTerms(editionPattern.ReplaceAllString(dl.Album, " ")),
	}

	if alt := strings.TrimSpace(normal.SearchText()); alt != "" &&
		!strings.EqualFold(alt, primary) {
		out = append(out, alt)
	}

	return out
}

var (
	// editionPattern finds an edition qualifier: a bracketed group that
	// names an edition, or a trailing " - 2011 Remaster".
	editionPattern = regexp.MustCompile(
		`(?i)\s*[(\[][^)\]]*\b(?:deluxe|edition|remaster(?:ed)?|expanded|` +
			`anniversary|bonus|explicit|reissue|special|collector'?s?|` +
			`version|mono|stereo)\b[^)\]]*[)\]]` +
			`|\s+-\s+(?:\d{4}\s+)?remaster(?:ed)?\b.*$`,
	)

	// nonWordPattern is everything that is not a letter or a digit.
	nonWordPattern = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

// normalizeSearchTerms reduces text to plain words.
func normalizeSearchTerms(s string) string {
	return strings.Join(strings.Fields(nonWordPattern.ReplaceAllString(s, " ")), " ")
}

// isVariousArtists reports whether an artist credit is a compilation's
// placeholder rather than an artist.
func isVariousArtists(artist string) bool {
	switch strings.ToLower(strings.TrimSpace(artist)) {
	case "various artists", "various", "va":
		return true
	default:
		return false
	}
}

// minFilesFor is the fewest audio files a folder must offer to be a
// candidate for this request.
//
// Soulseek answers a search with the files that match it, not with the
// folders they sit in.  An album query matches every file in the album's
// folder, because the folder name carries the terms; a *track* query
// usually matches one file per folder.  The two-file floor that filters
// out one-file noise for an album therefore filtered out every result
// for a track, and a single-track request could never be served here.
func minFilesFor(dl Download) int {
	if dl.RecordingMBID != "" {
		return 1
	}

	return slskdMinFiles
}

// awaitSearch polls until the search completes or the budget runs out.
// A timeout is not an error: partial Soulseek results are normal and
// often good enough.
//
// The poll asks for the search's state only.  It used to ask for every
// response on every one-second tick, which for a popular album is the
// same few thousand file entries serialised twenty times to be read
// once; searchResponses fetches them once at the end.
func (s *slskd) awaitSearch(ctx context.Context, searchID string) error {
	deadline := time.Now().Add(s.searchWait)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: search cancelled", ErrSlskdTimeout)
		case <-time.After(s.searchPoll):
		}

		var search slskdSearch

		if err := s.client.get(
			ctx, "/api/v0/searches/"+searchID, &search,
		); err != nil {
			return err
		}

		if search.IsComplete {
			return nil
		}
	}

	return nil
}

// searchResponses fetches a search's responses once.
//
// `/searches/{id}/responses` is the endpoint for that; a daemon that
// does not answer it is asked the older way, with the search itself
// carrying its responses, so an older slskd degrades to the previous
// behaviour rather than to no results at all.
func (s *slskd) searchResponses(
	ctx context.Context,
	searchID string,
) ([]slskdResponse, error) {
	var responses []slskdResponse

	err := s.client.get(
		ctx, "/api/v0/searches/"+searchID+"/responses", &responses,
	)
	if err == nil {
		return responses, nil
	}

	s.logger.Debug(
		"slskd responses endpoint failed; asking with the search",
		"error", err,
	)

	var search slskdSearch

	if err := s.client.get(
		ctx,
		"/api/v0/searches/"+searchID+"?includeResponses=true",
		&search,
	); err != nil {
		return nil, err
	}

	return search.Responses, nil
}

// candidatesFrom groups a search's responses into candidates, dropping
// folders with fewer than minFiles audio files.
func (s *slskd) candidatesFrom(
	responses []slskdResponse,
	minFiles int,
) []Candidate {
	out := make([]Candidate, 0, len(responses))

	for _, resp := range responses {
		for folder, files := range groupByFolder(resp.Files) {
			audio := 0

			cfiles := make([]CandidateFile, 0, len(files))

			var total int64

			for _, f := range files {
				format, isAudio := FormatForPath(f.Filename)
				if isAudio {
					audio++
				}

				cfiles = append(cfiles, CandidateFile{
					Path:    f.Filename,
					Size:    f.Size,
					Format:  format,
					Bitrate: f.BitRate,
					IsAudio: isAudio,

					LengthMillis: int64(f.Length) * millisPerSecond,
				})

				total += f.Size
			}

			if audio < minFiles {
				continue
			}

			out = append(out, Candidate{
				ID:        "slskd:" + resp.Username + ":" + folder,
				Kind:      KindSlskd,
				Protocol:  ProtocolDirect,
				Title:     path.Base(strings.ReplaceAll(folder, `\`, "/")),
				Origin:    resp.Username,
				Files:     cfiles,
				TotalSize: total,
				Health:    peerHealth(resp),
				Payload:   map[string]string{"username": resp.Username},
			})
		}
	}

	return out
}

// groupByFolder buckets a peer's files by the album directory they sit
// in — the containing directory, or the one above it for a disc folder
// (see AlbumDir), so a multi-disc rip is one candidate and not two.
func groupByFolder(files []slskdFile) map[string][]slskdFile {
	out := map[string][]slskdFile{}

	for _, f := range files {
		dir := AlbumDir(f.Filename)
		out[dir] = append(out[dir], f)
	}

	return out
}

// peerHealth scores how likely a peer is to actually deliver, in 0..1.
// On Soulseek this matters more than it does for torrents: a queue of
// 40 behind a single upload slot means the transfer starts tomorrow,
// and that is the difference between a good candidate and a bad one no
// matter how good the files look.
func peerHealth(r slskdResponse) float64 {
	score := 0.35

	if r.HasFreeUploadSlot {
		score += 0.4
	}

	switch {
	case r.QueueLength == 0:
		score += 0.15
	case r.QueueLength <= 3:
		score += 0.08
	case r.QueueLength > 20:
		score -= 0.2
	}

	// Anything above roughly 1 MB/s is fast enough that more speed does
	// not change the experience.
	const fastEnough = 1_000_000

	if r.UploadSpeed > 0 {
		ratio := float64(r.UploadSpeed) / fastEnough
		if ratio > 1 {
			ratio = 1
		}

		score += 0.1 * ratio
	}

	return clamp01(score)
}

// ---------------------------------------------------------------------------
// Transfer
// ---------------------------------------------------------------------------

// Grab enqueues a candidate's files with slskd, waits for the peer to
// send them, then moves them out of slskd's download directory into the
// staging directory.
func (s *slskd) Grab(
	ctx context.Context,
	c Candidate,
	dst string,
	onProgress ProgressFunc,
) (Result, error) {
	username := c.Payload["username"]
	if username == "" {
		return Result{}, fmt.Errorf(
			"%w: candidate has no peer username", ErrSlskdTransferFailed,
		)
	}

	// slskd keeps finished transfers listed until someone removes them,
	// and a transfer is matched to the request by filename.  A record
	// left by an earlier attempt at the same file from the same peer
	// would otherwise be read as this attempt's answer the moment the
	// first poll came back — an old failure failing a transfer that has
	// not started.  So what is already terminal is noted before enqueueing
	// and ignored after.
	release, err := lockSlskdFolders(ctx, s.localFolders(c))
	if err != nil {
		return Result{}, err
	}
	defer release()

	stale := s.terminalTransferIDs(ctx, username)

	wanted := make([]map[string]any, 0, len(c.Files))
	for _, f := range c.Files {
		wanted = append(wanted, map[string]any{
			"filename": f.Path,
			"size":     f.Size,
		})
	}

	if err := s.client.post(
		ctx, slskdDownloadsPath(username), wanted, nil,
	); err != nil {
		return Result{}, err
	}

	if err := s.awaitTransfers(
		ctx, username, stale, c, onProgress,
	); err != nil {
		return Result{}, err
	}

	return s.collect(c, dst)
}

// slskdFolders serialises grabs that land in the same local folder.
//
// slskd names a download's directory after the remote *leaf* folder, so
// two different albums both shared as "Greatest Hits" — or any two
// multi-disc rips, whose leaves are "CD1" and "CD2" — are written into
// one directory, and collect finds files by name there.  Run at once,
// a file one peer never sent is filled by the other peer's file of the
// same name.  One grab per peer made that impossible; several peers at
// once makes it likely.  It is package-level and keyed on the full
// path because two configured clients can share one daemon.
var slskdFolders keyedLock[string]

// localFolders returns the directories under downloadsPath a candidate's
// files will be written to, sorted so every grab takes them in the same
// order and two cannot each hold what the other waits for.
func (s *slskd) localFolders(c Candidate) []string {
	var out []string

	for _, f := range c.Files {
		norm := strings.ReplaceAll(f.Path, `\`, "/")
		out = append(out, filepath.Join(s.downloadsPath, path.Base(path.Dir(norm))))
	}

	slices.Sort(out)

	return slices.Compact(out)
}

// lockSlskdFolders takes every folder in order, releasing what it holds
// if the context ends part way.
func lockSlskdFolders(ctx context.Context, folders []string) (func(), error) {
	releases := make([]func(), 0, len(folders))

	releaseAll := func() {
		for _, r := range slices.Backward(releases) {
			r()
		}
	}

	for _, f := range folders {
		r, err := slskdFolders.acquire(ctx, f)
		if err != nil {
			releaseAll()

			return nil, err
		}

		releases = append(releases, r)
	}

	return releaseAll, nil
}

// slskdDownloadsPath is the transfers endpoint for one peer.  Soulseek
// usernames may contain spaces and punctuation, so the name is escaped
// rather than spliced into the path.
func slskdDownloadsPath(username string) string {
	return "/api/v0/transfers/downloads/" + url.PathEscape(username)
}

// terminalTransferIDs returns the ids of this peer's transfers that are
// already finished.  Best effort: slskd answers 404 for a peer it has no
// transfers with, and any failure here means only that there is nothing
// to ignore.
func (s *slskd) terminalTransferIDs(
	ctx context.Context,
	username string,
) map[string]bool {
	transfers, err := s.transfersFor(ctx, username)
	if err != nil {
		return nil
	}

	out := make(map[string]bool, len(transfers))

	for _, t := range transfers {
		if finished, _ := t.done(); finished && t.ID != "" {
			out[t.ID] = true
		}
	}

	return out
}

// awaitTransfers polls until every requested file reaches a terminal
// state, the transfer stalls, or the caller gives up.
//
// Soulseek queues are measured in hours, so there is no deadline on the
// transfer as a whole — but there is one on *progress*.  slskd's
// transfer limit is one, so a peer that holds us in its queue without
// sending a byte is not only failing this download, it is holding every
// other Soulseek download behind it.  After stallAfter with nothing
// moving the peer is given up on, and the manager tries another.
//
// Whatever way this ends short of every file finishing, the transfers
// still live in slskd are cancelled there.  Returning without doing so
// leaves the daemon downloading into its own folder for a request
// nobody is waiting on any more.
func (s *slskd) awaitTransfers(
	ctx context.Context,
	username string,
	stale map[string]bool,
	c Candidate,
	onProgress ProgressFunc,
) error {
	wanted := make(map[string]bool, len(c.Files))
	for _, f := range c.Files {
		wanted[f.Path] = true
	}

	var (
		started      = time.Now()
		lastProgress = started
		lastBytes    int64
		live         []slskdTransfer
	)

	for {
		select {
		case <-ctx.Done():
			s.cancelTransfers(username, live)

			return fmt.Errorf("%w: transfer cancelled", ErrSlskdTimeout)
		case <-time.After(s.transferPoll):
		}

		transfers, err := s.transfersFor(ctx, username)
		if err != nil {
			// A blip talking to the daemon should not abandon a
			// transfer that may be hours in — but a daemon that stays
			// away is a stall like any other.
			s.logger.Debug("slskd transfer poll failed", "error", err)

			if time.Since(lastProgress) >= s.stallAfter {
				s.cancelTransfers(username, live)

				return fmt.Errorf(
					"%w: slskd has not answered for %s: %w",
					ErrSlskdTimeout, s.stallAfter, err,
				)
			}

			continue
		}

		tally := tallyTransfers(
			transfers, wanted, stale,
			time.Since(started) >= s.absentGrace,
		)
		live = tally.live

		if tally.bytes > lastBytes {
			lastBytes = tally.bytes
			lastProgress = time.Now()
		}

		if onProgress != nil {
			onProgress(Progress{
				Current: tally.bytes,
				Total:   c.TotalSize,
				Phase: fmt.Sprintf(
					"Transferring from %s (%d/%d)",
					username, tally.done, len(wanted),
				),
			})
		}

		if tally.done+tally.failed >= len(wanted) {
			// Some files failing is normal — a peer goes offline
			// mid-folder.  Let the importer's completeness check decide
			// whether what arrived is enough, rather than discarding it
			// here.
			if tally.done == 0 {
				return fmt.Errorf(
					"%w: all %d files failed",
					ErrSlskdTransferFailed, tally.failed,
				)
			}

			return nil
		}

		if time.Since(lastProgress) < s.stallAfter {
			continue
		}

		s.cancelTransfers(username, live)

		// A folder that stalls on its last track is the same shape as
		// one whose last track failed, and goes forward the same way.
		if tally.done > 0 {
			s.logger.Info(
				"slskd transfer stalled; keeping what arrived",
				"peer", username,
				"done", tally.done,
				"wanted", len(wanted),
			)

			return nil
		}

		return fmt.Errorf(
			"%w: %s sent nothing in %s",
			ErrSlskdTimeout, username, s.stallAfter,
		)
	}
}

// transferTally is one poll's reading of the files a grab asked for.
type transferTally struct {
	done, failed int
	bytes        int64

	// live are the requested transfers slskd is still working on,
	// which are what has to be cancelled if the grab is abandoned.
	live []slskdTransfer
}

// tallyTransfers reads a peer's transfer list against the files a grab
// asked for.
//
// A requested file slskd does not list at all is one it never accepted
// — refused at enqueue, or dropped — and it will never reach a terminal
// state to be counted by.  Once absentExpired, such a file counts as
// failed, or the grab would wait on it until the six-hour ceiling.
func tallyTransfers(
	transfers []slskdTransfer,
	wanted map[string]bool,
	stale map[string]bool,
	absentExpired bool,
) transferTally {
	seen := make(map[string]slskdTransfer, len(wanted))

	for _, t := range transfers {
		if !wanted[t.Filename] || stale[t.ID] {
			continue
		}

		seen[t.Filename] = t
	}

	var out transferTally

	for name := range wanted {
		t, ok := seen[name]
		if !ok {
			if absentExpired {
				out.failed++
			}

			continue
		}

		out.bytes += t.BytesTransferred

		finished, succeeded := t.done()

		switch {
		case !finished:
			out.live = append(out.live, t)
		case succeeded:
			out.done++
		default:
			out.failed++
		}
	}

	return out
}

// cancelTransfers asks slskd to cancel and forget transfers this grab
// is abandoning.  It runs on a context of its own: the usual reason to
// be here is that the caller's context has just been cancelled, and a
// cleanup that inherited it would never be sent.
func (s *slskd) cancelTransfers(username string, live []slskdTransfer) {
	if len(live) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(
		context.Background(), slskdCancelTimeout,
	)
	defer cancel()

	for _, t := range live {
		if t.ID == "" {
			continue
		}

		endpoint := slskdDownloadsPath(username) + "/" +
			url.PathEscape(t.ID) + "?remove=true"

		if err := s.client.delete(ctx, endpoint); err != nil {
			s.logger.Warn(
				"could not cancel slskd transfer",
				"peer", username,
				"file", t.Filename,
				"error", err,
			)
		}
	}
}

// transfersFor returns a peer's current downloads.  slskd nests
// transfers under directories, so this flattens them.
func (s *slskd) transfersFor(
	ctx context.Context,
	username string,
) ([]slskdTransfer, error) {
	var raw struct {
		Directories []struct {
			Files []slskdTransfer `json:"files"`
		} `json:"directories"`
	}

	if err := s.client.get(ctx, slskdDownloadsPath(username), &raw); err != nil {
		return nil, err
	}

	out := make([]slskdTransfer, 0, len(raw.Directories))
	for _, d := range raw.Directories {
		out = append(out, d.Files...)
	}

	return out, nil
}

// collect moves finished files out of slskd's download directory into
// staging.  slskd lays them out as <downloads>/<folder>/<file>, so each
// wanted file is looked up by its base name under the folder slskd
// derived from the remote path.
func (s *slskd) collect(c Candidate, dst string) (Result, error) {
	result := Result{Dir: dst, Files: make([]string, 0, len(c.Files))}

	for _, f := range c.Files {
		norm := strings.ReplaceAll(f.Path, `\`, "/")
		folder := path.Base(path.Dir(norm))
		base := path.Base(norm)

		src := filepath.Join(s.downloadsPath, folder, base)

		info, err := os.Stat(src)
		if err != nil || info.Size() == 0 {
			// Not every requested file arrives; that is expected and
			// handled by completeness scoring downstream.
			continue
		}

		// A multi-disc candidate keeps its disc folders in staging.
		// Flattened, disc 2's "01 Intro.flac" overwrites disc 1's, and
		// the importer loses the folder it reads the disc number from.
		target := filepath.Join(dst, base)
		if _, ok := discFolder(folder); ok {
			target = filepath.Join(dst, folder, base)
		}

		if err := movePath(src, target); err != nil {
			return Result{}, fmt.Errorf("collect %s: %w", base, err)
		}

		result.Files = append(result.Files, target)
		result.BytesTransferred += info.Size()
	}

	if len(result.Files) == 0 {
		return Result{}, fmt.Errorf(
			"%w: nothing found under %s",
			ErrSlskdDownloadsPath, s.downloadsPath,
		)
	}

	return result, nil
}
