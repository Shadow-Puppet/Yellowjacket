package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// qBittorrent is a pure transport: it never searches, it just takes a
// magnet link Prowlarr found and moves the bytes.  That is the point of
// splitting Transporter out — this adapter knows nothing about music.
//
// Two things make it different from the others.  Its auth is a session
// cookie rather than a header, so it needs its own client.  And it can
// be told where to save, which means the transfer lands directly in our
// staging directory instead of needing to be collected afterwards —
// provided qBittorrent sees the same filesystem we do.

// qBittorrent provider errors.
var (
	// ErrQbitUnreachable means the instance did not answer.
	ErrQbitUnreachable = errors.New("qbittorrent is unreachable")

	// ErrQbitAuth means the credentials were rejected.
	ErrQbitAuth = errors.New("qbittorrent rejected the credentials")

	// ErrQbitNoHash means the candidate carried no usable torrent
	// identifier, so the transfer could not be tracked.
	ErrQbitNoHash = errors.New("candidate has no torrent hash")

	// ErrQbitTransferFailed means the torrent errored or stalled out.
	ErrQbitTransferFailed = errors.New("qbittorrent transfer failed")
)

// qBittorrent tuning.
const (
	// qbitHTTPTimeout bounds one API call.
	qbitHTTPTimeout = 30 * time.Second

	// qbitPollInterval is how often torrent state is checked.
	qbitPollInterval = 5 * time.Second

	// qbitAppearWait is how long to wait for a just-added torrent to
	// show up in the torrent list before concluding it was rejected.
	qbitAppearWait = 60 * time.Second
)

func init() {
	Register(
		Descriptor{
			Kind: KindQBittorrent,
			Name: "qBittorrent",
			Summary: "Download torrents found by an indexer. " +
				"Pairs with Prowlarr; does not search on its own.",
			RequiresExternal: "qBittorrent",
			Caps: Caps{
				CanTransport: true,
				CanCancel:    true,
				CanResume:    true,
				ReportsSize:  true,
				Transports:   []Protocol{ProtocolTorrent},
			},
			Fields: []Field{
				{
					Key:         "url",
					Label:       "qBittorrent URL",
					Placeholder: "http://localhost:8080",
					Required:    true,
					Default:     "http://localhost:8080",
				},
				{
					Key:      "username",
					Label:    "Username",
					Required: true,
					Default:  "admin",
				},
				{
					Key:      "password",
					Label:    "Password",
					Secret:   true,
					Required: true,
				},
				{
					Key:   "category",
					Label: "Category",
					Help: "qBittorrent category to tag these downloads with. " +
						"Useful for keeping them out of your other rules.",
					Default: "yellowjacket",
				},
			},
		},
		newQBittorrent,
	)
}

// qbittorrent is the qBittorrent transport.
type qbittorrent struct {
	info   ProviderInfo
	logger *slog.Logger
	client *http.Client

	baseURL  string
	username string
	password string
	category string

	// authMu guards the lazy login, so concurrent grabs share one
	// session instead of racing to create several.
	authMu        sync.Mutex
	authenticated bool

	pollInterval time.Duration
}

// newQBittorrent builds the transport from config.
func newQBittorrent(
	cfg Config,
	secrets SecretLookup,
	logger *slog.Logger,
) (Provider, error) {
	base := strings.TrimRight(cfg.Setting("url", ""), "/")
	if base == "" {
		return nil, fmt.Errorf(
			"%w: qBittorrent URL is required", ErrNotConfigured,
		)
	}

	password := ""

	if secrets != nil {
		pw, err := secrets("password")
		if err != nil {
			return nil, fmt.Errorf("%w: no password stored", ErrNotConfigured)
		}

		password = pw
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}

	return &qbittorrent{
		info: ProviderInfo{
			ID:       cfg.ID,
			Kind:     KindQBittorrent,
			Name:     cfg.Name,
			Enabled:  cfg.Enabled,
			Priority: cfg.Priority,
			Caps: Caps{
				CanTransport: true,
				CanCancel:    true,
				CanResume:    true,
				ReportsSize:  true,
				Transports:   []Protocol{ProtocolTorrent},
			},
		},
		logger: logger.With("provider", "qbittorrent"),
		client: &http.Client{
			Timeout: qbitHTTPTimeout,
			Jar:     jar,
		},
		baseURL:      base,
		username:     cfg.Setting("username", "admin"),
		password:     password,
		category:     cfg.Setting("category", "yellowjacket"),
		pollInterval: qbitPollInterval,
	}, nil
}

// Info returns the provider's identity.
func (q *qbittorrent) Info() ProviderInfo {
	return q.info
}

// Close is a no-op; the session expires on its own.
func (q *qbittorrent) Close() error {
	return nil
}

// Check logs in and asks for the version.
func (q *qbittorrent) Check(ctx context.Context) error {
	if err := q.login(ctx); err != nil {
		return err
	}

	_, err := q.call(ctx, "/api/v2/app/version", nil)

	return err
}

// login establishes a session cookie.  qBittorrent answers a bad login
// with 200 and the body "Fails.", not a 401, so the body is what has to
// be checked.
func (q *qbittorrent) login(ctx context.Context) error {
	q.authMu.Lock()
	defer q.authMu.Unlock()

	form := url.Values{}
	form.Set("username", q.username)
	form.Set("password", q.password)

	body, err := q.post(ctx, "/api/v2/auth/login", form)
	if err != nil {
		return err
	}

	if !strings.Contains(strings.ToLower(body), "ok") {
		q.authenticated = false

		return ErrQbitAuth
	}

	q.authenticated = true

	return nil
}

// ensureAuth logs in if this client has not yet done so.
func (q *qbittorrent) ensureAuth(ctx context.Context) error {
	q.authMu.Lock()
	done := q.authenticated
	q.authMu.Unlock()

	if done {
		return nil
	}

	return q.login(ctx)
}

// qbitTorrent is the subset of qBittorrent's torrent list used here.
type qbitTorrent struct {
	Hash        string  `json:"hash"`
	Name        string  `json:"name"`
	State       string  `json:"state"`
	Progress    float64 `json:"progress"`
	Size        int64   `json:"size"`
	Completed   int64   `json:"completed"`
	ContentPath string  `json:"content_path"`
	SavePath    string  `json:"save_path"`
}

// finished reports whether the torrent has all its data.
func (t qbitTorrent) finished() bool {
	switch t.State {
	case "uploading", "stalledUP", "queuedUP", "pausedUP", "forcedUP",
		"checkingUP":
		return true
	default:
		return t.Progress >= 1.0
	}
}

// failed reports whether the torrent is in an unrecoverable state.
func (t qbitTorrent) failed() bool {
	return t.State == "error" || t.State == "missingFiles"
}

// Grab adds the torrent, waits for it to complete, and collects its
// files into dst.
func (q *qbittorrent) Grab(
	ctx context.Context,
	c Candidate,
	dst string,
	onProgress ProgressFunc,
) (Result, error) {
	if err := q.ensureAuth(ctx); err != nil {
		return Result{}, err
	}

	link := c.Payload["link"]
	if link == "" {
		return Result{}, fmt.Errorf(
			"%w: candidate has no magnet or torrent URL", ErrQbitNoHash,
		)
	}

	form := url.Values{}
	form.Set("urls", link)
	form.Set("savepath", dst)
	form.Set("category", q.category)
	// Skip qBittorrent's own "move on completion" rules: the file must
	// stay where we put it until the import step decides otherwise.
	form.Set("autoTMM", "false")

	if _, err := q.post(ctx, "/api/v2/torrents/add", form); err != nil {
		return Result{}, err
	}

	hash, err := q.resolveHash(ctx, c, link)
	if err != nil {
		return Result{}, err
	}

	torrent, err := q.await(ctx, hash, c, onProgress)
	if err != nil {
		return Result{}, err
	}

	return collectTree(torrent.ContentPath, dst)
}

// resolveHash finds the torrent's hash, preferring the one the indexer
// supplied and falling back to matching the newest torrent in our
// category — qBittorrent's add endpoint returns nothing useful.
func (q *qbittorrent) resolveHash(
	ctx context.Context,
	c Candidate,
	link string,
) (string, error) {
	if h := c.Payload["infoHash"]; h != "" {
		return strings.ToLower(h), nil
	}

	if h := infoHashFromMagnet(link); h != "" {
		return h, nil
	}

	// Poll briefly for a torrent in our category that was not there
	// before; the add is asynchronous.
	deadline := time.Now().Add(qbitAppearWait)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("%w: cancelled", ErrQbitTransferFailed)
		case <-time.After(q.pollInterval):
		}

		torrents, err := q.list(ctx, "")
		if err != nil {
			continue
		}

		for _, t := range torrents {
			if strings.EqualFold(t.Name, c.Title) {
				return t.Hash, nil
			}
		}
	}

	return "", fmt.Errorf(
		"%w: torrent never appeared in qBittorrent", ErrQbitNoHash,
	)
}

// await polls until the torrent finishes or fails.
func (q *qbittorrent) await(
	ctx context.Context,
	hash string,
	c Candidate,
	onProgress ProgressFunc,
) (qbitTorrent, error) {
	for {
		select {
		case <-ctx.Done():
			return qbitTorrent{}, fmt.Errorf(
				"%w: cancelled", ErrQbitTransferFailed,
			)
		case <-time.After(q.pollInterval):
		}

		torrents, err := q.list(ctx, hash)
		if err != nil {
			q.logger.Debug("qbittorrent poll failed", "error", err)

			continue
		}

		if len(torrents) == 0 {
			continue
		}

		t := torrents[0]

		if onProgress != nil {
			total := t.Size
			if total == 0 {
				total = c.TotalSize
			}

			onProgress(Progress{
				Current: t.Completed,
				Total:   total,
				Phase:   "Downloading torrent (" + t.State + ")",
			})
		}

		if t.failed() {
			return qbitTorrent{}, fmt.Errorf(
				"%w: state %s", ErrQbitTransferFailed, t.State,
			)
		}

		if t.finished() {
			return t, nil
		}
	}
}

// list returns torrents, optionally filtered to one hash.
func (q *qbittorrent) list(
	ctx context.Context,
	hash string,
) ([]qbitTorrent, error) {
	params := url.Values{}
	if hash != "" {
		params.Set("hashes", hash)
	}

	var out []qbitTorrent

	if err := q.getJSON(ctx, "/api/v2/torrents/info", params, &out); err != nil {
		return nil, err
	}

	return out, nil
}

// infoHashFromMagnet extracts the btih hash from a magnet URI.
func infoHashFromMagnet(magnet string) string {
	if !strings.HasPrefix(magnet, "magnet:") {
		return ""
	}

	u, err := url.Parse(magnet)
	if err != nil {
		return ""
	}

	for _, xt := range u.Query()["xt"] {
		if after, ok := strings.CutPrefix(xt, "urn:btih:"); ok {
			return strings.ToLower(after)
		}
	}

	return ""
}

// collectTree gathers every file under root into dst.  A torrent may be
// a single file or a directory tree; either way the importer wants a
// flat set of paths inside the staging directory.
func collectTree(root, dst string) (Result, error) {
	result := Result{Dir: dst, Files: []string{}}

	info, err := os.Stat(root)
	if err != nil {
		return Result{}, fmt.Errorf("stat downloaded content: %w", err)
	}

	if !info.IsDir() {
		target := filepath.Join(dst, filepath.Base(root))

		if root != target {
			if err := movePath(root, target); err != nil {
				return Result{}, err
			}
		}

		result.Files = append(result.Files, target)
		result.BytesTransferred = info.Size()

		return result, nil
	}

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err //nolint:wrapcheck // walk error passthrough
		}

		fi, err := d.Info()
		if err != nil || fi.Size() == 0 {
			return nil //nolint:nilerr // skip unreadable entries
		}

		target := filepath.Join(dst, filepath.Base(path))

		if path != target {
			if err := movePath(path, target); err != nil {
				return err
			}
		}

		result.Files = append(result.Files, target)
		result.BytesTransferred += fi.Size()

		return nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("collect downloaded files: %w", err)
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

// call performs a GET and returns the raw body.
func (q *qbittorrent) call(
	ctx context.Context,
	endpoint string,
	params url.Values,
) (string, error) {
	target := q.baseURL + endpoint
	if len(params) > 0 {
		target += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", fmt.Errorf("build qbittorrent request: %w", err)
	}

	return q.send(req)
}

// getJSON performs a GET and decodes JSON.
func (q *qbittorrent) getJSON(
	ctx context.Context,
	endpoint string,
	params url.Values,
	out any,
) error {
	body, err := q.call(ctx, endpoint, params)
	if err != nil {
		return err
	}

	if err := decodeJSON(body, out); err != nil {
		return fmt.Errorf("decode qbittorrent response: %w", err)
	}

	return nil
}

// post performs a form POST and returns the raw body.
func (q *qbittorrent) post(
	ctx context.Context,
	endpoint string,
	form url.Values,
) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		q.baseURL+endpoint,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("build qbittorrent request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// qBittorrent rejects cross-origin requests unless Referer matches.
	req.Header.Set("Referer", q.baseURL)

	return q.send(req)
}

// send executes a request and normalizes failures.
func (q *qbittorrent) send(req *http.Request) (string, error) {
	resp, err := q.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrQbitUnreachable, err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch {
	case resp.StatusCode == http.StatusForbidden:
		return "", ErrQbitAuth
	case resp.StatusCode >= 400:
		return "", fmt.Errorf(
			"%w: HTTP %d: %s",
			ErrQbitUnreachable, resp.StatusCode, strings.TrimSpace(string(body)),
		)
	}

	return string(body), nil
}
