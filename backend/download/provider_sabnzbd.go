package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

// SABnzbd is the usenet half of the split-role pair: Prowlarr finds an
// NZB, SABnzbd fetches and unpacks it.
//
// Like slskd, it writes to its own completed-downloads directory rather
// than one we choose, and unlike qBittorrent there is no per-job save
// path we can set reliably across versions.  It does, however, report
// the final storage path in its history, so the collect step reads that
// rather than guessing.

// SABnzbd provider errors.
var (
	// ErrSabUnreachable means the instance did not answer.
	ErrSabUnreachable = errors.New("sabnzbd is unreachable")

	// ErrSabAuth means the API key was rejected.
	ErrSabAuth = errors.New("sabnzbd rejected the API key")

	// ErrSabTransferFailed means the job failed or was removed.
	ErrSabTransferFailed = errors.New("sabnzbd job failed")

	// ErrSabNoJob means the queued job vanished from both queue and
	// history without completing.
	ErrSabNoJob = errors.New("sabnzbd job disappeared")
)

// SABnzbd tuning.
const (
	// sabHTTPTimeout bounds one API call.
	sabHTTPTimeout = 30 * time.Second

	// sabPollInterval is how often job state is checked.
	sabPollInterval = 5 * time.Second
)

func init() {
	Register(
		Descriptor{
			Kind: KindSABnzbd,
			Name: "SABnzbd",
			Summary: "Download usenet releases found by an indexer. " +
				"Pairs with Prowlarr; does not search on its own.",
			RequiresExternal: "SABnzbd",
			Caps: Caps{
				CanTransport: true,
				CanCancel:    true,
				ReportsSize:  true,
				Transports:   []Protocol{ProtocolUsenet},
			},
			Fields: []Field{
				{
					Key:         "url",
					Label:       "SABnzbd URL",
					Placeholder: "http://localhost:8080",
					Required:    true,
					Default:     "http://localhost:8080",
				},
				{
					Key:      "apiKey",
					Label:    "API key",
					Secret:   true,
					Required: true,
					Help:     "SABnzbd → Config → General → API Key.",
				},
				{
					Key:   "category",
					Label: "Category",
					Help: "SABnzbd category for these downloads. " +
						"Its folder must be readable from this machine.",
					Default: "music",
				},
			},
		},
		newSABnzbd,
	)
}

// sabnzbd is the SABnzbd transport.
type sabnzbd struct {
	info   ProviderInfo
	logger *slog.Logger
	client *apiClient

	apiKey   string
	category string

	pollInterval time.Duration
}

// newSABnzbd builds the transport from config.
func newSABnzbd(
	cfg Config,
	secrets SecretLookup,
	logger *slog.Logger,
) (Provider, error) {
	base := strings.TrimRight(cfg.Setting("url", ""), "/")
	if base == "" {
		return nil, fmt.Errorf("%w: SABnzbd URL is required", ErrNotConfigured)
	}

	apiKey := ""

	if secrets != nil {
		key, err := secrets("apiKey")
		if err != nil {
			return nil, fmt.Errorf("%w: no API key stored", ErrNotConfigured)
		}

		apiKey = key
	}

	return &sabnzbd{
		info: ProviderInfo{
			ID:       cfg.ID,
			Kind:     KindSABnzbd,
			Name:     cfg.Name,
			Enabled:  cfg.Enabled,
			Priority: cfg.Priority,
			Caps: Caps{
				CanTransport: true,
				CanCancel:    true,
				ReportsSize:  true,
				Transports:   []Protocol{ProtocolUsenet},
			},
		},
		logger: logger.With("provider", "sabnzbd"),
		// SABnzbd authenticates by query parameter, not header, so the
		// shared client carries no auth header here.
		client: newAPIClient(
			base, "", "", sabHTTPTimeout, ErrSabUnreachable, ErrSabAuth,
		),
		apiKey:       apiKey,
		category:     cfg.Setting("category", "music"),
		pollInterval: sabPollInterval,
	}, nil
}

// Info returns the provider's identity.
func (s *sabnzbd) Info() ProviderInfo {
	return s.info
}

// Close is a no-op.
func (s *sabnzbd) Close() error {
	return nil
}

// sabResponse is SABnzbd's common envelope.  It answers a bad API key
// with HTTP 200 and status:false, so the body has to be inspected.
type sabResponse struct {
	Status bool     `json:"status"`
	Error  string   `json:"error"`
	NzoIDs []string `json:"nzo_ids"`
}

// sabQueue is the queue view.
type sabQueue struct {
	Queue struct {
		Slots []sabQueueSlot `json:"slots"`
	} `json:"queue"`
}

// sabQueueSlot is one in-flight job.
type sabQueueSlot struct {
	NzoID      string `json:"nzo_id"`
	Filename   string `json:"filename"`
	Status     string `json:"status"`
	Percentage string `json:"percentage"`
	MB         string `json:"mb"`
	MBLeft     string `json:"mbleft"`
}

// sabHistory is the history view.
type sabHistory struct {
	History struct {
		Slots []sabHistorySlot `json:"slots"`
	} `json:"history"`
}

// sabHistorySlot is one finished job.  Storage is the unpacked path,
// which is the only reliable way to find what SABnzbd produced.
type sabHistorySlot struct {
	NzoID   string `json:"nzo_id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Storage string `json:"storage"`
	FailMsg string `json:"fail_message"`
	Bytes   int64  `json:"bytes"`
}

// Check verifies the instance answers and the key is accepted.
func (s *sabnzbd) Check(ctx context.Context) error {
	var resp struct {
		Version string `json:"version"`
	}

	if err := s.call(ctx, url.Values{"mode": {"version"}}, &resp); err != nil {
		return err
	}

	// version answers without auth, so make one authenticated call too.
	var queue sabQueue

	return s.call(ctx, url.Values{"mode": {"queue"}}, &queue)
}

// Grab adds the NZB, waits for SABnzbd to finish, and moves the
// unpacked files into dst.
func (s *sabnzbd) Grab(
	ctx context.Context,
	c Candidate,
	dst string,
	onProgress ProgressFunc,
) (Result, error) {
	link := c.Payload["link"]
	if link == "" {
		return Result{}, fmt.Errorf(
			"%w: candidate has no NZB URL", ErrSabTransferFailed,
		)
	}

	if err := validateHTTPURL(link); err != nil {
		return Result{}, err
	}

	var added sabResponse

	if err := s.call(ctx, url.Values{
		"mode":     {"addurl"},
		"name":     {link},
		"cat":      {s.category},
		"nzbname":  {c.Title},
		"priority": {"0"},
	}, &added); err != nil {
		return Result{}, err
	}

	if !added.Status || len(added.NzoIDs) == 0 {
		return Result{}, fmt.Errorf(
			"%w: %s", ErrSabTransferFailed, added.Error,
		)
	}

	nzoID := added.NzoIDs[0]

	slot, err := s.await(ctx, nzoID, onProgress)
	if err != nil {
		return Result{}, err
	}

	if !strings.EqualFold(slot.Status, "Completed") {
		return Result{}, fmt.Errorf(
			"%w: %s: %s", ErrSabTransferFailed, slot.Status, slot.FailMsg,
		)
	}

	return collectTree(slot.Storage, dst)
}

// await polls the queue until the job leaves it, then reads history for
// the outcome.  SABnzbd moves a job from queue to history when it
// finishes post-processing, so history is where completion is truthful.
func (s *sabnzbd) await(
	ctx context.Context,
	nzoID string,
	onProgress ProgressFunc,
) (sabHistorySlot, error) {
	for {
		select {
		case <-ctx.Done():
			return sabHistorySlot{}, fmt.Errorf(
				"%w: cancelled", ErrSabTransferFailed,
			)
		case <-time.After(s.pollInterval):
		}

		inQueue, slot, err := s.queueSlot(ctx, nzoID)
		if err != nil {
			s.logger.Debug("sabnzbd queue poll failed", "error", err)

			continue
		}

		if inQueue {
			if onProgress != nil {
				onProgress(Progress{
					Current: parseMB(slot.MB) - parseMB(slot.MBLeft),
					Total:   parseMB(slot.MB),
					Phase:   "Downloading from usenet (" + slot.Status + ")",
				})
			}

			continue
		}

		found, hist, err := s.historySlot(ctx, nzoID)
		if err != nil {
			s.logger.Debug("sabnzbd history poll failed", "error", err)

			continue
		}

		if found {
			return hist, nil
		}

		// Not in the queue and not in history: the job was removed out
		// from under us.
		return sabHistorySlot{}, ErrSabNoJob
	}
}

// queueSlot looks for a job in the queue.
func (s *sabnzbd) queueSlot(
	ctx context.Context,
	nzoID string,
) (bool, sabQueueSlot, error) {
	var queue sabQueue

	if err := s.call(ctx, url.Values{"mode": {"queue"}}, &queue); err != nil {
		return false, sabQueueSlot{}, err
	}

	for _, slot := range queue.Queue.Slots {
		if slot.NzoID == nzoID {
			return true, slot, nil
		}
	}

	return false, sabQueueSlot{}, nil
}

// historySlot looks for a job in history.
func (s *sabnzbd) historySlot(
	ctx context.Context,
	nzoID string,
) (bool, sabHistorySlot, error) {
	var history sabHistory

	if err := s.call(
		ctx, url.Values{"mode": {"history"}}, &history,
	); err != nil {
		return false, sabHistorySlot{}, err
	}

	for _, slot := range history.History.Slots {
		if slot.NzoID == nzoID {
			return true, slot, nil
		}
	}

	return false, sabHistorySlot{}, nil
}

// call performs one API request.  SABnzbd puts everything on the query
// string of a single endpoint.
func (s *sabnzbd) call(ctx context.Context, params url.Values, out any) error {
	params.Set("apikey", s.apiKey)
	params.Set("output", "json")

	return s.client.get(ctx, "/api?"+params.Encode(), out)
}

// parseMB converts SABnzbd's megabyte strings to bytes.  Values are
// decimal strings like "1024.5"; a malformed one yields 0 rather than
// failing a transfer that is otherwise fine.
func parseMB(s string) int64 {
	const bytesPerMB = 1024 * 1024

	var mb float64

	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%f", &mb); err != nil {
		return 0
	}

	return int64(mb * bytesPerMB)
}
