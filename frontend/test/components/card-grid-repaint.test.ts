/**
 * A card grid repaints when the selection changes — and the thing that
 * makes it repaint is not obvious.
 *
 * `perf.m1` says `artists-view` and `genres-view` should hoist their
 * `.renderItem` / `.keyFunction` arrow functions to stable class fields
 * the way `cover-grid` does, because `LitVirtualizer` declares both as
 * plain properties and a fresh function marks them dirty on every host
 * update. That is true, and the fix is a regression: a parent
 * re-render only reaches the virtualizer's children *because* one of
 * those properties changed. Hoist them, and `LitVirtualizer` sees no
 * changed property, never re-renders, and the directive's `update()`
 * never runs — so the cards keep the classes they had.
 *
 * Measured in the running app on the fixture library: 1 highlighted
 * card before the change, 0 after. There was no compensating win to
 * pay for it, so the closures stay.
 *
 * This test is the reason they stay. It fails if someone applies m1
 * without also pushing an explicit `virtualizer.requestUpdate()` on
 * every piece of host state a card reads — which is nearly every reason
 * these views re-render, i.e. the same work under a longer name.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/artists-view/artists-view';
import '@components/genres-view/genres-view';
import { emit, stub, flush, resetHarness } from '@test/support/harness';
import { Events } from '../../src/events';
import { fixture, shadowAll } from '@test/support/render';

const ARTISTS = [
  { ID: 1, Name: 'Alpha', AlbumCount: 2, TrackCount: 9 },
  { ID: 2, Name: 'Beta', AlbumCount: 1, TrackCount: 4 },
];

const GENRES = [
  { name: 'Ambient', count: 12 },
  { name: 'Doom', count: 3 },
];

/** Give the virtualizer a viewport; a zero-height host renders nothing. */
function sized(el: HTMLElement): void {
  el.style.display = 'block';
  el.style.height = '600px';
  el.style.width = '900px';
}

async function settle(el: LitElement): Promise<void> {
  await flush();
  await el.updateComplete;
  await new Promise((r) => setTimeout(r, 80));
}

describe('a card grid shows its selection', () => {
  beforeEach(() => {
    resetHarness();
    stub('library.Library.GetAllArtists', ARTISTS);
    stub('library.Library.GetAllGenresWithCounts', GENRES);
    stub('library.Library.GetAllTracks', []);
    stub('library.Library.GetAllAlbums', []);
    // The views read through LibraryController, whose cache is only
    // primed by a scan-complete; without it they render nothing and the
    // assertion below fails for the wrong reason.
    emit(Events.LibraryScanComplete);
  });

  it('highlights an artist card that was ctrl-clicked', async () => {
    const el = await fixture<LitElement>('artists-view');

    sized(el);
    await settle(el);

    const card = shadowAll(el, '.artist-card')[0];

    expect(card, 'no artist cards rendered').toBeTruthy();

    card!.dispatchEvent(
      new MouseEvent('click', {
        bubbles: true,
        composed: true,
        ctrlKey: true,
      }),
    );
    await settle(el);

    // The clicked card specifically, not a count: a view is free to
    // arrive with something already selected, and the question here is
    // only whether the click reached the DOM.
    expect(shadowAll(el, '.artist-card')[0]?.classList.contains('selected')).toBe(
      true,
    );
  });

  it('highlights a genre card that was ctrl-clicked', async () => {
    const el = await fixture<LitElement>('genres-view');

    sized(el);
    await settle(el);

    const card = shadowAll(el, '.genre-card')[0];

    expect(card, 'no genre cards rendered').toBeTruthy();

    card!.dispatchEvent(
      new MouseEvent('click', {
        bubbles: true,
        composed: true,
        ctrlKey: true,
      }),
    );
    await settle(el);

    // The clicked card specifically, not a count: a view is free to
    // arrive with something already selected, and the question here is
    // only whether the click reached the DOM.
    expect(shadowAll(el, '.genre-card')[0]?.classList.contains('selected')).toBe(
      true,
    );
  });
});
