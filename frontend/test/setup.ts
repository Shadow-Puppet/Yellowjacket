/**
 * Runs before every test module. Two jobs, in this order:
 *
 *  1. Install the Wails fake. Store singletons call `EventsOn` and load
 *     from the backend *in their constructors*, which run when a test
 *     module imports them — so the globals have to exist first.
 *  2. Point Web Awesome at its assets, and register the *bundled* icon
 *     library — the same call `index.ts` makes. Without the second
 *     one this tier renders icons from fontawesome.com, so a green
 *     `make ui-test` would depend on the network and the screenshot
 *     baselines would be of something the app no longer ships.
 */
import { afterEach, beforeEach } from 'vitest';
import { setBasePath } from '@awesome.me/webawesome/dist/webawesome.js';
import '@awesome.me/webawesome/dist/styles/themes/default.css';
import { registerBundledIcons } from '../src/icons';
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
  ['library.Library.GetTracks', []],
  ['library.Library.GetAlbums', []],
  ['library.Library.GetArtists', []],
  ['library.Library.GetGenres', []],
  ['library.Library.GetAllLibrariesWithTrackCounts', []],
  ['playlist.Service.GetAllPlaylistsWithTracks', []],
];

for (const [path, value] of importTimeDefaults) {
  wails.stub(path, value);
}

// Vite serves the dependency's own directory, so Web Awesome's own
// assets resolve from node_modules rather than from the built
// `dist/webawesome` copy the app uses.
//
// Note this does *not* cover icons: `getBasePath` is read only by the
// component autoloader, never by the icon resolver, which is the
// original half of audit finding H-4 and the reason the line below
// exists rather than being implied by this one.
setBasePath('/node_modules/@awesome.me/webawesome/dist');
registerBundledIcons();

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

  // A test file does not get its own origin. `@vitest/browser-playwright`
  // opens one BrowserContext per session and runs several files in it,
  // one after another, so everything a component persists — the track
  // list's sort and column widths, the cover size, `now-playing`'s
  // scroll mode — is still there when the next file mounts the same
  // component. That is invisible until it is intermittent, because
  // which files share a tab and in what order changes run to run: it
  // cost #138 three scheduled runs, one of them a PR with no frontend
  // code in it at all.
  //
  // Clearing here rather than in the specs that write is deliberate —
  // the spec that *reads* is never the one that knows. It is safe for
  // the same reason the leak exists: files in a session are
  // sequential, so this cannot wipe storage a concurrent file is in
  // the middle of using.
  localStorage.clear();
});

afterEach(() => {
  cleanupFixtures();
});
