/**
 * Keyboard reachability.
 *
 * Tabbing through the whole app used to yield fourteen stops, every one
 * of them chrome: the sidebar was a list of `<li @click>`, a track list
 * had no tab stop at all, and the *closed* queue panel still had two
 * (`.planning/audits/2026-08-11-ui/hands-on.md`, H-5).
 */
import { describe, expect, it, beforeEach } from 'vitest';

import '@components/sidebar/app-sidebar';
import '@components/queue-panel/queue-panel';
import '@components/track-list/track-list';
import { stub, emit, flush } from '@test/support/harness';
import { Events } from '../../src/events';
import { fixture, shadow, shadowAll, update } from '@test/support/render';

/** Two fixture tracks, enough to move a focus ring between. */
const TRACKS = [
  {
    FilePath: '/music/a.mp3',
    TrackName: 'Alpha',
    ArtistName: 'One',
    Album: 'First',
    Duration: 100,
  },
  {
    FilePath: '/music/b.mp3',
    TrackName: 'Beta',
    ArtistName: 'Two',
    Album: 'First',
    Duration: 120,
  },
] as never[];

describe('<app-sidebar> is reachable', () => {
  // Eleven destinations assumes a configured download client, since
  // Downloads is not offered without one (#25).
  beforeEach(async () => {
    stub('download.Service.ListProviders', [
      { id: 1, kind: 'slskd', name: 'Sound', enabled: true, priority: 50 },
    ]);
    emit(Events.DownloadProvidersChanged);
    await flush();
  });

  it('renders every destination as a button, not a bare list item', async () => {
    const el = await fixture('app-sidebar');

    const items = shadowAll(el, 'li button');

    expect(items).toHaveLength(11);
    expect(items.every((item) => item.tagName === 'BUTTON')).toBe(true);
  });

  it('puts the nav in a landmark, so it can be jumped to', async () => {
    const el = await fixture('app-sidebar');

    expect(shadow(el, 'nav')?.getAttribute('aria-label')).toBe('Main');
  });
});

describe('<queue-panel> when closed', () => {
  it('is inert, so its buttons are not tab stops', async () => {
    const el = await fixture('queue-panel');

    expect(el.inert).toBe(true);
  });

  it('is not inert once opened', async () => {
    const el = await fixture('queue-panel');

    await update(el, { open: true });

    expect(el.inert).toBe(false);
  });
});

describe('<track-list> roving tabindex', () => {
  beforeEach(() => {
    stub('library.Library.GetTracks', TRACKS);
  });

  it('offers exactly one tab stop, however many rows there are', async () => {
    const el = await fixture('track-list', { externalTracks: TRACKS });

    const stops = shadowAll(el, '.track-row[tabindex="0"]');

    expect(stops).toHaveLength(1);
  });

  it('gives the rows grid semantics rather than none', async () => {
    const el = await fixture('track-list', { externalTracks: TRACKS });

    const row = shadow(el, '.track-row');

    expect({
      row: row?.getAttribute('role'),
      grid: shadow(el, '.table-container')?.getAttribute('role'),
      cell: shadow(el, '.track-row .cell')?.getAttribute('role'),
    }).toEqual({ row: 'row', grid: 'grid', cell: 'gridcell' });
  });

  it('moves the tab stop with the arrow keys', async () => {
    const el = await fixture('track-list', { externalTracks: TRACKS });

    shadow(el, '.table-container')?.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'ArrowDown',
        bubbles: true,
        cancelable: true,
      }),
    );
    await el.updateComplete;

    expect(
      shadow(el, '.track-row[tabindex="0"]')?.getAttribute('data-index'),
    ).toBe('1');
  });

  it('claims the tracklist shortcut scope while it is on screen', async () => {
    const el = await fixture('track-list', { externalTracks: TRACKS });

    expect(el.dataset['shortcutScope']).toBe('tracklist');
  });
});
