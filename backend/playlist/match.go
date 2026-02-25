// Package playlist provides playlist management functionality.
package playlist

import (
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Scoring weights for candidate matching.
const (
	weightFilename   = 0.50
	weightTitle      = 0.30
	weightDuration   = 0.10
	weightPathDirs   = 0.10
	autoMatchMinimum = 0.85
)

// maxCandidates is the default limit for search results.
const maxCandidates = 20

// maxLibrarySearchResults is the limit for manual library search.
const maxLibrarySearchResults = 50

// durationToleranceClose is the duration difference in seconds
// considered a near-exact match.
const durationToleranceClose = 1

// durationToleranceMedium is the medium tolerance threshold.
const durationToleranceMedium = 5

// durationToleranceFar is the maximum tolerance before scoring
// drops to zero.
const durationToleranceFar = 15

// separatorPattern splits file paths and names on common
// separators: slashes, hyphens, underscores, spaces, dots.
var separatorPattern = regexp.MustCompile(
	`[/\\\-_. ]+`,
)

// trackNumberPattern matches leading track numbers like
// "01", "1", "01.", "01 -", etc.
var trackNumberPattern = regexp.MustCompile(
	`^\d{1,3}[.\-\s]*$`,
)

// phantomProfile pre-computes all derived data for a phantom
// track so that scoring multiple candidates avoids redundant
// string processing.
type phantomProfile struct {
	baseLower   string   // lowercase basename
	baseStem    string   // basename without extension
	baseWords   []string // significant words from stem
	dirWords    []string // significant words from dir path
	displayLow  string   // lowercase display title
	parsedArt   string   // parsed artist from display title
	parsedTitle string   // parsed title from display title
	titleWords  []string // significant words from display title
	durationSec int      // phantom duration in seconds
}

// newPhantomProfile builds a phantomProfile from raw phantom
// data, performing all string splits and normalisation once.
func newPhantomProfile(
	phantomPath string,
	displayTitle string,
	durationSec int,
) phantomProfile {
	baseLower := strings.ToLower(
		filepath.Base(phantomPath),
	)
	baseStem := stripExtension(baseLower)
	displayLow := strings.ToLower(
		strings.TrimSpace(displayTitle),
	)
	parsedArt, parsedTitle := parseDisplayTitle(displayLow)

	return phantomProfile{
		baseLower:   baseLower,
		baseStem:    baseStem,
		baseWords:   significantWords(baseStem),
		dirWords:    pathDirWords(phantomPath),
		displayLow:  displayLow,
		parsedArt:   parsedArt,
		parsedTitle: parsedTitle,
		titleWords:  significantWords(displayLow),
		durationSec: durationSec,
	}
}

// scoreCandidate computes a match confidence (0.0-1.0) between
// a phantom track and a candidate library track.
func scoreCandidate(
	pp phantomProfile,
	candidatePath string,
	candidateTitle string,
	candidateArtist string,
	candidateDurationMs int64,
) float64 {
	fnScore := scoreFilename(pp, candidatePath)
	titleScore := scoreTitleArtist(
		pp, candidateTitle, candidateArtist,
	)
	durScore := scoreDuration(
		pp.durationSec, candidateDurationMs,
	)
	dirScore := scorePathDirs(pp, candidatePath)

	// If duration is unknown, redistribute its weight to
	// filename.
	fnWeight := weightFilename
	durWeight := weightDuration

	if pp.durationSec == 0 {
		fnWeight += durWeight
		durWeight = 0
	}

	return fnScore*fnWeight +
		titleScore*weightTitle +
		durScore*durWeight +
		dirScore*weightPathDirs
}

// scoreFilename compares the basenames of two file paths.
func scoreFilename(
	pp phantomProfile, candidatePath string,
) float64 {
	cBase := strings.ToLower(
		filepath.Base(candidatePath),
	)

	// Exact basename match.
	if pp.baseLower == cBase {
		return 1.0
	}

	// Match ignoring extension.
	cStem := stripExtension(cBase)

	if pp.baseStem == cStem {
		return 0.8
	}

	// Check if all significant words from phantom stem appear
	// in candidate stem.
	cWords := significantWords(cStem)

	if len(pp.baseWords) == 0 {
		return 0.0
	}

	return keywordOverlap(pp.baseWords, cWords)
}

// scoreTitleArtist compares the phantom's EXTINF display title
// against the candidate's DB title and artist fields.
func scoreTitleArtist(
	pp phantomProfile,
	candidateTitle, candidateArtist string,
) float64 {
	if pp.displayLow == "" {
		return 0.0
	}

	candidateTitle = strings.ToLower(
		strings.TrimSpace(candidateTitle),
	)
	candidateArtist = strings.ToLower(
		strings.TrimSpace(candidateArtist),
	)

	// Exact title match.
	if pp.parsedTitle != "" &&
		pp.parsedTitle == candidateTitle {
		if pp.parsedArt != "" &&
			pp.parsedArt == candidateArtist {
			return 1.0
		}

		return 0.8
	}

	// Keyword overlap between display title and combined
	// candidate metadata.
	combined := candidateTitle + " " + candidateArtist
	cWords := significantWords(combined)

	if len(pp.titleWords) == 0 {
		return 0.0
	}

	return keywordOverlap(pp.titleWords, cWords)
}

// scoreDuration computes a score based on duration proximity.
func scoreDuration(
	phantomSec int, candidateMs int64,
) float64 {
	if phantomSec == 0 || candidateMs == 0 {
		return 0.0
	}

	diff := math.Abs(
		float64(phantomSec) - float64(candidateMs)/1000.0,
	)

	switch {
	case diff <= float64(durationToleranceClose):
		return 1.0
	case diff <= float64(durationToleranceMedium):
		return 0.8
	case diff <= float64(durationToleranceFar):
		return 0.5
	default:
		return 0.0
	}
}

// scorePathDirs compares the directory components of two paths.
func scorePathDirs(
	pp phantomProfile, candidatePath string,
) float64 {
	if len(pp.dirWords) == 0 {
		return 0.0
	}

	cDirs := pathDirWords(candidatePath)

	return keywordOverlap(pp.dirWords, cDirs)
}

// parseDisplayTitle splits an EXTINF display title on " - " into
// (artist, title). If no separator is found, returns ("", full).
func parseDisplayTitle(dt string) (artist, title string) {
	idx := strings.Index(dt, " - ")
	if idx < 0 {
		return "", dt
	}

	return strings.TrimSpace(dt[:idx]),
		strings.TrimSpace(dt[idx+3:])
}

// extractKeywords extracts meaningful search keywords from a file
// path by splitting on separators, removing track numbers, common
// noise words, and the file extension.
func extractKeywords(filePath string) []string {
	// Remove extension.
	stem := stripExtension(filePath)

	// Split on separators.
	parts := separatorPattern.Split(stem, -1)

	var keywords []string

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Skip pure track numbers.
		if trackNumberPattern.MatchString(p) {
			continue
		}

		// Skip very short tokens.
		if len(p) < 2 {
			continue
		}

		keywords = append(keywords, strings.ToLower(p))
	}

	return dedupStrings(keywords)
}

// significantWords extracts meaningful lowercase words from a
// string, filtering out noise.
func significantWords(s string) []string {
	parts := separatorPattern.Split(s, -1)

	var words []string

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Skip pure track numbers.
		if trackNumberPattern.MatchString(p) {
			continue
		}

		// Skip single characters.
		if countRunes(p) < 2 {
			continue
		}

		words = append(words, strings.ToLower(p))
	}

	return words
}

// pathDirWords extracts lowercase words from the directory
// portion of a path (excluding the filename).
func pathDirWords(filePath string) []string {
	dir := filepath.Dir(filePath)
	if dir == "." || dir == "/" {
		return nil
	}

	return significantWords(dir)
}

// keywordOverlap calculates the proportion of source words that
// appear in target words (Jaccard-like, asymmetric).
func keywordOverlap(source, target []string) float64 {
	if len(source) == 0 {
		return 0.0
	}

	targetSet := make(map[string]struct{}, len(target))

	for _, w := range target {
		targetSet[w] = struct{}{}
	}

	var matches int

	for _, w := range source {
		if _, ok := targetSet[w]; ok {
			matches++
		}
	}

	return float64(matches) / float64(len(source))
}

// stripExtension removes the file extension from a path or
// filename.
func stripExtension(s string) string {
	ext := filepath.Ext(s)
	if ext == "" {
		return s
	}

	return s[:len(s)-len(ext)]
}

// dedupStrings removes duplicate strings, preserving order.
func dedupStrings(ss []string) []string {
	seen := make(map[string]struct{}, len(ss))

	var result []string

	for _, s := range ss {
		if _, ok := seen[s]; ok {
			continue
		}

		seen[s] = struct{}{}

		result = append(result, s)
	}

	return result
}

// countRunes returns the number of runes in a string.
func countRunes(s string) int {
	return utf8.RuneCountInString(s)
}
