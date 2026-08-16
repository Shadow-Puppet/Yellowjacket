/**
 * Expanding an album shows its tracks.
 *
 * `perf.p2` files `cover-grid`'s `renderSplitGrid` as dead code carried
 * in the bundle. It is not dead — it is a **missing feature**, and one
 * whose data path was already working: pressing Enter on an album card
 * fetched that album's tracks over the IPC, ran the whole split state
 * machine (measured in the running app: `splitMode: true`,
 * `splitIndex: 90` against a real container) and then drew the single
 * grid regardless, because `render()` never consulted `splitMode`.
 * `connectedCallback` referenced the method solely to satisfy
 * `noUnusedLocals`.
 *
 * It matters because it is the only route from the albums grid to a
 * *track*: a plain click on a card navigates to the catalog page, so
 * without the dropdown the view could not show what is on an album.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/cover-grid/cover-grid';
import { emit, stub, flush, resetHarness } from '@test/support/harness';
import { Events } from '../../src/events';
import { fixture, shadow, shadowAll } from '@test/support/render';

/**
 * Enough albums to fill more than one row.
 *
 * The dropdown is drawn *after the row the expanded album is in*, so a
 * library that fits on one row puts every album in the "before" half
 * and renders no "after" grid at all — which is correct, and is not the
 * arrangement this is checking.
 */
const ALBUMS = Array.from({ length: 24 }, (_, i) => ({
  ID: i + 1,
  Name: `Album ${i + 1}`,
  ArtistName: 'Aurora Fields',
  Year: 2019,
}));

const TRACKS = [
  { ID: 11, TrackName: 'Salt Air', FilePath: '/m/1.mp3', TrackNumber: 1 },
  { ID: 12, TrackName: 'Tideline', FilePath: '/m/2.mp3', TrackNumber: 2 },
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

/**
 * Open the dropdown the way the app does.
 *
 * A plain *click* navigates to the catalog page — Enter (or Space) on a
 * focused card is the only thing that expands one, which is why the
 * feature could be missing without anyone tripping over it.
 */
async function expandFirstCard(el: LitElement): Promise<void> {
  const card = shadowAll(el, '.album-card')[0] as HTMLElement;

  card.focus();
  card.dispatchEvent(
    new KeyboardEvent('keydown', {
      key: 'Enter',
      bubbles: true,
      composed: true,
    }),
  );

  await settle(el);
}

describe('the album dropdown', () => {
  beforeEach(() => {
    resetHarness();
    stub('library.Library.GetAlbums', ALBUMS);
    stub('library.Library.GetTracks', []);
    stub('library.Library.GetAlbumTracks', TRACKS);
    stub('library.Library.GetAlbumTracks', TRACKS);
    emit(Events.LibraryScanComplete);
  });

  it('draws the tracks it fetches, between two halves of the grid', async () => {
    const el = await fixture<LitElement>('cover-grid');

    sized(el);
    await settle(el);

    expect(shadow(el, 'album-dropdown')).toBeNull();

    await expandFirstCard(el);

    // The state machine was always correct; what was missing was the
    // render. Assert on what is *drawn*, so a future `render()` that
    // stops consulting `splitMode` fails here rather than silently
    // going back to fetching tracks nobody sees.
    const dropdown = shadow(el, 'album-dropdown');

    expect(dropdown).toBeTruthy();
    expect(
      dropdown!.shadowRoot!.querySelectorAll('.track-row'),
    ).toHaveLength(TRACKS.length);

    const ids = shadowAll(el, 'lit-virtualizer').map((v) => v.id);

    expect(ids).toEqual(['grid-before', 'grid-after']);
    expect(shadow(el, '#grid-single')).toBeNull();
  });

  it('keeps the listbox semantics on both halves', async () => {
    // The single grid became a `listbox` of `option`s in the ARIA pass;
    // the split path predates it. They are one control to the user, and
    // a selection spanning the dropdown has to be announced the same
    // way on either side of it.
    const el = await fixture<LitElement>('cover-grid');

    sized(el);
    await settle(el);
    await expandFirstCard(el);

    for (const v of shadowAll(el, 'lit-virtualizer')) {
      expect(v.getAttribute('role')).toBe('listbox');
      expect(v.getAttribute('aria-multiselectable')).toBe('true');
      expect(v.getAttribute('aria-label')).toBeTruthy();
    }
  });

  it('closes back to the single grid', async () => {
    const el = await fixture<LitElement>('cover-grid');

    sized(el);
    await settle(el);
    await expandFirstCard(el);
    expect(shadow(el, 'album-dropdown')).toBeTruthy();

    // Enter on the same card again is the toggle.
    await expandFirstCard(el);

    expect(shadow(el, 'album-dropdown')).toBeNull();
    expect(shadowAll(el, 'lit-virtualizer').map((v) => v.id)).toEqual([
      'grid-single',
    ]);
  });
});

describe('the albums grid scrolls', () => {
  beforeEach(() => {
    resetHarness();
    stub('library.Library.GetAlbums', ALBUMS);
    stub('library.Library.GetTracks', []);
    emit(Events.LibraryScanComplete);
  });

  it('gives its scroll container an overflow', async () => {
    // `cover-grid` carried the same `.grid-scroll-container` markup as
    // `artists-view` and `genres-view` with **no rule for the class**,
    // so the container grew to its full content height inside an
    // `overflow: hidden` host and nothing scrolled: measured at 5 000
    // albums, 186 984 px of content in a 772 px box, unreachable by
    // wheel, keyboard or scrollbar. Invisible on an eight-album fixture,
    // which is why a component test asserts the rule rather than the
    // symptom.
    const el = await fixture<LitElement>('cover-grid');

    sized(el);
    await settle(el);

    const container = shadow(el, '.grid-scroll-container')!;

    expect(container).toBeTruthy();
    expect(getComputedStyle(container).overflowY).toBe('auto');
  });
});
