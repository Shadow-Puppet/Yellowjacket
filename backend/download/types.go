// Package download acquires music from user-configured external
// services and imports it into the library.
//
// The services users connect are not the same kind of thing: some
// search, some move bytes, some are whole automation systems we hand a
// request to.  Rather than one interface every adapter half-implements,
// a provider fills one or more of three roles — Searcher, Transporter,
// Delegator — and declares which in its Caps.  The pipeline composes
// them: a search-only provider (Prowlarr) is paired with a transport
// (qBittorrent, SABnzbd) by protocol at grab time, while providers that
// do both (slskd, yt-dlp) pair with themselves.
//
// Nothing here downloads into the library.  Grabs land in a staging
// directory, are verified and tagged against the release the user
// actually asked for, and only then move into library paths.
package download

import (
	"slices"
	"strings"
	"time"
)

// Kind identifies a provider implementation.  It is stored in the
// database and used to look up the constructor in the registry, so
// values are stable strings and never renamed.
type Kind string

// Provider kinds.
const (
	KindSlskd       Kind = "slskd"
	KindYtDlp       Kind = "yt-dlp"
	KindLidarr      Kind = "lidarr"
	KindProwlarr    Kind = "prowlarr"
	KindQBittorrent Kind = "qbittorrent"
	KindSABnzbd     Kind = "sabnzbd"

	// KindFake is an in-memory provider used by tests.  It is never
	// offered in the UI.
	KindFake Kind = "fake"
)

// Protocol is how a candidate's bytes are moved.  Search-only providers
// report it so the pipeline can pick a compatible transport; providers
// that transport their own results use ProtocolDirect.
type Protocol string

// Transport protocols.
const (
	// ProtocolDirect means the finding provider also does the fetch.
	ProtocolDirect  Protocol = "direct"
	ProtocolTorrent Protocol = "torrent"
	ProtocolUsenet  Protocol = "usenet"
)

// Caps declares which roles a provider fills and which optional
// behaviours it supports.  The frontend renders controls from this
// rather than switching on Kind, so a provider that gains resume
// support later needs no frontend change.
type Caps struct {
	// Roles.
	CanSearch    bool `json:"canSearch"`
	CanTransport bool `json:"canTransport"`
	CanDelegate  bool `json:"canDelegate"`

	// CanList marks a provider that keeps a persistent wanted list of
	// its own, which the reconciler mirrors this app's list into.
	CanList bool `json:"canList"`

	// Optional behaviours.
	CanResume   bool `json:"canResume"`
	CanCancel   bool `json:"canCancel"`
	ReportsSize bool `json:"reportsSize"`

	// Protocols this provider can transport.  Empty for providers that
	// only fetch their own search results.
	Transports []Protocol `json:"transports"`
}

// Handles reports whether the provider can transport the given protocol.
func (c Caps) Handles(p Protocol) bool {
	return slices.Contains(c.Transports, p)
}

// Request is what the user asked for.  Requests that carry a MusicBrainz
// anchor are far more reliable than free-text ones, because the anchor
// gives the import step an expected tracklist to match against — so the
// pipeline records which it got and refuses to auto-pick without one.
type Request struct {
	ID string `json:"id"`

	// Anchors.  Any may be empty; all empty means free-text.
	ReleaseMBID      string `json:"releaseMbid,omitempty"`
	ReleaseGroupMBID string `json:"releaseGroupMbid,omitempty"`

	// RecordingMBID anchors a single-track request.  Its Expected holds
	// exactly that one track, which is what lets a track request be
	// scored — and therefore auto-picked — on the same footing as an
	// album.
	RecordingMBID string `json:"recordingMbid,omitempty"`

	// WantID links back to the wanted-list row this request was raised
	// for, or 0 for a request the user started by hand.  The reconciler
	// writes the outcome back through it.
	WantID int64 `json:"wantId,omitempty"`

	// Source records where the request came from, for the downloads
	// list.  Empty means "manual".
	Source string `json:"source,omitempty"`

	// Display and query text.  Artist/Album are what searches are built
	// from; Query overrides them when the user typed something raw.
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Query  string `json:"query,omitempty"`

	// Expected is the tracklist the anchor resolves to, used for
	// completeness scoring and for the autotag match at import.  Empty
	// for free-text requests.
	Expected []ExpectedTrack `json:"expected,omitempty"`

	// LibraryID is the library imported files belong to.
	LibraryID int64 `json:"libraryId"`

	CreatedAt time.Time `json:"createdAt"`
}

// Anchored reports whether the request carries a MusicBrainz ID.  Only
// anchored requests are eligible for auto-pick.
func (r Request) Anchored() bool {
	return r.ReleaseMBID != "" ||
		r.ReleaseGroupMBID != "" ||
		r.RecordingMBID != ""
}

// SearchText returns the string to hand a provider's search endpoint.
//
// Album titles routinely start with the artist name — self-titled
// albums ("Boston" / "Boston") and titles like "Blank Banshee 0" both
// do — so naively concatenating Artist and Album would search for
// "Blank Banshee Blank Banshee 0". That repeated term is enough to
// return zero results on providers that expect every term to appear
// in a match (Soulseek in particular), so the artist is dropped when
// the album title already leads with it.
func (r Request) SearchText() string {
	if r.Query != "" {
		return r.Query
	}

	if r.Artist != "" && albumLeadsWithArtist(r.Artist, r.Album) {
		return strings.TrimSpace(r.Album)
	}

	return strings.TrimSpace(r.Artist + " " + r.Album)
}

// albumLeadsWithArtist reports whether album starts with artist as a
// whole word, case-insensitively, so it is safe to drop the artist
// from a combined query without losing a real search term. A plain
// substring check would misfire on cases like artist "Air" against
// album "Repair".
func albumLeadsWithArtist(artist, album string) bool {
	a, b := strings.ToLower(strings.TrimSpace(artist)), strings.ToLower(strings.TrimSpace(album))
	if a == "" || !strings.HasPrefix(b, a) {
		return false
	}

	rest := b[len(a):]

	return rest == "" || !isWordChar(rune(rest[0]))
}

// isWordChar reports whether r continues a word for the purposes of
// albumLeadsWithArtist's boundary check.
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// ExpectedTrack is one track of the release the user asked for.
type ExpectedTrack struct {
	Position     int    `json:"position"`
	DiscNumber   int    `json:"discNumber"`
	Title        string `json:"title"`
	Artist       string `json:"artist"`
	LengthMillis int64  `json:"lengthMillis"`
}

// Candidate is one acquirable thing a provider found: a Soulseek user's
// folder, a torrent, a YouTube playlist.  Providers fill the descriptive
// fields; the ranker fills Match, Quality and Score.
type Candidate struct {
	// ID is unique within the provider that produced it, and is what
	// gets handed back to Grab.
	ID         string `json:"id"`
	ProviderID int64  `json:"providerId"`
	Kind       Kind   `json:"kind"`

	// Protocol determines which transport can fetch this.
	Protocol Protocol `json:"protocol"`

	// Descriptive.
	Title  string `json:"title"`
	Artist string `json:"artist,omitempty"`
	Origin string `json:"origin,omitempty"` // peer username, indexer name, channel

	Files     []CandidateFile `json:"files"`
	TotalSize int64           `json:"totalSize"`

	// Health is the provider's own availability signal, normalized to
	// 0..1: seeder count for torrents, free upload slots and queue
	// length for Soulseek.  0.5 when the provider has no signal.
	Health float64 `json:"health"`

	// Scores, filled by the ranker.
	Match   MatchScore   `json:"match"`
	Quality QualityScore `json:"quality"`
	Score   float64      `json:"score"`

	// Payload is provider-private data needed to fetch this candidate
	// (magnet URI, NZB URL, slskd file list).  Never shown to the user.
	Payload map[string]string `json:"-"`
}

// CandidateFile is one file inside a candidate.  Soulseek and torrent
// results give paths and sizes but no tags, so Format and duration are
// inferred from the path and size where possible.
type CandidateFile struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Format    Format `json:"format"`
	Bitrate   int    `json:"bitrate,omitempty"` // kbps, 0 when unknown
	IsAudio   bool   `json:"isAudio"`
	MatchedTo int    `json:"matchedTo,omitempty"` // expected track position
}

// Format is a normalized audio container/codec name.
type Format string

// Audio formats, ordered by the quality ranking in formatRank.
const (
	FormatUnknown Format = ""
	FormatFLAC    Format = "flac"
	FormatALAC    Format = "alac"
	FormatWAV     Format = "wav"
	FormatMP3     Format = "mp3"
	FormatAAC     Format = "aac"
	FormatOGG     Format = "ogg"
	FormatOpus    Format = "opus"
	FormatWMA     Format = "wma"
)

// Lossless reports whether the format preserves the source exactly.
func (f Format) Lossless() bool {
	return f == FormatFLAC || f == FormatALAC || f == FormatWAV
}

// Supported reports whether the player can decode this format.  Grabs
// of unsupported formats are still allowed — the user may want them —
// but they rank below playable ones.
func (f Format) Supported() bool {
	switch f {
	case FormatMP3, FormatFLAC, FormatOGG, FormatWAV:
		return true
	case FormatUnknown, FormatALAC, FormatAAC, FormatOpus, FormatWMA:
		return false
	default:
		return false
	}
}

// MatchScore answers "is this the release the user asked for?"  It is
// deliberately separate from QualityScore: a perfect match at 128kbps
// and a mediocre match in FLAC are different failures, and collapsing
// them into one number makes the ranking impossible to explain.
type MatchScore struct {
	// Overall is 0..1.
	Overall float64 `json:"overall"`

	TitleFit     float64 `json:"titleFit"`     // filenames vs expected titles
	ArtistFit    float64 `json:"artistFit"`    // path/origin vs expected artist
	AlbumFit     float64 `json:"albumFit"`     // folder name vs album title
	Completeness float64 `json:"completeness"` // audio files vs expected count

	// Anchored records whether an MBID drove this score.  Unanchored
	// matches are capped, because there is nothing to be right about.
	Anchored bool `json:"anchored"`
}

// QualityScore answers "is this a good copy?".
type QualityScore struct {
	// Overall is 0..1.
	Overall float64 `json:"overall"`

	FormatRank float64 `json:"formatRank"` // FLAC > V0 > 320 > lower
	Bitrate    float64 `json:"bitrate"`
	Health     float64 `json:"health"`   // seeders, free slots
	Priority   float64 `json:"priority"` // user's per-provider preference

	// Mixed marks a candidate whose files are not all the same format,
	// which usually means a hand-assembled folder rather than a rip.
	Mixed bool `json:"mixed"`
}

// AudioFiles returns only the audio entries of a candidate.
func (c Candidate) AudioFiles() []CandidateFile {
	out := make([]CandidateFile, 0, len(c.Files))

	for _, f := range c.Files {
		if f.IsAudio {
			out = append(out, f)
		}
	}

	return out
}

// State is the lifecycle position of a download item.
type State string

// Download item states.  Searching through Importing are live;
// Complete, Cancelled and Failed are terminal.
const (
	StateSearching State = "searching"
	StateFound     State = "found"
	StateQueued    State = "queued"
	StateGrabbing  State = "grabbing"
	StateVerifying State = "verifying"
	StateTagging   State = "tagging"
	StateImporting State = "importing"
	StateComplete  State = "complete"
	StateCancelled State = "cancelled"
	StateFailed    State = "failed"
)

// IsTerminal reports whether the state means no further progress will
// happen without a new attempt.
func (s State) IsTerminal() bool {
	return s == StateComplete || s == StateCancelled || s == StateFailed
}

// Progress is a transport's periodic report.  Total is 0 when the
// provider cannot say how large the transfer is.
type Progress struct {
	Current int64
	Total   int64
	Phase   string
}

// ProgressFunc receives transport progress.  Implementations must
// tolerate being called from any goroutine and at high frequency.
type ProgressFunc func(Progress)

// Result is what a transport produced.
type Result struct {
	// Dir is the staging directory the files landed in.
	Dir string

	// Files are absolute paths, all under Dir.
	Files []string

	// BytesTransferred is what actually moved, for reporting.
	BytesTransferred int64

	// Delegated marks a result produced by an external manager that has
	// already imported the files into its own library.  Files are then
	// absolute paths outside staging, and the pipeline records them
	// where they are instead of tagging and moving them.
	Delegated bool
}

// DelegateStatus is a delegating manager's answer to "are we there
// yet?".
type DelegateStatus struct {
	State State

	// Progress is 0..1 when the manager reports it, -1 when it does not.
	Progress float64

	// ImportedPaths are files the manager has already placed on disk.
	// A delegate that imports into its own library reports them here so
	// the pipeline can reconcile rather than re-import.
	ImportedPaths []string

	Message string
}
