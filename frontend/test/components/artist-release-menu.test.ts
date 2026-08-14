/**
 * The context menu on a release card on the artist page.
 *
 * The page already had one, for the top *tracks*, and the release cards
 * — which are most of the page — had none: a right-click did whatever
 * the browser does and the keyboard had no way in at all.
 *
 * What is worth pinning is not that the menu opens. It is that the
 * items shown match what the release can actually do, because the three
 * cases genuinely differ: an owned release has files to queue, an
 * unowned catalog release has nothing to play but can be requested, and
 * a library-only release has no catalog id, so it can be played and is
 * the one case with nothing to view upstream. A menu that offers Play
 * on a release with no files is the fault `library-status-indicator`
 * was rewritten to stop making, one control over.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/explore-artist-details/explore-artist-details';
import { stub, flush, resetHarness } from '@test/support/harness';
import { fixture, shadow } from '@test/support/render';

const ARTIST = 'artist-0001';

/** The labels of the open menu's items, trimmed. */
function menuItems(el: LitElement): string[] {
  const panel = shadow(el, '.context-menu-panel');

  if (!panel) return [];

  return [...panel.querySelectorAll('wa-dropdown-item')].map(
    (item) => item.textContent?.trim() ?? '',
  );
}

/** Right-click the nth discography card and let the menu render. */
async function openMenuOnAlbum(el: LitElement, index: number): Promise<void> {
  const cards = el.shadowRoot?.querySelectorAll('.album-card') ?? [];
  const card = cards[index];

  expect(card, `no album card at index ${index}`).toBeTruthy();

  card?.dispatchEvent(
    new MouseEvent('contextmenu', { bubbles: true, cancelable: true }),
  );

  await flush();
}

beforeEach(() => {
  resetHarness();

  stub('explore.Service.LookupArtist', { mbid: ARTIST, name: 'Tideline' });
  stub('explore.Service.TopReleaseGroupsForArtist', []);
  stub('explore.Service.TopRecordingsForArtist', []);
  stub('explore.Service.SimilarArtists', []);
  stub('explore.Service.PrefetchReleases', undefined);

  // One of each case, in the order the assertions below index them.
  stub('explore.Service.BrowseReleaseGroups', [
    {
      mbid: 'rg-owned',
      title: 'Foreshore',
      artistCredit: 'Tideline',
      primaryType: 'Album',
      inLibrary: true,
      localId: 7,
    },
    {
      mbid: 'rg-wanted',
      title: 'Backwash',
      artistCredit: 'Tideline',
      primaryType: 'Album',
    },
    {
      mbid: 'local:12',
      title: 'Bootleg Tape',
      artistCredit: 'Tideline',
      primaryType: 'Album',
      localId: 12,
    },
  ]);
});

async function mount(): Promise<LitElement> {
  const el = await fixture<LitElement>('explore-artist-details', {
    artistMBID: ARTIST,
    artistName: 'Tideline',
  });

  await flush();

  return el;
}

describe('the context menu on an artist page release', () => {
  it('opens on a right-click and names itself a release menu', async () => {
    const el = await mount();

    await openMenuOnAlbum(el, 0);

    const panel = shadow(el, '.context-menu-panel');

    expect(panel).toBeTruthy();
    // The panel is shared with the track menu, so a label that does not
    // move with the target is confidently wrong rather than merely
    // missing.
    expect(panel?.getAttribute('aria-label')).toBe('Release actions');
  });

  it('offers playback for a release with local files', async () => {
    const el = await mount();

    await openMenuOnAlbum(el, 0);

    const items = menuItems(el);

    expect(items).toContain('Play');
    expect(items).toContain('Add to Queue');
    expect(items).toContain('Play Next');
    // Owned: there is nothing left to ask for.
    expect(items).not.toContain('Want This');
  });

  it('offers a request, and no playback, for a release nobody owns', async () => {
    const el = await mount();

    await openMenuOnAlbum(el, 1);

    const items = menuItems(el);

    expect(items).not.toContain('Play');
    expect(items).not.toContain('Add to Queue');
    expect(items).toContain('Want This');
    expect(items).toContain('View on MusicBrainz');
  });

  it('drops the catalog items for a library-only release', async () => {
    const el = await mount();

    await openMenuOnAlbum(el, 2);

    const items = menuItems(el);

    // It has files, so it plays…
    expect(items).toContain('Play');
    // …but a `local:` id names nothing upstream, and wanting something
    // already in the library is not a thing to offer.
    expect(items).not.toContain('View on MusicBrainz');
    expect(items).not.toContain('Want This');
  });

  it('opens from the keyboard on Shift+F10', async () => {
    const el = await mount();

    const card = el.shadowRoot?.querySelectorAll('.album-card')[0];

    card?.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'F10',
        shiftKey: true,
        bubbles: true,
        cancelable: true,
      }),
    );

    await flush();

    expect(shadow(el, '.context-menu-panel')).toBeTruthy();
  });
});
