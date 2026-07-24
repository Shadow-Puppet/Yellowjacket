package explore

import "testing"

// TestMergeIndexHitsBlendedScale is the regression test for the
// merge→filter scale mismatch.  An exact-match, in-library album that
// exists only in the local index (not returned by MB) with very low
// popularity must survive filterAndCap and outrank a weakly-matching MB
// result — because index hits are now scored on the same blended scale.
//
// Under the previous behaviour the index hit was scored as
// scalePopularity(50)*0.5 ≈ 12, below minBlendedScore (15), so
// filterAndCap dropped it entirely.
func TestMergeIndexHitsBlendedScale(t *testing.T) {
	// A weakly-matching MB release group, already reranked: ~0.35
	// relevance, no popularity → blended Score ≈ 35.
	result := MBSearchResult{
		ReleaseGroups: []MBReleaseGroup{
			{MBID: "mb-weak", Title: "Live At Wembley", Score: 35, Popularity: 0},
		},
	}

	// An exact-match, in-library album from the local index with only
	// 50 listens.
	hits := []SearchIndexResult{
		{
			EntityType: "release_group",
			MBID:       "idx-exact",
			Title:      "Abbey Road",
			ArtistName: "The Beatles",
			Popularity: 50,
			InLibrary:  true,
		},
	}

	mergeIndexHits("abbey road", &result, hits)

	if got := result.ReleaseGroups[0].MBID; got != "idx-exact" {
		t.Fatalf("expected exact in-library index hit to rank first, got %q (score %d)",
			got, result.ReleaseGroups[0].Score)
	}

	idxScore := result.ReleaseGroups[0].Score
	if idxScore < minBlendedScore {
		t.Errorf("index hit score %d below minBlendedScore %d — would be filtered out",
			idxScore, minBlendedScore)
	}

	// It must survive the filter that previously dropped it.
	filterAndCap(&result)

	if len(result.ReleaseGroups) == 0 || result.ReleaseGroups[0].MBID != "idx-exact" {
		t.Fatalf("index hit did not survive filterAndCap: %+v", result.ReleaseGroups)
	}
}

// TestMergeIndexHitsSortsRecordings guards the recordings path, which
// previously had no post-merge sort: a high-scoring index recording was
// merged but its position depended on prepend order, not score.
func TestMergeIndexHitsSortsRecordings(t *testing.T) {
	result := MBSearchResult{
		Recordings: []MBRecording{
			{MBID: "mb-a", Title: "Filler", Score: 60, Popularity: 100},
			{MBID: "mb-b", Title: "Filler Two", Score: 20, Popularity: 50},
		},
	}

	hits := []SearchIndexResult{
		{
			EntityType: "recording",
			MBID:       "idx-exact",
			Title:      "Calling You",
			Popularity: 100_000,
			InLibrary:  true,
		},
	}

	mergeIndexHits("calling you", &result, hits)

	// Exact + in-library + decent popularity should land on top, and
	// the whole list must be in descending Score order.
	if result.Recordings[0].MBID != "idx-exact" {
		t.Errorf("exact in-library recording should rank first, got %q", result.Recordings[0].MBID)
	}

	for i := 1; i < len(result.Recordings); i++ {
		if result.Recordings[i-1].Score < result.Recordings[i].Score {
			t.Errorf("recordings not sorted by score descending: %+v", result.Recordings)

			break
		}
	}
}

func TestIndexHitRelevanceTiers(t *testing.T) {
	const q = "abbey road"

	exact := indexHitRelevance(q, "Abbey Road", "")
	prefix := indexHitRelevance(q, "Abbey Road Sessions", "")
	word := indexHitRelevance(q, "Live: Abbey Road Medley", "")
	none := indexHitRelevance(q, "Something Unrelated", "")

	if !(exact > prefix && prefix > word && word >= indexRelevanceFloor) {
		t.Errorf("relevance tiers not ordered: exact=%v prefix=%v word=%v", exact, prefix, word)
	}

	if exact != indexRelExact {
		t.Errorf("exact relevance = %v, want %v", exact, indexRelExact)
	}

	// A non-matching title still gets the floor (FTS matched something).
	if none != indexRelevanceFloor {
		t.Errorf("non-match relevance = %v, want floor %v", none, indexRelevanceFloor)
	}

	// Artist-credit match should count when the title doesn't.
	artistMatch := indexHitRelevance("the beatles", "Abbey Road", "The Beatles")
	if artistMatch != indexRelExact {
		t.Errorf("artist-credit exact match = %v, want %v", artistMatch, indexRelExact)
	}
}
