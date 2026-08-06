package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
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
	// more peers than bailing at the first response.
	slskdSearchWait = 12 * time.Second

	// slskdTransferPoll is how often transfer state is polled.
	slskdTransferPoll = 3 * time.Second

	// slskdMinFiles is the fewest audio files a folder needs before it
	// is offered as a candidate.  Soulseek returns a lot of one-file
	// noise for common queries.
	slskdMinFiles = 2

	// slskdHTTPTimeout bounds one API call.
	slskdHTTPTimeout = 20 * time.Second
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
	Username           string      `json:"username"`
	HasFreeUploadSlot  bool        `json:"hasFreeUploadSlot"`
	QueueLength        int         `json:"queueLength"`
	UploadSpeed        int64       `json:"uploadSpeed"`
	Files              []slskdFile `json:"files"`
	LockedFileCount    int         `json:"lockedFileCount"`
	FileCount          int         `json:"fileCount"`
	FreeUploadSlotFlag bool        `json:"freeUploadSlots"`
}

// slskdFile is one file a peer is offering.
type slskdFile struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	BitRate  int    `json:"bitRate"`
	Length   int    `json:"length"`
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
func (s *slskd) Search(ctx context.Context, req Request) ([]Candidate, error) {
	searchID := newID()

	body := map[string]any{
		"id":         searchID,
		"searchText": req.SearchText(),
	}

	if err := s.client.post(ctx, "/api/v0/searches", body, nil); err != nil {
		return nil, err
	}

	search, err := s.awaitSearch(ctx, searchID)
	if err != nil {
		return nil, err
	}

	// Best effort cleanup; a left-behind search is harmless but clutters
	// the slskd UI.
	defer func() {
		_ = s.client.delete(
			context.WithoutCancel(ctx), "/api/v0/searches/"+searchID,
		)
	}()

	return s.candidatesFrom(search), nil
}

// awaitSearch polls until the search completes or the budget runs out.
// A timeout is not an error: partial Soulseek results are normal and
// often good enough.
func (s *slskd) awaitSearch(
	ctx context.Context,
	searchID string,
) (slskdSearch, error) {
	deadline := time.Now().Add(s.searchWait)

	var last slskdSearch

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return last, fmt.Errorf("%w: search cancelled", ErrSlskdTimeout)
		case <-time.After(s.searchPoll):
		}

		var search slskdSearch

		if err := s.client.get(
			ctx,
			"/api/v0/searches/"+searchID+"?includeResponses=true",
			&search,
		); err != nil {
			return last, err
		}

		last = search

		if search.IsComplete {
			return search, nil
		}
	}

	return last, nil
}

// candidatesFrom groups a search's responses into candidates.
func (s *slskd) candidatesFrom(search slskdSearch) []Candidate {
	out := make([]Candidate, 0, len(search.Responses))

	for _, resp := range search.Responses {
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
				})

				total += f.Size
			}

			if audio < slskdMinFiles {
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

// groupByFolder buckets a peer's files by their containing directory.
func groupByFolder(files []slskdFile) map[string][]slskdFile {
	out := map[string][]slskdFile{}

	for _, f := range files {
		norm := strings.ReplaceAll(f.Filename, `\`, "/")
		out[path.Dir(norm)] = append(out[path.Dir(norm)], f)
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

	if r.HasFreeUploadSlot || r.FreeUploadSlotFlag {
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

	wanted := make([]map[string]any, 0, len(c.Files))
	for _, f := range c.Files {
		wanted = append(wanted, map[string]any{
			"filename": f.Path,
			"size":     f.Size,
		})
	}

	if err := s.client.post(
		ctx, "/api/v0/transfers/downloads/"+username, wanted, nil,
	); err != nil {
		return Result{}, err
	}

	if err := s.awaitTransfers(ctx, username, c, onProgress); err != nil {
		return Result{}, err
	}

	return s.collect(c, dst)
}

// awaitTransfers polls until every requested file reaches a terminal
// state.  Soulseek queues are measured in hours, so the only deadline
// is the caller's context.
func (s *slskd) awaitTransfers(
	ctx context.Context,
	username string,
	c Candidate,
	onProgress ProgressFunc,
) error {
	wanted := make(map[string]bool, len(c.Files))
	for _, f := range c.Files {
		wanted[f.Path] = true
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: transfer cancelled", ErrSlskdTimeout)
		case <-time.After(s.transferPoll):
		}

		transfers, err := s.transfersFor(ctx, username)
		if err != nil {
			// A blip talking to the daemon should not abandon a
			// transfer that may be hours in.
			s.logger.Debug("slskd transfer poll failed", "error", err)

			continue
		}

		var (
			done, failed int
			current      int64
		)

		for _, t := range transfers {
			if !wanted[t.Filename] {
				continue
			}

			current += t.BytesTransferred

			finished, ok := t.done()
			if !finished {
				continue
			}

			if ok {
				done++
			} else {
				failed++
			}
		}

		if onProgress != nil {
			onProgress(Progress{
				Current: current,
				Total:   c.TotalSize,
				Phase: fmt.Sprintf(
					"Transferring from %s (%d/%d)", username, done, len(wanted),
				),
			})
		}

		if done+failed < len(wanted) {
			continue
		}

		// Some files failing is normal — a peer goes offline mid-folder.
		// Let the importer's completeness check decide whether what
		// arrived is enough, rather than discarding it here.
		if done == 0 {
			return fmt.Errorf(
				"%w: all %d files failed", ErrSlskdTransferFailed, failed,
			)
		}

		return nil
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

	if err := s.client.get(
		ctx, "/api/v0/transfers/downloads/"+username, &raw,
	); err != nil {
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

		target := filepath.Join(dst, base)

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
