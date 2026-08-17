package explore

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"yellowjacket/backend/database"
)

// seedCredit writes one multi-artist credit and points an entity at it,
// the way the dump import and the artifact import both do.
func seedCredit(t *testing.T, db *database.DB, entity string, id int, parts []CreditPart) {
	t.Helper()

	pack := func(mbid string) []byte {
		raw, err := hex.DecodeString(strings.ReplaceAll(mbid, "-", ""))
		if err != nil || len(raw) != 16 {
			t.Fatalf("bad fixture mbid %q: %v", mbid, err)
		}

		return raw
	}

	if _, err := db.ExecContext(
		"INSERT INTO artist_credit_ref (mbid, credit_id) VALUES (?, ?)",
		pack(entity), id,
	); err != nil {
		t.Fatalf("seed ref: %v", err)
	}

	for _, p := range parts {
		if _, err := db.ExecContext(
			`INSERT INTO artist_credit_part
				(credit_id, position, artist_mbid, credited_name, join_phrase)
			 VALUES (?, ?, ?, ?, ?)`,
			id, p.Position, pack(p.ArtistMBID), p.CreditedName, p.JoinPhrase,
		); err != nil {
			t.Fatalf("seed part: %v", err)
		}
	}
}

// TestGetCreditsDecomposes: the parts come back in position order and
// concatenate to the credit they describe.
func TestGetCreditsDecomposes(t *testing.T) {
	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())

	rec := testMBID("rec-1")
	a, b := testMBID("artist-a"), testMBID("artist-b")

	seedCredit(t, db, rec, 7, []CreditPart{
		{Position: 0, ArtistMBID: a, CreditedName: "2Pac", JoinPhrase: " feat. "},
		{Position: 1, ArtistMBID: b, CreditedName: "Snoop Dogg"},
	})

	got, err := si.GetCredits([]string{rec})
	if err != nil {
		t.Fatalf("GetCredits: %v", err)
	}

	parts := got[rec]
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want 2", len(parts))
	}

	var rendered strings.Builder
	for _, p := range parts {
		rendered.WriteString(p.CreditedName)
		rendered.WriteString(p.JoinPhrase)
	}

	if rendered.String() != "2Pac feat. Snoop Dogg" {
		t.Errorf("rendered = %q, want %q", rendered.String(), "2Pac feat. Snoop Dogg")
	}

	// Dashed on the way out: a blob reaching the frontend is sixteen
	// bytes of mojibake, and nothing above mbid.go speaks that.
	if parts[0].ArtistMBID != a {
		t.Errorf("artist mbid = %q, want %q", parts[0].ArtistMBID, a)
	}
}

// TestGetCreditsOmitsSingleArtist: absence is the common case and means
// "nothing to decompose", so the caller renders its existing one link.
func TestGetCreditsOmitsSingleArtist(t *testing.T) {
	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())

	got, err := si.GetCredits([]string{testMBID("untagged"), ""})
	if err != nil {
		t.Fatalf("GetCredits: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("got %d credits, want none", len(got))
	}
}

// TestGetCreditsBatches: the lookup is asked about whole tracklists, so
// it must not build one statement per row or one SQLite refuses to
// parse.
func TestGetCreditsBatches(t *testing.T) {
	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())

	mbids := make([]string, 0, creditLookupBatch*2+7)
	for i := range creditLookupBatch*2 + 7 {
		mbids = append(mbids, testMBID(fmt.Sprintf("batch-%d", i)))
	}

	// One real credit somewhere past the first batch boundary.
	seedCredit(t, db, mbids[creditLookupBatch+3], 9, []CreditPart{
		{Position: 0, ArtistMBID: testMBID("a"), CreditedName: "A", JoinPhrase: " & "},
		{Position: 1, ArtistMBID: testMBID("b"), CreditedName: "B"},
	})

	got, err := si.GetCredits(mbids)
	if err != nil {
		t.Fatalf("GetCredits: %v", err)
	}

	if len(got[mbids[creditLookupBatch+3]]) != 2 {
		t.Errorf("a credit past the first batch boundary was not returned")
	}
}
