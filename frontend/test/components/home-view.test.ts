/**
 * The home page renders whatever `backend/home` decided, and nothing
 * else: the shelves, their stated reasons, and two things a cover can
 * do. So these tests are about the contract between the two — that a
 * shelf's reason is displayed rather than swallowed, that a card opens
 * the album it names, and that a play button plays it instead of
 * opening it.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import '@components/home-view/home-view';
import { stub, calls, lastArgs, stubFailure } from '@test/support/harness';
import { fixture, shadow, shadowAll, texts, text } from '@test/support/render';

function album(id: number, name: string, artist = 'Artist') {
  return {
    ID: id,
    Name: name,
    ArtistName: artist,
    MBID: '',
    Year: 2000,
    ReleaseYear: 2000,
    CoverArtPath: '',
    CoverArtSmall: '',
    CoverArtMedium: '',
    CoverArtLarge: '',
    ArtistMBID: '',
  };
}

const SHELVES = [
  {
    id: 'recently-played',
    kind: 'recently-played',
    title: 'Pick up where you left off',
    subtitle: 'The last albums you played',
    albums: [album(1, 'Kid A'), album(2, 'Amnesiac')],
  },
  {
    id: 'genre-Doom Jazz',
    kind: 'genre',
    title: 'Doom Jazz',
    subtitle: 'Because your library is full of it',
    albums: [album(3, 'Black Ships')],
  },
];

describe('home view', () => {
  beforeEach(() => {
    stub('home.Service.GetShelves', SHELVES);
    stub('library.Library.GetAlbumTracks', [
      { FilePath: '/music/1.mp3' },
      { FilePath: '/music/2.mp3' },
    ]);
  });

  it('renders a row per shelf, each with the reason it exists', async () => {
    const el = await fixture('home-view');
    await el.updateComplete;

    expect(texts(el, '.shelf-title')).toEqual([
      'Pick up where you left off',
      'Doom Jazz',
    ]);

    // The subtitle is the whole difference between a shelf and a grid.
    expect(texts(el, '.shelf-sub')).toEqual([
      'The last albums you played',
      'Because your library is full of it',
    ]);
  });

  it('draws a letter tile for an album with no cover, not a hole', async () => {
    // H-9: the missing-art placeholder was a small dim icon on a
    // surface the same colour as the page, so a shelf read as having
    // gaps in it — while the Albums and Artists grids both drew a
    // letter tile. Plainly visible in any Home screenshot, and
    // invisible to every assertion in this file until now.
    const el = await fixture('home-view');

    await el.updateComplete;
    await new Promise((r) => setTimeout(r, 20));

    const placeholders = shadowAll(el, '.art .placeholder');

    expect(placeholders.length, 'no placeholders for coverless albums').toBeGreaterThan(
      0,
    );
    // The initial of the album's own name, as cover-grid does it.
    expect(placeholders[0]?.textContent?.trim()).toBe('K');
    // And it is a tile: it fills the art box rather than sitting in it.
    expect(getComputedStyle(placeholders[0]!).backgroundImage).not.toBe('none');
  });

  it('keys each row by the kind the backend assigned', async () => {
    const el = await fixture('home-view');
    await el.updateComplete;

    expect(
      shadowAll(el, '.shelf').map((s) => s.getAttribute('data-kind')),
    ).toEqual(['recently-played', 'genre']);
  });

  it('opens the album a card names, by local id', async () => {
    const el = await fixture('home-view');
    await el.updateComplete;

    const seen: unknown[] = [];
    el.addEventListener('navigate', (e) => seen.push((e as CustomEvent).detail));

    shadow<HTMLElement>(el, '.card')!.click();

    expect(seen).toEqual([
      {
        view: 'explore-album-details',
        releaseGroupMBID: '',
        albumName: 'Kid A',
        artistName: 'Artist',
        localAlbumId: 1,
      },
    ]);
  });

  it('plays the album from the play button without navigating', async () => {
    const el = await fixture('home-view');
    await el.updateComplete;

    const seen: unknown[] = [];
    el.addEventListener('navigate', (e) => seen.push(e));

    shadow<HTMLElement>(el, '.play')!.click();
    await new Promise((r) => setTimeout(r, 0));

    expect(seen).toEqual([]);
    expect(lastArgs('library.Library.GetAlbumTracks')).toEqual([1, 0]);
    expect(lastArgs('queue.Queue.SetQueue')).toEqual([
      ['/music/1.mp3', '/music/2.mp3'],
      0,
      true,
      { type: 'album', id: 1, label: 'Kid A' },
    ]);
  });

  it('says so rather than rendering an empty page when there is nothing', async () => {
    stub('home.Service.GetShelves', []);

    const el = await fixture('home-view');
    await el.updateComplete;

    expect(text(el, '.empty')).toContain('Nothing to suggest yet');
  });

  it('reports a backend failure instead of pretending the library is empty', async () => {
    stubFailure('home.Service.GetShelves');

    const el = await fixture('home-view');
    await el.updateComplete;
    await el.updateComplete;

    expect(text(el, '.empty')).toContain('Could not read your library');
  });

  it('rebuilds on demand', async () => {
    const el = await fixture('home-view');
    await el.updateComplete;

    const before = calls('home.Service.GetShelves').length;

    shadow<HTMLElement>(el, 'wa-button')!.click();
    await el.updateComplete;

    expect(calls('home.Service.GetShelves').length).toBe(before + 1);
  });
});
