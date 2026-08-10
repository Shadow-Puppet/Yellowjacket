package download

import (
	"math"
	"sort"
	"strings"

	"yellowjacket/backend/autotag"
)

// Ranking keeps two questions apart:
//
//	match   — is this the release the user asked for?
//	quality — is it a good copy of it?
//
// They are reported separately because they fail differently and trade
// off against each other: a flawless FLAC of the wrong album is useless,
// a 128kbps rip of the right one is merely disappointing, and only the
// user knows which they will accept.  A single blended number cannot be
// explained, and the review UI has to explain itself.

// Ranking weights.  Match dominates, because a wrong album at any
// bitrate is a failed download.
const (
	weightMatch   = 0.72
	weightQuality = 0.28
)

// Match sub-weights.
const (
	weightTitleFit     = 0.40
	weightCompleteness = 0.30
	weightAlbumFit     = 0.18
	weightArtistFit    = 0.12
)

// Quality sub-weights.  They sum to 1.0 along with weightSizeFit below.
const (
	weightFormat   = 0.42
	weightBitrate  = 0.23
	weightHealth   = 0.20
	weightPriority = 0.10
	weightSizeFit  = 0.05
)

// unanchoredCap bounds the match score of a free-text request.  Without
// an MBID there is no tracklist to be right about, so a confident-
// looking score would be a lie — and auto-pick keys off this.
const unanchoredCap = 0.65

// AutoDownloadPrefs gates and scores what AutoPickable may choose
// without asking.  Zero values are permissive: no size window and no
// format restriction.
type AutoDownloadPrefs struct {
	// MinSizeMB and MaxSizeMB bound what auto-pick will grab.  Zero
	// means no bound on that side.  A candidate outside the window is
	// filtered out of auto-pick entirely, not merely scored down — a
	// tiny "sampler" torrent or a boxset ten times the expected size is
	// usually the wrong thing entirely, not a worse copy of the right
	// thing.
	MinSizeMB int `json:"minSizeMb"`
	MaxSizeMB int `json:"maxSizeMb"`

	// PreferredSizeMB nudges the score toward a target size within the
	// min/max window (a lossless rip and a heavily-padded lossless rip
	// can both pass the window).  Zero disables the nudge; sizeFit then
	// returns a neutral value that does not affect ranking.
	PreferredSizeMB int `json:"preferredSizeMb"`

	// AllowedFormats restricts auto-pick to candidates whose audio
	// files are all in one of these formats.  Empty means no
	// restriction.
	AllowedFormats []Format `json:"allowedFormats"`
}

// eligible reports whether a candidate may be auto-picked under these
// preferences: within the size window (when set) and, when a format
// list is given, every audio file in an allowed format.
func (p AutoDownloadPrefs) eligible(c Candidate) bool {
	const bytesPerMB = 1 << 20

	if p.MinSizeMB > 0 && c.TotalSize < int64(p.MinSizeMB)*bytesPerMB {
		return false
	}

	if p.MaxSizeMB > 0 && c.TotalSize > int64(p.MaxSizeMB)*bytesPerMB {
		return false
	}

	if len(p.AllowedFormats) == 0 {
		return true
	}

	allowed := make(map[Format]bool, len(p.AllowedFormats))
	for _, f := range p.AllowedFormats {
		allowed[f] = true
	}

	for _, f := range c.AudioFiles() {
		if !allowed[f.Format] {
			return false
		}
	}

	return true
}

// filter returns only the candidates these preferences allow to be
// auto-picked, in the same (already ranked) order.
func (p AutoDownloadPrefs) filter(ranked []Candidate) []Candidate {
	out := make([]Candidate, 0, len(ranked))

	for _, c := range ranked {
		if p.eligible(c) {
			out = append(out, c)
		}
	}

	return out
}

// sizeFit scores how close totalSize is to PreferredSizeMB, 0..1,
// falling off linearly as the size doubles or halves away from it.
// Returns a neutral 0.5 when no preference is set, so the absence of a
// preference does not bias ranking.
func (p AutoDownloadPrefs) sizeFit(totalSize int64) float64 {
	const (
		bytesPerMB = 1 << 20
		neutral    = 0.5
	)

	if p.PreferredSizeMB <= 0 || totalSize <= 0 {
		return neutral
	}

	preferred := float64(p.PreferredSizeMB) * bytesPerMB
	ratio := float64(totalSize) / preferred

	if ratio < 1 {
		ratio = 1 / ratio
	}

	// ratio is now >= 1: 1.0 is an exact match, 2.0 is double or half
	// the preferred size.  Falls to 0 at 2x away and beyond.
	fit := 1 - (ratio - 1)

	return clamp01(fit)
}

// Score fills a candidate's Match, Quality and Score fields.
func Score(dl Download, c Candidate, priority int, prefs AutoDownloadPrefs) Candidate {
	c.Files = AnnotateFiles(c.Files)

	audio := c.AudioFiles()

	matched, titleFit := matchFiles(audio, dl.Expected)

	// Write the alignment back so the picker can show which file maps
	// to which track.
	c.Files = mergeMatched(c.Files, matched)

	c.Match = scoreMatch(dl, c, audio, titleFit)
	c.Quality = scoreQuality(c, audio, priority, prefs)

	c.Score = weightMatch*c.Match.Overall + weightQuality*c.Quality.Overall

	return c
}

// scoreMatch answers whether this candidate is the requested release.
func scoreMatch(
	dl Download,
	c Candidate,
	audio []CandidateFile,
	titleFit float64,
) MatchScore {
	m := MatchScore{
		Anchored: dl.Anchored(),
		TitleFit: titleFit,
	}

	m.Completeness = completeness(len(audio), len(dl.Expected))

	// The candidate's own title, and the folder its files sit in, are
	// two independent guesses at the album name.  Take the better one:
	// providers vary in which is meaningful.
	folder := ""
	if len(audio) > 0 {
		folder = ParsePath(audio[0].Path).Folder
	}

	m.AlbumFit = math.Max(
		autotag.TitleSimilarity(dl.Album, c.Title),
		autotag.TitleSimilarity(dl.Album, folder),
	)

	m.ArtistFit = artistFit(dl.Artist, c)

	// With no expected tracklist there is no title signal at all, so
	// redistribute its weight onto the album/artist evidence rather
	// than scoring every free-text result as half-wrong.
	if len(dl.Expected) == 0 {
		m.Overall = 0.55*m.AlbumFit + 0.45*m.ArtistFit
	} else {
		m.Overall = weightTitleFit*m.TitleFit +
			weightCompleteness*m.Completeness +
			weightAlbumFit*m.AlbumFit +
			weightArtistFit*m.ArtistFit
	}

	if !m.Anchored {
		m.Overall = math.Min(m.Overall, unanchoredCap)
	}

	return m
}

// artistFit compares the requested artist against the candidate's
// artist field, its title, and the path of its first audio file, taking
// the best.  Providers disagree about where the artist name lands.
func artistFit(want string, c Candidate) float64 {
	if strings.TrimSpace(want) == "" {
		return 0.5
	}

	best := autotag.TitleSimilarity(want, c.Artist)

	if s := autotag.TitleSimilarity(want, c.Title); s > best {
		best = s
	}

	// A path containing the artist name anywhere is weak but real
	// evidence — most folders are "Artist - Album".
	norm := autotag.Normalize(want)
	if norm != "" {
		for _, f := range c.Files {
			if strings.Contains(autotag.Normalize(f.Path), norm) {
				if best < 0.8 {
					best = 0.8
				}

				break
			}
		}
	}

	return best
}

// completeness scores audio file count against the expected track
// count.  Extra files are penalized far more gently than missing ones:
// a folder with bonus tracks or a stray intro is still the album, while
// a folder missing half the tracks is not.
func completeness(got, want int) float64 {
	if want == 0 {
		if got > 0 {
			return 0.5
		}

		return 0
	}

	if got == 0 {
		return 0
	}

	if got >= want {
		extra := float64(got-want) / float64(want)

		return math.Max(0.75, 1.0-0.25*extra)
	}

	return float64(got) / float64(want)
}

// scoreQuality answers whether this is a good copy.
func scoreQuality(
	c Candidate,
	audio []CandidateFile,
	priority int,
	prefs AutoDownloadPrefs,
) QualityScore {
	q := QualityScore{
		Health:   clamp01(c.Health),
		Priority: clamp01(float64(priority) / 100.0),
		SizeFit:  prefs.sizeFit(c.TotalSize),
	}

	if len(audio) == 0 {
		return q
	}

	// Format: score the worst file, not the average.  A folder that is
	// mostly FLAC with three MP3s transcoded in is a worse copy than
	// its average suggests, and that is exactly what the user would
	// want flagged.
	worst := 1.0
	first := audio[0].Format

	for _, f := range audio {
		if r := formatRank(f.Format); r < worst {
			worst = r
		}

		if f.Format != first {
			q.Mixed = true
		}
	}

	q.FormatRank = worst
	q.Bitrate = bitrateScore(audio)

	q.Overall = weightFormat*q.FormatRank +
		weightBitrate*q.Bitrate +
		weightHealth*q.Health +
		weightPriority*q.Priority +
		weightSizeFit*q.SizeFit

	if q.Mixed {
		q.Overall *= 0.9
	}

	return q
}

// formatRank scores a format on its own terms, in 0..1.  Lossless
// formats top out; lossy formats sit below and are further separated by
// bitrate.  Formats the player cannot decode are penalized but not
// zeroed — the user may be acquiring them deliberately.
func formatRank(f Format) float64 {
	base := 0.0

	switch f {
	case FormatFLAC:
		base = 1.0
	case FormatALAC:
		base = 0.95
	case FormatWAV:
		base = 0.85 // lossless, but untaggable and huge
	case FormatMP3:
		base = 0.6
	case FormatAAC, FormatOpus:
		base = 0.6
	case FormatOGG:
		base = 0.55
	case FormatWMA:
		base = 0.3
	case FormatUnknown:
		base = 0.2
	default:
		base = 0.2
	}

	if !f.Supported() && f != FormatUnknown {
		base *= 0.8
	}

	return base
}

// bitrateScore maps the mean stated bitrate of lossy files onto 0..1.
// Lossless files score 1.0 and are excluded from the mean.  Returns a
// neutral 0.5 when nothing states a bitrate, which is the common case
// for Soulseek results.
func bitrateScore(audio []CandidateFile) float64 {
	var (
		sum   float64
		count int
	)

	for _, f := range audio {
		if f.Format.Lossless() {
			sum += 1.0
			count++

			continue
		}

		if f.Bitrate == 0 {
			continue
		}

		sum += lossyBitrateScore(f.Bitrate)
		count++
	}

	if count == 0 {
		return 0.5
	}

	return sum / float64(count)
}

// lossyBitrateScore maps kbps onto 0..1 with the knee where it belongs
// perceptually: the gap between 128 and 192 matters much more than the
// gap between 256 and 320.
func lossyBitrateScore(kbps int) float64 {
	switch {
	case kbps >= 320:
		return 1.0
	case kbps >= 256:
		return 0.9
	case kbps >= 224:
		return 0.82
	case kbps >= 192:
		return 0.72
	case kbps >= 160:
		return 0.55
	case kbps >= 128:
		return 0.4
	case kbps >= 96:
		return 0.2
	default:
		return 0.1
	}
}

// Rank scores every candidate and returns them best-first.  Ties break
// on match, then on provider priority, then on file count, so the order
// is stable across runs rather than map-iteration dependent.
func Rank(
	dl Download,
	candidates []Candidate,
	priority func(providerID int64) int,
	prefs AutoDownloadPrefs,
) []Candidate {
	out := make([]Candidate, 0, len(candidates))

	for _, c := range candidates {
		p := 50
		if priority != nil {
			p = priority(c.ProviderID)
		}

		out = append(out, Score(dl, c, p, prefs))
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}

		if out[i].Match.Overall != out[j].Match.Overall {
			return out[i].Match.Overall > out[j].Match.Overall
		}

		if out[i].Quality.Priority != out[j].Quality.Priority {
			return out[i].Quality.Priority > out[j].Quality.Priority
		}

		return len(out[i].Files) > len(out[j].Files)
	})

	return out
}

// AutoPickable reports whether a ranked list has a clear enough winner
// to grab without asking.  It demands an anchored request, a high match,
// decent quality, and daylight between first and second place — if two
// candidates are close, the choice is the user's.
func AutoPickable(dl Download, ranked []Candidate, prefs AutoDownloadPrefs) bool {
	const (
		minMatch   = 0.85
		minQuality = 0.5
		minLead    = 0.08
	)

	if !dl.Anchored() || len(ranked) == 0 {
		return false
	}

	// An anchor with no tracklist behind it is an anchor in name only:
	// the match score then rests on album and artist text alone, which
	// is exactly the evidence a wrong-album candidate also has.  This
	// matters most for the request list, where nobody is watching.
	if len(dl.Expected) == 0 {
		return false
	}

	// The guardrails apply before the match/quality/lead checks: a
	// candidate outside the allowed size or format is not a worse
	// choice, it is not a choice auto-pick may make at all, so it must
	// not count as "the winner" nor as "second place" for the lead
	// check below.
	eligible := prefs.filter(ranked)
	if len(eligible) == 0 {
		return false
	}

	best := eligible[0]
	if best.Match.Overall < minMatch || best.Quality.Overall < minQuality {
		return false
	}

	if len(eligible) > 1 && best.Score-eligible[1].Score < minLead {
		return false
	}

	return true
}

// mergeMatched copies MatchedTo assignments from the audio-only slice
// back onto the full file list.
func mergeMatched(all, matched []CandidateFile) []CandidateFile {
	if len(matched) == 0 {
		return all
	}

	byPath := make(map[string]int, len(matched))
	for _, m := range matched {
		byPath[m.Path] = m.MatchedTo
	}

	out := make([]CandidateFile, len(all))
	copy(out, all)

	for i := range out {
		if pos, ok := byPath[out[i].Path]; ok {
			out[i].MatchedTo = pos
		}
	}

	return out
}

// clamp01 bounds a value to 0..1.
func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}
