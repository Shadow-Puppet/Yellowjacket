package autotag

import "testing"

func junkDrawerTracks() []LocalTrack {
	return []LocalTrack{
		{
			Title: "Song A", Artist: "Artist One",
			AlbumTag: "Album One", AlbumArtistTag: "Artist One",
		},
		{
			Title: "Song B", Artist: "Artist One",
			AlbumTag: "Album One", AlbumArtistTag: "Artist One",
		},
		{
			Title: "Song C", Artist: "Artist Two",
			AlbumTag: "Album Two", AlbumArtistTag: "Artist Two",
		},
		{
			Title: "Song D", Artist: "Artist Two",
			AlbumTag: "Album Two", AlbumArtistTag: "Artist Two",
		},
		{
			Title: "Song E", Artist: "Artist Three",
			AlbumTag: "Album Three", AlbumArtistTag: "Artist Three",
		},
	}
}

func TestIsMixedBag_DetectsJunkDrawer(t *testing.T) {
	t.Parallel()

	g := Group{Tracks: junkDrawerTracks()}

	if !IsMixedBag(g) {
		t.Fatal("expected a folder with no artist or album consensus to be flagged mixed-bag")
	}
}

func TestIsMixedBag_RealAlbumNotFlagged(t *testing.T) {
	t.Parallel()

	g := Group{
		AlbumArtist: "The Beatles",
		Tracks: []LocalTrack{
			{Title: "Come Together", Artist: "The Beatles"},
			{Title: "Something", Artist: "The Beatles"},
			{Title: "Maxwell's Silver Hammer", Artist: "The Beatles"},
			{Title: "Oh! Darling", Artist: "The Beatles"},
		},
	}

	if IsMixedBag(g) {
		t.Fatal("a coherent single-artist album must not be flagged mixed-bag")
	}
}

func TestIsMixedBag_ExplicitAlbumArtistOverridesHeuristic(t *testing.T) {
	t.Parallel()

	// Per-track artists disagree (feat. credits, remixers, etc.) but
	// the folder carries a real album-artist tag — trust it.
	g := Group{
		AlbumArtist: "Some Artist",
		Tracks: []LocalTrack{
			{Title: "Track 1", Artist: "Some Artist"},
			{Title: "Track 2", Artist: "Some Artist feat. Guest"},
			{Title: "Track 3", Artist: "Someone Else"},
			{Title: "Track 4", Artist: "Some Artist"},
		},
	}

	if IsMixedBag(g) {
		t.Fatal("explicit non-VA album-artist tag should override the divergence heuristic")
	}
}

func TestIsMixedBag_VACompilationNotFlagged(t *testing.T) {
	t.Parallel()

	// Various-artists compilation: artists diverge but every track
	// agrees on the album — this is vaLikely's case, not a junk
	// drawer, so IsMixedBag must require album divergence too.
	g := Group{
		Tracks: []LocalTrack{
			{Title: "Track 1", Artist: "Artist One", AlbumTag: "Now That's What I Call Music"},
			{Title: "Track 2", Artist: "Artist Two", AlbumTag: "Now That's What I Call Music"},
			{Title: "Track 3", Artist: "Artist Three", AlbumTag: "Now That's What I Call Music"},
			{Title: "Track 4", Artist: "Artist Four", AlbumTag: "Now That's What I Call Music"},
		},
	}

	if IsMixedBag(g) {
		t.Fatal("a VA compilation with consistent album tags must not be flagged mixed-bag")
	}
}

func TestIsMixedBag_TooFewTracksNotFlagged(t *testing.T) {
	t.Parallel()

	g := Group{
		Tracks: []LocalTrack{
			{Title: "Track 1", Artist: "Artist One", AlbumTag: "Album One"},
			{Title: "Track 2", Artist: "Artist Two", AlbumTag: "Album Two"},
		},
	}

	if IsMixedBag(g) {
		t.Fatal("a folder below mixedBagMinTracks must not be flagged, even if it diverges")
	}
}

func TestClusterByAlbumArtist_FindsSubAlbums(t *testing.T) {
	t.Parallel()

	tracks := junkDrawerTracks() // two 2-track clusters + one true singleton

	clusters := ClusterByAlbumArtist(tracks)

	if len(clusters) != 2 { //nolint:mnd
		t.Fatalf("expected 2 clusters (Album One, Album Two), got %d: %+v", len(clusters), clusters)
	}

	for _, c := range clusters {
		if len(c.Tracks) != 2 { //nolint:mnd
			t.Errorf("cluster %q: expected 2 tracks, got %d", c.AlbumName, len(c.Tracks))
		}
	}

	total := 0
	for _, c := range clusters {
		total += len(c.Tracks)
	}

	if total != 4 { //nolint:mnd
		t.Errorf(
			"expected 4 clustered tracks total (Song E stays unclustered), got %d",
			total,
		)
	}
}

func TestClusterByAlbumArtist_TypoVariantsMergeIntoOneCluster(t *testing.T) {
	t.Parallel()

	// A dropped diacritic and a stray trailing space are the kind of
	// noise exact Normalize()-equality clustering used to treat as
	// two different albums, splitting one real album across clusters
	// even though a candidate search on either would land on the same
	// release. Fuzzy clustering absorbs both into one cluster.
	tracks := []LocalTrack{
		{
			Title:          "Song A",
			Artist:         "Sigur Ros",
			AlbumTag:       "Agaetis Byrjun",
			AlbumArtistTag: "Sigur Ros",
		},
		{
			Title:          "Song B",
			Artist:         "Sigur Ros",
			AlbumTag:       "Ágætis byrjun",
			AlbumArtistTag: "Sigur Ros",
		},
		{
			Title:          "Song C",
			Artist:         "Sigur Ros",
			AlbumTag:       "Agaetis Byrjun ",
			AlbumArtistTag: "Sigur Ros",
		},
	}

	clusters := ClusterByAlbumArtist(tracks)

	if len(clusters) != 1 {
		t.Fatalf(
			"expected typo variants to merge into 1 cluster, got %d: %+v",
			len(clusters),
			clusters,
		)
	}

	if len(clusters[0].Tracks) != 3 { //nolint:mnd
		t.Fatalf("expected all 3 tracks in the merged cluster, got %d", len(clusters[0].Tracks))
	}
}

func TestClusterByAlbumArtist_DifferentAlbumsBySameArtistStaySeparate(t *testing.T) {
	t.Parallel()

	// Fuzzy clustering must not blur genuinely different albums by
	// the same artist into one cluster just because they share an
	// artist tag — the threshold has to stay tight enough for this.
	tracks := []LocalTrack{
		{
			Title:          "Song A",
			Artist:         "Radiohead",
			AlbumTag:       "OK Computer",
			AlbumArtistTag: "Radiohead",
		},
		{
			Title:          "Song B",
			Artist:         "Radiohead",
			AlbumTag:       "OK Computer",
			AlbumArtistTag: "Radiohead",
		},
		{Title: "Song C", Artist: "Radiohead", AlbumTag: "Kid A", AlbumArtistTag: "Radiohead"},
		{Title: "Song D", Artist: "Radiohead", AlbumTag: "Kid A", AlbumArtistTag: "Radiohead"},
	}

	clusters := ClusterByAlbumArtist(tracks)

	if len(clusters) != 2 { //nolint:mnd
		t.Fatalf(
			"expected OK Computer and Kid A to stay separate, got %d clusters: %+v",
			len(clusters),
			clusters,
		)
	}
}

func TestClusterByAlbumArtist_NoAlbumTagStaysUnclustered(t *testing.T) {
	t.Parallel()

	tracks := []LocalTrack{
		{Title: "Track 1", Artist: "Artist One"},
		{Title: "Track 2", Artist: "Artist One"},
	}

	if clusters := ClusterByAlbumArtist(tracks); len(clusters) != 0 {
		t.Fatalf("tracks with no album tag must never cluster, got %+v", clusters)
	}
}

func TestClusterByAlbumArtist_DeterministicOrder(t *testing.T) {
	t.Parallel()

	tracks := junkDrawerTracks()

	first := ClusterByAlbumArtist(tracks)
	second := ClusterByAlbumArtist(tracks)

	if len(first) != len(second) {
		t.Fatalf("non-deterministic cluster count: %d vs %d", len(first), len(second))
	}

	for i := range first {
		if first[i].AlbumName != second[i].AlbumName {
			t.Errorf(
				"non-deterministic cluster order at %d: %q vs %q",
				i,
				first[i].AlbumName,
				second[i].AlbumName,
			)
		}
	}

	if first[0].AlbumName != "Album One" {
		t.Errorf("expected first-seen cluster order, got %q first", first[0].AlbumName)
	}
}

func TestSplitPlan_ClustersPlusSingletonForEveryLeftover(t *testing.T) {
	t.Parallel()

	tracks := junkDrawerTracks() // two 2-track clusters + one true singleton (Song E)

	plan := SplitPlan(tracks)

	total := 0
	for _, c := range plan {
		total += len(c.Tracks)
	}

	if total != len(tracks) {
		t.Fatalf("expected every track accounted for, got %d of %d", total, len(tracks))
	}

	var singletons, clustered int

	for _, c := range plan {
		switch len(c.Tracks) {
		case 1:
			singletons++
		case 2: //nolint:mnd
			clustered++
		default:
			t.Errorf("unexpected cluster size %d: %+v", len(c.Tracks), c)
		}
	}

	if singletons != 1 {
		t.Errorf("expected exactly 1 singleton (Song E), got %d", singletons)
	}

	if clustered != 2 { //nolint:mnd
		t.Errorf("expected exactly 2 clustered groups, got %d", clustered)
	}
}

func TestSplitPlan_AllUnrelatedTracksAllBecomeSingletons(t *testing.T) {
	t.Parallel()

	tracks := []LocalTrack{
		{Title: "Track 1", Artist: "Artist One", AlbumTag: "Album A"},
		{Title: "Track 2", Artist: "Artist Two", AlbumTag: "Album B"},
		{Title: "Track 3", Artist: "Artist Three"}, // no album tag at all
	}

	plan := SplitPlan(tracks)

	if len(plan) != len(tracks) {
		t.Fatalf(
			"expected every unrelated track to become its own singleton, got %d clusters for %d tracks",
			len(plan),
			len(tracks),
		)
	}

	for _, c := range plan {
		if len(c.Tracks) != 1 {
			t.Errorf("expected singleton cluster, got %d tracks: %+v", len(c.Tracks), c)
		}
	}
}
