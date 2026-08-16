//go:build indexbuild

package explore

import (
	"archive/tar"
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// Stages 2+3 of the dump import.  Stage 2 picks per-entity popularity
// floors from the aggregated listen counts (top-N by listen count,
// with an absolute minimum).  Stage 3 streams the MusicBrainz
// canonical dump (~2GB tar.zst of CSVs) and keeps only rows above the
// floors, assembling them directly into explore_index.  Nothing except
// the final index rows touches disk; the canonical stream is cheap
// enough to simply restart after an interruption.

const (
	// Entity budgets: the floors are chosen as "the listen count of
	// the Nth most-listened entity", clamped to minListenFloor.
	keepRecordings   = 1_500_000
	keepReleases     = 2_000_000
	keepArtists      = 250_000
	keepReleaseGroup = 400_000

	// minListenFloor cuts long-tail noise even when a budget isn't
	// reached (small dumps, sparse entities).
	minListenFloor = 10

	// releaseMinListenFloor is the (lower) floor for releases, which
	// are not indexed themselves but roll up into release groups.
	releaseMinListenFloor = 5

	// dumpAssembleBatch is the number of index rows per upsert
	// transaction during assembly.
	dumpAssembleBatch = 2000

	// canonicalProgressRows controls progress reporting during the
	// canonical CSV scan (~30M rows total).
	canonicalProgressRows = 2_000_000

	// maxReleaseTracks caps the per-release track count, so a malformed
	// row cannot turn the denominator into nonsense.  Well above the
	// longest real release, and it is a cap rather than a rejection
	// because a box set reporting 999 is still a better answer than one
	// reporting nothing.
	maxReleaseTracks = 999
)

// Per-artist discography coverage (S2).  The global budgets above keep
// the most-listened entities overall; on their own they leave a
// moderately popular artist with little or nothing on their detail page,
// forcing a slow live ListenBrainz fetch on first view.  S2 guarantees
// each browse-likely artist a graded slice of their own top tracks and
// release groups, selected offline from the same dump — no API calls.
const (
	// perArtistArtistBudget is how many top artists (by listen count)
	// receive graded per-artist coverage.  Library artists are always
	// covered in full regardless of rank (see markLibraryArtists).
	perArtistArtistBudget = 10_000

	// Rank boundaries (1-based) within the top perArtistArtistBudget
	// artists.  Tier A is the most-listened; everything from tier B's
	// boundary down to perArtistArtistBudget is tier C.
	perArtistTierA = 1_000
	perArtistTierB = 4_000

	// Per-tier track/release-group budgets.  These are caps that count
	// globally-kept entities toward the total, so a superstar whose
	// catalogue is already in the global set adds ~nothing.
	perArtistTierATrack = 50
	perArtistTierBTrack = 25
	perArtistTierCTrack = 12
	perArtistTierARG    = 15
	perArtistTierBRG    = 10
	perArtistTierCRG    = 5

	// Library artists get effectively their full discography, capped to
	// bound memory for pathologically prolific credits.
	perArtistLibraryTrack = 500
	perArtistLibraryRG    = 100

	// keepRecordingsSecondary bounds the recording listen counts retained
	// through the canonical scan for per-artist selection (a superset of
	// the global kept set).  Set well above keepRecordings so a top-10K
	// artist's deep cuts still qualify as candidates.
	keepRecordingsSecondary = 5_000_000
)

// uuid16 is a parsed MBID.
type uuid16 [16]byte

// keptSets holds the entities that survived the popularity threshold,
// with their listen counts.
type keptSets struct {
	recordings map[uuid16]uint32
	releases   map[uuid16]uint32
	artists    map[uuid16]uint32

	recFloor uint32
	relFloor uint32
	artFloor uint32

	// Per-artist coverage (S2).  recSecondary holds recording listen
	// counts down to a lower floor than recordings (a superset of it),
	// so a target artist's sub-global-floor tracks stay selectable.
	// trackBudget/rgBudget give the per-artist top-N cap for each browse
	// target (top perArtistArtistBudget artists by listens, plus every
	// library artist).
	recSecondary map[uuid16]uint32
	trackBudget  map[uuid16]int
	rgBudget     map[uuid16]int
}

// computeThreshold picks per-entity floors and builds the kept sets.
// This is the "statistical analysis" step: listen counts follow a
// power law, so budget-based cutoffs (keep the top N) adapt to the
// actual distribution instead of hardcoding a magic listen count.
func (imp *dumpImporter) computeThreshold(counts map[mbidKey]uint32) *keptSets {
	var recVals, relVals, artVals []uint32

	var recTotal, relTotal, artTotal uint64

	for k, v := range counts {
		switch k[0] {
		case countKindRecording:
			recVals = append(recVals, v)
			recTotal += uint64(v)
		case countKindRelease:
			relVals = append(relVals, v)
			relTotal += uint64(v)
		case countKindArtist:
			artVals = append(artVals, v)
			artTotal += uint64(v)
		}
	}

	ks := &keptSets{
		recFloor: floorForBudget(recVals, keepRecordings, minListenFloor),
		relFloor: floorForBudget(relVals, keepReleases, releaseMinListenFloor),
		artFloor: floorForBudget(artVals, keepArtists, minListenFloor),

		recordings: make(map[uuid16]uint32, keepRecordings),
		releases:   make(map[uuid16]uint32, keepReleases),
		artists:    make(map[uuid16]uint32, keepArtists),

		recSecondary: make(map[uuid16]uint32, keepRecordingsSecondary),
		trackBudget:  make(map[uuid16]int),
		rgBudget:     make(map[uuid16]int),
	}

	// Per-artist coverage (S2): retain recording counts down to a lower
	// secondary floor, and derive rank-based artist tier floors so each
	// of the top perArtistArtistBudget artists gets a graded track/RG
	// budget.  artVals is sorted descending so rank N is at index N-1.
	recSecFloor := floorForBudget(recVals, keepRecordingsSecondary, minListenFloor)

	sort.Slice(artVals, func(i, j int) bool { return artVals[i] > artVals[j] })
	artTierAFloor := rankFloor(artVals, perArtistTierA)
	artTierBFloor := rankFloor(artVals, perArtistTierB)
	artTierCFloor := rankFloor(artVals, perArtistArtistBudget)

	var recKept, relKept, artKept uint64

	for k, v := range counts {
		var id uuid16

		copy(id[:], k[1:])

		switch {
		case k[0] == countKindRecording && v >= ks.recFloor:
			ks.recordings[id] = v
			ks.recSecondary[id] = v
			recKept += uint64(v)
		case k[0] == countKindRecording && v >= recSecFloor:
			ks.recSecondary[id] = v
		case k[0] == countKindRelease && v >= ks.relFloor:
			ks.releases[id] = v
			relKept += uint64(v)
		case k[0] == countKindArtist && v >= ks.artFloor:
			ks.artists[id] = v
			artKept += uint64(v)

			if v >= artTierCFloor {
				ks.trackBudget[id], ks.rgBudget[id] = tierBudget(
					v, artTierAFloor, artTierBFloor,
				)
			}
		}
	}

	// Library artists are always covered in full — override any rank tier
	// and include those below the global artist floor.
	imp.markLibraryArtists(ks)

	imp.logger.Info("dump import: popularity thresholds chosen",
		"recordingFloor", ks.recFloor,
		"recordings", len(ks.recordings),
		"recordingCoverage", coveragePct(recKept, recTotal),
		"releaseFloor", ks.relFloor,
		"releases", len(ks.releases),
		"artistFloor", ks.artFloor,
		"artists", len(ks.artists),
		"artistCoverage", coveragePct(artKept, artTotal),
		"secondaryFloor", recSecFloor,
		"secondaryRecordings", len(ks.recSecondary),
		"targetArtists", len(ks.trackBudget),
	)

	return ks
}

// rankFloor returns the listen count at the given 1-based rank in a slice
// already sorted in descending order, clamped to the slice length.  Used
// to turn artist-rank tier boundaries into concrete listen-count floors
// that adapt to each dump's distribution.
func rankFloor(sortedDesc []uint32, rank int) uint32 {
	if len(sortedDesc) == 0 {
		return 0
	}

	if rank > len(sortedDesc) {
		rank = len(sortedDesc)
	}

	return sortedDesc[rank-1]
}

// tierBudget maps an artist's listen count to its per-artist track and
// release-group budgets via the rank-derived tier floors.
func tierBudget(listens, tierAFloor, tierBFloor uint32) (int, int) {
	switch {
	case listens >= tierAFloor:
		return perArtistTierATrack, perArtistTierARG
	case listens >= tierBFloor:
		return perArtistTierBTrack, perArtistTierBRG
	default:
		return perArtistTierCTrack, perArtistTierCRG
	}
}

// markLibraryArtists grants full per-artist coverage to every library
// artist, overriding any rank-based tier and including artists below the
// global artist floor so their discography is covered even when globally
// obscure.
func (imp *dumpImporter) markLibraryArtists(ks *keptSets) {
	n := 0

	for _, s := range imp.si.getLibraryArtistMBIDs() {
		var id uuid16

		if !parseUUID(s, id[:]) {
			continue
		}

		ks.trackBudget[id] = perArtistLibraryTrack
		ks.rgBudget[id] = perArtistLibraryRG
		n++
	}

	imp.logger.Info("dump import: library artists granted full coverage", "artists", n)
}

// floorForBudget returns the listen count of the budget-th largest
// value (so keeping everything >= floor yields ~budget entries),
// clamped below by minFloor.
func floorForBudget(vals []uint32, budget int, minFloor uint32) uint32 {
	if len(vals) == 0 {
		return minFloor
	}

	if len(vals) <= budget {
		return minFloor
	}

	sort.Slice(vals, func(i, j int) bool { return vals[i] > vals[j] })

	floor := vals[budget-1]
	if floor < minFloor {
		floor = minFloor
	}

	return floor
}

func coveragePct(kept, total uint64) string {
	if total == 0 {
		return "0%"
	}

	return fmt.Sprintf("%.1f%%", float64(kept)/float64(total)*100)
}

// ---------------------------------------------------------------------------
// Canonical dump scan
// ---------------------------------------------------------------------------

// keptRecordingRow is a canonical-dump row that survived the filter.
type keptRecordingRow struct {
	mbid        uuid16
	name        string
	artistName  string
	artistMBID  string
	releaseMBID uuid16
	releaseName string
	listens     uint32
}

// releaseInfo carries the display fields for a kept release, used to
// title its release group.
type releaseInfo struct {
	name       string
	artistName string
	artistMBID string
}

// rgTarget is a release's redirect target.
type rgTarget struct {
	rg        uuid16
	canonical uuid16
}

// canonicalScan is the in-RAM result of streaming the canonical dump.
type canonicalScan struct {
	recordings   []keptRecordingRow
	releaseInfos map[uuid16]releaseInfo
	releaseToRG  map[uuid16]rgTarget
	artistNames  map[uuid16]string

	// releaseTracks counts the canonical dump's rows per kept release,
	// which is that release's track count: the dump carries one row per
	// recording per canonical release.
	//
	// It is counted *before* the popularity filter below, unlike almost
	// everything else here, because a denominator built from the kept
	// recordings would say "9" about a twelve-track album whose other
	// three are unpopular - which is worse than saying nothing, and is
	// exactly the confident lie that kept whole tracklists out of the
	// artifact.  Bounded by the kept release set, not by MusicBrainz.
	releaseTracks map[uuid16]uint16

	// artistTracks accumulates each target artist's top recordings for
	// S2 coverage; merged into recordings before assembly.
	artistTracks *perArtistTracks
}

// perArtistTracks holds, per target artist, that artist's top recordings
// by listen count.  Populated during the canonical scan.
type perArtistTracks struct {
	byArtist map[uuid16]*artistTopN
}

func newPerArtistTracks() *perArtistTracks {
	return &perArtistTracks{byArtist: make(map[uuid16]*artistTopN)}
}

func (p *perArtistTracks) add(artist uuid16, budget int, row keptRecordingRow) {
	a := p.byArtist[artist]
	if a == nil {
		a = &artistTopN{n: budget, inSet: make(map[uuid16]struct{}, budget)}
		p.byArtist[artist] = a
	}

	a.add(row)
}

// artistTopN keeps the top-n recordings for a single artist by listen
// count, deduplicated by recording MBID.  n is small (tens to a few
// hundred), so a linear min-scan on eviction is cheaper than the overhead
// of container/heap.
type artistTopN struct {
	n     int
	rows  []keptRecordingRow
	inSet map[uuid16]struct{}
}

func (a *artistTopN) add(row keptRecordingRow) {
	if _, dup := a.inSet[row.mbid]; dup {
		return
	}

	if len(a.rows) < a.n {
		a.rows = append(a.rows, row)
		a.inSet[row.mbid] = struct{}{}

		return
	}

	minIdx := 0

	for i := 1; i < len(a.rows); i++ {
		if a.rows[i].listens < a.rows[minIdx].listens {
			minIdx = i
		}
	}

	if row.listens <= a.rows[minIdx].listens {
		return
	}

	delete(a.inSet, a.rows[minIdx].mbid)
	a.rows[minIdx] = row
	a.inSet[row.mbid] = struct{}{}
}

// rgCandidate is a release group offered to an artist's top-K selection.
type rgCandidate struct {
	rg      uuid16
	listens uint32
}

// artistTopRG keeps the top-n release groups for a single artist by
// rolled-up listen count, deduplicated by release-group MBID.
type artistTopRG struct {
	n     int
	rgs   []rgCandidate
	inSet map[uuid16]struct{}
}

func (a *artistTopRG) add(rg uuid16, listens uint32) {
	if _, dup := a.inSet[rg]; dup {
		return
	}

	if len(a.rgs) < a.n {
		a.rgs = append(a.rgs, rgCandidate{rg: rg, listens: listens})
		a.inSet[rg] = struct{}{}

		return
	}

	minIdx := 0

	for i := 1; i < len(a.rgs); i++ {
		if a.rgs[i].listens < a.rgs[minIdx].listens {
			minIdx = i
		}
	}

	if listens <= a.rgs[minIdx].listens {
		return
	}

	delete(a.inSet, a.rgs[minIdx].rg)
	a.rgs[minIdx] = rgCandidate{rg: rg, listens: listens}
	a.inSet[rg] = struct{}{}
}

// scanCanonicalDump streams the canonical dump and filters it against
// the kept sets.  Member order inside the tar doesn't matter — the two
// CSVs populate independent maps that are joined during assembly.
func (imp *dumpImporter) scanCanonicalDump(
	ctx context.Context, url string, ks *keptSets,
) (*canonicalScan, error) {
	stream := imp.openDumpStream(ctx, url, 0)

	defer func() { _ = stream.Close() }()

	zr, err := zstd.NewReader(bufio.NewReaderSize(stream, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("canonical zstd: %w", err)
	}

	defer zr.Close()

	scan := &canonicalScan{
		releaseInfos:  make(map[uuid16]releaseInfo, len(ks.releases)),
		releaseToRG:   make(map[uuid16]rgTarget, len(ks.releases)),
		artistNames:   make(map[uuid16]string, len(ks.artists)),
		releaseTracks: make(map[uuid16]uint16, len(ks.releases)),
		artistTracks:  newPerArtistTracks(),
	}

	sawData, sawRedirect := false, false
	tr := tar.NewReader(zr)

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("canonical tar: %w", err)
		}

		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		var member io.Reader = tr

		// Individual members may themselves be zstd-compressed.
		if strings.HasSuffix(hdr.Name, ".zst") {
			mzr, err := zstd.NewReader(member)
			if err != nil {
				return nil, fmt.Errorf("canonical member zstd: %w", err)
			}

			member = mzr.IOReadCloser()
		}

		switch {
		case strings.Contains(hdr.Name, "canonical_musicbrainz_data.csv"):
			if err := imp.scanCanonicalData(ctx, member, ks, scan); err != nil {
				return nil, err
			}

			sawData = true
		case strings.Contains(hdr.Name, "canonical_release_redirect.csv"):
			if err := imp.scanReleaseRedirect(ctx, member, ks, scan); err != nil {
				return nil, err
			}

			sawRedirect = true
		}
	}

	if !sawData || !sawRedirect {
		return nil, fmt.Errorf("%w: canonical dump missing expected CSVs (data=%t redirect=%t)",
			ErrDumpFormat, sawData, sawRedirect)
	}

	return scan, nil
}

// canonicalDataColumns maps the columns of canonical_musicbrainz_data.csv.
type canonicalDataColumns struct {
	artistMBIDs      int
	artistCreditName int
	releaseMBID      int
	releaseName      int
	recordingMBID    int
	recordingName    int
}

// defaultCanonicalDataColumns matches the documented column order.
func defaultCanonicalDataColumns() canonicalDataColumns {
	return canonicalDataColumns{
		artistMBIDs:      2,
		artistCreditName: 3,
		releaseMBID:      4,
		releaseName:      5,
		recordingMBID:    6,
		recordingName:    7,
	}
}

func (imp *dumpImporter) scanCanonicalData(
	ctx context.Context, r io.Reader, ks *keptSets, scan *canonicalScan,
) error {
	cr := csv.NewReader(bufio.NewReaderSize(r, 1<<20))
	cr.ReuseRecord = true
	cr.FieldsPerRecord = -1

	cols := defaultCanonicalDataColumns()
	rows := 0

	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("canonical data csv: %w", err)
		}

		rows++

		if rows == 1 && looksLikeHeader(rec) {
			cols = headerColumns(rec, cols)

			continue
		}

		if rows%canonicalProgressRows == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}

			imp.logger.Info("dump import: canonical scan progress",
				"rows", rows,
				"kept", len(scan.recordings),
			)
			imp.setStageProgress(dumpStageCatalog, rows, 0)
		}

		maxCol := cols.recordingName
		if cols.recordingMBID > maxCol {
			maxCol = cols.recordingMBID
		}

		if len(rec) <= maxCol {
			continue
		}

		var recMBID uuid16

		if !parseUUID(rec[cols.recordingMBID], recMBID[:]) {
			continue
		}

		var relMBID uuid16

		relOK := parseUUID(rec[cols.releaseMBID], relMBID[:])

		artistMBIDs := parsePGStringArray(rec[cols.artistMBIDs])
		creditName := rec[cols.artistCreditName]

		// Artist names: a single-artist credit names that artist.
		if len(artistMBIDs) == 1 {
			var artMBID uuid16

			if parseUUID(artistMBIDs[0], artMBID[:]) {
				if _, kept := ks.artists[artMBID]; kept {
					if _, seen := scan.artistNames[artMBID]; !seen {
						scan.artistNames[artMBID] = creditName
					}
				}
			}
		}

		// Release display info for release-group titling, and the
		// release's track count.  Both are for kept releases only, and
		// the count is taken here rather than below because every row
		// of this dump is one track of its release regardless of how
		// often anyone played it.
		if relOK {
			if _, kept := ks.releases[relMBID]; kept {
				if n := scan.releaseTracks[relMBID]; n < maxReleaseTracks {
					scan.releaseTracks[relMBID] = n + 1
				}

				if _, seen := scan.releaseInfos[relMBID]; !seen {
					firstArtist := ""
					if len(artistMBIDs) > 0 {
						firstArtist = artistMBIDs[0]
					}

					scan.releaseInfos[relMBID] = releaseInfo{
						name:       rec[cols.releaseName],
						artistName: creditName,
						artistMBID: firstArtist,
					}
				}
			}
		}

		globalListens, keptGlobal := ks.recordings[recMBID]
		secListens, keptSecondary := ks.recSecondary[recMBID]

		// Below even the secondary floor: neither globally kept nor a
		// per-artist candidate, so there's nothing to do.
		if !keptGlobal && !keptSecondary {
			continue
		}

		firstArtist := ""
		if len(artistMBIDs) > 0 {
			firstArtist = artistMBIDs[0]
		}

		// Build the row once (globalListens == secListens for global-kept
		// recordings, since the global set is a subset of the secondary).
		row := keptRecordingRow{
			mbid:        recMBID,
			name:        strings.Clone(rec[cols.recordingName]),
			artistName:  strings.Clone(creditName),
			artistMBID:  firstArtist,
			releaseMBID: relMBID,
			releaseName: strings.Clone(rec[cols.releaseName]),
			listens:     secListens,
		}

		// S2: offer this recording to its primary artist's top-N when
		// that artist is a browse target.
		if keptSecondary && firstArtist != "" {
			var faID uuid16

			if parseUUID(firstArtist, faID[:]) {
				if budget, target := ks.trackBudget[faID]; target {
					scan.artistTracks.add(faID, budget, row)
				}
			}
		}

		// Global-kept recordings are written unconditionally.
		if keptGlobal {
			row.listens = globalListens
			scan.recordings = append(scan.recordings, row)
		}
	}

	imp.logger.Info("dump import: canonical data scanned",
		"rows", rows,
		"keptRecordings", len(scan.recordings),
		"artistNames", len(scan.artistNames),
	)

	return nil
}

func (imp *dumpImporter) scanReleaseRedirect(
	ctx context.Context, r io.Reader, ks *keptSets, scan *canonicalScan,
) error {
	cr := csv.NewReader(bufio.NewReaderSize(r, 1<<20))
	cr.ReuseRecord = true
	cr.FieldsPerRecord = -1

	// Documented order: release_mbid, canonical_release_mbid,
	// release_group_mbid.
	relCol, canonCol, rgCol := 0, 1, 2
	rows := 0

	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("release redirect csv: %w", err)
		}

		rows++

		if rows == 1 && looksLikeHeader(rec) {
			for i, name := range rec {
				switch strings.TrimSpace(name) {
				case "release_mbid":
					relCol = i
				case "canonical_release_mbid":
					canonCol = i
				case "release_group_mbid":
					rgCol = i
				}
			}

			continue
		}

		if rows%canonicalProgressRows == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}

		if len(rec) <= rgCol || len(rec) <= relCol || len(rec) <= canonCol {
			continue
		}

		var rel uuid16

		if !parseUUID(rec[relCol], rel[:]) {
			continue
		}

		if _, kept := ks.releases[rel]; !kept {
			continue
		}

		var target rgTarget

		if !parseUUID(rec[rgCol], target.rg[:]) {
			continue
		}

		if !parseUUID(rec[canonCol], target.canonical[:]) {
			target.canonical = rel
		}

		scan.releaseToRG[rel] = target
	}

	imp.logger.Info("dump import: release redirects scanned",
		"rows", rows,
		"keptReleases", len(scan.releaseToRG),
	)

	return nil
}

// looksLikeHeader reports whether a CSV record is a header row (no
// parseable UUIDs, contains a known column name).
func looksLikeHeader(rec []string) bool {
	for _, f := range rec {
		switch strings.TrimSpace(f) {
		case "recording_mbid", "release_mbid", "artist_mbids", "release_group_mbid":
			return true
		}
	}

	return false
}

// headerColumns resolves column indexes from a header row, falling
// back to the documented defaults for any missing name.
func headerColumns(rec []string, fallback canonicalDataColumns) canonicalDataColumns {
	cols := fallback

	for i, name := range rec {
		switch strings.TrimSpace(name) {
		case "artist_mbids":
			cols.artistMBIDs = i
		case "artist_credit_name":
			cols.artistCreditName = i
		case "release_mbid":
			cols.releaseMBID = i
		case "release_name":
			cols.releaseName = i
		case "recording_mbid":
			cols.recordingMBID = i
		case "recording_name":
			cols.recordingName = i
		}
	}

	return cols
}

// parsePGStringArray parses the artist_mbids CSV field, tolerating
// Postgres array syntax ({a,b}), JSON-ish lists (['a', 'b']), and bare
// single values.
func parsePGStringArray(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '{' && last == '}') || (first == '[' && last == ']') {
			s = s[1 : len(s)-1]
		}
	}

	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))

	for _, p := range parts {
		p = strings.Trim(strings.TrimSpace(p), `"'`)
		if p != "" {
			out = append(out, p)
		}
	}

	return out
}

// ---------------------------------------------------------------------------
// Assembly into explore_index
// ---------------------------------------------------------------------------

// assembleIndex writes the filtered catalog into explore_index via the
// standard upsert path (idempotent — safe to re-run after a crash).
func (imp *dumpImporter) assembleIndex(
	ctx context.Context, ks *keptSets, scan *canonicalScan,
) error {
	batch := make([]SearchIndexResult, 0, dumpAssembleBatch)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		imp.si.upsertBatch(batch)
		batch = batch[:0]

		return nil
	}

	// S2: fold each target artist's top recordings into the kept set.
	imp.mergePerArtistTracks(ks, scan)

	// Recordings.
	written := 0

	for _, row := range scan.recordings {
		caa := row.releaseMBID
		if target, ok := scan.releaseToRG[row.releaseMBID]; ok {
			caa = target.canonical
		}

		batch = append(batch, SearchIndexResult{
			EntityType:     "recording",
			MBID:           formatUUID(row.mbid[:]),
			Title:          row.name,
			ArtistName:     row.artistName,
			ArtistMBID:     row.artistMBID,
			Popularity:     int(row.listens),
			ReleaseName:    row.releaseName,
			CAAReleaseMBID: formatUUID(caa[:]),
		})

		written++

		if len(batch) >= dumpAssembleBatch {
			if err := flush(); err != nil {
				return err
			}

			if written%500_000 == 0 {
				imp.logger.Info("dump import: assembling recordings", "written", written)
				imp.setStageProgress(dumpStageCatalog, written, len(scan.recordings))
			}
		}
	}

	if err := flush(); err != nil {
		return err
	}

	// Release groups: roll release listen counts up to the redirect
	// target, keep the top keepReleaseGroup, and title each group
	// after its most-listened kept release.
	type rgAgg struct {
		listens   uint32
		bestRel   uuid16
		bestCnt   uint32
		canonical uuid16
	}

	rgs := make(map[uuid16]*rgAgg, len(scan.releaseToRG))

	for rel, target := range scan.releaseToRG {
		cnt := ks.releases[rel]

		agg := rgs[target.rg]
		if agg == nil {
			agg = &rgAgg{}
			rgs[target.rg] = agg
		}

		agg.listens += cnt

		if cnt >= agg.bestCnt {
			agg.bestCnt = cnt
			agg.bestRel = rel
			agg.canonical = target.canonical
		}
	}

	rgFloorVals := make([]uint32, 0, len(rgs))
	for _, agg := range rgs {
		rgFloorVals = append(rgFloorVals, agg.listens)
	}

	rgFloor := floorForBudget(rgFloorVals, keepReleaseGroup, releaseMinListenFloor)

	// S2: keep each target artist's top release groups even below the
	// global RG floor.  Attributed via the best release's artist credit.
	keepRG := make(map[uuid16]struct{})

	rgPickers := make(map[uuid16]*artistTopRG)

	for rg, agg := range rgs {
		info, ok := scan.releaseInfos[agg.bestRel]
		if !ok {
			continue
		}

		var aID uuid16

		if !parseUUID(info.artistMBID, aID[:]) {
			continue
		}

		budget, target := ks.rgBudget[aID]
		if !target {
			continue
		}

		p := rgPickers[aID]
		if p == nil {
			p = &artistTopRG{n: budget, inSet: make(map[uuid16]struct{}, budget)}
			rgPickers[aID] = p
		}

		p.add(rg, agg.listens)
	}

	for _, p := range rgPickers {
		for _, c := range p.rgs {
			keepRG[c.rg] = struct{}{}
		}
	}

	rgWritten := 0
	rgFromPerArtist := 0

	for rg, agg := range rgs {
		if agg.listens < rgFloor {
			if _, keep := keepRG[rg]; !keep {
				continue
			}

			rgFromPerArtist++
		}

		info, ok := scan.releaseInfos[agg.bestRel]
		if !ok {
			// No kept release carries display info for this group.
			continue
		}

		batch = append(batch, SearchIndexResult{
			EntityType:     "release_group",
			MBID:           formatUUID(rg[:]),
			Title:          info.name,
			ArtistName:     info.artistName,
			ArtistMBID:     info.artistMBID,
			Popularity:     int(agg.listens),
			TotalTracks:    int(scan.releaseTracks[agg.bestRel]),
			CAAReleaseMBID: formatUUID(agg.canonical[:]),
		})

		rgWritten++

		if len(batch) >= dumpAssembleBatch {
			if err := flush(); err != nil {
				return err
			}
		}
	}

	if err := flush(); err != nil {
		return err
	}

	// Artists (only those whose name is derivable from a solo credit;
	// the metadata patch pass fills the rest from ListenBrainz).
	artWritten := 0

	for mbid, name := range scan.artistNames {
		batch = append(batch, SearchIndexResult{
			EntityType: "artist",
			MBID:       formatUUID(mbid[:]),
			Title:      name,
			ArtistName: name,
			ArtistMBID: formatUUID(mbid[:]),
			Popularity: int(ks.artists[mbid]),
		})

		artWritten++

		if len(batch) >= dumpAssembleBatch {
			if err := flush(); err != nil {
				return err
			}
		}
	}

	if err := flush(); err != nil {
		return err
	}

	imp.logger.Info("dump import: index assembled",
		"recordings", written,
		"releaseGroups", rgWritten,
		"releaseGroupsPerArtist", rgFromPerArtist,
		"artists", artWritten,
	)

	return nil
}

// mergePerArtistTracks folds the S2 per-artist top-N recordings into the
// kept recordings, skipping any already covered by the global floor so no
// row is written twice (upsert dedupes by MBID as a backstop anyway).
func (imp *dumpImporter) mergePerArtistTracks(ks *keptSets, scan *canonicalScan) {
	added := 0

	for _, a := range scan.artistTracks.byArtist {
		for _, row := range a.rows {
			if _, global := ks.recordings[row.mbid]; global {
				continue
			}

			scan.recordings = append(scan.recordings, row)
			added++
		}
	}

	imp.logger.Info("dump import: per-artist track coverage",
		"targetArtists", len(scan.artistTracks.byArtist),
		"recordingsAdded", added,
	)
}
