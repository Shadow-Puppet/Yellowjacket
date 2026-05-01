# 009 — Autotag: Scoring Engine & MB Orchestration

> Second v1.3 phase. Given an album-group, produce a ranked list of candidate releases with per-track alignment data, using as few MusicBrainz calls as the API-minimization playbook allows.  No UI yet — 010 surfaces this to the user.

**Shipped:** 2026-04-21 · **Sub-plans:** types + normalization + distance, local resolver, MB orchestration, ranker + persistence, test corpus

## What landed

- **`backend/autotag/` domain types** — `Candidate`, `TrackAlignment`, `GroupScore`, `LocalTrack`, `CandidateSource` (`local` / `musicbrainz`), `AlignmentStatus` (`matched` / `missing` / `extra` / `mismatched`).
- **`Normalize(s)`** — Unicode NFC → qualifier-suffix strip (`(Remastered 2009)`, `[Bonus Track]`, `(feat. X)`, etc.) → case fold → punctuation drop → whitespace collapse.  Comparison-only, not human-readable.
- **Per-track distance** — weighted 60% title similarity (1.0 − Levenshtein/max-len), 30% length delta (linear: ≤1 s = 1.0, ≥30 s = 0.0, neutral 0.5 when either side unknown), 10% track-number match.
- **Greedy alignment** — `AlignTracks` picks the highest-scoring (local, cand) pair iteratively; not Hungarian-optimal but fine at album cardinalities. Emits `matched` / `missing` / `extra` / `mismatched` rows so the review UI can render the diff.
- **Local-first resolver** — `ListLocalReleaseGroupCandidates` sqlc query pre-filters on `rg.mbid != '' AND r.mbid != '' AND rg.name = ? COLLATE NOCASE`, then Go applies full normalization.  Candidate track lists only include recordings that themselves have MBIDs (avoids untagged dupes polluting the "canonical" view when the same `(name, artist)` release group is shared across libraries).
- **MB orchestration** — `MBClient` interface (`SearchReleaseGroups`, `BrowseReleases`, `LookupArtist`) hides `explore.MusicBrainzClient`; `backend/explore/autotagclient.go` adapts one to the other.  `buildMBQuery` assembles `release:"X" AND arid:<mbid> AND tracks:N` — `arid:` makes the search cache key deterministic, `tracks:N` filters out box-set-style releases.  One search per album + one `BrowseReleases` per candidate RG.
- **Release-level ranker** — aggregate track score (70%) + track-count match (15%, zero at ≥50% delta) + meta (15%, avg of year/Official/country bonuses).  `Scorer.ScoreGroup` hits local first, runs MB only when no local candidate scores ≥ 0.90.
- **Persistence** — `SetTaggingItemBestMatch` writes `best_match_release_mbid`, `score`, `last_checked_at = CURRENT_TIMESTAMP`, `status = 'matched'`.

## Key decisions retained

- **SHA-1-grade normalization vs. full MB-equivalent.** Qualifier regex handles the common cases (remaster, deluxe, explicit, feat., bonus, etc.) without dragging in a full MB title-parsing library. Edges that bite in real libraries will show up in 010 review UX and can be patched then.
- **`*sqlcgen.Queries` as the DB boundary for autotag**, not `*database.DB`. The `database` package already depends on `autotag.GroupKey` (from 008.3), so reversing the dependency via `database` would create a cycle. Using `sqlcgen` directly is acyclic and keeps `autotag` swappable.
- **Scorer constructor takes `MBClient` as interface, not `*explore.MusicBrainzClient`.** Lets tests inject a stub without spinning up the HTTP + cache layer.  The concrete adapter lives in `explore/autotagclient.go`.
- **`localSufficient = 0.90` threshold for skipping MB.** Empirical guess — will get retuned in 011 auto-accept phase when we observe real corpus scores.
- **Weights `60/30/10` for title/length/track-number.** Cribbed from beets' broad intuition; tuned to emphasize title-matching since length data can be unreliable from Vorbis Comments.  Tests document the expected floors (e.g. "exact match should score ≥ 0.99") so nudging weights won't silently regress.
- **`release_groups` and `recordings` must both carry MBIDs** for a local candidate.  Otherwise untagged dupes of the same album (across libraries) falsely expand the "canonical" track list.
- **UTF-8 em-dashes in SQL comments broke sqlc's string-literal emitter**, truncating generated query strings mid-word. All autotag SQL comments use ASCII punctuation.

## Known follow-ups into 010+

- **`guessArtistMBID` is a stub** returning `""` because `LocalTrack` doesn't currently carry artist MBIDs. 010 should thread artist MBIDs through `ListAudioFilesInTaggingGroup` so the MB resolver can use `arid:` filters.
- **`yearBonus` uses `time.Now().Year()`** as a placeholder target. Should become the earliest release-date hint from the group's tracks once 010 provides it.
- **VA compilation detection threshold** is still open (noted in `.planning/NOTES.md`). The scorer doesn't special-case per-track artist credits differing from album-artist.
