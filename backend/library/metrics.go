package library

import (
	"sync"
	"time"
)

// ScanMetrics holds timing and count data collected during a library scan.
// Worker-pool fields are protected by a mutex; DB-writer fields are
// single-threaded and use plain addition.
type ScanMetrics struct {
	mu sync.Mutex

	// Top-level phases (wall-clock).
	Total               time.Duration `json:"total"`
	LoadExisting        time.Duration `json:"loadExisting"`
	WalkDuration        time.Duration `json:"walkDuration"`
	ExtractionWallClock time.Duration `json:"extractionWallClock"`
	DBWritesWallClock   time.Duration `json:"dbWritesWallClock"`
	OrphanCleanup       time.Duration `json:"orphanCleanup"`
	PostScanVariants    time.Duration `json:"postScanVariants"`

	// Per-format extraction (cumulative across workers).
	FormatExtraction map[string]int64 `json:"formatExtraction"`
	FormatCount      map[string]int64 `json:"formatCount"`

	// Sub-operation cumulative times (across workers).
	TagExtraction      time.Duration `json:"tagExtraction"`
	DurationExtraction time.Duration `json:"durationExtraction"`

	// DB sub-operations (cumulative, single-threaded DB writer).
	BatchCommits time.Duration `json:"batchCommits"`
	CoverArtSave time.Duration `json:"coverArtSave"`

	// Thumbnail generation (async worker pool).
	ThumbnailWallClock  time.Duration `json:"thumbnailWallClock"`
	ThumbnailGeneration time.Duration `json:"thumbnailGeneration"`
	ThumbnailSmall      time.Duration `json:"thumbnailSmall"`
	ThumbnailMedium     time.Duration `json:"thumbnailMedium"`
	ThumbnailLarge      time.Duration `json:"thumbnailLarge"`

	// Full-rescan-specific phases.
	ClearQueue      time.Duration `json:"clearQueue"`
	ClearDatabase   time.Duration `json:"clearDatabase"`
	ClearCoverFiles time.Duration `json:"clearCoverFiles"`

	// File counts.
	Added   int64 `json:"added"`
	Updated int64 `json:"updated"`
	Skipped int64 `json:"skipped"`
	Removed int64 `json:"removed"`

	// Cancelled is true when the scan was stopped via CancelScan.
	Cancelled bool `json:"cancelled"`

	// Non-fatal issues encountered during scanning.
	Warnings []ScanWarning `json:"warnings"`
}

// ScanProgress is the payload emitted periodically during a scan to
// report live progress to the frontend.
type ScanProgress struct {
	Phase     string `json:"phase"`     // "counting", "scanning", "orphans", "thumbnails"
	Total     int64  `json:"total"`     // total audio files from pre-walk count
	Processed int64  `json:"processed"` // added + skipped + updated so far
	Added     int64  `json:"added"`
	Skipped   int64  `json:"skipped"`
	Updated   int64  `json:"updated"`
}

// ScanWarning represents a non-fatal issue encountered during scanning.
type ScanWarning struct {
	FilePath string `json:"filePath"`
	Phase    string `json:"phase"`
	Err      error  `json:"err"`
}

func newScanMetrics() *ScanMetrics {
	return &ScanMetrics{
		FormatExtraction: make(map[string]int64),
		FormatCount:      make(map[string]int64),
	}
}

// addExtraction records per-file extraction timing from a worker
// goroutine.  It is safe for concurrent use.
func (m *ScanMetrics) addExtraction(
	fileType string,
	tagTime, durationTime time.Duration,
) {
	m.mu.Lock()
	defer m.mu.Unlock()

	total := tagTime + durationTime
	m.FormatExtraction[fileType] += total.Milliseconds()
	m.FormatCount[fileType]++
	m.TagExtraction += tagTime
	m.DurationExtraction += durationTime
}

// addCoverArtSave records the time spent saving an original cover
// art file.  Called from the single-threaded DB writer.
func (m *ScanMetrics) addCoverArtSave(d time.Duration) {
	m.CoverArtSave += d
}

// addWarning records a non-fatal scan issue.  Safe for concurrent use.
func (m *ScanMetrics) addWarning(filePath, phase string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Warnings = append(m.Warnings, ScanWarning{
		FilePath: filePath,
		Phase:    phase,
		Err:      err,
	})
}

// addThumbnailTier records the time spent generating a single
// thumbnail tier.  Safe for concurrent use from the thumbnail
// worker pool.
func (m *ScanMetrics) addThumbnailTier(
	suffix string,
	d time.Duration,
) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ThumbnailGeneration += d

	switch suffix {
	case "_sm":
		m.ThumbnailSmall += d
	case "_md":
		m.ThumbnailMedium += d
	case "_lg":
		m.ThumbnailLarge += d
	}
}
