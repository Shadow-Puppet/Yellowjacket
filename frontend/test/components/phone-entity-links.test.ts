/**
 * A name is not a link on a phone, and the menu is where it went (#67).
 *
 * `utils/explore-link.ts` makes every track, album and artist name
 * navigable, with click handling that is explicitly a desktop
 * compromise — the navigation is held for one double-click interval so
 * double-clicking the row can still play it. On touch that is a delay
 * on an ambiguous target, and since #63 the row's own tap claims the
 * click anyway, so the link was unreachable as well as fiddly.
 *
 * So below the phone breakpoint a name renders as plain text and the
 * row's context menu carries "Go to Artist" / "Go to Album" instead.
 * The two halves are asserted together on purpose: a suppressed link
 * with no menu item behind it is not a smaller affordance, it is a
 * destination that cannot be reached, which is what plan 018 promises
 * against.
 *
 * The breakpoint is stubbed rather than emulated for the reason
 * `now-playing-phone.test.ts` states: this tier's viewport is fixed at
 * 1280x800 by the runner, and `matchMedia` is the seam.
 */
import { describe, expect, it, beforeEach, afterEach } from 'vitest';
import { html, render } from 'lit';
import type { LitElement } from 'lit';

import '@components/playlist-details/playlist-details';
import {
  albumLink,
  artistLink,
  creditLink,
  trackLink,
} from '@utils/explore-link';
import { goToMenuItems } from '@utils/go-to-menu';
import { stub, flush, resetHarness } from '@test/support/harness';
import { fixture, shadowAll } from '@test/support/render';

/** Answer the phone breakpoint, and hand back the undo. */
function atPhone(phone: boolean): () => void {
  const real = window.matchMedia.bind(window);

  window.matchMedia = ((q: string) =>
    q.includes('max-width: 599px')
      ? {
          matches: phone,
          media: q,
          addEventListener() {},
          removeEventListener() {},
        }
      : real(q)) as typeof window.matchMedia;

  return () => {
    window.matchMedia = real as typeof window.matchMedia;
  };
}

/** Render a template into a detached container and hand it back. */
function draw(template: unknown): HTMLElement {
  const host = document.createElement('div');

  document.body.append(host);
  render(html`${template}`, host);

  return host;
}

describe('an inline name below the phone breakpoint', () => {
  let restore: () => void = () => {};

  afterEach(() => {
    restore();
    document.querySelectorAll('body > div').forEach((el) => el.remove());
  });

  it('is a link on a desktop', () => {
    restore = atPhone(false);

    const host = draw(artistLink('Cocteau Twins', 'artist-mbid'));

    expect(host.querySelector('a.explore-link')).not.toBeNull();
    expect(host.textContent?.trim()).toBe('Cocteau Twins');
  });

  it('is plain text on a phone, for all four shapes', () => {
    restore = atPhone(true);

    const host = draw(html`
      ${artistLink('Cocteau Twins', 'artist-mbid')}
      ${albumLink('Heaven or Las Vegas', 'rg-mbid')}
      ${trackLink('Iceblink Luck', 'Heaven or Las Vegas', 'rg-mbid', 'rec-mbid')}
      ${creditLink(
        [
          {
            creditedName: 'Skrillex',
            artistMbid: 'a1',
            joinPhrase: ' feat. ',
          },
          { creditedName: 'Swae Lee', artistMbid: 'a2', joinPhrase: '' },
        ],
        'Skrillex & Swae Lee',
        'a1',
      )}
    `);

    expect(host.querySelectorAll('a.explore-link')).toHaveLength(0);

    // The words survive, join phrases included — a decomposed credit is
    // still assembled from its parts, so the text does not change with
    // the affordance.
    expect(host.textContent).toContain('Cocteau Twins');
    expect(host.textContent).toContain('Heaven or Las Vegas');
    expect(host.textContent).toContain('Iceblink Luck');
    expect(host.textContent).toContain('Skrillex feat. Swae Lee');
  });

  it('stays a link where the caller has no menu to carry it', () => {
    restore = atPhone(true);

    const host = draw(
      albumLink('Heaven or Las Vegas', 'rg-mbid', undefined, 'Cocteau Twins', {
        keepOnPhone: true,
      }),
    );

    expect(host.querySelector('a.explore-link')).not.toBeNull();
  });
});

describe('the "Go to" menu items', () => {
  let restore: () => void = () => {};

  afterEach(() => {
    restore();
    document.querySelectorAll('body > div').forEach((el) => el.remove());
  });

  it('are absent on a desktop, where the name beside them is a link', () => {
    restore = atPhone(false);

    const host = draw(
      goToMenuItems({ artistName: 'Cocteau Twins', albumName: 'Treasure' }),
    );

    expect(host.querySelectorAll('wa-dropdown-item')).toHaveLength(0);
  });

  it('offer only what the target knows', () => {
    restore = atPhone(true);

    const both = draw(
      goToMenuItems({ artistName: 'Cocteau Twins', albumName: 'Treasure' }),
    );
    const artistOnly = draw(goToMenuItems({ artistName: 'Cocteau Twins' }));
    const neither = draw(goToMenuItems({}));

    expect(both.querySelectorAll('wa-dropdown-item')).toHaveLength(2);
    expect(artistOnly.querySelectorAll('wa-dropdown-item')).toHaveLength(1);
    expect(neither.querySelectorAll('wa-dropdown-item')).toHaveLength(0);
  });
});

// =====================================================================
// The menu that carries the destination
// =====================================================================

function playlistTracks(n: number) {
  return Array.from({ length: n }, (_, i) => ({
    ID: i + 1,
    FilePath: `/music/track-${i}.mp3`,
    Title: `Track ${i}`,
    Artist: 'Cocteau Twins',
    ArtistMBID: 'artist-mbid',
    Album: 'Heaven or Las Vegas',
    ReleaseGroupMBID: 'rg-mbid',
    Duration: 180000,
    Phantom: false,
  }));
}

describe('a playlist row’s context menu on a phone', () => {
  let el: LitElement;
  let restore: () => void = () => {};

  beforeEach(async () => {
    resetHarness();
    restore = atPhone(true);
    stub('playlist.Service.GetPlaylistTracks', playlistTracks(8));
    stub('playlist.Service.GetAllPlaylists', []);

    el = await fixture<LitElement>('playlist-details', {
      playlistId: 1,
      playlistName: 'A playlist',
    });
    el.style.display = 'block';
    el.style.height = '600px';
    await flush();
    await el.updateComplete;
    await new Promise((r) => setTimeout(r, 60));
  });

  afterEach(() => {
    restore();
  });

  /** Right-click a row and hand back the menu's items. */
  async function openMenu(index: number): Promise<HTMLElement[]> {
    const row = shadowAll(el, '.track-item').find(
      (r) => r.getAttribute('data-index') === String(index),
    );

    row!.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, composed: true }),
    );
    await el.updateComplete;

    return shadowAll<HTMLElement>(el, 'wa-dropdown-item');
  }

  it('carries the artist and the album the row stopped linking to', async () => {
    const labels = (await openMenu(3)).map((i) => i.textContent?.trim());

    expect(labels).toContain('Go to Artist');
    expect(labels).toContain('Go to Album');
  });

  it('navigates where the name would have', async () => {
    const seen: CustomEvent[] = [];
    const listen = (e: Event) => seen.push(e as CustomEvent);

    document.addEventListener('navigate', listen);

    try {
      const items = await openMenu(3);

      items
        .find((i) => i.textContent?.trim() === 'Go to Artist')!
        .click();
      await flush();
    } finally {
      document.removeEventListener('navigate', listen);
    }

    expect(seen.map((e) => e.detail)).toEqual([
      {
        view: 'explore-artist-details',
        artistMBID: 'artist-mbid',
        artistName: 'Cocteau Twins',
      },
    ]);
  });

  it('is absent while several rows are selected', async () => {
    // "Go to the album" of five different albums means nothing, which
    // is the rule the Play item already follows: one row is a
    // position, several are an explicit choice of those tracks.
    const rows = shadowAll(el, '.track-item');
    const click = (i: number, modifiers: MouseEventInit) =>
      rows
        .find((r) => r.getAttribute('data-index') === String(i))!
        .dispatchEvent(
          new MouseEvent('click', {
            bubbles: true,
            composed: true,
            ...modifiers,
          }),
        );

    click(1, {});
    click(4, { ctrlKey: true });
    await el.updateComplete;

    const labels = (await openMenu(4)).map((i) => i.textContent?.trim());

    expect(labels).not.toContain('Go to Artist');
  });
});
