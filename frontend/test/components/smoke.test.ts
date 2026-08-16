/**
 * Every custom element in the tree, mounted against an empty backend.
 *
 * This is deliberately shallow: it asserts each component renders
 * *something* and logs no error, which is the state an agent's change
 * most often breaks and which nothing else here would notice. Depth
 * belongs in the per-component specs and in e2e; breadth belongs here,
 * because 46 elements is more than anyone will write specs for.
 */
import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest';

// One import per component module, so a component that fails to even
// load is a failure here rather than a silent absence.
import '@components/artist-details/artist-details';
import '@components/artists-view/artists-view';
import '@components/audio-player/audio-player';
import '@components/audio-player/controls/player-controls';
import '@components/audio-player/seekbar/seek-bar';
import '@components/audio-player/volume-control/volume-control';
import '@components/autotag-view/autotag-view';
import '@components/combobox/combobox';
import '@components/config-page/config-page';
import '@components/config-page/config-field';
import '@components/config-page/config-section';
import '@components/config-page/download-clients';
import '@components/config-page/shortcut-capture';
import '@components/cover-grid/cover-grid';
import '@components/cover-grid/album-dropdown';
import '@components/download-picker/download-picker';
import '@components/download-picker/candidate-row';
import '@components/downloads-view/downloads-view';
import '@components/duplicate-tracks-dialog/duplicate-tracks-dialog';
import '@components/explore-album-details/explore-album-details';
import '@components/explore-artist-details/explore-artist-details';
import '@components/explore-view/explore-view';
import '@components/first-run-wizard/first-run-wizard';
import '@components/genre-details/genre-details';
import '@components/genres-view/genres-view';
import '@components/jobs/job-details-drawer';
import '@components/jobs/job-indicator';
import '@components/jobs/job-log-view';
import '@components/jobs/job-row';
import '@components/jobs/jobs-view';
import '@components/library-filter/library-filter';
import '@components/library-status-indicator/library-status-indicator';
import '@components/now-playing/now-playing';
import '@components/phantom-resolver/phantom-resolver';
import '@components/playlist-details/playlist-details';
import '@components/playlist-picker/playlist-picker';
import '@components/playlist-view/playlist-view';
import '@components/queue-panel/queue-panel';
import '@components/search-bar/search-bar';
import '@components/sidebar/app-sidebar';
import '@components/smart-playlist-details/smart-playlist-details';
import '@components/smart-playlist-editor/smart-playlist-editor';
import '@components/top-results-row/top-results-row';
import '@components/track-details/track-details';
import '@components/track-info/track-info';
import '@components/track-list/track-list';

import { flush, stub } from '@test/support/harness';
import { fixture } from '@test/support/render';

/** Every element the app registers, in registration order. */
const TAGS = [
  'album-dropdown',
  'app-sidebar',
  'artist-details',
  'artists-view',
  'audio-player',
  'autotag-view',
  'candidate-row',
  'config-field',
  'config-page',
  'config-section',
  'cover-grid',
  'download-clients',
  'download-picker',
  'downloads-view',
  'duplicate-tracks-dialog',
  'explore-album-details',
  'explore-artist-details',
  'explore-view',
  'first-run-wizard',
  'genre-details',
  'genres-view',
  'job-details-drawer',
  'job-indicator',
  'job-log-view',
  'job-row',
  'jobs-view',
  'library-filter',
  'library-status-indicator',
  'now-playing',
  'phantom-resolver',
  'player-controls',
  'playlist-details',
  'playlist-picker',
  'playlist-view',
  'queue-panel',
  'search-bar',
  'seek-bar',
  'shortcut-capture',
  'smart-playlist-details',
  'smart-playlist-editor',
  'top-results-row',
  'track-details',
  'track-info',
  'track-list',
  'volume-control',
  'yj-combobox',
];

/**
 * An empty-but-valid backend. Unstubbed bindings resolve undefined,
 * which is not what Go sends — an empty list is.
 */
function stubEmptyBackend(): void {
  const emptyLists = [
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
  ];

  for (const path of emptyLists) stub(path, []);

  stub('config.Config.GetShortcuts', {});
  stub('config.Config.GetDownloadPreferences', {});
  stub('config.Config.GetThemeAccentColor', '#ffd43b');
  stub('config.Config.GetThemeBackgroundShade', 'dark');
}

describe('every component mounts on an empty library', () => {
  let errors: unknown[][] = [];

  beforeEach(() => {
    stubEmptyBackend();
    errors = [];
    vi.spyOn(console, 'error').mockImplementation((...args: unknown[]) => {
      errors.push(args);
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('registers every element it defines', () => {
    const missing = TAGS.filter((tag) => !customElements.get(tag));

    expect(missing).toEqual([]);
  });

  for (const tag of TAGS) {
    it(`<${tag}> renders without logging an error`, async () => {
      const el = await fixture(tag);

      await flush();
      await el.updateComplete;

      expect({ tag, root: el.shadowRoot !== null, errors }).toEqual({
        tag,
        root: true,
        errors: [],
      });
    });
  }
});
