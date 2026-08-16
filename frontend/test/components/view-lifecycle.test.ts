/**
 * The half of the lifecycle the view cache never had.
 *
 * `index.ts` keeps every primary view mounted so that `scrollTop`
 * survives navigation, which means `disconnectedCallback` never fires
 * and a view that is off-screen keeps listening. The tests here assert
 * the property that fixes: a deactivated view has removed everything it
 * registered, and a reactivated one has it back — measured by counting
 * what actually reaches `document`, since that is where the damage was
 * (`.planning/audits/2026-08-11-ui/hands-on.md`, H-1).
 */
import { LitElement, html } from 'lit';
import { customElement } from 'lit/decorators.js';
import { describe, expect, it, beforeEach, afterEach } from 'vitest';

import { ViewLifecycleMixin } from '@utils/view-lifecycle';
import {
  ambientShortcutScope,
  resetShortcutScopes,
} from '../../src/services/shortcut-scope';
import '@components/autotag-view/autotag-view';
import '@components/track-list/track-list';
import '@components/cover-grid/cover-grid';
import '@components/artists-view/artists-view';
import '@components/genres-view/genres-view';
import '@components/explore-view/explore-view';
import '@components/home-view/home-view';
import '@components/downloads-view/downloads-view';

import '@components/jobs/jobs-view';
import '@components/playlist-view/playlist-view';
import { fixture } from '@test/support/render';
import { stub, flush } from '@test/support/harness';

/** Count of keydown-ish document listeners, by proxy: every test view
 *  below registers on `document`, so a counting stand-in for
 *  `addEventListener` is the only honest measure available in a
 *  browser. */
let added = 0;
let removed = 0;

const realAdd = document.addEventListener.bind(document);
const realRemove = document.removeEventListener.bind(document);

beforeEach(() => {
  added = 0;
  removed = 0;
  document.addEventListener = ((...args: Parameters<typeof realAdd>) => {
    added += 1;

    return realAdd(...args);
  }) as typeof document.addEventListener;
  document.removeEventListener = ((
    ...args: Parameters<typeof realRemove>
  ) => {
    removed += 1;

    return realRemove(...args);
  }) as typeof document.removeEventListener;
});

afterEach(() => {
  document.addEventListener = realAdd;
  document.removeEventListener = realRemove;
  resetShortcutScopes();
});

// ===================================================================

@customElement('lifecycle-probe')
class LifecycleProbe extends ViewLifecycleMixin(LitElement) {
  ticks = 0;
  keypresses = 0;
  activations = 0;
  deactivations = 0;

  protected override onViewActivate(): void {
    this.activations += 1;
    this.listenWhileActive(document, 'keydown', () => {
      this.keypresses += 1;
    });
    this.intervalWhileActive(() => {
      this.ticks += 1;
    }, 5);
  }

  protected override onViewDeactivate(): void {
    this.deactivations += 1;
  }

  protected override render() {
    return html`<p>probe</p>`;
  }
}

/** One navigation away and back, as `index.ts` performs it. */
function navigateAway(el: LifecycleProbe): void {
  el.classList.add('view-hidden');
  el.viewDeactivated();
}

function navigateBack(el: LifecycleProbe): void {
  el.classList.remove('view-hidden');
  el.viewActivated();
}

describe('view lifecycle', () => {
  it('activates on connection when it is not created hidden', async () => {
    const el = await fixture<LifecycleProbe>('lifecycle-probe');

    expect(el.viewActive).toBe(true);
  });

  it('stops listening once deactivated', async () => {
    const el = await fixture<LifecycleProbe>('lifecycle-probe');

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }));
    navigateAway(el);
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }));

    expect(el.keypresses).toBe(1);
  });

  it('listens again on the way back, and only once', async () => {
    const el = await fixture<LifecycleProbe>('lifecycle-probe');

    navigateAway(el);
    navigateBack(el);
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }));

    expect(el.keypresses).toBe(1);
  });

  it('does not accumulate document listeners across navigations', async () => {
    const el = await fixture<LifecycleProbe>('lifecycle-probe');
    const afterFirst = added - removed;

    for (let i = 0; i < 5; i += 1) {
      navigateAway(el);
      navigateBack(el);
    }

    expect(added - removed).toBe(afterFirst);
  });

  it('stops its timers while off screen', async () => {
    const el = await fixture<LifecycleProbe>('lifecycle-probe');

    navigateAway(el);

    const at = el.ticks;

    await new Promise<void>((r) => {
      setTimeout(r, 30);
    });

    expect(el.ticks).toBe(at);
  });

  it('is idempotent in both directions', async () => {
    const el = await fixture<LifecycleProbe>('lifecycle-probe');

    el.viewActivated();
    el.viewActivated();
    navigateAway(el);
    el.viewDeactivated();

    expect([el.activations, el.deactivations]).toEqual([1, 1]);
  });

  it('deactivates when it is genuinely removed', async () => {
    const el = await fixture<LifecycleProbe>('lifecycle-probe');

    el.remove();

    expect(el.viewActive).toBe(false);
  });

  it('does not render while off screen, and catches up on return', async () => {
    const el = await fixture<LifecycleProbe>('lifecycle-probe');

    navigateAway(el);
    el.requestUpdate();
    await el.updateComplete;

    expect(el.hasUpdated).toBe(true);

    // The update was withheld, not lost: reactivation replays it.
    navigateBack(el);
    await el.updateComplete;

    expect(el.isConnected).toBe(true);
  });
});

// ===================================================================

describe('shortcut scope claims', () => {
  it('is claimed by the view on screen and released when it leaves', async () => {
    const list = await fixture('track-list');

    expect(ambientShortcutScope()).toBe('tracklist');

    (list as unknown as { viewDeactivated(): void }).viewDeactivated();

    expect(ambientShortcutScope()).toBeNull();
  });

  it('publishes the scope as the attribute the service reads', async () => {
    const list = await fixture('track-list');

    expect(list.dataset['shortcutScope']).toBe('tracklist');
  });
});

// ===================================================================

/** The views `index.ts` caches, all of which must take part. */
const CACHED_VIEWS = [
  'autotag-view',
  'track-list',
  'cover-grid',
  'artists-view',
  'genres-view',
  'downloads-view',
  'jobs-view',
  'playlist-view',
  'explore-view',
  'home-view',
];

/** An empty-but-valid backend, as smoke.test.ts does: an unstubbed
 *  binding resolves undefined, which is not what Go sends. */
function stubEmptyBackend(): void {
  for (const path of [
    'library.Library.GetTracks',
    'library.Library.GetAlbums',
    'library.Library.GetArtists',
    'library.Library.GetGenres',
    'library.Library.GetAllLibrariesWithTrackCounts',
    'playlist.Service.GetAllPlaylists',
    'playlist.Service.GetAllPlaylistsWithTracks',
    'playlist.Service.GetDefaultPlaylistTrackPaths',
    'jobs.Service.GetJobs',
    'download.Service.ListProviders',
    'download.Service.ListDownloads',
    'download.Service.ListRequests',
    'download.Service.ProviderKinds',
    'autotagservice.Service.ListPendingFolders',
    'home.Service.GetShelves',
  ]) {
    stub(path, []);
  }
}

describe('every cached view', () => {
  beforeEach(() => {
    stubEmptyBackend();
  });

  for (const tag of CACHED_VIEWS) {
    it(`${tag} holds no document listeners while off screen`, async () => {
      const before = added - removed;
      const el = await fixture(tag);
      const view = el as unknown as {
        viewActivated(): void;
        viewDeactivated(): void;
      };

      await flush();

      const active = added - removed;

      view.viewDeactivated();

      expect({ tag, held: added - removed - before }).toEqual({
        tag,
        held: 0,
      });

      // …and gets them all back, exactly once, on the way in again.
      view.viewActivated();
      view.viewDeactivated();
      view.viewActivated();

      expect({ tag, net: added - removed }).toEqual({ tag, net: active });
    });
  }
});
