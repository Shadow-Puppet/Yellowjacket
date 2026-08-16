package explore

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// testMBID turns a short fixture label into a well-formed MusicBrainz
// id, and passes a real one through unchanged.
//
// The catalog stores an id as its 16 raw bytes and the column says so
// (`CHECK(length(mbid) = 16)`), so a fixture can no longer call itself
// "rh". Deriving one from the label keeps the fixtures readable — the
// same label is the same id in a seed and in the assertion that reads
// it back — without letting a test write something the app could not.
func testMBID(label string) string {
	if len(mbidBytes(label)) == mbidLen {
		return label
	}

	sum := sha256.Sum256([]byte(label))
	h := hex.EncodeToString(sum[:mbidLen])

	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// TestEntityCodesAreStable pins the stored entity-type codes.
//
// They are a storage format, not an enum: the queries that name a type
// inline write the number with the name beside it
// (`entity_type = 1 /* artist */`), so changing one here without
// changing those would leave the catalog answering the wrong questions
// silently.
func TestEntityCodesAreStable(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]int{
		EntityArtist:       1,
		EntityReleaseGroup: 2,
		EntityRecording:    3,
	} {
		if got := entityCode(name); got != want {
			t.Errorf("entityCode(%q) = %d, want %d", name, got, want)
		}

		if back := entityName(want); back != name {
			t.Errorf("entityName(%d) = %q, want %q", want, back, name)
		}
	}

	if got := entityCode("nonsense"); got != 0 {
		t.Errorf("entityCode of an unknown name = %d, want 0 (matches nothing)", got)
	}
}

// TestMBIDRoundTrip pins the encoding both ways, including the two
// values that are not ids: empty, which is how "no MBID" is spelled
// everywhere, and rubbish, which must not become a valid-looking id.
func TestMBIDRoundTrip(t *testing.T) {
	t.Parallel()

	const canonical = "c0b2500e-0cef-4130-9b13-1b9d9a2f2c07"

	encoded := mbidBytes(canonical)
	if len(encoded) != mbidLen {
		t.Fatalf("encoded length = %d, want %d", len(encoded), mbidLen)
	}

	back, err := mbidFromBytes(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if back != canonical {
		t.Errorf("round trip = %q, want %q", back, canonical)
	}

	if got := mbidBytes(""); len(got) != 0 {
		t.Errorf("empty encoded to %d bytes, want 0", len(got))
	}

	if got := mbidBytes("not-an-mbid"); len(got) != 0 {
		t.Errorf("rubbish encoded to %d bytes, want 0", len(got))
	}

	// A stored value of the wrong length is an error, not a guess.
	if _, err := mbidFromBytes([]byte{1, 2, 3}); err == nil {
		t.Error("decoding three bytes should fail")
	}
}

// TestMBIDScanRejectsText is the guard for the failure this encoding
// could otherwise hide: SQLite does not coerce TEXT to BLOB, so a
// column holding the old 36-character form would silently compare equal
// to nothing. Scanning it must say so.
func TestMBIDScanRejectsText(t *testing.T) {
	t.Parallel()

	var m dbMBID

	if err := m.Scan([]byte("c0b2500e-0cef-4130-9b13-1b9d9a2f2c07")); err == nil {
		t.Error("scanning 36 bytes as an MBID should fail")
	}
}
