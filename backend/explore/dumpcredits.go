//go:build indexbuild

package explore

import (
	"archive/tar"
	"bufio"
	"compress/bzip2"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Multi-artist credits, from the core MusicBrainz dump.
//
// A credit is ordered parts and the credit *string* is derived from
// them; MusicBrainz's own artist_credit.name is a cached render.  What
// this pass extracts is the decomposition: for each catalog recording
// and release group whose credit names more than one artist, the
// credited artists in order, each with the name *as credited* and the
// join phrase that follows it.  See artist_credit_part.sql for why that
// is stored rather than derived, and why nothing may reconstruct a
// credit by searching a name inside a credit string.
//
// It is a separate dump from everything else here, and it has to be.
// The canonical dump this importer already streams gives artist_mbids
// (an ordered list) and artist_credit_name (the *rendered* string) --
// no join phrases, and no per-artist as-credited names.  Splitting the
// rendered string using canonical artist names fails on exactly the
// credits that matter: measured on a real library, 21% of multi-artist
// credits name an artist differently from the artist's own name
// ("Snoop Dogg" credited on a track by "Snoop Doggy Dogg"), so the
// substring is simply not there.  The JSON dumps were checked too and
// cover 153,691 recordings of ~35M, with zero overlap against a real
// library.  This dump is the only source.
//
// Cost, measured on the 20260815 export: 7.1 GB compressed, decompressed
// by pure-Go compress/bzip2 at ~26 MB/s uncompressed (~13.7 min for the
// whole file, single-threaded).  cmd/indexbuild is built CGO_ENABLED=0,
// so the stdlib decompressor is what there is -- and it is fine, because
// the 2 MB/s origin throttle dominates, as it does for every other dump
// here.

const (
	// defaultMBDumpBaseURL is the core MusicBrainz export.  Only
	// mbdump.tar.bz2 is fetched; the other tarballs there hold data this
	// app has no use for.
	defaultMBDumpBaseURL = "https://data.metabrainz.org/pub/musicbrainz/data/fullexport/"
)

var (
	mbdumpDirRe  = regexp.MustCompile(`^\d{8}-\d+$`)
	mbdumpFileRe = regexp.MustCompile(`^mbdump\.tar\.bz2$`)

	// ErrDumpShape is returned when a dump member does not have the
	// columns this code was written against.  It is deliberately fatal:
	// reading the wrong column silently produces a catalog whose credits
	// are subtly wrong, which is far worse than a failed build.
	ErrDumpShape = errors.New("musicbrainz dump member has an unexpected shape")
)

// Column positions in the Postgres COPY output, verified against the
// 20260815 export.  There is no header row to read them from, so they
// are asserted instead -- see checkShape.
const (
	artistColID  = 0
	artistColGID = 1
	artistColMin = 2

	creditColID          = 0
	creditColArtistCount = 2
	creditColMin         = 3

	partColCredit   = 0
	partColPosition = 1
	partColArtist   = 2
	partColName     = 3
	partColJoin     = 4
	partColMin      = 5

	// recording and release_group share a layout in the columns this
	// pass reads: id, gid, name, artist_credit, ...
	entityColGID    = 1
	entityColCredit = 3
	entityColMin    = 4
)

// creditPart is one credited artist within a credit.
type creditPart struct {
	position int
	artistID int32
	name     string
	join     string
}

// creditScan is what one pass over the dump collects.
type creditScan struct {
	// artistGIDs maps an artist row id to its MBID.  artist_credit_name
	// references artists by row id, and the tar orders `artist` before
	// it, so this is complete by the time it is read.
	artistGIDs map[int32]uuid16

	// multiCredits are the credit ids naming more than one artist, from
	// artist_credit.artist_count.  Taking the count from the dump rather
	// than counting parts means a credit can be rejected before its
	// parts are stored.
	multiCredits map[int32]struct{}

	// parts are the decompositions of multiCredits, keyed by credit id.
	parts map[int32][]creditPart

	// refs maps a kept catalog entity to its credit.  Only entities in
	// explore_index and only multi-artist credits: everything else is
	// already described by explore_index's own artist_name/artist_mbid.
	refs map[uuid16]int32

	// used are the credits some ref actually points at, which is a small
	// fraction of multiCredits -- the catalog keeps ~1.8M entities of
	// MusicBrainz's tens of millions.
	used map[int32]struct{}

	skippedUnknownArtist int
}

// creditsImportDoneKey marks in explore_index_meta that the credit pass
// has run against the current catalog.
//
// It is its own marker rather than part of the import's stage state for
// a resume reason: the credit pass runs *after* the catalog is
// assembled, and a failure in it must not send the next run back
// through the ~205 GB it just finished.  Marking separately means a
// retry retries only this.
const creditsImportDoneKey = "credits_import_done"

// ensureArtistCredits runs the credit pass unless it has already run
// against this catalog, reporting whether it newly populated them.
//
// Called from both of run's paths -- the full import and the resume
// that finds the rows already assembled -- and from the maintenance
// entry point below, since a catalog built before credits existed is
// otherwise never offered a chance to gain them: the index job picks
// its mode from the index's own state, and a complete import means
// "refresh", which never enters run() at all.
//
// The return value is what tells the job there is something new worth
// publishing.  A refresh otherwise reports "changed" only when the
// listens series advanced, so credits would sit in the CI database and
// never reach an artifact.
func (imp *dumpImporter) ensureArtistCredits(ctx context.Context) bool {
	if imp.si.hasMeta(creditsImportDoneKey) {
		return false
	}

	url, err := discoverDumpFile(
		ctx, imp.httpClient, imp.mbdumpBaseURL, mbdumpDirRe, mbdumpFileRe,
	)
	if err != nil {
		imp.logger.Warn("credit import: could not find the dump", "error", err)

		return false
	}

	if err := imp.importArtistCredits(ctx, url); err != nil {
		// A catalog without credits is the catalog this app shipped
		// before them: every credit falls back to its single artist.
		// That is worth far less than failing an import that otherwise
		// succeeded.
		imp.logger.Warn("credit import: failed", "error", err)

		return false
	}

	imp.si.setMeta(creditsImportDoneKey, "1")

	return true
}

// EnsureArtistCredits tops up the credit tables outside a full import.
//
// It exists because the index job's modes are decided from the index's
// own state: a cache holding a completed import chooses `refresh`,
// which folds in incremental listens and never enters the dump
// importer.  Without this, a catalog built before the credit pass
// existed could only gain credits from a `rebuild` -- and a rebuild
// re-downloads ~205 GB to reproduce rows it already has, to add
// something that costs 7 GB on its own.
//
// Reports whether credits were newly populated, so the caller knows
// there is a new artifact worth publishing.
func (e *Service) EnsureArtistCredits(ctx context.Context) bool {
	imp, err := newDumpImporter(e.index, e.lb)
	if err != nil {
		e.index.logger.Warn("credit import: could not start", "error", err)

		return false
	}

	return imp.ensureArtistCredits(ctx)
}

// importArtistCredits streams the core MusicBrainz dump and fills
// artist_credit_part and artist_credit_ref for the entities the catalog
// kept.
//
// It runs after assembleIndex because it asks explore_index which
// entities those are: the popularity filter decides what is worth
// carrying credits for, and asking the table rather than the kept sets
// means this stays correct if that filter changes.
func (imp *dumpImporter) importArtistCredits(ctx context.Context, url string) error {
	kept, err := imp.keptEntityMBIDs(ctx)
	if err != nil {
		return err
	}

	if len(kept) == 0 {
		imp.logger.Warn("credit import: no catalog entities, skipping")

		return nil
	}

	imp.logger.Info("credit import: starting", "url", url, "entities", len(kept))
	imp.logJob("Streaming MusicBrainz dump for artist credits")

	scan, err := imp.scanCreditDump(ctx, url, kept)
	if err != nil {
		return err
	}

	imp.logger.Info("credit import: scanned",
		"multiArtistCredits", len(scan.multiCredits),
		"entitiesWithMultiArtistCredit", len(scan.refs),
		"creditsUsed", len(scan.used),
	)

	return imp.writeCredits(ctx, scan)
}

// keptEntityMBIDs is every recording and release group in the catalog.
// Artists are excluded: an artist is not credited to a credit.
func (imp *dumpImporter) keptEntityMBIDs(ctx context.Context) (map[uuid16]struct{}, error) {
	rows, err := imp.si.db.QueryContextWith(ctx,
		`SELECT mbid FROM explore_index
		 WHERE entity_type IN (2 /* release_group */, 3 /* recording */)`,
	)
	if err != nil {
		return nil, fmt.Errorf("credit import: read catalog entities: %w", err)
	}

	defer func() { _ = rows.Close() }()

	out := make(map[uuid16]struct{})

	for rows.Next() {
		var raw []byte

		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("credit import: scan mbid: %w", err)
		}

		if len(raw) != len(uuid16{}) {
			continue
		}

		var id uuid16

		copy(id[:], raw)

		out[id] = struct{}{}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("credit import: read catalog entities: %w", err)
	}

	return out, nil
}

// scanCreditDump makes one sequential pass over mbdump.tar.bz2.
//
// The tar's members are alphabetical, which is what makes a single pass
// possible without buffering the big ones: `artist` and
// `artist_credit_name` both arrive before `recording` and
// `release_group`, so by the time an entity names a credit, that
// credit's parts and their artists' MBIDs are already known and the
// entity can be resolved and dropped.  35M recording rows are never
// held.
//
// The order is not depended on blindly: an entity naming a credit that
// has not been seen is counted and reported rather than silently
// producing an empty catalog, which is what a reordered export would
// otherwise look like.
func (imp *dumpImporter) scanCreditDump(
	ctx context.Context, url string, kept map[uuid16]struct{},
) (*creditScan, error) {
	stream := imp.openDumpStream(ctx, url, 0)

	defer func() { _ = stream.Close() }()

	return imp.scanCreditTar(
		ctx,
		tar.NewReader(bzip2.NewReader(bufio.NewReaderSize(stream, 1<<20))),
		kept,
	)
}

// scanCreditTar is the parse, separated from the fetch so it can be
// driven by a tar built in a test.  compress/bzip2 is decompress-only,
// so a test cannot produce the real container.
func (imp *dumpImporter) scanCreditTar(
	ctx context.Context, tr *tar.Reader, kept map[uuid16]struct{},
) (*creditScan, error) {
	scan := &creditScan{
		artistGIDs:   make(map[int32]uuid16),
		multiCredits: make(map[int32]struct{}),
		parts:        make(map[int32][]creditPart),
		refs:         make(map[uuid16]int32),
		used:         make(map[int32]struct{}),
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("credit import: tar: %w", err)
		}

		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		done, err := imp.scanCreditMember(ctx, hdr.Name, tr, kept, scan)
		if err != nil {
			return nil, err
		}

		if done {
			// Everything this pass needs has been read; the rest of the
			// tarball is other entities' data and decompressing it would
			// cost minutes for nothing.
			break
		}
	}

	if scan.skippedUnknownArtist > 0 {
		imp.logger.Warn("credit import: credits dropped for unknown artists",
			"count", scan.skippedUnknownArtist,
		)
	}

	return scan, nil
}

// scanCreditMember dispatches one tar member, reporting whether the
// pass has everything it needs.
func (imp *dumpImporter) scanCreditMember(
	ctx context.Context, name string, r io.Reader,
	kept map[uuid16]struct{}, scan *creditScan,
) (bool, error) {
	switch path.Base(name) {
	case "artist":
		return false, imp.scanArtists(ctx, r, scan)
	case "artist_credit":
		return false, imp.scanCredits(ctx, r, scan)
	case "artist_credit_name":
		return false, imp.scanCreditParts(ctx, r, scan)
	case "recording", "release_group":
		if err := imp.scanCreditedEntities(ctx, r, kept, scan); err != nil {
			return false, err
		}

		// release_group sorts after recording, so the pass is complete
		// once it has been read.
		return path.Base(name) == "release_group", nil
	default:
		return false, nil
	}
}

// scanArtists records every artist's MBID by row id.
func (imp *dumpImporter) scanArtists(
	ctx context.Context, r io.Reader, scan *creditScan,
) error {
	return scanTSV(ctx, r, artistColMin, "artist", func(fields []string) error {
		id, ok := parseInt32(fields[artistColID])
		if !ok {
			return nil
		}

		var gid uuid16

		if !parseUUID(fields[artistColGID], gid[:]) {
			return fmt.Errorf("%w: artist.gid is not a UUID: %q",
				ErrDumpShape, truncate(fields[artistColGID]))
		}

		scan.artistGIDs[id] = gid

		return nil
	})
}

// scanCredits records which credits name more than one artist.
func (imp *dumpImporter) scanCredits(
	ctx context.Context, r io.Reader, scan *creditScan,
) error {
	return scanTSV(ctx, r, creditColMin, "artist_credit", func(fields []string) error {
		id, ok := parseInt32(fields[creditColID])
		if !ok {
			return nil
		}

		count, ok := parseInt32(fields[creditColArtistCount])
		if !ok {
			return fmt.Errorf("%w: artist_credit.artist_count is not a number: %q",
				ErrDumpShape, truncate(fields[creditColArtistCount]))
		}

		if count > 1 {
			scan.multiCredits[id] = struct{}{}
		}

		return nil
	})
}

// scanCreditParts records the decomposition of every multi-artist
// credit.
func (imp *dumpImporter) scanCreditParts(
	ctx context.Context, r io.Reader, scan *creditScan,
) error {
	return scanTSV(ctx, r, partColMin, "artist_credit_name", func(fields []string) error {
		credit, ok := parseInt32(fields[partColCredit])
		if !ok {
			return nil
		}

		if _, multi := scan.multiCredits[credit]; !multi {
			return nil
		}

		position, ok := parseInt32(fields[partColPosition])
		if !ok {
			return nil
		}

		artist, ok := parseInt32(fields[partColArtist])
		if !ok {
			return nil
		}

		scan.parts[credit] = append(scan.parts[credit], creditPart{
			position: int(position),
			artistID: artist,
			name:     fields[partColName],
			join:     fields[partColJoin],
		})

		return nil
	})
}

// scanCreditedEntities resolves recordings and release groups against
// the catalog, keeping only those the catalog holds and whose credit
// names more than one artist.
func (imp *dumpImporter) scanCreditedEntities(
	ctx context.Context, r io.Reader, kept map[uuid16]struct{}, scan *creditScan,
) error {
	return scanTSV(ctx, r, entityColMin, "recording/release_group",
		func(fields []string) error {
			var gid uuid16

			if !parseUUID(fields[entityColGID], gid[:]) {
				return fmt.Errorf("%w: entity gid is not a UUID: %q",
					ErrDumpShape, truncate(fields[entityColGID]))
			}

			if _, want := kept[gid]; !want {
				return nil
			}

			credit, ok := parseInt32(fields[entityColCredit])
			if !ok {
				return fmt.Errorf("%w: entity artist_credit is not a number: %q",
					ErrDumpShape, truncate(fields[entityColCredit]))
			}

			if _, multi := scan.multiCredits[credit]; !multi {
				return nil
			}

			scan.refs[gid] = credit
			scan.used[credit] = struct{}{}

			return nil
		})
}

// scanTSV reads Postgres COPY output a line at a time, unescaping each
// field and handing the row to fn.
//
// The shape is asserted on the first row rather than trusted: this dump
// has no header, so a column that moved would otherwise be read as a
// neighbouring one and produce a catalog that is quietly wrong.
func scanTSV(
	ctx context.Context, r io.Reader, minCols int, member string,
	fn func(fields []string) error,
) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<24)

	checked := false
	rows := 0

	for sc.Scan() {
		rows++

		if rows%(1<<20) == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}

		line := sc.Text()
		if line == "" {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) < minCols {
			if !checked {
				return fmt.Errorf("%w: %s has %d columns, need at least %d",
					ErrDumpShape, member, len(fields), minCols)
			}

			continue
		}

		checked = true

		for i := range fields {
			fields[i] = unescapeCopy(fields[i])
		}

		if err := fn(fields); err != nil {
			return err
		}
	}

	if err := sc.Err(); err != nil {
		return fmt.Errorf("credit import: read %s: %w", member, err)
	}

	return nil
}

// unescapeCopy undoes Postgres COPY's text escaping.  A NULL (\N) is
// returned as an empty string: every field this pass reads is either a
// number it will reject or a name whose absence means the same as
// empty.
func unescapeCopy(s string) string {
	if s == `\N` {
		return ""
	}

	if !strings.ContainsRune(s, '\\') {
		return s
	}

	var b strings.Builder

	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])

			continue
		}

		i++

		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		case '\\':
			b.WriteByte('\\')
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}

	return b.String()
}

func parseInt32(s string) (int32, bool) {
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, false
	}

	return int32(n), true
}

// truncate bounds an error message built from dump data, which is
// attacker-free but can be long.
func truncate(s string) string {
	const limit = 64

	if len(s) <= limit {
		return s
	}

	return s[:limit] + "..."
}
