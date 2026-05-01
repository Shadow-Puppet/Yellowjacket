# 011 — Autotag: Auto-Accept & Entry Points

> Fourth v1.3 phase. The strict all-match auto-accept path runs as a background job; the tool is reachable from every place a user expects.

**Status:** pending · **Requirements:** AUTO-01..06 · **Depends on:** 010 (needs the apply pipeline)

## Success criteria

1. An album-group qualifies for auto-accept iff: exact track-count match, every track's normalized title matches, every track's length within ±2s, no cover-art replacement required, no existing-MBID conflicts. Decision uses already-cached candidate data — **no additional MB calls**.
2. Auto-accept job processes all qualifying groups in the queue, emits progress events, is cancellable at any point, honors the shared rate limiter.
3. Right-click on a track / album / artist exposes "Autotag this album" (queues + jumps to review) and "Retag" (flips status to `untagged` and requeues).
4. After a library scan finishes with N new untagged albums, a non-blocking toast appears linking to `/autotag`.
5. Sidebar has an "Autotag" nav entry with a pending-count badge, updates reactively.
6. Pasting a MB release URL into the Paste-URL dialog renders a full diff against the current album with one `LookupRelease` call.

## Sub-plans

- Strict all-match rule + unit tests.
- Auto-accept background job — progress events, cancellation, queue traversal.
- Context menu integrations on track/album/artist views.
- Post-scan toast wiring via the existing scan-complete event.
- Sidebar nav entry + pending-count badge store integration.

## Risk callout

Release selection can pick the wrong edition. The exact-track-count gate prevents most silent misbehavior, but bonus-track editions and remaster reissues with matching track counts are genuine ambiguity. Manual review handles the edge cases — that's why auto-accept is strict by design, and the slider for fuzzy auto-accept is explicitly out of scope.
