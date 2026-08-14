// Package autotagservice exposes the autotag pipeline to the Wails
// frontend.  It composes the scoring engine (backend/autotag) with
// the cached MusicBrainz client (backend/explore), the tag writer
// (backend/tagwriter), and the cover-art embedder to produce a
// single binding surface the review UI can drive.
package autotagservice

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"yellowjacket/backend/autotag"
	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/events"
	"yellowjacket/backend/explore"
	"yellowjacket/backend/jobs"
	"yellowjacket/backend/metadata"
	"yellowjacket/backend/tagwriter"
)

const (
	// getCandidatesTimeout bounds a single foreground re-score so a
	// hung MusicBrainz query (network black hole, MB outage,
	// pathological cascade) doesn't lock the review UI forever.
	getCandidatesTimeout = 60 * time.Second

	// prefetchYieldPoll is how often the prefetch loop checks
	// whether a foreground GetCandidates is in progress.  Short
	// enough that the user's click resumes prefetch promptly when
	// they navigate away; long enough that we don't spin.
	prefetchYieldPoll = 500 * time.Millisecond
)

// folderSubPath derives the album folder's path relative to its
// library root from one of its tracks' file paths.  Returns "" when
// either input is empty, the file isn't under the library root, or
// the parent directory equals the root itself (album files dropped
// loose at the library top — display nothing rather than ".").
func folderSubPath(libraryPath, sampleFilePath string) string {
	if libraryPath == "" || sampleFilePath == "" {
		return ""
	}

	dir := filepath.Dir(sampleFilePath)
	root := filepath.Clean(libraryPath)

	if !strings.HasPrefix(dir, root) {
		return ""
	}

	rel := strings.TrimPrefix(dir, root)

	rel = strings.TrimLeft(rel, `/\`)
	if rel == "" || rel == "." {
		return ""
	}

	// Normalise to forward slashes — friendlier in the UI than
	// raw Windows backslashes, and unambiguous on every platform
	// since music libraries don't tend to contain literal "/" or
	// "\" in folder names.
	return filepath.ToSlash(rel)
}

// Service is the Wails-bound surface for the review UI.  All
// exported methods become TypeScript stubs that the frontend calls.
type Service struct {
	db      *database.DB
	scorer  *autotag.Scorer
	mb      autotag.MBClient
	mbr     *autotag.MBResolver
	applier *autotag.Applier
	exp     *explore.Service
	logger  *slog.Logger
	ctx     context.Context

	// Registry for the apply job, wired after construction like every
	// other subsystem's.  Guarded by mu.
	jobsReg *jobs.Registry

	// Queue cursor — the group_key of the last item returned.
	// GetNextPending uses it to advance.  Reset by StartAutotagQueue.
	mu        sync.Mutex
	cursor    string
	libraryID int64

	// In-flight set for ApplyAsync.  Protects against the user
	// firing two Apply jobs on the same group while the first is
	// still running.  Keyed by group_key; entries removed when the
	// background goroutine returns.  Guarded by mu.
	runningApplies map[string]struct{}

	// Background prefetch worker state — scores pending tagging
	// items asynchronously so the sidebar pills populate without
	// the user having to open each folder.  prefetchRunning ensures
	// at most one worker exists at a time; prefetchCancel signals
	// the running worker to stop early (library filter changed, etc).
	// Guarded by mu.
	prefetchRunning bool
	prefetchCancel  context.CancelFunc

	// foregroundCount is the number of in-flight foreground
	// GetCandidates calls.  The prefetch worker checks this and
	// yields between items so the user's click doesn't queue
	// behind ~K rate-limited prefetch requests.  Guarded by mu.
	foregroundCount int
}

// markForegroundBusy / markForegroundIdle / waitForForegroundIdle
// implement a cheap two-tier scheduler: the prefetch worker pauses
// while any foreground GetCandidates call is in progress so the
// MB rate limiter belongs to the user, not the background sweep.
func (s *Service) markForegroundBusy() {
	s.mu.Lock()
	s.foregroundCount++
	s.mu.Unlock()
}

func (s *Service) markForegroundIdle() {
	s.mu.Lock()

	if s.foregroundCount > 0 {
		s.foregroundCount--
	}

	s.mu.Unlock()
}

// waitForForegroundIdle blocks until no foreground request is in
// flight or ctx is cancelled.  Returns true when idle, false when
// the caller should give up (cancellation).
func (s *Service) waitForForegroundIdle(ctx context.Context) bool {
	for {
		s.mu.Lock()
		idle := s.foregroundCount == 0
		s.mu.Unlock()

		if idle {
			return true
		}

		select {
		case <-ctx.Done():
			return false
		case <-time.After(prefetchYieldPoll):
		}
	}
}

// ErrApplyInFlight is returned by ApplyAsync when a previous job
// for the same group hasn't finished yet.  The caller should
// treat this as a no-op rather than a hard failure.
var ErrApplyInFlight = errors.New("autotag: apply already in flight for this group")

// NewService wires the autotag service.  The explore service is
// used as the shared MB/LB/CAA client bundle; tagwriter handles
// per-track file writes.  Pass a nil cover-art embedder to skip
// cover-art embedding entirely.
func NewService(
	logger *slog.Logger,
	db *database.DB,
	exp *explore.Service,
	tw *tagwriter.TagWriter,
) *Service {
	mbAdapter := explore.NewAutotagClient(exp)
	scorer := autotag.NewScorer(db.Queries, mbAdapter, logger.WithGroup("autotag"))
	mbr := autotag.NewMBResolver(mbAdapter, logger.WithGroup("autotag-mb"))

	coverArt := explore.NewAutotagCoverArt(exp.CAALimiter(), logger.WithGroup("autotag-cover"))
	tagAdapter := &twAdapter{tw: tw}
	applier := autotag.NewApplier(
		db.Queries,
		tagAdapter,
		coverArt,
		logger.WithGroup("autotag-apply"),
	)

	return &Service{
		db:             db,
		scorer:         scorer,
		mb:             mbAdapter,
		mbr:            mbr,
		applier:        applier,
		exp:            exp,
		logger:         logger,
		ctx:            context.Background(),
		runningApplies: make(map[string]struct{}),
	}
}

// ServiceStartup is v3's service lifecycle hook: it runs once the
// runtime exists, and ctx is cancelled when the app shuts down.  It
// replaces v2's SetContext, which had to be called by hand from
// OnStartup and was exported, so it was also bound to the frontend.
func (s *Service) ServiceStartup(
	ctx context.Context,
	_ application.ServiceOptions,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ctx = ctx

	return nil
}

// emitEvent emits a Wails runtime event under the service lock, which
// the background prefetch/apply sweeps need because they can emit
// before OnStartup has wired the real context.  events.Emit tolerates
// that; see its doc comment.
func (s *Service) emitEvent(eventName string, data any) {
	s.mu.Lock()
	ctx := s.ctx
	s.mu.Unlock()

	events.Emit(ctx, eventName, data)
}

// StartBackgroundPrefetch kicks off (or restarts) the prefetch
// worker over all libraries.  Called from app startup hooks so
// unscored pending items get their match-% backfilled while the
// user does other things — by the time they navigate to the
// autotag page, the sidebar pills are already populated.
// Idempotent: items with an existing score are skipped, so it's
// safe to call after every library scan and on every app launch.
func (s *Service) StartBackgroundPrefetch() {
	go s.startPrefetch(0)
}

// StartAutotagQueue resets the cursor for a new review session
// and kicks off the background prefetch worker so sidebar pills
// populate while the user reviews the first folder.  Cancels any
// previous prefetch (library filter may have changed).  Pass
// libraryID=0 to cover all libraries.
func (s *Service) StartAutotagQueue(libraryID int64) {
	s.mu.Lock()
	s.cursor = ""
	s.libraryID = libraryID

	// Cancel any in-flight prefetch from a previous library
	// selection — its work is no longer relevant.
	if s.prefetchCancel != nil {
		s.prefetchCancel()
		s.prefetchCancel = nil
	}

	s.mu.Unlock()

	go s.startPrefetch(libraryID)
}

// startPrefetch walks the pending tagging items in the given
// library (or all libraries when libraryID=0) whose score is NULL
// and runs the scorer on each, persisting the top score back to
// the row so the sidebar pill shows it.  Runs sequentially to
// avoid hammering MusicBrainz; emits AutotagPrefetchProgress
// after each item and AutotagPrefetchFinished at the end.  No-op
// when a worker is already running.
func (s *Service) startPrefetch(libraryID int64) {
	ctx, cancel := context.WithCancel(s.ctx)

	s.mu.Lock()

	if s.prefetchRunning {
		s.mu.Unlock()
		cancel()

		return
	}

	s.prefetchRunning = true
	s.prefetchCancel = cancel
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.prefetchRunning = false

		if s.prefetchCancel != nil {
			// Avoid leaking the context if no one cancelled us.
			s.prefetchCancel()
			s.prefetchCancel = nil
		}

		s.mu.Unlock()
	}()

	// Self-heal before enumerating: don't burn a scoring pass on rows
	// whose bookkeeping never ran or drifted (see ListPendingFolders).
	if err := s.db.Queries.PruneOrphanedTaggingItems(ctx); err != nil {
		s.logger.Warn("prefetch: prune orphaned items failed", "err", err)
	}

	// Find all pending items missing a score.  Ordered alphabetically
	// for stable progress reporting; libraryID=0 fans out to all.
	const maxPrefetch = 5000

	rows, err := s.db.Queries.ListPendingTaggingItemsAlphabetical(
		ctx,
		sqlcgen.ListPendingTaggingItemsAlphabeticalParams{
			LibraryID:    libraryID,
			StatusFilter: "pending",
			RowOffset:    0,
			RowLimit:     maxPrefetch,
		},
	)
	if err != nil {
		s.logger.Warn("prefetch: list pending failed", "err", err)

		return
	}

	pending := make([]string, 0, len(rows))

	for _, row := range rows {
		if !row.Score.Valid || row.Score.Float64 == 0 {
			pending = append(pending, row.GroupKey)
		}
	}

	total := len(pending)
	if total == 0 {
		return
	}

	s.logger.Info("autotag prefetch: starting", "groups", total, "library_id", libraryID)

	for i, key := range pending {
		if ctx.Err() != nil {
			s.logger.Info("autotag prefetch: cancelled", "processed", i, "total", total)

			return
		}

		// Yield the MB rate limiter to any in-flight foreground
		// GetCandidates call so the user's click doesn't queue
		// behind the prefetch sweep.  Returns false on context
		// cancel.
		if !s.waitForForegroundIdle(ctx) {
			s.logger.Info(
				"autotag prefetch: cancelled while yielding",
				"processed",
				i,
				"total",
				total,
			)

			return
		}

		// While we were yielding, the user may have opened this
		// folder, which populates the cache via the foreground
		// GetCandidates path.  Skip the redundant re-score.
		if cached := s.lookupCachedCandidates(key); len(cached) > 0 {
			s.emitEvent(events.AutotagPrefetchProgress, map[string]any{
				"processed": i + 1,
				"total":     total,
			})

			continue
		}

		// A folder that looks like a pile of unrelated tracks gets
		// torn apart before scoring — otherwise the scorer treats
		// the whole pile as one album candidate and every track that
		// doesn't fit the best partial match gets counted as an
		// "extra" of it, rather than being matched on its own.  The
		// original group key is gone once every track has moved to a
		// synthetic child, so score those instead of key.
		if newKeys := s.autoSplitMixedBag(ctx, key); len(newKeys) > 0 {
			for _, nk := range newKeys {
				s.scoreAndPersist(ctx, nk, "prefetch: score synthetic group")
			}

			s.emitEvent(events.AutotagPrefetchProgress, map[string]any{
				"processed": i + 1,
				"total":     total,
			})

			continue
		}

		// Local-first: the background sweep skips the MusicBrainz
		// cascade when a local candidate already scores well, so a
		// library with cross-library duplicates costs no network here.
		s.scoreAndPersist(ctx, key, "prefetch: score failed — skipping")

		s.emitEvent(events.AutotagPrefetchProgress, map[string]any{
			"processed": i + 1,
			"total":     total,
		})
	}

	s.emitEvent(events.AutotagPrefetchFinished, map[string]any{
		"processed": total,
		"total":     total,
	})
	s.logger.Info("autotag prefetch: done", "groups", total)
}

// scoreAndPersist runs the cheap local-first score for one group and
// caches + persists the result, logging (never failing the caller)
// on error.  failMsg labels the debug log line when scoring itself
// errors.
func (s *Service) scoreAndPersist(ctx context.Context, groupKey, failMsg string) {
	score, err := s.scorer.ScoreGroupLocalFirst(ctx, groupKey)
	if err != nil {
		s.logger.Debug(failMsg, "group_key", groupKey, "err", err)

		return
	}

	s.cacheCandidates(groupKey, score.Candidates)

	if perr := s.scorer.PersistScore(ctx, score); perr != nil {
		s.logger.Debug(
			"prefetch: persist failed",
			"group_key", groupKey, "err", perr,
		)
	}
}

// autoSplitMixedBag detects a folder that looks like a pile of
// unrelated tracks (autotag.IsMixedBag) and, if so, tears it apart
// via the same clustering SplitMixedFolder uses (autotag.SplitPlan)
// before the background sweep scores it — otherwise the scorer
// treats the whole pile as one album candidate and every track that
// doesn't fit the best partial match gets counted as an "extra" of
// it.  Returns the new synthetic group keys, or nil when the folder
// isn't a mixed bag (or had nothing to split).
func (s *Service) autoSplitMixedBag(ctx context.Context, groupKey string) []string {
	item, err := s.db.Queries.GetTaggingItem(ctx, groupKey)
	if err != nil {
		return nil
	}

	locals, err := s.scorer.LocalTracksForGroup(ctx, groupKey)
	if err != nil {
		s.logger.Debug("prefetch: auto-split load locals failed", "group_key", groupKey, "err", err)

		return nil
	}

	g := autotag.Group{AlbumName: item.AlbumName, AlbumArtist: item.AlbumArtist, Tracks: locals}
	if !autotag.IsMixedBag(g) {
		return nil
	}

	clusters := autotag.SplitPlan(locals)
	if len(clusters) <= 1 {
		return nil
	}

	newKeys, err := s.splitIntoSyntheticGroups(groupKey, item.LibraryID, clusters)
	if err != nil {
		s.logger.Warn("prefetch: auto-split failed", "group_key", groupKey, "err", err)

		return nil
	}

	s.logger.Info(
		"autotag prefetch: auto-split mixed-bag folder",
		"group_key", groupKey, "into", len(newKeys),
	)

	return newKeys
}

// PendingItem is a projection of tagging_items that's safe to hand
// to the frontend.  Score is dereferenced to 0 when NULL so TS sees
// a plain number.
type PendingItem struct {
	GroupKey    string `json:"groupKey"`
	LibraryID   int64  `json:"libraryId"`
	LibraryName string `json:"libraryName"`
	// FolderSubPath is the album folder's path relative to its
	// library root (e.g. "Beatles/Abbey Road").  Empty when the
	// path can't be derived (no audio files yet, missing library
	// row, etc.).  Populated by ListPendingFolders and
	// GetPendingFolder; not present on rows returned by other
	// list/get queries that haven't been extended.
	FolderSubPath        string  `json:"folderSubPath"`
	TrackCount           int64   `json:"trackCount"`
	AlbumName            string  `json:"albumName"`
	AlbumArtist          string  `json:"albumArtist"`
	DiscNumber           int64   `json:"discNumber"`
	BestMatchReleaseMbid string  `json:"bestMatchReleaseMbid"`
	Score                float64 `json:"score"`
	Status               string  `json:"status"`
	// Synthetic marks a group SplitMixedFolder carved out of a
	// bigger folder by matching tags rather than a directory — the
	// review UI labels these distinctly since several may share the
	// same FolderSubPath.
	Synthetic bool `json:"synthetic"`
	// LikelyMixedBag is a cheap SQL-side approximation of autotag.
	// IsMixedBag, computed for the whole library in one pass by
	// ListPendingFolders (see ListLikelyMixedBagGroupKeys) rather
	// than hydrating every group's tracks in Go.  It's a badge hint,
	// not a guarantee — ScoreView.MixedBag (computed from the real
	// track list when a folder is opened) is the authoritative check
	// that gates the SplitMixedFolder action itself.
	LikelyMixedBag bool `json:"likelyMixedBag"`
}

// GetNextPending returns the next pending tagging item after the
// internal cursor, or nil when the queue is empty.  Advances the
// cursor on success.
func (s *Service) GetNextPending() (*PendingItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row, err := s.db.Queries.GetNextPendingTaggingItem(
		s.ctx,
		sqlcgen.GetNextPendingTaggingItemParams{
			LibraryID:     s.libraryID,
			AfterGroupKey: s.cursor,
		},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || isNoRows(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("next pending: %w", err)
	}

	s.cursor = row.GroupKey

	out := &PendingItem{
		GroupKey:    row.GroupKey,
		LibraryID:   row.LibraryID,
		LibraryName: row.LibraryName,
		TrackCount:  row.TrackCount,
		AlbumName:   row.AlbumName,
		AlbumArtist: row.AlbumArtist,
		DiscNumber:  row.DiscNumber,
		Status:      row.Status,
	}
	if row.BestMatchReleaseMbid.Valid {
		out.BestMatchReleaseMbid = row.BestMatchReleaseMbid.String
	}

	if row.Score.Valid {
		out.Score = row.Score.Float64
	}

	return out, nil
}

// ListPendingFolders returns every tagging item in the given
// library (or all libraries when libraryID = 0) that has not been
// explicitly cleared.  Includes items in all review states —
// pending, skipped, and confirmed — so the sidebar can group
// them into sections (the user wanted skipped/completed to stay
// visible at the bottom rather than vanish).  Sorted by score
// descending so the UI surfaces the most confident matches first
// (NULL scores fall to the bottom); the frontend re-groups by
// status for display.
func (s *Service) ListPendingFolders(libraryID int64) ([]PendingItem, error) {
	const maxFolders = 5000

	// Self-heal before listing: a row whose bookkeeping (scan orphan
	// cleanup, maybeRebindTaggingGroup, SplitMixedFolder) never ran
	// or drifted otherwise lingers here indefinitely, showing as an
	// "old/nonexistent" entry with no folder path. Best-effort — a
	// failed prune shouldn't block the list itself.
	if err := s.db.Queries.PruneOrphanedTaggingItems(s.ctx); err != nil {
		s.logger.Warn("list pending folders: prune orphaned items failed", "err", err)
	}

	rows, err := s.db.Queries.ListPendingTaggingItemsByScore(
		s.ctx,
		sqlcgen.ListPendingTaggingItemsByScoreParams{
			LibraryID:    libraryID,
			StatusFilter: "all",
			RowOffset:    0,
			RowLimit:     maxFolders,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list pending folders: %w", err)
	}

	mixedBagKeys, err := s.db.Queries.ListLikelyMixedBagGroupKeys(s.ctx)
	if err != nil {
		// A cheap badge hint isn't worth failing the whole list for.
		s.logger.Warn("list pending folders: mixed-bag triage failed", "err", err)
	}

	mixedBag := make(map[string]bool, len(mixedBagKeys))
	for _, k := range mixedBagKeys {
		mixedBag[k] = true
	}

	out := make([]PendingItem, 0, len(rows))

	for _, row := range rows {
		item := PendingItem{
			GroupKey:       row.GroupKey,
			LibraryID:      row.LibraryID,
			LibraryName:    row.LibraryName,
			FolderSubPath:  folderSubPath(row.LibraryPath, row.SampleFilePath),
			TrackCount:     row.TrackCount,
			AlbumName:      row.AlbumName,
			AlbumArtist:    row.AlbumArtist,
			DiscNumber:     row.DiscNumber,
			Status:         row.Status,
			Synthetic:      row.Synthetic != 0,
			LikelyMixedBag: mixedBag[row.GroupKey],
		}

		if row.BestMatchReleaseMbid.Valid {
			item.BestMatchReleaseMbid = row.BestMatchReleaseMbid.String
		}

		if row.Score.Valid {
			item.Score = row.Score.Float64
		}

		out = append(out, item)
	}

	return out, nil
}

// GetPendingFolder returns the tagging item for a specific group
// key — used by the sidebar after the user clicks an entry, so
// the header can render even when the cursor-based queue iteration
// hasn't visited that key yet.
func (s *Service) GetPendingFolder(groupKey string) (*PendingItem, error) {
	row, err := s.db.Queries.GetPendingFolderDetail(s.ctx, groupKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || isNoRows(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("get tagging item: %w", err)
	}

	out := &PendingItem{
		GroupKey:      row.GroupKey,
		LibraryID:     row.LibraryID,
		LibraryName:   row.LibraryName,
		FolderSubPath: folderSubPath(row.LibraryPath, row.SampleFilePath),
		TrackCount:    row.TrackCount,
		AlbumName:     row.AlbumName,
		AlbumArtist:   row.AlbumArtist,
		DiscNumber:    row.DiscNumber,
		Status:        row.Status,
		Synthetic:     row.Synthetic != 0,
	}

	if row.BestMatchReleaseMbid.Valid {
		out.BestMatchReleaseMbid = row.BestMatchReleaseMbid.String
	}

	if row.Score.Valid {
		out.Score = row.Score.Float64
	}

	return out, nil
}

// GetCandidateCoverArt is the lazy-load entrypoint for cover art
// when the user picks a non-top candidate.  Goes through the
// CAA-only chain (disk cache → network) — never the local
// library-by-name index, since that index conflates embedded ID3
// art with externally-fetched art and the embedded path produces
// stale or wrong-version covers for autotag review.
func (s *Service) GetCandidateCoverArt(
	releaseMBID, releaseGroupMBID string,
) string {
	if s.exp == nil {
		return ""
	}

	return s.exp.GetCandidateThumbnail(releaseMBID, releaseGroupMBID)
}

// GetLocalCoverArt returns a data-URI for the artwork associated
// with the group's local files, so the review UI can show "what the
// folder already looks like" beside the fetched candidate art.
// It first checks each file for embedded ID3/Vorbis pictures, then
// falls back to a sidecar cover image (cover.jpg, folder.png, …)
// sitting in the album directory.  Untagged folders carry one or the
// other far more often than they have a DB release-group cover, so we
// read the files directly.  Returns "" when nothing is found.
func (s *Service) GetLocalCoverArt(groupKey string) string {
	locals, err := s.scorer.LocalTracksForGroup(s.ctx, groupKey)
	if err != nil {
		s.logger.Warn(
			"local cover art: list tracks failed",
			"group_key", groupKey, "err", err,
		)

		return ""
	}

	// 1. Embedded artwork — guaranteed to travel with the track.
	for _, t := range locals {
		if t.FilePath == "" {
			continue
		}

		meta, err := metadata.ExtractTags(t.FilePath)
		if err != nil || meta == nil || meta.Picture == nil {
			continue
		}

		if len(meta.Picture.Data) == 0 {
			continue
		}

		mime := meta.Picture.MIMEType
		if mime == "" {
			mime = "image/jpeg"
		}

		return "data:" + mime + ";base64," +
			base64.StdEncoding.EncodeToString(meta.Picture.Data)
	}

	// 2. Sidecar cover image file in the album directory.  Scan the
	// distinct directories the tracks live in (usually just one).
	seen := make(map[string]struct{}, 1)

	for _, t := range locals {
		if t.FilePath == "" {
			continue
		}

		dir := filepath.Dir(t.FilePath)
		if _, ok := seen[dir]; ok {
			continue
		}

		seen[dir] = struct{}{}

		if uri := folderCoverDataURI(dir); uri != "" {
			return uri
		}
	}

	return ""
}

// coverImageExts maps recognised sidecar cover-image extensions to
// their MIME type for the data-URI.
var coverImageExts = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
}

// preferredCoverStems lists the base filenames (lower-cased, minus
// extension) commonly used for album artwork, in priority order.
var preferredCoverStems = []string{
	"cover", "folder", "front", "album", "albumart", "albumartsmall", "thumb",
}

// folderCoverDataURI looks for a conventional cover-image file in dir
// and returns it as a base64 data-URI, or "" when none is found.
// Preferred filenames (cover.*, folder.*, …) win; any other image
// file in the directory is used as a last resort.  Matching is
// case-insensitive so COVER.JPG and Folder.Png both hit.
func folderCoverDataURI(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	byStem := make(map[string]string) // lower stem -> filename

	var fallback string

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))

		if _, ok := coverImageExts[ext]; !ok {
			continue
		}

		stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		if _, dup := byStem[stem]; !dup {
			byStem[stem] = name
		}

		if fallback == "" {
			fallback = name
		}
	}

	name := fallback

	for _, stem := range preferredCoverStems {
		if match, ok := byStem[stem]; ok {
			name = match

			break
		}
	}

	if name == "" {
		return ""
	}

	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil || len(data) == 0 {
		return ""
	}

	mime := coverImageExts[strings.ToLower(filepath.Ext(name))]
	if mime == "" {
		mime = "image/jpeg"
	}

	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// ScoreView is the Wails-friendly projection of autotag.GroupScore.
type ScoreView struct {
	GroupKey    string           `json:"groupKey"`
	LocalTracks []LocalTrackView `json:"localTracks"`
	Candidates  []CandidateView  `json:"candidates"`
	// Recommendation is the qualitative confidence tier for the
	// ranked list: "none", "low", "medium", or "strong".  Unlike the
	// raw score it accounts for ambiguity (a rival release group
	// scoring nearly as high) and alignment defects.
	Recommendation string `json:"recommendation"`
	// MixedBag is true when this group's tracks look like an
	// unrelated pile rather than one release (autotag.IsMixedBag) —
	// the review UI offers SplitMixedFolder when set.  Always false
	// for a group that's already Synthetic; a split group doesn't
	// get split again.
	MixedBag bool `json:"mixedBag"`
	// Synthetic mirrors PendingItem.Synthetic for the currently
	// open group.
	Synthetic bool `json:"synthetic"`
}

// LocalTrackView mirrors autotag.LocalTrack.
type LocalTrackView struct {
	AudioFileID   int64  `json:"audioFileId"`
	FilePath      string `json:"filePath"`
	Title         string `json:"title"`
	Artist        string `json:"artist"`
	TrackNumber   int    `json:"trackNumber"`
	DiscNumber    int    `json:"discNumber"`
	LengthMillis  int64  `json:"lengthMillis"`
	RecordingMBID string `json:"recordingMbid"`
}

// CandidateView mirrors autotag.Candidate + its alignments.  The
// CoverArtURL is populated eagerly only for the top-ranked
// candidate (eager network fetch); the frontend calls
// GetCandidateCoverArt to lazy-load art for the others when the
// user selects them.  Date is this release's own date (re-issue
// year for remasters); OriginalDate is the release-group's
// first-release-date (the original album year).
type CandidateView struct {
	ReleaseMBID      string             `json:"releaseMbid"`
	ReleaseGroupMBID string             `json:"releaseGroupMbid"`
	Title            string             `json:"title"`
	ArtistCredit     string             `json:"artistCredit"`
	Date             string             `json:"date"`
	OriginalDate     string             `json:"originalDate"`
	Country          string             `json:"country"`
	Status           string             `json:"status"`
	PrimaryType      string             `json:"primaryType"`
	TrackCount       int                `json:"trackCount"`
	Score            float64            `json:"score"`
	Breakdown        ScoreBreakdownView `json:"breakdown"`
	Source           string             `json:"source"`
	Provenance       string             `json:"provenance"`
	CoverArtURL      string             `json:"coverArtUrl"`
	Alignments       []AlignmentView    `json:"alignments"`
}

// ScoreBreakdownView mirrors autotag.ScoreBreakdown for display.
// Each field is in [0, 1].
type ScoreBreakdownView struct {
	TitleAvg      float64 `json:"titleAvg"`
	LengthAvg     float64 `json:"lengthAvg"`
	ArtistFit     float64 `json:"artistFit"`
	AlbumFit      float64 `json:"albumFit"`
	TrackCountFit float64 `json:"trackCountFit"`
	ReleaseMeta   float64 `json:"releaseMeta"`
	Evidence      float64 `json:"evidence"`
}

// AlignmentView mirrors autotag.TrackAlignment.  LocalIndex of -1
// means "candidate has this track, folder doesn't" (status=missing).
type AlignmentView struct {
	LocalIndex          int     `json:"localIndex"`
	LocalTitle          string  `json:"localTitle"`
	LocalLengthMillis   int64   `json:"localLengthMillis"`
	CandidatePosition   int     `json:"candidatePosition"`
	CandidateDiscNumber int     `json:"candidateDiscNumber"`
	CandidateTitle      string  `json:"candidateTitle"`
	CandidateMBID       string  `json:"candidateMbid"`
	CandidateLength     int64   `json:"candidateLength"`
	TitleScore          float64 `json:"titleScore"`
	LengthDeltaMs       int64   `json:"lengthDeltaMs"`
	TrackNumberOK       bool    `json:"trackNumberOk"`
	Status              string  `json:"status"`
}

// GetCandidates runs the scorer for a group and returns the full
// candidate list (ordered best-first) along with the local tracks.
// Caches the scored candidate list so Apply can operate on the
// exact release the user saw — even if a concurrent rescore would
// have shuffled the ranking.  Also writes the top candidate's
// score back to the tagging_items row so the sidebar's match-%
// pill and the score-desc list sort reflect the freshly-computed
// ranking instead of the scanner's pre-review estimate.
func (s *Service) GetCandidates(groupKey string) (*ScoreView, error) {
	// Cache hit — usually populated by the prefetch worker.  Skip
	// the re-score (and the MB rate-limiter wait it implies) and
	// reconstruct the same view shape we'd compute fresh.  Loads
	// the local-track list from the DB which is a single fast
	// query, no MB calls.
	if cached := s.lookupCachedCandidates(groupKey); len(cached) > 0 {
		locals, err := s.scorer.LocalTracksForGroup(s.ctx, groupKey)
		if err != nil {
			return nil, fmt.Errorf("load locals: %w", err)
		}

		return scoreToView(&autotag.GroupScore{
			GroupKey:    groupKey,
			LocalTracks: locals,
			Candidates:  cached,
		}, s.exp), nil
	}

	// Cache miss — re-score now.  Mark this call as foreground so
	// the prefetch worker yields the rate limiter, and bound the
	// total time so a hung MB query can't lock the UI forever.
	s.markForegroundBusy()
	defer s.markForegroundIdle()

	ctx, cancel := context.WithTimeout(s.ctx, getCandidatesTimeout)
	defer cancel()

	score, err := s.scorer.ScoreGroup(ctx, groupKey)
	if err != nil {
		return nil, fmt.Errorf("score group: %w", err)
	}

	s.cacheCandidates(groupKey, score.Candidates)

	// Persist the score (not the status) so the pending folder
	// stays in the review queue.  A previous version of this
	// path called PersistBest, which flipped status='matched' and
	// dropped the row out from under the user — see the
	// "conflating folders" bug fix.
	if err := s.scorer.PersistScore(s.ctx, score); err != nil {
		s.logger.Warn(
			"persist score failed — pill / sort will reflect stale score",
			"group_key", groupKey, "err", err,
		)
	}

	return scoreToView(score, s.exp), nil
}

// cacheCandidates durably persists the scored candidate list for a
// group as a JSON blob in tagging_candidates.  This freezes the exact
// list the user saw (so Apply operates on the release they selected
// even if a racing rescore would reorder it) AND survives process
// restarts, so a later session reuses the result instead of re-hitting
// MusicBrainz.  Errors are logged, not returned — a failed persist
// only costs a recompute, never correctness.
func (s *Service) cacheCandidates(groupKey string, cands []autotag.Candidate) {
	blob, err := json.Marshal(cands)
	if err != nil {
		s.logger.Warn("cache candidates: marshal failed", "group_key", groupKey, "err", err)

		return
	}

	if err := s.db.Queries.UpsertTaggingCandidates(s.ctx, sqlcgen.UpsertTaggingCandidatesParams{
		GroupKey:   groupKey,
		Candidates: string(blob),
	}); err != nil {
		s.logger.Warn("cache candidates: persist failed", "group_key", groupKey, "err", err)
	}
}

// lookupCachedCandidates returns the durably-stored candidate list for
// a group, or nil when none has been computed yet (or the blob can't
// be decoded — treated as a miss so the caller recomputes).
func (s *Service) lookupCachedCandidates(groupKey string) []autotag.Candidate {
	blob, err := s.db.Queries.GetTaggingCandidates(s.ctx, groupKey)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) && !isNoRows(err) {
			s.logger.Warn("lookup candidates: query failed", "group_key", groupKey, "err", err)
		}

		return nil
	}

	var cands []autotag.Candidate
	if err := json.Unmarshal([]byte(blob), &cands); err != nil {
		s.logger.Warn("lookup candidates: unmarshal failed", "group_key", groupKey, "err", err)

		return nil
	}

	return cands
}

// ApplyResultView mirrors autotag.ApplyResult for the frontend.
type ApplyResultView struct {
	GroupKey  string        `json:"groupKey"`
	Succeeded int           `json:"succeeded"`
	Failed    int           `json:"failed"`
	Failures  []FailureView `json:"failures"`
}

// FailureView carries one failed track for the frontend's toast.
type FailureView struct {
	FilePath string `json:"filePath"`
	Error    string `json:"error"`
}

// Apply runs the apply pipeline for the given group + chosen
// release MBID.  Uses the cached candidate list from the last
// GetCandidates / GetCandidatesForPasteURL call so the user
// always applies the exact release they saw in the UI.
// Empty releaseMBID picks the top-scored candidate.
func (s *Service) Apply(groupKey, releaseMBID string) (*ApplyResultView, error) {
	plan, err := s.prepareApplyPlan(groupKey, releaseMBID)
	if err != nil {
		return nil, err
	}

	result, err := s.applier.Apply(s.ctx, plan, nil)
	if err != nil {
		return nil, fmt.Errorf("apply: %w", err)
	}

	out := &ApplyResultView{
		GroupKey:  result.GroupKey,
		Succeeded: result.Succeeded,
		Failed:    result.Failed,
	}

	for _, f := range result.Failures {
		out.Failures = append(out.Failures, FailureView{
			FilePath: f.FilePath, Error: f.Error,
		})
	}

	return out, nil
}

// ApplyAsync dispatches an Apply for the given group in a
// background goroutine and returns immediately.  Progress is
// reported to the frontend via three Wails events:
//
//   - AutotagApplyStarted   {groupKey, total}
//   - AutotagApplyProgress  {groupKey, current, total, succeeded, failed}
//   - AutotagApplyFinished  {groupKey, succeeded, failed, error}
//
// The candidate list and locals are snapshot synchronously before
// returning so a concurrent GetCandidates can't shuffle the
// selected release out from under the running job.  Returns
// ErrApplyInFlight when a job for the same groupKey is already
// running; otherwise nil.  Job-level errors land in the Finished
// event payload, not the return value.
func (s *Service) ApplyAsync(groupKey, releaseMBID string) error {
	if !s.tryStartApply(groupKey) {
		return ErrApplyInFlight
	}

	// Synchronous prep: snapshot cache, load locals, build plan.
	// Any error here surfaces to the caller and we release the
	// in-flight slot — the goroutine never starts.
	plan, err := s.prepareApplyPlan(groupKey, releaseMBID)
	if err != nil {
		s.endApply(groupKey)

		return err
	}

	total := 0

	for _, tr := range plan.Tracks {
		if tr.Aligned {
			total++
		}
	}

	s.emitEvent(events.AutotagApplyStarted, map[string]any{
		"groupKey": groupKey,
		"total":    total,
	})

	handle, ctx, cancel := s.startApplyJob(groupKey, total)

	go s.runApply(ctx, cancel, handle, groupKey, plan, total)

	return nil
}

// prepareApplyPlan rebuilds the data Apply needs without doing any
// disk or network writes — same shape as Apply()'s opening, lifted
// out so ApplyAsync can call it on the caller's goroutine.
func (s *Service) prepareApplyPlan(
	groupKey, releaseMBID string,
) (*autotag.ApplyPlan, error) {
	cands := s.lookupCachedCandidates(groupKey)
	if len(cands) == 0 {
		score, err := s.scorer.ScoreGroup(s.ctx, groupKey)
		if err != nil {
			return nil, fmt.Errorf("score before apply: %w", err)
		}

		cands = score.Candidates
		s.cacheCandidates(groupKey, cands)
	}

	locals, err := s.scorer.LocalTracksForGroup(s.ctx, groupKey)
	if err != nil {
		return nil, fmt.Errorf("load locals: %w", err)
	}

	groupScore := &autotag.GroupScore{
		GroupKey:    groupKey,
		LocalTracks: locals,
		Candidates:  cands,
	}

	plan, err := s.applier.BuildPlan(groupScore, releaseMBID)
	if err != nil {
		return nil, fmt.Errorf("build plan: %w", err)
	}

	return plan, nil
}

// runApply executes the plan in the background and emits progress
// + completion events.  Always releases the in-flight slot when
// it returns, even on panic.
//
// The job handle is the same progress and cancel surface every other
// long-running operation has; the events stay because the autotag page
// drives its per-folder row from them.
func (s *Service) runApply(
	ctx context.Context,
	cancel context.CancelFunc,
	handle *jobs.Handle,
	groupKey string,
	plan *autotag.ApplyPlan,
	total int,
) {
	defer s.endApply(groupKey)
	defer cancel()

	onProgress := func(current, total, succeeded, failed int) {
		if handle != nil {
			handle.SetProgress(int64(current), int64(total))
		}

		s.emitEvent(events.AutotagApplyProgress, map[string]any{
			"groupKey":  groupKey,
			"current":   current,
			"total":     total,
			"succeeded": succeeded,
			"failed":    failed,
		})
	}

	result, err := s.applier.Apply(ctx, plan, onProgress)

	finished := map[string]any{
		"groupKey":  groupKey,
		"total":     total,
		"succeeded": 0,
		"failed":    0,
		"error":     "",
	}
	if result != nil {
		finished["succeeded"] = result.Succeeded
		finished["failed"] = result.Failed
	}

	if err != nil {
		finished["error"] = err.Error()
	}

	s.finishApplyJob(ctx, handle, result, err)

	s.emitEvent(events.AutotagApplyFinished, finished)
}

// finishApplyJob closes the job out in the state the run ended in, so
// a cancelled apply reads as cancelled rather than as a failure and a
// partial write says how far it got.
func (s *Service) finishApplyJob(
	ctx context.Context,
	handle *jobs.Handle,
	result *autotag.ApplyResult,
	err error,
) {
	if handle == nil {
		return
	}

	switch {
	case ctx.Err() != nil:
		handle.Cancelled()
	case err != nil:
		handle.Fail(err)
	case result != nil && result.Failed > 0:
		handle.Logf(jobs.LevelWarn, fmt.Sprintf(
			"%d of %d tracks could not be written",
			result.Failed, result.Succeeded+result.Failed,
		))
		handle.Complete()
	default:
		handle.Complete()
	}
}

// tryStartApply records that an Apply for the given group is
// running.  Returns false when a previous job for the same key
// hasn't finished yet — caller should treat that as
// ErrApplyInFlight.
func (s *Service) tryStartApply(groupKey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.runningApplies[groupKey]; exists {
		return false
	}

	s.runningApplies[groupKey] = struct{}{}

	return true
}

// endApply releases the in-flight slot for the given group.
func (s *Service) endApply(groupKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.runningApplies, groupKey)
}

// Skip marks the current group as skipped — it stays in the
// queue but renders in the "Skipped" section at the bottom of
// the sidebar, separate from pending items, so the user can
// revisit it later if they change their mind.
func (s *Service) Skip(groupKey string) error {
	return s.db.Queries.SetTaggingItemStatus(
		s.ctx,
		sqlcgen.SetTaggingItemStatusParams{Status: "skipped", GroupKey: groupKey},
	)
}

// ClearCompletedEntries marks every confirmed item in the given
// library (or all libraries when libraryID = 0) as cleared.  The
// rows stay in the table — so a re-scan of the same folder
// won't bounce them back into 'pending' — but they no longer
// appear in the review queue.  Returns the number of rows
// affected so the frontend can show a quick toast.
func (s *Service) ClearCompletedEntries(libraryID int64) error {
	return s.db.Queries.ClearCompletedTaggingItems(s.ctx, libraryID)
}

// LeaveAsIs is the "local tags are correct, don't rescore"
// acknowledgement — bumps status to 'confirmed' without touching
// any files.
func (s *Service) LeaveAsIs(groupKey string) error {
	return s.db.Queries.SetTaggingItemStatus(
		s.ctx,
		sqlcgen.SetTaggingItemStatusParams{Status: "confirmed", GroupKey: groupKey},
	)
}

// RetagGroup flips a group back to 'pending' so the user can
// re-review after an apply or skip.  Drops the durably-cached
// candidate list too, so the next open recomputes against fresh
// MusicBrainz data rather than reusing the stale stored result.
func (s *Service) RetagGroup(groupKey string) error {
	if err := s.db.Queries.DeleteTaggingCandidates(s.ctx, groupKey); err != nil {
		s.logger.Warn("retag: clear cached candidates failed", "group_key", groupKey, "err", err)
	}

	return s.db.Queries.SetTaggingItemStatus(
		s.ctx,
		sqlcgen.SetTaggingItemStatusParams{Status: "pending", GroupKey: groupKey},
	)
}

// errNothingToSplit is returned by SplitMixedFolder when the
// folder's tracks carry no repeated (album, album-artist) tag pair
// to cluster on — nothing to split out.
var errNothingToSplit = errors.New("autotag: no tag-matched sub-albums to split out")

// SplitMixedFolder is the "this folder is a pile of unrelated
// tracks" escape hatch: it partitions the group's local tracks via
// autotag.SplitPlan — tag-matched sub-albums (see
// autotag.ClusterByAlbumArtist) plus a one-track cluster for every
// track that didn't share an (album, album-artist) pair with
// anything else — and carves each piece out into its own synthetic
// tagging group, reassigning just those audio_files rows (no files
// move on disk).  Every track leaves the original group; nothing is
// left behind to be scored as "extra tracks" of whichever piece
// happens to match first.  The synthetic groups are scored with
// relaxed missing-track handling (rank.go, recommend.go), since
// they're expected to be an incomplete subset of whatever release
// they belong to.
//
// Returns the resulting PendingItems — the leftover original group
// first (if anything remains in it), then the new synthetic groups
// — so the frontend can splice them into the sidebar without a full
// reload.  Errors with errNothingToSplit when the folder is already
// one coherent unit (SplitPlan produces a single cluster covering
// every track); callers should treat that as "nothing to show", not
// a failure.
func (s *Service) SplitMixedFolder(groupKey string) ([]PendingItem, error) {
	item, err := s.db.Queries.GetTaggingItem(s.ctx, groupKey)
	if err != nil {
		return nil, fmt.Errorf("get tagging item: %w", err)
	}

	locals, err := s.scorer.LocalTracksForGroup(s.ctx, groupKey)
	if err != nil {
		return nil, fmt.Errorf("load locals: %w", err)
	}

	clusters := autotag.SplitPlan(locals)
	if len(clusters) <= 1 {
		return nil, errNothingToSplit
	}

	newKeys, err := s.splitIntoSyntheticGroups(groupKey, item.LibraryID, clusters)
	if err != nil {
		return nil, err
	}

	out := make([]PendingItem, 0, len(newKeys)+1)

	if leftover, err := s.GetPendingFolder(groupKey); err != nil {
		s.logger.Warn("split: reload leftover parent", "group_key", groupKey, "err", err)
	} else if leftover != nil {
		out = append(out, *leftover)
	}

	for _, k := range newKeys {
		child, err := s.GetPendingFolder(k)
		if err != nil || child == nil {
			s.logger.Warn("split: reload synthetic group", "group_key", k, "err", err)

			continue
		}

		out = append(out, *child)
	}

	return out, nil
}

// splitIntoSyntheticGroups performs the actual DB migration inside a
// single transaction: each cluster's tracks are reassigned onto a
// deterministic synthetic group key, the parent's track count is
// decremented per track moved, and the parent row is dropped if it
// ends up empty.  Returns the new group keys in cluster order.
func (s *Service) splitIntoSyntheticGroups(
	parentKey string, libraryID int64, clusters []autotag.TrackCluster,
) ([]string, error) {
	tx, err := s.db.BeginTx()
	if err != nil {
		return nil, fmt.Errorf("begin split tx: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	q := s.db.Queries.WithTx(tx)
	newKeys := make([]string, 0, len(clusters))

	for _, c := range clusters {
		var newKey string
		if len(c.Tracks) == 1 {
			// A lone leftover track from SplitPlan's singleton
			// fallback may carry an empty (or shared-but-coincidental)
			// album/album-artist tag — key on the track itself so two
			// untagged leftovers can't collide.
			newKey = autotag.SyntheticTrackGroupKey(parentKey, c.Tracks[0].AudioFileID)
		} else {
			newKey = autotag.SyntheticGroupKey(parentKey, c.AlbumName, c.AlbumArtist)
		}

		newKeys = append(newKeys, newKey)

		for _, t := range c.Tracks {
			if err := q.DecrementTaggingItemTrackCount(s.ctx, parentKey); err != nil {
				return nil, fmt.Errorf("decrement parent group: %w", err)
			}

			upsertParams := sqlcgen.UpsertTaggingItemOnTrackAddParams{
				GroupKey:    newKey,
				LibraryID:   libraryID,
				AlbumName:   c.AlbumName,
				AlbumArtist: c.AlbumArtist,
				DiscNumber:  0,
			}
			if err := q.UpsertTaggingItemOnTrackAdd(s.ctx, upsertParams); err != nil {
				return nil, fmt.Errorf("upsert synthetic group: %w", err)
			}

			if err := q.SetAudioFileGroupKey(s.ctx, sqlcgen.SetAudioFileGroupKeyParams{
				GroupKey: newKey,
				ID:       t.AudioFileID,
			}); err != nil {
				return nil, fmt.Errorf("reassign track %d: %w", t.AudioFileID, err)
			}
		}

		if err := q.MarkTaggingItemSynthetic(s.ctx, sqlcgen.MarkTaggingItemSyntheticParams{
			ParentGroupKey: parentKey,
			GroupKey:       newKey,
		}); err != nil {
			return nil, fmt.Errorf("mark synthetic: %w", err)
		}
	}

	if err := q.DeleteTaggingItemIfEmpty(s.ctx, parentKey); err != nil {
		return nil, fmt.Errorf("cleanup leftover parent: %w", err)
	}

	// The parent's cached candidates (if it still exists) no longer
	// reflect its track set now that some tracks moved out.
	if err := q.DeleteTaggingCandidates(s.ctx, parentKey); err != nil {
		s.logger.Warn("split: drop stale parent candidates", "group_key", parentKey, "err", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit split: %w", err)
	}

	return newKeys, nil
}

// AckLibraryWarning records that the user has seen the first-
// time-apply irreversibility warning for this library.
func (s *Service) AckLibraryWarning(libraryID int64) error {
	return s.db.Queries.AckLibraryAutotagWarning(s.ctx, libraryID)
}

// GetCandidatesForPasteURL resolves a MusicBrainz release-or-
// release-group URL into a ScoreView by fetching the pasted entity
// from MB, running track alignment against the local group, and
// prepending a fully-scored candidate to the existing list.  The
// pasted candidate's provenance is "paste" so the UI can label it.
func (s *Service) GetCandidatesForPasteURL(
	groupKey, mbReleaseURL string,
) (*ScoreView, error) {
	mbid := extractReleaseMBID(mbReleaseURL)
	if mbid == "" {
		return nil, errInvalidMBURL
	}

	score, err := s.scorer.ScoreGroup(s.ctx, groupKey)
	if err != nil {
		return nil, fmt.Errorf("score group: %w", err)
	}

	pasted, err := s.mbr.ResolveOneReleaseMBID(s.ctx, mbid)
	if err != nil {
		return nil, fmt.Errorf("fetch pasted MBID: %w", err)
	}

	// Score the pasted candidate against the local tracks and
	// splice it to the top of the list.  RankCandidates would
	// re-sort by score; we explicitly keep the pasted candidate
	// first so the user's manual pick is visually anchored.
	scored := autotag.ScoreCandidate(autotag.Group{
		AlbumName:   score.AlbumName,
		AlbumArtist: score.AlbumArtist,
		Tracks:      score.LocalTracks,
		Synthetic:   score.Synthetic,
	}, pasted)
	merged := append([]autotag.Candidate{scored}, score.Candidates...)
	score.Candidates = merged

	s.cacheCandidates(groupKey, merged)

	return scoreToView(score, s.exp), nil
}

// SearchHitView is one in-app MusicBrainz search result surfaced to
// the review UI.  Kind is "releasegroup" or "recording" so the
// frontend knows which resolve path SelectSearchCandidate must take.
type SearchHitView struct {
	MBID   string `json:"mbid"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Detail string `json:"detail"`
}

// SearchCandidates runs an in-app MusicBrainz search — the "suggest a
// new candidate" escape hatch when the automatic cascade misses.
// kind is "recording" (title + artist, for singletons) or anything
// else (album + artist release-group search).  Returns lightweight
// hits; SelectSearchCandidate resolves the picked one into a scored
// candidate.
func (s *Service) SearchCandidates(
	kind, query, artist string,
) ([]SearchHitView, error) {
	if kind == "recording" {
		hits, err := s.mbr.SearchRecordingHits(s.ctx, query, artist)
		if err != nil {
			return nil, fmt.Errorf("search recordings: %w", err)
		}

		out := make([]SearchHitView, 0, len(hits))
		for _, h := range hits {
			out = append(out, SearchHitView{
				MBID:   h.MBID,
				Kind:   "recording",
				Title:  h.Title,
				Artist: h.ArtistCredit,
				Detail: formatMillis(h.LengthMillis),
			})
		}

		return out, nil
	}

	hits, err := s.mbr.SearchReleaseGroupHits(s.ctx, query, artist)
	if err != nil {
		return nil, fmt.Errorf("search release groups: %w", err)
	}

	out := make([]SearchHitView, 0, len(hits))
	for _, h := range hits {
		detail := h.PrimaryType
		if y := h.FirstDate; len(y) >= 4 { //nolint:mnd
			detail = strings.TrimSpace(y[:4] + " " + detail)
		}

		out = append(out, SearchHitView{
			MBID:   h.MBID,
			Kind:   "releasegroup",
			Title:  h.Title,
			Artist: h.ArtistCredit,
			Detail: detail,
		})
	}

	return out, nil
}

// SelectSearchCandidate resolves a picked search hit into a fully-
// scored candidate and splices it to the top of the group's candidate
// list — the same shape as the paste-URL path, so Apply works
// unchanged.  A "recording" hit is resolved to a representative
// release first.
func (s *Service) SelectSearchCandidate(
	groupKey, kind, mbid string,
) (*ScoreView, error) {
	score, err := s.scorer.ScoreGroup(s.ctx, groupKey)
	if err != nil {
		return nil, fmt.Errorf("score group: %w", err)
	}

	var picked autotag.Candidate

	if kind == "recording" {
		picked, err = s.mbr.ResolveOneRecordingMBID(s.ctx, mbid)
	} else {
		picked, err = s.mbr.ResolveOneReleaseMBID(s.ctx, mbid)
	}

	if err != nil {
		return nil, fmt.Errorf("resolve %s %s: %w", kind, mbid, err)
	}

	scored := autotag.ScoreCandidate(autotag.Group{
		AlbumName:   score.AlbumName,
		AlbumArtist: score.AlbumArtist,
		Tracks:      score.LocalTracks,
	}, picked)
	merged := append([]autotag.Candidate{scored}, score.Candidates...)
	score.Candidates = merged

	s.cacheCandidates(groupKey, merged)

	return scoreToView(score, s.exp), nil
}

// formatMillis renders a millisecond duration as m:ss for the search
// result detail line; empty for unknown lengths.
func formatMillis(ms int64) string {
	if ms <= 0 {
		return ""
	}

	sec := ms / 1000
	minutes := sec / 60
	seconds := sec % 60

	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

var errInvalidMBURL = errors.New("autotag: not a valid MusicBrainz release URL")

// extractReleaseMBID pulls the UUID out of a MusicBrainz release
// or release-group URL.  Accepts both /release/<uuid> and
// /release-group/<uuid> forms.  Returns "" for anything else so
// the caller can error cleanly.
func extractReleaseMBID(url string) string {
	const mbidLen = 36

	for _, prefix := range []string{"/release-group/", "/release/"} {
		idx := strings.Index(url, prefix)
		if idx < 0 {
			continue
		}

		rest := url[idx+len(prefix):]
		if len(rest) < mbidLen {
			continue
		}

		candidate := rest[:mbidLen]
		ok := true

		for _, r := range candidate {
			if r != '-' && (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
				ok = false

				break
			}
		}

		if ok {
			return candidate
		}
	}

	return ""
}

// scoreToView converts the internal GroupScore to the Wails-
// friendly projection.  Pass exp to eager-fetch cover art for the
// top-ranked candidate; pass nil to skip cover art entirely (used
// only by paths that don't need art).
func scoreToView(s *autotag.GroupScore, exp *explore.Service) *ScoreView {
	group := autotag.Group{
		AlbumName:   s.AlbumName,
		AlbumArtist: s.AlbumArtist,
		Tracks:      s.LocalTracks,
		Synthetic:   s.Synthetic,
	}

	rec := s.Recommendation
	if rec == "" {
		// Paths that rebuild a GroupScore from cached candidates
		// don't run the scorer; derive the tier here.
		rec = autotag.Recommend(group, s.Candidates)
	}

	out := &ScoreView{
		GroupKey:       s.GroupKey,
		Recommendation: string(rec),
		Synthetic:      s.Synthetic,
		MixedBag:       !s.Synthetic && autotag.IsMixedBag(group),
	}

	for _, l := range s.LocalTracks {
		out.LocalTracks = append(out.LocalTracks, LocalTrackView{
			AudioFileID:   l.AudioFileID,
			FilePath:      l.FilePath,
			Title:         l.Title,
			Artist:        l.Artist,
			TrackNumber:   l.TrackNumber,
			DiscNumber:    l.DiscNumber,
			LengthMillis:  l.LengthMillis,
			RecordingMBID: l.RecordingMBID,
		})
	}

	for i, c := range s.Candidates {
		cv := CandidateView{
			ReleaseMBID:      c.ReleaseMBID,
			ReleaseGroupMBID: c.ReleaseGroupMBID,
			Title:            c.Title,
			ArtistCredit:     c.ArtistCredit,
			Date:             c.Date,
			OriginalDate:     c.OriginalDate,
			Country:          c.Country,
			Status:           c.Status,
			PrimaryType:      c.PrimaryType,
			TrackCount:       c.TrackCount,
			Score:            c.Score,
			Source:           string(c.Source),
			Provenance:       c.Provenance,
			Breakdown: ScoreBreakdownView{
				TitleAvg:      c.Breakdown.TitleAvg,
				LengthAvg:     c.Breakdown.LengthAvg,
				ArtistFit:     c.Breakdown.ArtistFit,
				AlbumFit:      c.Breakdown.AlbumFit,
				TrackCountFit: c.Breakdown.TrackCountFit,
				ReleaseMeta:   c.Breakdown.ReleaseMeta,
				Evidence:      c.Breakdown.Evidence,
			},
		}

		// Eager-fetch art only for the top candidate.  Others lazy-
		// load via GetCandidateCoverArt when the user selects them
		// — keeps the GetCandidates round-trip from blocking on a
		// long network fetch chain for releases the user may never
		// look at.  Use the candidate-specific lookup so embedded
		// ID3 art on the user's existing files doesn't masquerade
		// as the candidate's cover.
		if exp != nil && i == 0 {
			cv.CoverArtURL = exp.GetCandidateThumbnail(
				c.ReleaseMBID, c.ReleaseGroupMBID,
			)
		}

		for _, a := range c.Alignments {
			cv.Alignments = append(cv.Alignments, AlignmentView{
				LocalIndex:          a.LocalIndex,
				LocalTitle:          a.LocalTitle,
				LocalLengthMillis:   a.LocalLengthMillis,
				CandidatePosition:   a.CandidatePosition,
				CandidateDiscNumber: a.CandidateDiscNumber,
				CandidateTitle:      a.CandidateTitle,
				CandidateMBID:       a.CandidateMBID,
				CandidateLength:     a.CandidateLength,
				TitleScore:          a.TitleScore,
				LengthDeltaMs:       a.LengthDeltaMs,
				TrackNumberOK:       a.TrackNumberOK,
				Status:              string(a.Status),
			})
		}

		out.Candidates = append(out.Candidates, cv)
	}

	return out
}

// twAdapter bridges autotag.TagWriter to the concrete
// tagwriter.TagWriter.  The map is passed through unchanged since
// autotag.TagChanges has the same string keys as tagwriter's.
type twAdapter struct{ tw *tagwriter.TagWriter }

func (a *twAdapter) WriteTrackTagsByPath(filePath string, changes autotag.TagChanges) error {
	return a.tw.WriteTrackTagsByPath(filePath, tagwriter.TagChanges(changes))
}

// isNoRows returns true when err signals "no rows in result set" —
// modernc.org/sqlite doesn't always return sql.ErrNoRows directly,
// depending on the code path.
func isNoRows(err error) bool {
	if err == nil {
		return false
	}

	return strings.Contains(err.Error(), "no rows")
}
