package explore

import (
	"fmt"
	"strings"
)

// Reading multi-artist credits back out of the catalog.
//
// The tables are filled centrally (backend/explore/dumpcredits.go, and
// the artifact import) and hold only credits naming more than one
// artist: an entity with no rows here is credited to one artist, which
// explore_index's own artist_name and artist_mbid already describe.
// Absence is the common case and means "nothing to decompose", never
// "unknown".
//
// The lookup is keyed on the *recording* MBID, which both sides of the
// app already have -- a catalog row carries it and so does a local
// file (library.Track.RecordingMBID) -- so one query serves the Explore
// pages and the library's own lists without either needing to know
// where the other gets its rows.

// CreditPart is one credited artist within a credit, in credit order.
//
// CreditedName is the name *as credited*, which is not the artist's own
// name: MusicBrainz credits "Snoop Dogg" on a track by the artist
// called "Snoop Doggy Dogg".  Display uses it; navigation uses
// ArtistMBID.  JoinPhrase is the literal connector that follows this
// part, so a credit renders by concatenation and never by searching a
// name inside a credit string.
type CreditPart struct {
	Position     int    `json:"position"`
	ArtistMBID   string `json:"artistMbid"`
	CreditedName string `json:"creditedName"`
	JoinPhrase   string `json:"joinPhrase"`
}

// creditLookupBatch bounds how many MBIDs go into one IN clause.  A
// tracklist is the caller here, so the realistic ceiling is a few
// hundred; the bound exists so a 50,000-row selection cannot build a
// statement SQLite refuses to parse.
const creditLookupBatch = 500

// GetCredits returns the decomposition of every multi-artist credit
// among the given entity MBIDs, keyed by MBID.
//
// MBIDs with a single-artist credit are simply absent from the result,
// which is what the caller wants: it renders its existing single link
// for those, and that is the same answer it would have rendered anyway.
func (si *SearchIndex) GetCredits(mbids []string) (map[string][]CreditPart, error) {
	out := make(map[string][]CreditPart)

	for start := 0; start < len(mbids); start += creditLookupBatch {
		end := min(start+creditLookupBatch, len(mbids))

		if err := si.appendCredits(mbids[start:end], out); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// appendCredits runs one batch into the accumulating result.
func (si *SearchIndex) appendCredits(
	mbids []string, out map[string][]CreditPart,
) error {
	args := make([]any, 0, len(mbids))
	holders := make([]string, 0, len(mbids))

	for _, mbid := range mbids {
		if mbid == "" {
			continue
		}

		args = append(args, dbMBID(mbid))
		holders = append(holders, "?")
	}

	if len(args) == 0 {
		return nil
	}

	// Ordered by position because that ordering *is* the credit's
	// meaning; the caller concatenates in the order it receives.
	rows, err := si.db.QueryContext(
		`SELECT r.mbid, p.position, p.artist_mbid, p.credited_name, p.join_phrase
		 FROM artist_credit_ref r
		 JOIN artist_credit_part p ON p.credit_id = r.credit_id
		 WHERE r.mbid IN (`+strings.Join(holders, ",")+`)
		 ORDER BY r.mbid, p.position`,
		args...,
	)
	if err != nil {
		return fmt.Errorf("read artist credits: %w", err)
	}

	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			entity dbMBID
			artist dbMBID
			part   CreditPart
		)

		if err := rows.Scan(
			&entity, &part.Position, &artist, &part.CreditedName, &part.JoinPhrase,
		); err != nil {
			return fmt.Errorf("scan artist credit: %w", err)
		}

		part.ArtistMBID = string(artist)
		out[string(entity)] = append(out[string(entity)], part)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("read artist credits: %w", err)
	}

	return nil
}

// GetCredits is the bound form: the frontend asks for a tracklist's
// worth of MBIDs at once rather than one per row.
//
// Batched for the reason every other per-row backend question here is:
// asking on hover or on render turns a list into N IPC round trips, and
// this one is asked about every row of every list in the app.
func (e *Service) GetCredits(mbids []string) (map[string][]CreditPart, error) {
	return e.index.GetCredits(mbids)
}
