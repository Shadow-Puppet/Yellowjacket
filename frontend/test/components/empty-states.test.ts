/**
 * "Loading tracks…" used to be what the track list said when it was
 * loading, when it was empty, and when the query had failed — including
 * on the first screen a new user ever sees, behind the first-run wizard
 * (errors.M2, H-12). Three situations, three different things to say.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/track-list/track-list';
import { Events } from '../../src/events';
import { emit, stub, stubFailure, flush, resetHarness } from '@test/support/harness';
import { fixture, shadow, text } from '@test/support/render';

/** Drop the library store's cache so the list has to fetch. */
async function emptyLibrary(): Promise<void> {
  resetHarness();
  stub('library.Library.GetAllTracks', []);
  stub('library.Library.GetAllAlbums', []);
  stub('library.Library.GetAllArtists', []);
  stub('library.Library.GetAllGenresWithCounts', []);
  emit(Events.LibraryScanComplete);
  await flush();
}

describe('<track-list> empty, loading and failed', () => {
  beforeEach(async () => {
    await emptyLibrary();
  });

  it('says the library is empty when it is empty', async () => {
    const el = await fixture<LitElement>('track-list');

    await flush();
    await el.updateComplete;

    expect(text(el, '[data-testid="track-list-empty"]')).toContain(
      'No tracks yet',
    );
  });

  it('says the query failed, and offers to try again', async () => {
    stubFailure('library.Library.GetAllTracks', 'sql: database is locked');
    emit(Events.LibraryScanComplete);
    await flush();

    const el = await fixture<LitElement>('track-list');

    await flush();
    await el.updateComplete;

    expect([
      text(el, '[data-testid="track-list-error"]'),
      shadow(el, '[data-testid="track-list-loading"]'),
    ]).toEqual([expect.stringContaining('busy'), null]);
  });

  it('does not claim to be loading a list it was handed', async () => {
    const el = await fixture<LitElement>('track-list', { externalTracks: [] });

    await flush();
    await el.updateComplete;

    expect(text(el, '[data-testid="track-list-empty"]')).toBe(
      'Nothing here yet.',
    );
  });
});
