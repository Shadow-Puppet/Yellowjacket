/**
 * "Review in Autotag" has to land on *that* album.
 *
 * The album page can now say the autotagger has a match for what you
 * are looking at (#28), and the review link is only worth having if it
 * opens the same album. The queue is sorted by score, so the intended
 * folder is often near the top — but "often" is a link that sometimes
 * opens a different album, which is worse than no link.
 *
 * Autotag is a **cached primary view**: `index.ts` creates it once and
 * reuses it, so there is no construction to pass a value to. The
 * request arrives as an attribute, which is why the interesting part
 * is that it is *consumed* — an attribute left on a cached element
 * would reopen the same folder on every later visit to the page.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/autotag-view/autotag-view';
import { flush, resetHarness, stub } from '@test/support/harness';
import { fixture } from '@test/support/render';

const FOLDERS = 'autotagservice.Service.ListPendingFolders';
const CANDIDATES = 'autotagservice.Service.GetCandidates';

function folder(groupKey: string, album: string) {
  return {
    groupKey,
    libraryId: 0,
    libraryName: 'Test',
    folderSubPath: album,
    trackCount: 10,
    albumName: album,
    albumArtist: 'Tideline',
    discNumber: 0,
    status: 'pending',
    score: groupKey === 'grp-top' ? 0.99 : 0.5,
    bestMatchReleaseMbid: 'rel-1',
    synthetic: false,
    likelyMixedBag: false,
  };
}

/** Mount the view and run its activation, as navigation would. */
async function autotag(groupKey?: string): Promise<LitElement> {
  const el = await fixture<LitElement>('autotag-view');

  if (groupKey !== undefined) el.setAttribute('group-key', groupKey);

  (el as unknown as { onViewActivate: () => void }).onViewActivate();

  await flush();
  await el.updateComplete;
  await flush();
  await el.updateComplete;

  return el;
}

/** The folder the view has selected. */
function selected(el: LitElement): string | undefined {
  return (el as unknown as { current?: { groupKey: string } }).current
    ?.groupKey;
}

beforeEach(() => {
  resetHarness();
  stub('autotagservice.Service.StartAutotagQueue', null);
  stub('autotagservice.Service.GetLocalCoverArt', '');
  stub('autotagservice.Service.AckLibraryWarning', null);
  stub('library.Library.GetAllLibrariesWithTrackCounts', []);
  stub(FOLDERS, [folder('grp-top', 'Loudest Match'), folder('grp-asked', 'Glass Harbour')]);
  stub(CANDIDATES, {
    groupKey: 'grp-asked',
    recommendation: 'strong',
    localTracks: [],
    candidates: [],
    synthetic: false,
    mixedBag: false,
  });
});

describe('arriving at Autotag from an album page', () => {
  it('opens the folder that was asked for, not the top of the queue', async () => {
    const el = await autotag('grp-asked');

    expect(selected(el)).toBe('grp-asked');
  });

  it('still lands on the best pending folder when nothing was asked', async () => {
    const el = await autotag();

    expect(selected(el)).toBe('grp-top');
  });

  /**
   * The view is cached and never unmounts, so an attribute left behind
   * is a standing instruction: every later visit to Autotag would
   * reopen an album the user finished with three navigations ago.
   */
  it('consumes the request rather than remembering it', async () => {
    const el = await autotag('grp-asked');

    expect(el.hasAttribute('group-key')).toBe(false);

    // A second visit, with no new request: whatever the user had
    // selected stays selected.
    (el as unknown as { onViewActivate: () => void }).onViewActivate();
    await flush();
    await el.updateComplete;

    expect(selected(el)).toBe('grp-asked');
  });
});
