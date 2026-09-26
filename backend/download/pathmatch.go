package download

import (
	"path"
	"regexp"
	"strconv"
	"strings"

	"yellowjacket/backend/autotag"
)

// Candidate files arrive as paths, not tags — a Soulseek result is
// `@@abc\Music\Pink Floyd - The Wall (1979) [FLAC]\1-01 In The Flesh.flac`
// and nothing more.  Everything the ranker knows about whether a
// candidate is the right album comes from parsing that string, so the
// heuristics here carry real weight.

// audioExtensions maps a lowercase file extension to its format.
var audioExtensions = map[string]Format{
	".flac": FormatFLAC,
	".mp3":  FormatMP3,
	".ogg":  FormatOGG,
	".oga":  FormatOGG,
	".opus": FormatOpus,
	".wav":  FormatWAV,
	".m4a":  FormatAAC,
	".aac":  FormatAAC,
	".alac": FormatALAC,
	".wma":  FormatWMA,
	".ape":  FormatUnknown,
	".wv":   FormatUnknown,
}

var (
	// trackNumPattern matches a leading track number in the common
	// shapes: "01 - Title", "1. Title", "1-01 Title" (disc-track),
	// "[01] Title".  The disc group is optional.
	trackNumPattern = regexp.MustCompile(
		`^\s*\[?(?:(\d{1,2})\s*[-_.]\s*)?(\d{1,3})\]?\s*[-_.)\]]?\s+`,
	)

	// bareTrackNumPattern matches a number with no separator at all
	// ("01Title" is rare, but "01 Title" with a single space is not).
	bareTrackNumPattern = regexp.MustCompile(`^\s*(\d{1,3})\s+`)

	// bitratePattern finds a bitrate hint in a folder or file name:
	// "[320]", "V0", "320kbps", "(V2)".
	bitratePattern = regexp.MustCompile(
		`(?i)\b(\d{2,4})\s*k(?:bps|b/s)?\b|\[(\d{2,4})\]`,
	)

	// vbrPattern finds LAME VBR preset names, which imply a bitrate
	// band rather than a number.
	vbrPattern = regexp.MustCompile(`(?i)\b(V[0-2])\b`)

	// yearPattern finds a 4-digit year in parentheses or brackets.
	yearPattern = regexp.MustCompile(`[(\[](19|20)\d{2}[)\]]`)

	// junkSuffixPattern strips scene/rip tags from a folder name before
	// comparing it to an album title.
	junkSuffixPattern = regexp.MustCompile(
		`(?i)[\[(]\s*(flac|mp3|web|cd|vinyl|24bit|16bit|lossless|` +
			`v0|v2|320|256|192|128|kbps|reissue|remaster(ed)?|` +
			`\d{2,3}\s*k(bps)?)\s*[^\])]*[\])]`,
	)

	// separatorPattern splits "Artist - Album" style folder names.
	separatorPattern = regexp.MustCompile(`\s+[-–—]\s+`)

	// discFolderPattern matches a directory that holds one disc of an
	// album rather than the album: "CD1", "CD 2", "Disc 3", "Disk-1",
	// "[Disc 2]", "CD1 - The Early Years".  A number is required, so a
	// folder merely called "CDs" is not one.
	discFolderPattern = regexp.MustCompile(
		`(?i)^\s*[\[(]?\s*(?:cd|disc|disk)\s*[-_.#]?\s*(\d{1,2})\b`,
	)
)

// FormatForPath returns the audio format implied by a path's extension,
// and whether the path is audio at all.  Cue sheets, logs, playlists
// and cover images are not.
func FormatForPath(p string) (Format, bool) {
	ext := strings.ToLower(path.Ext(strings.ReplaceAll(p, `\`, "/")))

	f, ok := audioExtensions[ext]

	return f, ok
}

// TrackHint is what a single candidate file's path reveals about the
// track it holds.  Every field is best-effort and may be zero.
type TrackHint struct {
	Disc  int
	Track int

	// Title is the filename with the extension, track number and any
	// leading artist credit removed.
	Title string

	// Folder is the immediate parent directory name, cleaned of scene
	// tags — the best available proxy for the album title.
	Folder string
}

// discFolder reports whether a directory name is one disc of an album,
// and which.
func discFolder(name string) (int, bool) {
	m := discFolderPattern.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}

	n, err := strconv.Atoi(m[1])
	if err != nil || n == 0 {
		return 0, false
	}

	return n, true
}

// AlbumDir is the directory that holds a file's *album*: its parent,
// or its grandparent when the parent is a disc folder.
//
// Multi-disc rips are shared as `Album/CD1/…` and `Album/CD2/…`, and
// grouping candidates by the immediate parent split one album into two
// half-albums, each titled "CD1".  Neither could clear the completeness
// or album-title bars, so a multi-disc release could not be auto-picked
// at all.  A disc folder at the root has no album above it and is
// returned as it is.
func AlbumDir(p string) string {
	dir := path.Dir(strings.ReplaceAll(p, `\`, "/"))

	if _, ok := discFolder(path.Base(dir)); !ok {
		return dir
	}

	parent := path.Dir(dir)
	if parent == "." || parent == "/" || parent == "" {
		return dir
	}

	return parent
}

// ParsePath extracts what it can from one candidate file path.
func ParsePath(p string) TrackHint {
	// Soulseek paths are Windows-style; normalize before splitting.
	norm := strings.ReplaceAll(p, `\`, "/")
	base := path.Base(norm)

	name := strings.TrimSuffix(base, path.Ext(base))

	// The album's name is the album directory's, not a disc folder's,
	// and the disc folder is where a multi-disc rip says which disc a
	// file is on.  A disc number in the filename ("2-01 …") is more
	// specific and overrides it below.
	hint := TrackHint{Folder: cleanAlbumName(path.Base(AlbumDir(norm)))}

	if disc, ok := discFolder(path.Base(path.Dir(norm))); ok {
		hint.Disc = disc
	}

	if m := trackNumPattern.FindStringSubmatch(name); m != nil {
		if m[1] != "" {
			hint.Disc, _ = strconv.Atoi(m[1])
		}

		hint.Track, _ = strconv.Atoi(m[2])
		name = name[len(m[0]):]
	} else if m := bareTrackNumPattern.FindStringSubmatch(name); m != nil {
		hint.Track, _ = strconv.Atoi(m[1])
		name = name[len(m[0]):]
	}

	// "Artist - Title" inside the filename: drop the leading credit
	// when what follows is substantial.  Guessing wrong here costs a
	// little title similarity; not doing it costs a lot, because most
	// Soulseek folders name the artist in every file.
	if parts := separatorPattern.Split(name, 2); len(parts) == 2 {
		if len(strings.TrimSpace(parts[1])) >= 3 {
			name = parts[1]
		}
	}

	hint.Title = strings.TrimSpace(name)

	return hint
}

// cleanAlbumName strips year markers and scene tags from a folder name
// so it can be compared against a release title.
func cleanAlbumName(folder string) string {
	s := junkSuffixPattern.ReplaceAllString(folder, " ")
	s = yearPattern.ReplaceAllString(s, " ")

	// A folder is often "Artist - Album"; keep the right-hand side when
	// there is one, since the album is what we compare against.
	if parts := separatorPattern.Split(s, 2); len(parts) == 2 {
		if len(strings.TrimSpace(parts[1])) >= 2 {
			s = parts[1]
		}
	}

	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// BitrateForPath infers a bitrate in kbps from path text.  Returns 0
// when nothing is stated.  VBR presets map to their nominal average.
func BitrateForPath(p string) int {
	if m := vbrPattern.FindStringSubmatch(p); m != nil {
		switch strings.ToUpper(m[1]) {
		case "V0":
			return 245
		case "V1":
			return 225
		case "V2":
			return 190
		}
	}

	if m := bitratePattern.FindStringSubmatch(p); m != nil {
		raw := m[1]
		if raw == "" {
			raw = m[2]
		}

		if n, err := strconv.Atoi(raw); err == nil && n >= 32 && n <= 3000 {
			return n
		}
	}

	return 0
}

// AnnotateFiles fills in Format, IsAudio and Bitrate for a candidate's
// files.  Providers call this so each adapter does not re-derive the
// same things from the same paths.
func AnnotateFiles(files []CandidateFile) []CandidateFile {
	out := make([]CandidateFile, len(files))

	for i, f := range files {
		format, isAudio := FormatForPath(f.Path)

		f.IsAudio = isAudio
		if f.Format == FormatUnknown {
			f.Format = format
		}

		if f.Bitrate == 0 {
			f.Bitrate = BitrateForPath(f.Path)
		}

		out[i] = f
	}

	return out
}

// matchFiles aligns a candidate's audio files to the expected tracklist
// and returns the per-file assignment plus the mean title similarity of
// the aligned pairs.  alignFiles is the same alignment with the
// duration evidence as well.
func matchFiles(
	files []CandidateFile,
	expected []ExpectedTrack,
) ([]CandidateFile, float64) {
	a := alignFiles(files, expected)

	return a.files, a.titleFit
}

// alignment is what aligning a candidate to a tracklist found.
type alignment struct {
	files []CandidateFile

	// titleFit is the mean title similarity over aligned pairs.
	titleFit float64

	// durationFit is the mean duration agreement over aligned pairs
	// where both sides state a length, and timedPairs is how many such
	// pairs there were.
	durationFit float64
	timedPairs  int
	aligned     int
}

// durationAgreement scores how well a file's length matches the
// expected track's, in 0..1.  Rips of the same master differ by a
// second or two of silence; a different edit, a live take or a
// truncated file differs by tens of seconds.
func durationAgreement(got, want int64) float64 {
	const (
		exactMillis = 3_000
		wrongMillis = 30_000
	)

	d := got - want
	if d < 0 {
		d = -d
	}

	switch {
	case d <= exactMillis:
		return 1
	case d >= wrongMillis:
		return 0
	default:
		return 1 - float64(d-exactMillis)/float64(wrongMillis-exactMillis)
	}
}

// alignFiles aligns a candidate's audio files to the expected tracklist.
//
// Alignment is greedy by score rather than optimal: candidate folders
// are small (a few dozen files at most) and the common cases — correct
// track numbers, or clean "NN Title" names — are unambiguous, so the
// extra machinery of Hungarian assignment buys nothing here.
func alignFiles(
	files []CandidateFile,
	expected []ExpectedTrack,
) alignment {
	annotated := make([]CandidateFile, len(files))
	copy(annotated, files)

	if len(expected) == 0 {
		return alignment{files: annotated}
	}

	hints := make([]TrackHint, len(annotated))
	for i, f := range annotated {
		hints[i] = ParsePath(f.Path)
	}

	takenExpected := make(map[int]bool, len(expected))

	var (
		total   float64
		matched int

		durTotal float64
		timed    int
	)

	// timing adds a pair's duration evidence when both sides state one.
	timing := func(f CandidateFile, e ExpectedTrack) {
		if f.LengthMillis > 0 && e.LengthMillis > 0 {
			durTotal += durationAgreement(f.LengthMillis, e.LengthMillis)
			timed++
		}
	}

	// Pass 1: trust explicit track numbers when they are unique and in
	// range.  A folder that numbers its files correctly is the strong
	// case, and title comparison only adds noise there.
	for i := range annotated {
		if !annotated[i].IsAudio || hints[i].Track == 0 {
			continue
		}

		idx := indexForPosition(expected, hints[i].Disc, hints[i].Track)
		if idx < 0 || takenExpected[idx] {
			continue
		}

		takenExpected[idx] = true
		annotated[i].MatchedTo = expected[idx].Position

		total += autotag.TitleSimilarity(hints[i].Title, expected[idx].Title)
		matched++

		timing(annotated[i], expected[idx])
	}

	// Pass 2: title similarity for whatever is left.
	for i := range annotated {
		if !annotated[i].IsAudio || annotated[i].MatchedTo != 0 {
			continue
		}

		bestIdx, bestSim := -1, 0.0

		for j := range expected {
			if takenExpected[j] {
				continue
			}

			sim := autotag.TitleSimilarity(hints[i].Title, expected[j].Title)
			if sim > bestSim {
				bestIdx, bestSim = j, sim
			}
		}

		// Below this the "match" is two unrelated strings sharing a few
		// characters, and counting it drags the mean toward noise.
		const minTitleSim = 0.55

		if bestIdx < 0 || bestSim < minTitleSim {
			continue
		}

		takenExpected[bestIdx] = true
		annotated[i].MatchedTo = expected[bestIdx].Position

		total += bestSim
		matched++

		timing(annotated[i], expected[bestIdx])
	}

	if matched == 0 {
		return alignment{files: annotated}
	}

	a := alignment{
		files:      annotated,
		titleFit:   total / float64(matched),
		timedPairs: timed,
		aligned:    matched,
	}

	if timed > 0 {
		a.durationFit = durTotal / float64(timed)
	}

	return a
}

// indexForPosition finds the expected track at a disc/track position.
// A zero disc hint matches on track number alone, which is right for
// single-disc releases and the best guess for multi-disc folders that
// do not encode the disc.
func indexForPosition(expected []ExpectedTrack, disc, track int) int {
	for i, e := range expected {
		if e.Position != track {
			continue
		}

		if disc != 0 && e.DiscNumber != 0 && e.DiscNumber != disc {
			continue
		}

		return i
	}

	return -1
}
