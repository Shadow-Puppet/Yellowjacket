/**
 * Runs before every test module. Two jobs, in this order:
 *
 *  1. Install the Wails fake. Store singletons call `EventsOn` and load
 *     from the backend *in their constructors*, which run when a test
 *     module imports them — so the globals have to exist first.
 *  2. Point Web Awesome at its assets. Without this every `<wa-icon>`
 *     silently 404s and screenshots come out with holes in them.
 */
import { afterEach, beforeEach } from 'vitest';
import { setBasePath } from '@awesome.me/webawesome/dist/webawesome.js';
import '@awesome.me/webawesome/dist/styles/themes/default.css';
import { installWailsFake, wails } from './support/wails-fake';
import { resetHarness } from './support/harness';
import { cleanupFixtures } from './support/render';

installWailsFake();

/**
 * A few stores read config in their constructor, which runs when a test
 * module imports them — before any test has had a chance to stub. An
 * unstubbed binding resolves undefined, and `themeStore` in particular
 * then derives a colour ramp from `undefined` and throws inside its own
 * failure handler. These defaults keep import-time loads on the happy
 * path; tests still stub whatever they assert on.
 */
const importTimeDefaults: Array<[string, unknown]> = [
  ['config.Config.GetThemeAccentColor', '#ffd43b'],
  ['config.Config.GetThemeBackgroundShade', 'dark'],
  ['config.Config.GetShortcuts', {}],
  // libraryStore and playlistStore fetch eagerly at import. Left
  // unstubbed they would cache `undefined` — not the empty list Go
  // sends — and every consumer would then crash on `.length`.
  ['library.Library.GetAllTracks', []],
  ['library.Library.GetAllAlbums', []],
  ['library.Library.GetAllArtists', []],
  ['library.Library.GetAllGenresWithCounts', []],
  ['library.Library.GetAllLibrariesWithTrackCounts', []],
  ['playlist.Service.GetAllPlaylistsWithTracks', []],
];

for (const [path, value] of importTimeDefaults) {
  wails.stub(path, value);
}

// Vite serves the dependency's own directory, so icons resolve from
// node_modules rather than from the built `dist/webawesome` copy the
// app uses.
setBasePath('/node_modules/@awesome.me/webawesome/dist');

// index.ts imports the theme store for its side effect: it derives the
// --yj-* custom properties and applies them to :root, where every
// shadow root inherits them. Without it a component renders white text
// on a white page and screenshots come out blank.
await import('@store/theme-store');

// The app's surface colours come from index.css, whose grid layout is
// not wanted here — take the two declarations that matter.
document.body.style.backgroundColor = 'var(--yj-bg-base, black)';
document.body.style.color = 'var(--yj-text-primary, white)';
document.body.style.margin = '0';

beforeEach(() => {
  resetHarness();
});

afterEach(() => {
  cleanupFixtures();
});
