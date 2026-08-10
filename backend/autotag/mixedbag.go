package autotag

// mixedBagMinTracks is the smallest folder IsMixedBag will flag.
// Below this, artist/album divergence is just as likely to be
// sampling noise (a 2-track folder with two different artists could
// easily be a legitimate 2-track EP with a featured artist) as it is
// a genuine junk-drawer folder.
const mixedBagMinTracks = 4

// clusterMinSize is the smallest tag-matched group ClusterByAlbumArtist
// will surface as a splittable cluster.  A single track sharing no
// album/artist with anything else in the folder gains nothing from
// becoming its own one-track group — it stays in the leftover folder,
// which the existing evidence-scaling (rank.go) already treats
// appropriately harshly for a 1-track match.
const clusterMinSize = 2

// IsMixedBag reports whether a group's local tracks look like an
// unrelated pile of songs rather than one release: no artist
// consensus AND no album consensus, across enough tracks that the
// divergence isn't just noise.  An explicit, non-VA album-artist tag
// on the folder overrides the heuristic — a user (or a prior tagger)
// who set a real album-artist meant this to read as one release.
func IsMixedBag(g Group) bool {
	if len(g.Tracks) < mixedBagMinTracks {
		return false
	}

	if g.AlbumArtist != "" && !isVAName(g.AlbumArtist) {
		return false
	}

	return !hasTagConsensus(trackArtistTags(g.Tracks)) &&
		!hasTagConsensus(trackAlbumTags(g.Tracks))
}

// hasTagConsensus reports whether every non-empty value in vals
// normalizes to the same string.  Empty values are ignored — missing
// tags are unknown, not disagreement.  A folder with zero non-empty
// values has no consensus either way; callers only reach here after
// already requiring enough tracks to matter.
func hasTagConsensus(vals []string) bool {
	distinct := make(map[string]bool, 2) //nolint:mnd

	for _, v := range vals {
		if v == "" {
			continue
		}

		distinct[Normalize(v)] = true

		if len(distinct) > 1 {
			return false
		}
	}

	return len(distinct) == 1
}

func trackArtistTags(tracks []LocalTrack) []string {
	out := make([]string, len(tracks))
	for i, t := range tracks {
		out[i] = t.Artist
	}

	return out
}

func trackAlbumTags(tracks []LocalTrack) []string {
	out := make([]string, len(tracks))
	for i, t := range tracks {
		out[i] = t.AlbumTag
	}

	return out
}

// TrackCluster is a set of local tracks sharing a non-empty (album,
// album-artist) tag pair — a candidate sub-album hiding inside a
// mixed-bag folder.
type TrackCluster struct {
	AlbumName   string
	AlbumArtist string
	Tracks      []LocalTrack
}

// ClusterByAlbumArtist groups tracks by normalized (album tag,
// album-artist tag) and returns the clusters with at least
// clusterMinSize members, in first-seen order (the caller typically
// passes tracks already ordered by disc/track/path, so this stays
// deterministic run to run).  Tracks with no album tag, or whose
// cluster never reaches clusterMinSize, are omitted — they belong in
// the leftover folder, not a synthetic group of their own.
func ClusterByAlbumArtist(tracks []LocalTrack) []TrackCluster {
	type key struct{ album, artist string }

	index := make(map[key]int, 4) //nolint:mnd

	var clusters []TrackCluster

	for _, t := range tracks {
		album := Normalize(t.AlbumTag)
		if album == "" {
			continue
		}

		k := key{album: album, artist: Normalize(t.AlbumArtistTag)}

		if i, ok := index[k]; ok {
			clusters[i].Tracks = append(clusters[i].Tracks, t)

			continue
		}

		index[k] = len(clusters)
		clusters = append(clusters, TrackCluster{
			AlbumName:   t.AlbumTag,
			AlbumArtist: t.AlbumArtistTag,
			Tracks:      []LocalTrack{t},
		})
	}

	out := clusters[:0]

	for _, c := range clusters {
		if len(c.Tracks) >= clusterMinSize {
			out = append(out, c)
		}
	}

	return out
}

// SplitPlan returns the full set of synthetic groups a mixed-bag
// folder should be torn into: ClusterByAlbumArtist's tag-matched
// sub-albums, plus a one-track cluster for every track that didn't
// share an (album, album-artist) pair with anything else in the
// folder. Unlike ClusterByAlbumArtist alone — which leaves
// unclustered tracks behind in the parent group, where they'd still
// get folded into whatever partial-album match the scorer finds for
// the rest of the pile — this guarantees every track leaves the
// parent, so a folder of entirely unrelated singles (no two tracks
// share an album tag) still gets torn apart instead of being scored
// as one bogus album with a pile of "extra" tracks. Each singleton's
// evidence-scaled score (rank.go) keeps it appropriately humble on
// its own — it just no longer drags an unrelated release's score
// down, or gets dragged down by one.
func SplitPlan(tracks []LocalTrack) []TrackCluster {
	type key struct{ album, artist string }

	index := make(map[key]int, 4) //nolint:mnd

	var clusters []TrackCluster

	// memberOf[i] is 1+the cluster index track i was assigned to (by
	// album/artist tag match), or 0 if it never matched anything.
	// Tracked by slice position rather than any LocalTrack field —
	// AudioFileID/FilePath are frequently zero-valued in this
	// package's own tests and would collide, wrongly treating
	// distinct untagged tracks as duplicates of one another.
	memberOf := make([]int, len(tracks))

	for i, t := range tracks {
		album := Normalize(t.AlbumTag)
		if album == "" {
			continue
		}

		k := key{album: album, artist: Normalize(t.AlbumArtistTag)}

		if ci, ok := index[k]; ok {
			clusters[ci].Tracks = append(clusters[ci].Tracks, t)
			memberOf[i] = ci + 1

			continue
		}

		index[k] = len(clusters)
		memberOf[i] = len(clusters) + 1
		clusters = append(clusters, TrackCluster{
			AlbumName:   t.AlbumTag,
			AlbumArtist: t.AlbumArtistTag,
			Tracks:      []LocalTrack{t},
		})
	}

	// Clusters that never reached clusterMinSize don't survive as a
	// group; their sole member falls through to the singleton pass
	// below instead.
	kept := make([]TrackCluster, 0, len(clusters))
	keptIndex := make(map[int]int, len(clusters))

	for oldIdx, c := range clusters {
		if len(c.Tracks) >= clusterMinSize {
			keptIndex[oldIdx] = len(kept)
			kept = append(kept, c)
		}
	}

	for i, t := range tracks {
		if ci := memberOf[i] - 1; ci >= 0 {
			if _, ok := keptIndex[ci]; ok {
				continue
			}
		}

		kept = append(kept, TrackCluster{
			AlbumName:   t.AlbumTag,
			AlbumArtist: t.AlbumArtistTag,
			Tracks:      []LocalTrack{t},
		})
	}

	return kept
}
