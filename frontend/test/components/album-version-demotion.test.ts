/**
 * Choosing a pressing is a repair job, not the album page's headline.
 *
 * The version selector sat directly above the tracklist with a heading,
 * a `<select>` and a paragraph explaining how our clustering picks a
 * "standard version" — the most valuable space on the page spent on a
 * control a normal user never touches (#17). Two more blocks shared
 * that slot and were not even guarded by "is there a choice": a
 * `Versions / Loading releases…` spinner about the same fetch
 * `renderTracklist` was already reporting, and a `Versions / <error>`
 * block duplicating what `catalog-scope-notice` shows at the top of the
 * page with a retry.
 *
 * What is pinned here is the demotion and the three things that must
 * survive it: the control is still reachable, the page still says which
 * version you are looking at once you have chosen one, and a failed
 * fetch still says so somewhere a collapsed panel is not.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';
import { page } from 'vitest/browser';

import '@components/explore-album-details/explore-album-details';
import { stub, stubFailure, flush, resetHarness } from '@test/support/harness';
import { fixture, shadow, shadowAll } from '@test/support/render';

const MBID = 'rg-0001';

function track(n: number) {
  return {
    position: n,
    discNumber: 1,
    title: `Track ${n}`,
    length: 200000,
    mbid: `rec-${n}`,
    inLibrary: false,
  };
}

function release(mbid: string, date: string, trackCount: number) {
  return {
    mbid,
    title: 'Glass Harbour',
    date,
    status: 'Official',
    tracks: Array.from({ length: trackCount }, (_, i) => track(i + 1)),
  };
}

const UNKNOWN = { owned: 0, expected: 0, known: false, complete: false };

/** Two releases whose tracklists genuinely differ, so there is a choice. */
const TWO = [release('rel-1', '2019-04-01', 10), release('rel-2', '2020-09-01', 14)];

async function album(releases: unknown[] = TWO): Promise<LitElement> {
  stub('explore.Service.BrowseReleases', releases);
  stub('library.Library.GetAlbumCompleteness', UNKNOWN);
  stub('library.Library.GetAlbumTracks', []);

  const el = await fixture<LitElement>('explore-album-details', {
    releaseGroupMBID: MBID,
    localAlbumId: 7,
    albumName: 'Glass Harbour',
  });

  await flush();
  await el.updateComplete;

  return el;
}

/** Positions of two selectors within the shadow root, in document order. */
function order(el: Element, first: string, second: string): [number, number] {
  const all = [...(el.shadowRoot?.querySelectorAll('*') ?? [])];
  const a = all.findIndex((n) => n.matches(first));
  const b = all.findIndex((n) => n.matches(second));

  return [a, b];
}

beforeEach(() => {
  resetHarness();
  stub('explore.Service.LookupReleaseGroup', {
    mbid: MBID,
    title: 'Glass Harbour',
    artistCredit: 'Tideline',
  });
  stub('explore.Service.GetThumbnail', '');
  stub('library.Library.GetAllLibrariesWithTrackCounts', []);
  stub('library.Library.GetFilePathsByRecordingMBIDs', {});
});

describe('the version selector is no longer the headline', () => {
  it('renders after the tracklist, not before it', async () => {
    const el = await album();
    const [tracklist, versions] = order(el, '.tracklist', '.versions');

    expect(tracklist).toBeGreaterThan(-1);
    expect(versions).toBeGreaterThan(tracklist);
  });

  it('starts collapsed', async () => {
    const el = await album();

    expect(shadow(el, '#versions-body')?.hasAttribute('hidden')).toBe(true);
  });

  /**
   * `aria-controls` has to name an element that is in the DOM, so the
   * body renders unconditionally and is toggled with `hidden` — the
   * rule `config-section` states and the reason a conditional body
   * would be wrong here too.
   */
  it('keeps the panel in the DOM while it is shut', async () => {
    const el = await album();

    expect(shadow(el, '#versions-body')).not.toBeNull();
    expect(
      shadow(el, '.versions-toggle')?.getAttribute('aria-controls'),
    ).toBe('versions-body');
  });

  /**
   * The browser's own answer: a disclosure that cannot be tabbed to is
   * the fault `config-section` shipped for every setting in the app,
   * and a shadow-root query cannot tell you a control has a name.
   */
  it('is a named, expandable button', async () => {
    await album();

    await expect
      .element(
        page.getByRole('button', { name: /Other versions of this album \(2\)/ }),
      )
      .toBeInTheDocument();
  });

  it('opens when the button is pressed', async () => {
    const el = await album();
    const toggle = shadow<HTMLButtonElement>(el, '.versions-toggle');

    expect(toggle?.getAttribute('aria-expanded')).toBe('false');

    toggle?.click();
    await el.updateComplete;

    expect(toggle?.getAttribute('aria-expanded')).toBe('true');
    expect(shadow(el, '#versions-body')?.hasAttribute('hidden')).toBe(false);
  });

  it('says nothing at all when there is only one tracklist', async () => {
    const el = await album([release('rel-1', '2019-04-01', 10)]);

    expect(shadow(el, '.versions')).toBeNull();
  });
});

describe('the two blocks that shared that slot', () => {
  /**
   * The spinner was unguarded, so `Versions / Loading releases…` took
   * the primary position on *every* album load — including the ones
   * that would never offer a choice — beside `renderTracklist`'s own
   * "Loading tracks…" about the same fetch.
   */
  it('no longer reports the same fetch twice while loading', async () => {
    stub('library.Library.GetAlbumCompleteness', UNKNOWN);
    stub('library.Library.GetAlbumTracks', []);
    stub('explore.Service.BrowseReleases', () => new Promise(() => {}));

    const el = await fixture<LitElement>('explore-album-details', {
      releaseGroupMBID: MBID,
      albumName: 'Glass Harbour',
    });

    const loading = shadowAll(el, '.section-loading');

    expect(loading).toHaveLength(1);
    expect(loading[0]?.textContent).toContain('Loading tracks');
  });

  /**
   * A failed browse must still be visible, and it cannot be visible
   * from inside a collapsed disclosure. It belongs to the list that is
   * missing because of it — `renderTracklist` used to return `nothing`
   * here and lean on the selector's own error block, which is exactly
   * the coupling that made this a rewrite rather than a move.
   */
  it('reports a failed fetch in the tracklist, once', async () => {
    stub('library.Library.GetAlbumCompleteness', UNKNOWN);
    stub('library.Library.GetAlbumTracks', []);
    stubFailure('explore.Service.BrowseReleases', 'the catalog said no');

    const el = await fixture<LitElement>('explore-album-details', {
      releaseGroupMBID: MBID,
      albumName: 'Glass Harbour',
    });

    await flush();
    await el.updateComplete;

    const errors = shadowAll(el, '.section-error');

    expect(errors).toHaveLength(1);
    expect(errors[0]?.textContent).toContain('versions');
    expect(shadow(el, '.versions')).toBeNull();
  });
});

describe('which version is on screen', () => {
  /**
   * The default is what the header already describes, so a line saying
   * so on every album would be the thing this issue removed, one size
   * smaller.
   */
  it('is not stated while the page picked it', async () => {
    const el = await album();

    expect(shadow(el, '.chosen-version')).toBeNull();
  });

  /**
   * The moment someone chooses another, the tracklist and the header
   * disagree — and the control that explains it is now off the bottom
   * of the page.
   */
  it('is stated above the tracklist once the user chooses', async () => {
    const el = await album();
    const select = shadow<HTMLSelectElement>(el, '#version-select')!;
    const other = [...select.options].find((o) => o.value !== select.value)!;

    select.value = other.value;
    select.dispatchEvent(new Event('change'));
    await el.updateComplete;

    const line = shadow(el, '.chosen-version');

    expect(line).not.toBeNull();

    const [chosen, tracklist] = order(el, '.chosen-version', '.tracklist');

    expect(chosen).toBeGreaterThan(-1);
    expect(tracklist).toBeGreaterThan(chosen);
  });

  it('offers a way back, which clears the line', async () => {
    const el = await album();
    const select = shadow<HTMLSelectElement>(el, '#version-select')!;
    const first = select.value;
    const other = [...select.options].find((o) => o.value !== first)!;

    select.value = other.value;
    select.dispatchEvent(new Event('change'));
    await el.updateComplete;

    shadow<HTMLButtonElement>(el, '.chosen-version-reset')?.click();
    await el.updateComplete;

    expect(shadow<HTMLSelectElement>(el, '#version-select')?.value).toBe(first);
    expect(shadow(el, '.chosen-version')).toBeNull();
  });

  /** A panel that shuts on use cannot be used twice. */
  it('leaves the disclosure open after a choice', async () => {
    const el = await album();

    shadow<HTMLButtonElement>(el, '.versions-toggle')?.click();
    await el.updateComplete;

    const select = shadow<HTMLSelectElement>(el, '#version-select')!;
    const other = [...select.options].find((o) => o.value !== select.value)!;

    select.value = other.value;
    select.dispatchEvent(new Event('change'));
    await el.updateComplete;

    expect(shadow(el, '#versions-body')?.hasAttribute('hidden')).toBe(false);
  });
});
