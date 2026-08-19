/**
 * Being told about a match while looking at the album.
 *
 * The complaint (#28) is that the user had to notice their metadata
 * was missing and then go and hunt the album down on the Autotag page.
 * So the suggestion is drawn here, with something to do about it — and
 * the something rewrites files on disk, which is what most of this
 * file is about.
 *
 * The confidence tier behind "MusicBrainz has a match" is decided in
 * the backend (`autotag.ConfidentTier`) so this page and strict
 * auto-accept cannot disagree about what it means; what is pinned here
 * is only what the page does with the answer.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';
import { page } from 'vitest/browser';

import '@components/explore-album-details/explore-album-details';
import { stub, stubFailure, flush, resetHarness, calls } from '@test/support/harness';
import { notificationStore } from '@store/notification-store';
import '@components/notifications/notification-host';
import { fixture, shadow, shadowAll } from '@test/support/render';

const MATCH = 'autotagservice.Service.MatchForAlbum';
const APPLY = 'autotagservice.Service.ApplyAsync';

function match(over: Record<string, unknown> = {}) {
  return {
    groupKey: 'grp-1',
    recommendation: 'strong',
    score: 0.95,
    releaseMbid: 'rel-1',
    title: 'Glass Harbour',
    artistCredit: 'Tideline',
    trackCount: 10,
    groupCount: 1,
    ...over,
  };
}

async function albumPage(): Promise<LitElement> {
  const el = await fixture<LitElement>('explore-album-details', {
    albumName: 'Glass Harbour',
    localAlbumId: 7,
  });

  await flush();
  await el.updateComplete;

  return el;
}

/** The confirm dialog attaches itself to the document on first use. */
function confirmHost(): (LitElement & { shadowRoot: ShadowRoot | null }) | null {
  return document.querySelector('confirm-dialog');
}

/** Press one of the dialog's own buttons, the way a person would. */
async function pressConfirm(testid: string): Promise<void> {
  const host = confirmHost();

  if (!host) throw new Error('confirm-dialog did not mount itself');

  await host.updateComplete;
  host.shadowRoot
    ?.querySelector<HTMLButtonElement>(`[data-testid="${testid}"]`)
    ?.click();
  await host.updateComplete;
  await flush();
}

/** Click one of the banner's buttons by its label. */
async function pressBanner(el: LitElement, label: string): Promise<void> {
  shadowAll<HTMLElement>(el, '.autotag-match-actions wa-button')
    .find((b) => b.textContent?.includes(label))
    ?.click();
  await flush();
}

beforeEach(() => {
  resetHarness();
  notificationStore.clear();
  stub('explore.Service.BrowseReleases', []);
  stub('explore.Service.LookupReleaseGroup', null);
  stub('explore.Service.GetThumbnail', '');
  stub('library.Library.GetAlbumTracks', []);
  stub('library.Library.GetAlbumCompleteness', {
    owned: 0,
    expected: 0,
    known: false,
    complete: false,
  });
  stub('library.Library.GetAllLibrariesWithTrackCounts', []);
  stub('library.Library.GetFilePathsByRecordingMBIDs', {});
  stub(MATCH, null);
});

describe('the autotag suggestion', () => {
  it('says nothing when the backend has nothing confident', async () => {
    const el = await albumPage();

    expect(shadow(el, '.autotag-match')).toBeNull();
  });

  /**
   * A pure catalog page has no files to retag, so the question is not
   * asked at all — this runs on every album open and a call that
   * cannot have an answer is a call not worth making.
   */
  it('is not even asked about an album with no local files', async () => {
    stub(MATCH, match());

    const el = await fixture<LitElement>('explore-album-details', {
      albumName: 'Glass Harbour',
      releaseGroupMBID: 'rg-1',
    });

    await flush();
    await el.updateComplete;

    expect(calls(MATCH)).toHaveLength(0);
    expect(shadow(el, '.autotag-match')).toBeNull();
  });

  /**
   * The banner names the release rather than quoting a number: 0.95
   * reads as a probability and is not one, and which release it is, is
   * the thing the user can actually judge.
   */
  it('names the release it is offering', async () => {
    stub(MATCH, match());

    const el = await albumPage();
    const text = shadow(el, '.autotag-match')?.textContent ?? '';

    expect(text).toContain('MusicBrainz has a match');
    expect(text).toContain('Glass Harbour');
    expect(text).toContain('Tideline');
    expect(text).not.toContain('95');
  });

  it('offers both an apply and a review', async () => {
    stub(MATCH, match());

    await albumPage();

    await expect
      .element(page.getByRole('button', { name: 'Apply tags' }))
      .toBeInTheDocument();
    await expect
      .element(page.getByRole('button', { name: 'Review in Autotag' }))
      .toBeInTheDocument();
  });

  /**
   * A tagging group is a folder, so a multi-disc album is several. One
   * button that applied to the best-scoring one would leave the album
   * holding a mix of old and new tags — which is the case the app's
   * Blocking notification level exists for, and is worth not creating.
   */
  it('will not apply to an album filed as several folders', async () => {
    stub(MATCH, match({ groupCount: 2 }));

    const el = await albumPage();
    const labels = shadowAll(el, '.autotag-match-actions wa-button').map(
      (b) => b.textContent?.trim(),
    );

    expect(labels).toEqual(['Review in Autotag']);
    expect(shadow(el, '.autotag-match')?.textContent).toContain('2 folders');
  });

  it('navigates to Autotag carrying the group key', async () => {
    stub(MATCH, match());

    const el = await albumPage();
    const seen: CustomEvent[] = [];

    el.addEventListener('navigate', (e) => seen.push(e as CustomEvent));

    await pressBanner(el, 'Review');

    expect(seen).toHaveLength(1);
    expect(seen[0]?.detail).toMatchObject({
      view: 'autotag',
      groupKey: 'grp-1',
    });
  });
});

describe('applying from the album page', () => {
  /**
   * This rewrites tags in files on disk and cannot be undone, so it
   * asks first — and cancelling has to be a true no-op, not a
   * confirmation that fires the call anyway.
   */
  it('asks before it writes, and cancelling writes nothing', async () => {
    stub(MATCH, match());
    stub(APPLY, null);

    const el = await albumPage();

    await pressBanner(el, 'Apply');

    expect(confirmHost()).not.toBeNull();
    expect(calls(APPLY)).toHaveLength(0);

    await pressConfirm('confirm-cancel');
    await el.updateComplete;

    expect(calls(APPLY)).toHaveLength(0);
    expect(shadow(el, '.autotag-match')).not.toBeNull();
  });

  /**
   * The impact line has to say the thing that cannot be taken back, in
   * those words — "cannot be undone" — and that nothing is deleted,
   * because "rewrites your files" reads worse than it is.
   */
  it('says what cannot be undone', async () => {
    stub(MATCH, match());

    const el = await albumPage();

    await pressBanner(el, 'Apply');

    const text = confirmHost()?.shadowRoot?.textContent ?? '';

    expect(text).toContain('cannot be');
    expect(text).toContain('undone');
    expect(text.toLowerCase()).toContain('nothing is moved or deleted');
  });

  /**
   * `ApplyAsync` is the registered-job path, so progress belongs to
   * the jobs indicator and this page does not grow a second one. What
   * it owes the user is an acknowledgement, because the button is
   * here — and the suggestion has to stop inviting a second click.
   */
  it('hands the work to the job registry and stands down', async () => {
    stub(MATCH, match());
    stub(APPLY, null);

    const el = await albumPage();

    await pressBanner(el, 'Apply');
    await pressConfirm('confirm-accept');
    await el.updateComplete;

    expect(calls(APPLY)).toHaveLength(1);
    // The release is passed explicitly: a rescore between the page
    // rendering and the click must not swap the album out from under
    // a button the user has already read.
    expect(calls(APPLY)[0]?.args).toEqual(['grp-1', 'rel-1']);
    expect(shadow(el, '.autotag-match')).toBeNull();
  });

  /**
   * A failure is Persistent, not Transient: the user asked for
   * something that did not happen and retrying is meaningful, which is
   * the notification store's own rule for choosing the level.
   */
  it('keeps a failure on screen', async () => {
    stub(MATCH, match());
    stubFailure(APPLY, 'the tag writer refused');

    const el = await albumPage();

    await pressBanner(el, 'Apply');
    await pressConfirm('confirm-accept');
    await el.updateComplete;

    // Read it the way a person would: the app's one notification
    // surface, rendered.
    const host = await fixture<LitElement>('notification-host');

    await host.updateComplete;

    const shown = shadowAll(host, '[data-testid="notification"]').map(
      (n) => n.textContent ?? '',
    );

    expect(shown.join(' ')).toContain('could not be');
  });
});
