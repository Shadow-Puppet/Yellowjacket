/*
 * Which icons does this app actually use?
 *
 * A static grep cannot answer that: twenty call sites pass a computed
 * name (`this.favCtrl.iconName`, `jobIcon(job)`, `TONE_ICONS[tone]`),
 * and the answer depends on state.  So ask the running app instead —
 * before the icons are bundled, every one of them is a request to
 * ka-f.fontawesome.com, which makes the CDN request log an exact
 * inventory of what has to be vendored.
 *
 * This is a one-shot development tool, not part of any build.  It is
 * kept because the list it produces will go stale the first time a
 * component grows a new state, and rerunning it is the cheapest way to
 * find out.  `frontend/src/icons/manifest.ts` is the committed answer.
 *
 * Usage: make dev-headless SEED=default, then
 *   node frontend/scripts/icon-sweep.mjs
 */

import { chromium } from '../../e2e/node_modules/@playwright/test/index.mjs';

const URL_BASE = process.env.YJ_URL ?? 'http://localhost:34115';

const VIEWS = [
	'home', 'tracks', 'albums', 'artists', 'genres', 'playlists',
	'explore', 'autotag', 'downloads', 'jobs', 'settings',
];

const found = new Set();

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });

page.on('request', (req) => {
	const m = /fontawesome\.com\/.*\/svgs\/([^/]+)\/([^/?]+)\.svg/.exec(req.url());
	if (m) found.add(`${m[1]}/${m[2]}`);
});

await page.goto(URL_BASE, { waitUntil: 'load' });
await page.waitForTimeout(3000);

for (const view of VIEWS) {
	await page.evaluate((v) => document.dispatchEvent(
		new CustomEvent('navigate', { detail: { view: v } }),
	), view);
	await page.waitForTimeout(1500);
}

// Also collect what is in the DOM but may have been served from the
// icon module's own cache rather than re-requested.
const inDom = await page.evaluate(() => {
	const names = new Set();
	const walk = (root) => {
		for (const el of root.querySelectorAll('*')) {
			if (el.tagName === 'WA-ICON' && el.getAttribute('name')) {
				names.add(el.getAttribute('name'));
			}
			if (el.shadowRoot) walk(el.shadowRoot);
		}
	};
	walk(document);

	return [...names];
});

await browser.close();

for (const n of inDom) {
	if (![...found].some((f) => f.endsWith('/' + n))) found.add(`?/${n}`);
}

console.log([...found].sort().join('\n'));
console.log(`\n${found.size} icons`);
