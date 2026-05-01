package autotag

// LocalTrack is an audio_files row projected into the shape the
// scorer cares about.  All durations are in milliseconds to match
// what the MB client returns.
type LocalTrack struct {
	AudioFileID   int64
	FilePath      string
	Title         string
	Artist        string
	TrackNumber   int
	DiscNumber    int
	LengthMillis  int64
	RecordingMBID string
}

// CandidateSource distinguishes candidates served from the local
// release_groups cache (zero network cost) from those fetched live.
type CandidateSource string

// CandidateSource values.
const (
	SourceLocal       CandidateSource = "local"
	SourceMusicBrainz CandidateSource = "musicbrainz"
)

// Candidate is a single release that could match an album-group.
// Track alignments are produced by the scorer and carry the per-
// field diff data the review UI renders.
//
// Date is this *specific release's* date (e.g. 2010 for a remaster);
// OriginalDate is the *release group's* first-release-date (the
// original album year, e.g. 1973), populated from MB's
// release-group.first-release-date and equal to Date when the
// release-group has only one release.
type Candidate struct {
	ReleaseMBID      string
	ReleaseGroupMBID string
	Title            string
	ArtistCredit     string
	Date             string // "YYYY" or "YYYY-MM-DD" — this release
	OriginalDate     string // "YYYY" or "YYYY-MM-DD" — release group's first release
	Country          string
	Status           string // "Official", "Promotion", ...
	TrackCount       int
	Tracks           []CandidateTrack
	Alignments       []TrackAlignment
	Score            float64 // 0..1, higher is better
	Breakdown        ScoreBreakdown
	Source           CandidateSource
	Provenance       string // cascade step that produced this ("strict", "fuzzy-title", "paste", "local")
}

// ScoreBreakdown exposes the four inputs that go into Candidate.Score
// so the review UI can explain the ranking to the user.
type ScoreBreakdown struct {
	TitleAvg      float64 // average per-track title similarity (0..1)
	LengthAvg     float64 // average per-track length similarity (0..1)
	TrackCountFit float64 // 1.0 when local and candidate track counts match
	ReleaseMeta   float64 // year + official + country, averaged
}

// CandidateTrack is one track inside a candidate release.
type CandidateTrack struct {
	Position     int
	DiscNumber   int
	Title        string
	LengthMillis int64
	MBID         string
}

// AlignmentStatus classifies what happened when aligning one local
// track to the best-matching candidate track.
type AlignmentStatus string

// AlignmentStatus values.
const (
	AlignmentMatched    AlignmentStatus = "matched"
	AlignmentMissing    AlignmentStatus = "missing"    // candidate has the track, folder doesn't
	AlignmentUnmatched  AlignmentStatus = "unmatched"  // folder has the track, candidate doesn't
	AlignmentMismatched AlignmentStatus = "mismatched" // aligned but low confidence
)

// TrackAlignment carries the per-track diff data for one local-
// track ↔ candidate-track pairing.  LocalIndex is -1 when the
// candidate track has no local file (missing); CandidatePosition
// is 0 when the local track has no match (unmatched).
type TrackAlignment struct {
	LocalIndex        int // index into LocalTracks, or -1
	LocalTitle        string
	LocalLengthMillis int64

	CandidatePosition   int
	CandidateDiscNumber int
	CandidateTitle      string
	CandidateMBID       string
	CandidateLength     int64

	TitleScore    float64 // 0..1 from normalized-title edit distance
	LengthDeltaMs int64   // abs(local - candidate); 0 if either side missing
	TrackNumberOK bool    // local track number matches candidate position
	Status        AlignmentStatus
}

// GroupScore is the scorer's full output for one tagging group.
type GroupScore struct {
	GroupKey    string
	LocalTracks []LocalTrack
	Candidates  []Candidate // sorted by Score, descending
}
