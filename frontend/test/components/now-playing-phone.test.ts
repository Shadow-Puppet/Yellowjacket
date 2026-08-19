/**
 * The mini player's links are a desktop affordance.
 *
 * `utils/explore-link.ts` makes every track and artist name navigate,
 * and `utils/queue-source-link.ts` makes "Playing from X" navigate — in
 * the bottom bar those are a few characters of text at a font size
 * chosen for a bar, which is not a touch target. Worse, explore-link
 * holds the navigation for one double-click interval and drops it if a
 * second click arrives: a gesture that exists so double-clicking a row
 * can play it, and which means nothing at all on touch.
 *
 * So below the shell's phone breakpoint the three render as plain text
 * and the whole bar's cover art opens the full-screen Now Playing view,
 * which is where the links live.
 *
 * The breakpoint is stubbed rather than emulated for the reason
 * track-list-phone.test.ts states: this tier's viewport is fixed at
 * 1280x800 by the runner, and the component reads matchMedia in
 * connectedCallback precisely so a test can answer it first.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import '@components/now-playing/now-playing';
import { Events } from '../../src/events';
import { emit, flush } from '@test/support/harness';
import { fixture, shadow, shadowAll, text } from '@test/support/render';
import type { TrackInfo } from '@store/player-store';
import type { QueueTrack } from '@store/queue-store';

const TRACK: TrackInfo = {
  fileName: 'ashes.mp3',
  filePath: '/music/ashes.mp3',
  trackLength: 215,
  seekPosition: 0,
  state: 'playing',
  title: 'Ashes to Ashes',
  artist: 'David Bowie',
  album: 'Scary Monsters',
  coverArt: '',
  coverArtSmall: '',
  coverArtMedium: '',
  coverArtLarge: '',
  trackChangeId: 1,
  artistMbid: '',
  releaseGroupMbid: '',
  recordingMbid: '',
};

function queueTrack(n: number, title: string): QueueTrack {
  return {
    id: n,
    audioFileId: n,
    filePath: `/music/${n}.mp3`,
    position: n,
    title,
    artist: 'David Bowie',
    album: 'Scary Monsters',
    coverArtPath: '',
    artistMbid: '',
    releaseGroupMbid: '',
    recordingMbid: '',
  };
}

/** Mount the bar with the phone breakpoint answering `matches`. */
async function mountAt(phone: boolean) {
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

  try {
    const el = await fixture('now-playing');

    emit(Events.TrackChanged, { ...TRACK, trackChangeId: 20 });
    emit(Events.QueueChanged, {
      tracks: [queueTrack(1, 'Ashes to Ashes')],
      currentIndex: 0,
      source: { type: 'album', id: 7, label: 'Scary Monsters' },
    });
    await flush();
    await el.updateComplete;

    return el;
  } finally {
    window.matchMedia = real;
  }
}

describe('the mini player on a phone', () => {
  beforeEach(() => {
    emit(Events.TrackChanged, { ...TRACK, trackChangeId: 1 });
  });

  it('renders the title and artist as plain text', async () => {
    const el = await mountAt(true);

    expect(shadowAll(el, '.explore-link').length).toBe(0);

    // The words are unchanged — this is about what they are, not about
    // hiding them. A fix that dropped the text would pass an assertion
    // about links alone.
    expect(text(el, '[data-testid="now-playing-title"]')).toContain(
      'Ashes to Ashes',
    );
    expect(text(el, '[data-testid="now-playing-artist"]')).toContain(
      'David Bowie',
    );
  });

  it('does not navigate from the source line', async () => {
    const el = await mountAt(true);
    const source = shadow<HTMLElement>(el, '[data-testid="now-playing-source"]');

    expect(source?.classList.contains('navigable')).toBe(false);

    let navigated = false;
    el.addEventListener('navigate', () => {
      navigated = true;
    });

    source?.click();

    expect(navigated).toBe(false);
  });

  it('still says where the queue came from', async () => {
    const el = await mountAt(true);

    // Dropping the *link* is the change; dropping the information would
    // be a different and worse one.
    expect(text(el, '[data-testid="now-playing-source"]')).toBe(
      'Playing from Scary Monsters',
    );
  });

  it('leaves the desktop bar exactly as it was', async () => {
    const el = await mountAt(false);

    expect(shadowAll(el, '.explore-link').length).toBeGreaterThan(0);

    const source = shadow<HTMLElement>(el, '[data-testid="now-playing-source"]');

    expect(source?.classList.contains('navigable')).toBe(true);

    let detail: unknown;
    el.addEventListener('navigate', (e) => {
      detail = (e as CustomEvent).detail;
    });

    source?.click();

    expect(detail).toEqual({
      view: 'explore-album-details',
      localAlbumId: 7,
      albumName: 'Scary Monsters',
    });
  });
});
