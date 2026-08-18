package download

import (
	"fmt"
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

// Quality sub-weights.  Each set sums to 1.0.
//
// There are two of them because a stated preference changes what the
// other numbers are *for*.  `formatRank` and `bitrateScore` are the
// app guessing at how good a copy is — FLAC over MP3, 320 over 128 —
// and that guess exists precisely because the user has not said.  Once
// they have, the guess should not outvote them: with the old single set
// a preference of 320 kbps moved a candidate's score by at most 0.05
// against the 0.42 riding on format, so asking for 320 and being handed
// a FLAC every time was the *designed* behaviour.  That is the same
// fault the megabyte window had — a preference the user can express and
// the ranking can ignore.
const (
	weightFormat     = 0.42
	weightBitrate    = 0.23
	weightHealth     = 0.20
	weightPriority   = 0.10
	weightBitrateFit = 0.05
)

// Quality sub-weights when the user has named a preferred bitrate.
// The weight comes off format and bitrate — the two proxies the
// preference replaces — and health and priority are untouched, since
// neither is a stand-in for anything the user just said.
const (
	statedWeightFormat     = 0.20
	statedWeightBitrate    = 0.10
	statedWeightHealth     = 0.20
	statedWeightPriority   = 0.10
	statedWeightBitrateFit = 0.40
)

// qualityWeights picks the set, in the order scoreQuality applies them.
func qualityWeights(p AutoDownloadPrefs) (
	format, bitrate, health, priority, fit float64,
) {
	if p.PreferredKbps > 0 {
		return statedWeightFormat,
			statedWeightBitrate,
			statedWeightHealth,
			statedWeightPriority,
			statedWeightBitrateFit
	}

	return weightFormat,
		weightBitrate,
		weightHealth,
		weightPriority,
		weightBitrateFit
}

// unanchoredCap bounds the match score of a free-text request.  Without
// an MBID there is no tracklist to be right about, so a confident-
// looking score would be a lie — and auto-pick keys off this.
const unanchoredCap = 0.65

// AutoDownloadPrefs gates and scores what AutoPickable may choose
// without asking.  Zero values are permissive: no bitrate window, no
// size ceiling and no format restriction.
//
// **The window is a rate, not a size.**  It used to be three numbers in
// megabytes, which cannot mean anything on their own: 300 MB is a
// generous FLAC single and a suspiciously small boxset, and the user
// setting the number has no idea which release the pipeline will
// eventually apply it to.  A bitrate is the same statement normalised
// by how long the music is, so one number holds across a 9-minute EP
// and a 3-hour opera — and it is the unit the thing being described is
// actually measured in.  The runtime is known for every request
// auto-pick can act on (`Download.Expected` carries per-track lengths,
// and an anchored request is the only kind that reaches here), so this
// costs no extra lookup.
type AutoDownloadPrefs struct {
	// MinKbps and MaxKbps bound the average bitrate auto-pick will
	// grab.  Zero means no bound on that side.  A candidate outside the
	// window is filtered out of auto-pick entirely, not merely scored
	// down — a 96 kbps rip of the right album is not a worse copy the
	// user might accept, it is one they said not to take unattended.
	//
	// For reference: 320 is the top of MP3, ~500–1000 is FLAC depending
	// on the material, and anything under ~128 is a transcode.
	MinKbps int `json:"minKbps"`
	MaxKbps int `json:"maxKbps"`

	// PreferredKbps nudges the score toward a target rate within the
	// window, and breaks the tie when several candidates are equally
	// good matches.  Zero disables the nudge; bitrateFit then returns a
	// neutral value that does not affect ranking.
	PreferredKbps int `json:"preferredKbps"`

	// MaxSizeMB is a hard ceiling on the whole candidate, and it is
	// deliberately still a size.  It answers a different question from
	// the window above — not "is this the quality I want" but "is this
	// going to fill the disk" — and it has to hold even for a candidate
	// whose bitrate cannot be worked out, which is exactly the shape a
	// mislabelled boxset arrives in.  Zero means no ceiling.
	MaxSizeMB int `json:"maxSizeMb"`

	// AllowedFormats restricts auto-pick to candidates whose audio
	// files are all in one of these formats.  Empty means no
	// restriction.
	AllowedFormats []Format `json:"allowedFormats"`
}

// eligible reports whether a candidate may be auto-picked under these
// preferences: inside the bitrate window and the size ceiling (when
// set) and, when a format list is given, every audio file in an
// allowed format.
//
// `runtimeMillis` is how long the requested release is, and 0 means
// nobody knows.  An unknown runtime **passes** the bitrate window
// rather than failing it: the window is a statement about quality, and
// refusing everything the moment a tracklist is missing a length would
// turn a gap in MusicBrainz into a silent embargo.  The size ceiling
// still applies, which is why it exists separately.
func (p AutoDownloadPrefs) eligible(c Candidate, runtimeMillis int64) bool {
	const bytesPerMB = 1 << 20

	if p.MaxSizeMB > 0 && c.TotalSize > int64(p.MaxSizeMB)*bytesPerMB {
		return false
	}

	if kbps := candidateKbps(c, runtimeMillis); kbps > 0 {
		if p.MinKbps > 0 && kbps < float64(p.MinKbps) {
			return false
		}

		if p.MaxKbps > 0 && kbps > float64(p.MaxKbps) {
			return false
		}
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
func (p AutoDownloadPrefs) filter(
	ranked []Candidate,
	runtimeMillis int64,
) []Candidate {
	out := make([]Candidate, 0, len(ranked))

	for _, c := range ranked {
		if p.eligible(c, runtimeMillis) {
			out = append(out, c)
		}
	}

	return out
}

// bitrateFit scores how close a candidate's average bitrate is to
// PreferredKbps, falling off linearly as it doubles or halves away
// from it.
//
// The range is **0.5 to 1.0, not 0 to 1**, and the floor is the point.
// This carries 0.40 of the quality score once a preference is set, so a
// span down to zero would let a preference of 320 kbps push a perfectly
// good FLAC under `minQuality` and out of auto-pick altogether —
// turning "I like 320" into "never take anything else", silently.  A
// preference may promote the copy that matches it; it may not
// disqualify the others.  That is what `MinKbps`/`MaxKbps` are for, and
// they say so out loud.
//
// Returns the neutral floor when no preference is set or the rate
// cannot be worked out, so neither an absent preference nor an absent
// runtime biases ranking.
func (p AutoDownloadPrefs) bitrateFit(
	c Candidate,
	runtimeMillis int64,
) float64 {
	const (
		neutral = 0.5
		span    = 0.5
	)

	if p.PreferredKbps <= 0 {
		return neutral
	}

	kbps := candidateKbps(c, runtimeMillis)
	if kbps <= 0 {
		return neutral
	}

	ratio := kbps / float64(p.PreferredKbps)
	if ratio < 1 {
		ratio = 1 / ratio
	}

	// ratio is now >= 1: 1.0 is an exact match, 2.0 is double or half
	// the preferred rate, where the closeness term reaches 0.
	return neutral + span*clamp01(1-(ratio-1))
}

// candidateKbps is a candidate's average audio bitrate, or 0 when it
// cannot be worked out.
//
// Two sources, in this order, and the order matters:
//
//   - **Derived from bytes over runtime**, which is the honest one. It
//     covers lossless (where a stated bitrate rarely exists), it cannot
//     be lied to by a filename, and it is what the user's window means.
//     Only the *audio* files count: cover scans and a log file are not
//     part of the bitrate, and a folder with 30 MB of artwork would
//     otherwise read as a better rip than the same music without it.
//   - **The mean stated bitrate**, when the runtime is unknown. Weaker
//     — a provider that parses it from an MP3 header states it and one
//     that guesses from the filename also "states" it — but a number
//     from the file itself beats no number at all.
func candidateKbps(c Candidate, runtimeMillis int64) float64 {
	const bitsPerByte = 8

	audio := c.AudioFiles()
	if len(audio) == 0 {
		return 0
	}

	if runtimeMillis > 0 {
		var bytes int64
		for _, f := range audio {
			bytes += f.Size
		}

		if bytes > 0 {
			// bytes×8 bits over seconds, expressed in kbps: the two
			// factors of 1000 (millis→seconds, bits→kilobits) cancel.
			return float64(bytes) * bitsPerByte /
				float64(runtimeMillis)
		}
	}

	var (
		sum   int
		count int
	)

	for _, f := range audio {
		if f.Bitrate > 0 {
			sum += f.Bitrate
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return float64(sum) / float64(count)
}

// runtimeMillis is how long the requested release is, summed over its
// expected tracklist.  Zero when the tracklist is absent or carries no
// lengths, which is what every caller here treats as "unknown".
func (d Download) runtimeMillis() int64 {
	var total int64
	for _, t := range d.Expected {
		total += t.LengthMillis
	}

	return total
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
	c.Quality = scoreQuality(
		c, audio, priority, prefs, dl.runtimeMillis(),
	)

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
	runtimeMillis int64,
) QualityScore {
	q := QualityScore{
		Health:     clamp01(c.Health),
		Priority:   clamp01(float64(priority) / 100.0),
		BitrateFit: prefs.bitrateFit(c, runtimeMillis),
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

	wFormat, wBitrate, wHealth, wPriority, wFit := qualityWeights(prefs)

	q.Overall = wFormat*q.FormatRank +
		wBitrate*q.Bitrate +
		wHealth*q.Health +
		wPriority*q.Priority +
		wFit*q.BitrateFit

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

		// Closest to the preferred bitrate wins the tie.
		//
		// This is what decides which copy is taken now that auto-pick
		// no longer requires the winner to be clear of the field: when
		// several candidates are equally good matches of equal overall
		// quality, the one the user said they wanted the shape of is
		// the answer, ahead of provider priority.  With no preference
		// set every BitrateFit is the same neutral value and this
		// falls through, exactly as before.
		if out[i].Quality.BitrateFit != out[j].Quality.BitrateFit {
			return out[i].Quality.BitrateFit > out[j].Quality.BitrateFit
		}

		if out[i].Quality.Priority != out[j].Quality.Priority {
			return out[i].Quality.Priority > out[j].Quality.Priority
		}

		return len(out[i].Files) > len(out[j].Files)
	})

	return out
}

// Auto-pick gates.  Named rather than inlined because AutoPickVeto
// reports which of them refused, and a number in a sentence the user
// reads should be the same number the decision used.
const (
	minMatch   = 0.85
	minQuality = 0.5
)

// AutoPickable reports whether a ranked list has a candidate worth
// grabbing without asking: an anchored request with a tracklist behind
// it, and a candidate that clears the match and quality bars inside the
// user's guardrails.
//
// **It does not require the winner to be better than the runner-up.**
// It used to demand 0.08 of daylight on the combined score, which meant
// the check fired hardest in the case it was never written for: a
// popular album turns up five *correct* copies, all matching the
// tracklist at 95%+ and differing only in format and seeders, their
// scores land within a point of each other, and auto-pick refused
// forever on the grounds that the choice was the user's.  It was not.
// There was no question about *what* to fetch, only about which copy —
// and abundance is the one condition under which that question matters
// least.  A candidate does not need to be the best one, only one that
// meets the criteria; where several do, `Rank` puts the one closest to
// the preferred bitrate first.
func AutoPickable(dl Download, ranked []Candidate, prefs AutoDownloadPrefs) bool {
	return AutoPickVeto(dl, ranked, prefs) == ""
}

// AutoPickVeto returns the reason auto-pick declined, or "" when it
// would go ahead.
//
// It exists because "it rejected all of them" was indistinguishable
// from "it found nothing good".  The request list's message was built
// from `ranked[0]` — the best candidate *before* the size and format
// guardrails, and before the lead check — so a request refused because
// the user's maximum size excluded every copy, or because three equally
// good copies were found, reported "best of 12 found is not a confident
// enough match (match 96%, quality 88%)".  Numbers that clear both
// thresholds, beside a refusal, is a message that teaches the user the
// matcher is broken.  Each gate names itself now.
func AutoPickVeto(
	dl Download,
	ranked []Candidate,
	prefs AutoDownloadPrefs,
) string {
	if len(ranked) == 0 {
		return "nothing found"
	}

	if !dl.Anchored() {
		return "the request is free text, so there is no release to be right about"
	}

	// An anchor with no tracklist behind it is an anchor in name only:
	// the match score then rests on album and artist text alone, which
	// is exactly the evidence a wrong-album candidate also has.  This
	// matters most for the request list, where nobody is watching.
	if len(dl.Expected) == 0 {
		return "no tracklist for this release is known yet, so a candidate cannot be checked against it"
	}

	// The guardrails apply before the match and quality checks: a
	// candidate outside the allowed bitrate, size or format is not a
	// worse choice, it is not a choice auto-pick may make at all, so it
	// must not count as "the winner" either.
	eligible := prefs.filter(ranked, dl.runtimeMillis())
	if len(eligible) == 0 {
		return fmt.Sprintf(
			"all %d found are outside the auto-download bitrate, size or format limits",
			len(ranked),
		)
	}

	best := eligible[0]

	if best.Match.Overall < minMatch {
		return fmt.Sprintf(
			"best of %d found matches this release only %.0f%% (needs %.0f%%)",
			len(ranked),
			best.Match.Overall*100, //nolint:mnd // percent
			minMatch*100,           //nolint:mnd // percent
		)
	}

	if best.Quality.Overall < minQuality {
		return fmt.Sprintf(
			"best of %d found is the right release but scores %.0f%% on quality (needs %.0f%%)",
			len(ranked),
			best.Quality.Overall*100, //nolint:mnd // percent
			minQuality*100,           //nolint:mnd // percent
		)
	}

	return ""
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
