/**
 * Bundled icons.
 *
 * Web Awesome's default icon library resolves every `<wa-icon>` to
 * `https://ka-f.fontawesome.com/releases/v7.1.0/svgs/<style>/<name>.svg`
 * and fetches it at runtime.  `setBasePath()` does not change that —
 * it is only read by the component autoloader — so a desktop music
 * player offline, on a captive portal or behind a firewall rendered no
 * icons at all (audit H-4, perf.M9), and a cold start waited on
 * fontawesome.com for up to 36 cross-origin requests.
 *
 * Overriding the library named `default` replaces that resolver for
 * every existing call site at once: no component changes, no icon
 * renamed, nothing to remember at the next one.
 *
 * The SVGs are emitted as assets rather than inlined into the JS.
 * Inlining 64 files would put ~270 kB of markup into a bundle that is
 * already the subject of perf.M10, and would make every icon part of
 * the startup parse; as assets they are served by the app's own asset
 * handler, cached by the browser, and fetched only when first used.
 */

import { registerIconLibrary } from '@awesome.me/webawesome/dist/webawesome.js';

/**
 * name -> emitted asset URL, built at compile time.
 *
 * `eager` matters: a lazy glob would make the resolver async, and Web
 * Awesome's resolver is synchronous.
 */
const FILES = import.meta.glob<string>(
    '../assets/icons/fa/**/*.svg',
    { eager: true, query: '?url', import: 'default' },
);

/** `solid/house` and `house` both resolve; call sites use the latter. */
const BY_NAME = new Map<string, string>();

for (const [path, url] of Object.entries(FILES)) {
    const m = /\/fa\/([^/]+)\/([^/]+)\.svg$/.exec(path);

    if (!m?.[1] || !m[2]) continue;

    const family = m[1];
    const name = m[2];

    BY_NAME.set(`${family}/${name}`, url);

    // `solid` is Web Awesome's default family, so a bare name means the
    // solid one.  A regular-family icon that shares its name (heart)
    // must not overwrite it.
    if (family === 'solid' || !BY_NAME.has(name)) {
        BY_NAME.set(name, url);
    }
}

/**
 * Names asked for that are not bundled.
 *
 * A missing icon used to be invisible — the CDN had everything, so
 * nothing ever failed.  Now it has to be *findable*, because twenty
 * call sites compute their name from state and no static check can
 * enumerate them (see `src/icons/names.txt`).  Recording the miss and
 * rendering a placeholder makes `frontend/scripts/icon-sweep.mjs` able
 * to report them, and makes a real one look wrong rather than absent.
 */
const misses = new Set<string>();

declare global {
    interface Window {
        __yjIconMisses?: string[];
    }
}

function report(name: string): void {
    if (misses.has(name)) return;

    misses.add(name);
    window.__yjIconMisses = [...misses];
    console.error(
        `icon '${name}' is not bundled; add it to src/icons/names.txt ` +
        'and run: node frontend/scripts/fetch-icons.mjs',
    );
}

const FALLBACK = BY_NAME.get('circle-question') ?? '';

export function registerBundledIcons(): void {
    registerIconLibrary('default', {
        resolver: (name: string) => {
            const url = BY_NAME.get(name);

            if (url) return url;

            report(name);

            return FALLBACK;
        },
    });
}

/** Exported for the test that asserts every listed name resolves. */
export const bundledIconNames = (): string[] => [...BY_NAME.keys()];
