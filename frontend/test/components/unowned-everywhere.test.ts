/**
 * Owned is plain; unowned is what gets marked.
 *
 * `explore-album-details` had this right for one tracklist and nothing
 * else did: Explore's cards, the top-results row and the artist page's
 * three card shapes all mixed owned and unowned with a small badge as
 * the only difference — and drew a green tick on the *common* case,
 * which is the treatment the album page's own green ticks were removed
 * for.
 *
 * What is pinned here is the rule rather than any one surface, because
 * the fault this replaced was eight call sites each holding their own
 * version of it:
 *
 *  - an owned thing draws **no badge at all**;
 *  - an unowned one is dimmed *and* says so in its accessible name,
 *    because dimming is a colour and cannot be the only signal;
 *  - ownership is a **file** (`localId`), never the catalog's
 *    `inLibrary` ratchet, which is a flag that happens to agree;
 *  - and a partly-held album says *how* partly, which is the one thing
 *    a tick cannot.
 */
import { beforeEach, describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';

import '@components/explore-view/explore-view';
import '@components/top-results-row/top-results-row';
import { flush, stub, resetHarness } from '@test/support/harness';
import { fixture, shadow, shadowAll, update } from '@test/support/render';
import { completenessStore } from '@store/completeness-store';

const SEARCH = 'explore.Service.SearchLocal';
const SHELVES = 'explore.Service.GetExploreShelves';
const COMPLETENESS = 'library.Library.GetAlbumsCompleteness';

/** A release group as the backend projects one. */
function album(
  title: string,
  { localId = 0, inLibrary = false }: { localId?: number; inLibrary?: boolean },
) {
  return {
    mbid: `rg-${title}`,
    title,
    artistCredit: 'An Artist',
    artistMbid: 'ar-1',
    primaryType: 'Album',
    firstReleaseDate: '1994-05-01',
    popularity: 100,
    listenerCount: 10,
    secondaryTypes: [],
    inLibrary,
    localId,
  };
}

/** A recording as the backend projects one. */
function recording(
  title: string,
  { localId = 0, inLibrary = false }: { localId?: number; inLibrary?: boolean },
) {
  return {
    mbid: `rec-${title}`,
    title,
    artistCredit: 'An Artist',
    artistMbid: 'ar-1',
    length: 200000,
    popularity: 0,
    listenerCount: 0,
    inLibrary,
    localId,
  };
}

/** Mount Explore showing one page of results. */
async function exploreShowing(results: {
  releaseGroups?: unknown[];
  recordings?: unknown[];
  artists?: unknown[];
}) {
  stub(SHELVES, { shelves: [], state: 'ready' });
  stub(SEARCH, {
    artists: [],
    releaseGroups: [],
    recordings: [],
    ...results,
  });

  const el = await fixture('explore-view');

  // A cached primary view only fetches on arrival, and the search is
  // what these cards come from.
  (el as unknown as { onViewActivate: () => void }).onViewActivate?.();
  await update(el, { results: { artists: [], releaseGroups: [], recordings: [], ...results } });
  await flush();
  await el.updateComplete;

  return el;
}

beforeEach(() => {
  resetHarness();
  stub(COMPLETENESS, {});

  // The store is a singleton and caches an *answer*, including the
  // absent one — which is the point, or 87% of a grid re-asks forever.
  // Two tests in one file are two sessions as far as it is concerned,
  // so a stale entry from the test above would otherwise decide the
  // one below. Found by writing the assertion the wrong way round.
  completenessStore.invalidate();
});

describe('an owned thing is plain', () => {
  it('draws no badge on an album card it has files for', async () => {
    const el = await exploreShowing({
      releaseGroups: [album('Held', { localId: 7 })],
    });

    expect(shadowAll(el, '.album-card')).toHaveLength(1);
    expect(shadow(el, '.album-card library-status-indicator')).toBeNull();
  });

  it('draws no badge on a track row it has a file for', async () => {
    const el = await exploreShowing({
      recordings: [recording('Held', { localId: 9 })],
    });

    expect(shadowAll(el, '.track-item')).toHaveLength(1);
    expect(shadow(el, '.track-item library-status-indicator')).toBeNull();
  });

  it('does not dim it', async () => {
    const el = await exploreShowing({
      releaseGroups: [album('Held', { localId: 7 })],
    });

    expect(shadow(el, '.album-card')?.classList.contains('unowned')).toBe(
      false,
    );
  });
});

describe('an unowned thing is marked', () => {
  it('dims the card and keeps its request badge', async () => {
    const el = await exploreShowing({
      releaseGroups: [album('Absent', {})],
    });

    expect(shadow(el, '.album-card')?.classList.contains('unowned')).toBe(true);
    expect(shadow(el, '.album-card library-status-indicator')).not.toBeNull();
  });

  /**
   * The name is the half of this that reaches anyone not seeing the
   * dimming, so it has to be the browser's own answer — a shadow-root
   * query cannot compute a name, and this repo has shipped a nameless
   * control three times.
   */
  it('says so in the name the browser computes', async () => {
    await exploreShowing({ releaseGroups: [album('Absent', {})] });

    await expect
      .element(page.getByRole('button', { name: /Absent — not in your library/ }))
      .toBeInTheDocument();
  });

  /**
   * A track row is `aria-disabled` and a card is not, and the
   * difference is not cosmetic: activating an unowned row does nothing
   * (`onRecordingRowDblClick` returns early), while a card navigates to
   * the catalog page for it, which is a perfectly good thing to do with
   * something you do not own.
   */
  it('marks a row that cannot be played as disabled', async () => {
    const el = await exploreShowing({
      recordings: [recording('Absent', {})],
    });

    expect(shadow(el, '.track-item')?.getAttribute('aria-disabled')).toBe(
      'true',
    );
  });

  it('leaves a card that still navigates enabled', async () => {
    const el = await exploreShowing({
      releaseGroups: [album('Absent', {})],
    });

    expect(shadow(el, '.album-card')?.getAttribute('aria-disabled')).toBeNull();
  });
});

/**
 * The decision this issue turned on.
 *
 * `inLibrary` is written by the same pass that writes the local ids, so
 * the two agree in a healthy database — but it is a one-way ratchet
 * (`MAX(in_library, excluded.in_library)`) whose only clearing pass is
 * gated on a non-null `local_*_id`, so it cannot be un-set on its own.
 * A row carrying it with no local id behind it is a claim of ownership
 * with no file, which is exactly what the album page refuses to trust.
 */
describe('ownership is a file, not a flag', () => {
  it('treats a card flagged inLibrary with no local row as unowned', async () => {
    const el = await exploreShowing({
      releaseGroups: [album('Phantom', { inLibrary: true, localId: 0 })],
    });

    expect(shadow(el, '.album-card')?.classList.contains('unowned')).toBe(true);
    expect(shadow(el, '.album-card library-status-indicator')).not.toBeNull();
  });

  it('does the same for a track row', async () => {
    const el = await exploreShowing({
      recordings: [recording('Phantom', { inLibrary: true, localId: 0 })],
    });

    expect(shadow(el, '.track-item')?.getAttribute('aria-disabled')).toBe(
      'true',
    );
  });
});

/**
 * The count, which is what `#16`'s deferred third step asked for: an
 * album held 2 tracks of 10 wore the same green tick as one held whole,
 * on every grid in the app.
 */
describe('a partly-held album says how partly', () => {
  it('draws the ring and puts the count in the badge name', async () => {
    stub(COMPLETENESS, {
      '7': { owned: 9, expected: 12, known: true, complete: false },
    });

    const el = await exploreShowing({
      releaseGroups: [album('Partly', { localId: 7 })],
    });

    // The store batches into the next frame, so the answer lands one
    // repaint after the cards do — which is the thing the subscription
    // exists for.
    await new Promise((r) => requestAnimationFrame(() => r(null)));
    await flush();
    await el.updateComplete;

    const badge = shadow(el, '.album-card library-status-indicator');

    expect(badge?.getAttribute('status')).toBe('partial');

    // A partly-held album is *actionable* — it has three tracks left to
    // ask for — so the badge is a button, and the name has to carry the
    // action and the count. Naming it after the action alone left the
    // one state the ring exists for as the one state whose name did not
    // mention it.
    await expect
      .element(
        page.getByRole('button', {
          name: /Request the rest of album .*Partly.* — 9 of 12 tracks/,
        }),
      )
      .toBeInTheDocument();
  });

  /**
   * Where the tags never declared a total, `known` is false and the
   * card must say nothing — most of an untagged library is in that
   * state, and a ring drawn from its absence would mark all of it
   * incomplete on no evidence. That is the rule `Known` exists for.
   */
  it('says nothing when the total was never declared', async () => {
    stub(COMPLETENESS, {
      '7': { owned: 3, expected: 0, known: false, complete: false },
    });

    const el = await exploreShowing({
      releaseGroups: [album('Untotalled', { localId: 7 })],
    });

    await new Promise((r) => requestAnimationFrame(() => r(null)));
    await flush();
    await el.updateComplete;

    expect(shadow(el, '.album-card library-status-indicator')).toBeNull();
  });

  it('asks about the owned albums only, in one call', async () => {
    const seen: unknown[][] = [];

    stub(COMPLETENESS, (...args: unknown[]) => {
      seen.push(args);

      return {};
    });

    await exploreShowing({
      releaseGroups: [
        album('Held', { localId: 7 }),
        album('Also held', { localId: 8 }),
        album('Absent', {}),
        album('Phantom', { inLibrary: true, localId: 0 }),
      ],
    });

    await new Promise((r) => requestAnimationFrame(() => r(null)));
    await flush();

    expect(seen).toHaveLength(1);
    expect(seen[0]?.[0]).toEqual([7, 8]);
  });
});

describe('the top-results row follows the same rule', () => {
  const result = (
    name: string,
    entityType: string,
    extra: Record<string, unknown> = {},
  ) => ({
    entityType,
    mbid: `top-${name}`,
    name,
    artistCredit: 'An Artist',
    intentScore: 1,
    inLibrary: false,
    ...extra,
  });

  it('draws no badge on something it owns', async () => {
    const el = await fixture('top-results-row', {
      results: [result('Held', 'release_group', { localId: 7 })],
      query: 'held',
    });

    expect(shadow(el, '.card library-status-indicator')).toBeNull();
    expect(shadow(el, '.card')?.classList.contains('unowned')).toBe(false);
  });

  it('dims and names something it does not', async () => {
    const el = await fixture('top-results-row', {
      results: [result('Absent', 'release_group')],
      query: 'absent',
    });

    expect(shadow(el, '.card')?.classList.contains('unowned')).toBe(true);

    await expect
      .element(page.getByRole('button', { name: /Absent — not in your library/ }))
      .toBeInTheDocument();
  });

  /**
   * An artist card has never had a badge — a discography subscription
   * is the artist page's Follow button, which can say what it commits
   * to — so the dimming and the name are the whole signal there.
   */
  it('marks an unowned artist without offering a request', async () => {
    const el = await fixture('top-results-row', {
      results: [result('An Artist', 'artist')],
      query: 'an artist',
    });

    expect(shadow(el, '.card')?.classList.contains('unowned')).toBe(true);
    expect(shadow(el, '.card library-status-indicator')).toBeNull();
  });
});
