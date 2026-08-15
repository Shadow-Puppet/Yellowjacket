/*
 * Plan 007 phase 4 is verified by measurement, not by assertion.
 *
 * That is a different kind of harness from `e2e/specs/`, and it lives
 * next to them rather than among them on purpose: a spec fails or
 * passes, and this produces numbers that have to be read.  Nothing here
 * runs in CI.  The point is a before/after table on the same machine,
 * against the same seeded library, in the same session shape — so the
 * comparison is between two builds and not between two afternoons.
 *
 * Four numbers, one per finding family in
 * `.planning/audits/2026-08-11-ui/perf.md`:
 *
 *   startup       first contentful paint, first row on screen, JS bytes,
 *                 and the count of *cross-origin* requests — which is
 *                 M9 (icons from fontawesome.com) stated as a number
 *                 rather than as a complaint.
 *   keystroke     time from an input event in the header search box to
 *                 the frame that paints its result, plus the main-thread
 *                 blocking behind it.  M1/M2.
 *   trackchange   what a naturally finished track costs: the bindings it
 *                 provokes, the bytes they return, and the longest task
 *                 it blocks the main thread with.  C1/C2.
 *   favourite     what toggling one heart costs, against a library that
 *                 has playlists in it.  C5.
 *   viewopen      the cost of the bundle's shape: bytes of JS evaluated
 *                 before first paint, and how long each view takes to
 *                 appear the first time it is opened — which is what a
 *                 route split trades away if it trades badly.  M10.
 *   playlistopen  what opening a 2 000-track playlist costs: elements
 *                 retained, eager cover requests, heap, and what one
 *                 update pass rebinds.  M5.
 *   settings      what sitting on the Settings page costs while doing
 *                 nothing: status events received and re-renders they
 *                 provoke.  M6/H-14.
 *   scroll        what scrolling a long list costs: image bytes pulled
 *                 per screen with the Art column on, and main-thread
 *                 blocking through the artist grid.  M3/M4.
 *   explore       what a *long Explore session* retains: heap sampled
 *                 post-GC after each of twelve searches, plus the size
 *                 of every registered cache.  M7/M8 — and note that the
 *                 browse number below could not see this, because it
 *                 visits Explore without ever typing in it.
 *   heap          JS heap after a scripted browse, post-GC.  m3.
 *
 * Usage:
 *   node e2e/perf/measure.mjs --label before
 *   node e2e/perf/measure.mjs --label after
 *   node e2e/perf/measure.mjs --compare before after
 *
 * Requires a running app: `make dev-headless SEED=bulk`.
 */

import { readFileSync, writeFileSync, mkdirSync, existsSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from '@playwright/test';

import { methodIDs } from '../support/method-ids.mjs';

const HERE = dirname(fileURLToPath(import.meta.url));
const REPO = resolve(HERE, '../..');
const OUT_DIR = resolve(REPO, '.dev/perf');
const BRIDGE = resolve(REPO, '.playwright/init-events.js');

const BASE_URL = process.env.YJ_URL ?? 'http://localhost:34115';

/**
 * methodID -> 'pkg.Type.Method', derived from frontend/bindings/.
 *
 * A binding call carries only the id, so this is what turns a
 * measurement's "which bindings did that provoke" back into names.  It
 * is derived rather than written down for the reason plan 009 phase 6b
 * gives: a hand-maintained list goes stale silently.
 */
const METHOD_NAMES = Object.fromEntries(methodIDs());

// The browse script visited for the heap measurement.  Deliberately the
// views the audit named as retaining: explore (two unbounded caches),
// artists and genres (per-frame work), settings (the 3 s ticker).
const BROWSE_VIEWS = [
	'home', 'tracks', 'albums', 'artists', 'genres',
	'playlists', 'explore', 'jobs', 'settings', 'tracks',
];

const SEARCH_TERMS = ['t', 'ti', 'tid', 'tide', 'tidel', 'tideli'];

// Mirrors `VIEW_TAGS` in `frontend/index.ts`: which custom element each
// view name renders as. The view-open measurement has to wait for the
// incoming element specifically, and the outgoing one is still on screen
// until it arrives.
const VIEW_ELEMENT_TAGS = {
	home: 'home-view',
	tracks: 'track-list',
	albums: 'cover-grid',
	artists: 'artists-view',
	genres: 'genres-view',
	playlists: 'playlist-view',
	explore: 'explore-view',
	autotag: 'autotag-view',
	downloads: 'downloads-view',
	jobs: 'jobs-view',
	settings: 'config-page',
};

// C5 is proportional to the total number of playlist *rows* in the
// database, and the bulk seed ships with one empty playlist — against
// which the finding costs nothing and cannot be reproduced.  So the
// measurement stages its own, idempotently by name, and a before and an
// after therefore see the same shape.  Ten playlists of five hundred
// tracks is a heavy-but-real user, and is the smallest thing that makes
// "every track of every playlist" mean something.
const PERF_PLAYLIST_PREFIX = '__perf_';
const PERF_PLAYLISTS = 10;
const PERF_PLAYLIST_TRACKS = 500;

// M5 is proportional to the length of *one* playlist, which the ten
// above are deliberately too short to show.  Staged separately, and by
// its own name, so the favourite numbers above stay comparable with
// every measurement taken before this one existed.
const PERF_BIG_PLAYLIST = '__perfbig_';
const PERF_BIG_PLAYLIST_TRACKS = 2000;

function parseArgs(argv) {
	const args = { label: null, compare: null, viewport: { width: 1440, height: 900 } };

	for (let i = 0; i < argv.length; i++) {
		if (argv[i] === '--label') args.label = argv[++i];
		else if (argv[i] === '--compare') args.compare = [argv[++i], argv[++i]];
	}

	return args;
}

/* -------------------------------------------------------------------- */

/**
 * Say which bindings a user action provoked, and how much they
 * returned.
 *
 * This used to walk `window.go` and wrap every bound method in place,
 * which worked because v2's generated stubs looked their target up at
 * call time.  v3 has no such object — the bindings are bundled modules
 * — so `.playwright/init-events.js` records every call off the single
 * POST v3 routes them all through, and this reads that log.  It is
 * strictly better: it needs no walk, sees calls from any module, and
 * cannot miss one made before a wrapper was installed, which is what
 * the old "runs twice" dance was working around.
 *
 * `bytes` is the response size, so `measureBytes` is turned on here —
 * only a measurement wants to pay for a clone-and-read of every body.
 */
const INSTRUMENT = `(names) => {
	if (window.__yjPerf) return;

	window.__yjEvents.measureBytes = true;

	window.__yjPerf = {
		get calls() {
			return window.__yjEvents.bindings.map((c) => ({
				path: names[c.methodID] || ('#' + c.methodID),
				methodID: c.methodID,
				start: c.start,
				ms: c.ms,
				bytes: c.bytes,
			}));
		},
		reset: () => { window.__yjEvents.reset(); },
		since: (t) => window.__yjPerf.calls.filter((c) => c.start >= t),
		longtasks: [],
	};

	// Long tasks are the honest form of "the app stalls": a 25 MB JSON
	// parse on the main thread shows up here and nowhere else.
	try {
		new PerformanceObserver((list) => {
			for (const e of list.getEntries()) {
				window.__yjPerf.longtasks.push({ start: e.startTime, ms: e.duration });
			}
		}).observe({ entryTypes: ['longtask'] });
	} catch { /* not every engine has it; Chromium does */ }
}`;

// `search-bar` debounces by 150 ms, so "keystroke to paint" measured
// against the next frame measures the input echoing its own character
// — 16 ms, one frame, on any build, which is a number that cannot move
// and therefore cannot be evidence of anything.  The measurement has to
// wait past the debounce for the render the keystroke actually caused.
const SEARCH_DEBOUNCE_MS = 150;

/* -------------------------------------------------------------------- */

async function measureStartup(page) {
	const nav = await page.evaluate(() => {
		const n = performance.getEntriesByType('navigation')[0];
		const fcp = performance.getEntriesByName('first-contentful-paint')[0];
		const res = performance.getEntriesByType('resource');

		const scripts = res
			.filter((r) => r.initiatorType === 'script' || r.name.endsWith('.js'));
		const scriptBytes = scripts
			.reduce((n2, r) => n2 + (r.encodedBodySize || 0), 0);

		// M10's actual claim is not "how much JS exists" but "how much is
		// parsed and side-effect-evaluated before anything appears".  The
		// view chunks are warmed on idle *after* paint, so a total taken
		// later counts them and reports no change at all.
		const paintedAt = fcp ? fcp.startTime : Infinity;
		const scriptBytesBeforePaint = scripts
			.filter((r) => r.responseEnd <= paintedAt)
			.reduce((n2, r) => n2 + (r.encodedBodySize || 0), 0);

		// The icon finding, stated numerically.  Anything not served by
		// the app's own origin is something a closed network breaks.
		const origin = location.origin;
		const crossOrigin = res
			.filter((r) => !r.name.startsWith(origin) && !r.name.startsWith('data:'))
			.map((r) => r.name);

		return {
			domContentLoadedMs: n ? Math.round(n.domContentLoadedEventEnd) : null,
			firstContentfulPaintMs: fcp ? Math.round(fcp.startTime) : null,
			scriptBytes,
			scriptBytesBeforePaint,
			requests: res.length,
			crossOriginRequests: crossOrigin.length,
			crossOriginHosts: [...new Set(crossOrigin.map((u) => new URL(u).host))],
		};
	});

	// "First row on screen" is the number a user experiences as startup;
	// FCP fires on the chrome around an empty list.
	const firstRowMs = await page.evaluate(async () => {
		const t0 = performance.now();
		const deadline = t0 + 60000;

		for (;;) {
			const list = document.querySelector('track-list');
			const row = list?.shadowRoot?.querySelector('[role="row"], .track-row');
			if (row) return Math.round(performance.now() - t0 + (performance.timeOrigin ? 0 : 0));
			if (performance.now() > deadline) return null;
			await new Promise((r) => setTimeout(r, 16));
		}
	});

	return { ...nav, firstRowAfterLoadMs: firstRowMs };
}

async function measureKeystroke(page) {
	await page.evaluate(() => document.dispatchEvent(
		new CustomEvent('navigate', { detail: { view: 'tracks' } }),
	));
	await page.waitForTimeout(500);

	const samples = await page.evaluate(
		async ([terms, debounceMs]) => {
			const deepFind = (root, tag) => {
				const hit = root.querySelector(tag);
				if (hit) return hit;
				for (const el of root.querySelectorAll('*')) {
					if (el.shadowRoot) {
						const deep = deepFind(el.shadowRoot, tag);
						if (deep) return deep;
					}
				}

				return null;
			};

			const bar = deepFind(document, 'search-bar');
			const input = bar?.shadowRoot?.querySelector('input');
			if (!input) return { error: 'no search input found' };

			const out = [];

			for (const term of terms) {
				window.__yjPerf.longtasks.length = 0;
				const t0 = performance.now();

				input.value = term;
				input.dispatchEvent(new Event('input', { bubbles: true, composed: true }));

				// Past the debounce, then let the view it belongs to finish
				// rendering, then wait for the frame that shows it.
				await new Promise((r) => setTimeout(r, debounceMs + 10));

				const view = document.querySelector('#main-content > :not(.view-hidden)');
				if (view && view.updateComplete) await view.updateComplete;

				await new Promise((r) =>
					requestAnimationFrame(() => requestAnimationFrame(r)));

				const blocking = window.__yjPerf.longtasks
					.filter((l) => l.start >= t0)
					.reduce((n, l) => n + l.ms, 0);

				out.push({
					term,
					// Net of the debounce, so the number is work rather
					// than a constant the app chose.
					ms: performance.now() - t0 - debounceMs,
					blockingMs: blocking,
				});

				await new Promise((r) => setTimeout(r, 300));
			}

			// Leave the app as we found it.
			input.value = '';
			input.dispatchEvent(new Event('input', { bubbles: true, composed: true }));

			return { samples: out };
		},
		[SEARCH_TERMS, SEARCH_DEBOUNCE_MS],
	);

	if (samples.error) return { error: samples.error };

	const ms = samples.samples.map((s) => s.ms).sort((a, b) => a - b);
	const blocking = samples.samples.map((s) => s.blockingMs).sort((a, b) => a - b);

	return {
		medianMs: round(ms[Math.floor(ms.length / 2)]),
		worstMs: round(ms[ms.length - 1]),
		medianBlockingMs: round(blocking[Math.floor(blocking.length / 2)]),
		samples: samples.samples.map((s) => ({ ...s, ms: round(s.ms), blockingMs: round(s.blockingMs) })),
	};
}

async function measureTrackChange(page) {
	// Play from the top of the queue and let a track finish naturally —
	// the transition C1 is about is the *natural* one, since that is
	// what calls recordPlay.
	const result = await page.evaluate(async () => {
		const ev = window.__yjEvents;
		const perf = window.__yjPerf;

		await ev.call('queue.Queue.Clear', [], 10000).catch(() => {});

		const tracks = await ev.call('library.Library.GetAllTracks', [], 60000);
		const paths = (tracks ?? []).slice(0, 4).map((t) => t.FilePath);
		if (paths.length < 2) return { error: 'library too small to measure' };

		await ev.call(
			'queue.Queue.SetQueue',
			// The fourth argument is the queue's source; these are
			// ad-hoc tracks, so it is the empty one.  v3 rejects a
			// call with the wrong argument count where v2 filled the
			// gap with a zero value.
			[paths, 0, false, { type: '', id: 0, label: '' }],
			15000,
		);
		await ev.call('queue.Queue.PlayIndex', [0], 15000);

		// Settle mid-track before starting to record.  Starting playback
		// emits its own TrackChanged, and measuring that one would measure
		// a user's click rather than the natural finish recordPlay hangs
		// off — which is the transition C1 is actually about.
		await new Promise((r) => setTimeout(r, 1200));

		ev.reset();
		perf.reset();
		perf.longtasks.length = 0;
		const t0 = performance.now();

		// Wait for the natural finish: the queue advances by itself.
		await ev.wait('TrackChanged', { timeoutMs: 120000 });
		const changedAt = performance.now();

		// Give the consequences a moment to land — the refetch this is
		// measuring is provoked *by* the change, not simultaneous with it.
		await new Promise((r) => setTimeout(r, 4000));

		const calls = perf.since(t0);
		const byPath = {};
		for (const c of calls) {
			byPath[c.path] ??= { calls: 0, ms: 0, bytes: 0 };
			byPath[c.path].calls++;
			byPath[c.path].ms += c.ms;
			byPath[c.path].bytes += Math.max(0, c.bytes);
		}

		const longest = perf.longtasks
			.filter((l) => l.start >= changedAt - 500)
			.reduce((m, l) => Math.max(m, l.ms), 0);
		const blocking = perf.longtasks
			.filter((l) => l.start >= changedAt - 500)
			.reduce((n, l) => n + l.ms, 0);

		await ev.call('player.Player.Pause', [], 5000).catch(() => {});

		return {
			bindingCalls: calls.length,
			bindingBytes: calls.reduce((n, c) => n + Math.max(0, c.bytes), 0),
			longestTaskMs: longest,
			blockingMs: blocking,
			events: ev.names(),
			byPath,
		};
	});

	if (result.error) return result;

	return {
		...result,
		bindingBytes: result.bindingBytes,
		longestTaskMs: round(result.longestTaskMs),
		blockingMs: round(result.blockingMs),
		byPath: Object.fromEntries(
			Object.entries(result.byPath)
				.sort((a, b) => b[1].bytes - a[1].bytes)
				.slice(0, 8)
				.map(([k, v]) => [k, { ...v, ms: round(v.ms) }]),
		),
	};
}

/**
 * `perf.C5`: toggling one heart refetches every track of every playlist.
 *
 * Staged rather than assumed — see PERF_PLAYLISTS.  The toggle goes
 * through the same binding the heart column calls
 * (`ToggleDefaultPlaylistTrack`), so what is measured is the user
 * action and not a synthetic event.
 *
 * Measured twice, because the finding has two halves and one number
 * would hide the other.  `closed` is a user who has never opened the
 * Playlists view — nothing should be fetched at all.  `open` is one who
 * has, so the cache exists and has to be brought up to date: that is
 * the path the *patch* is on, and a zero there would mean the patch is
 * not running rather than that it is cheap.
 */
async function measureFavouriteToggle(page) {
	const result = await page.evaluate(async ([prefix, count, per]) => {
		const ev = window.__yjEvents;
		const perf = window.__yjPerf;

		const tracks = await ev.call('library.Library.GetAllTracks', [], 60000);
		const paths = (tracks ?? []).map((t) => t.FilePath);
		if (paths.length < per * count) {
			return { error: `library too small: ${paths.length} tracks` };
		}

		// Stage, idempotently by name.
		const existing = await ev.call('playlist.Service.GetAllPlaylists', [], 30000);
		const have = new Set((existing ?? []).map((p) => p.Name ?? p.name));

		for (let i = 0; i < count; i++) {
			const name = `${prefix}${i}`;
			if (have.has(name)) continue;
			await ev.call(
				'playlist.Service.CreatePlaylistWithTracks',
				[name, paths.slice(i * per, (i + 1) * per)],
				60000,
			);
		}

		// The heart the measurement toggles must not be one of the staged
		// tracks' — favouriting is itself a playlist edit, and toggling a
		// path that is already in a staged list would measure two writes.
		const victim = paths[paths.length - 1];

		const navigate = async (view) => {
			document.dispatchEvent(
				new CustomEvent('navigate', { detail: { view } }),
			);
			await new Promise((r) => setTimeout(r, 2500));
		};

		const toggleOnce = async () => {
			// Let whatever came before drain, or its refetches are
			// attributed to this heart.
			await new Promise((r) => setTimeout(r, 3000));

			ev.reset();
			perf.reset();
			perf.longtasks.length = 0;
			const t0 = performance.now();

			await ev.call('playlist.Service.ToggleDefaultPlaylistTrack', [victim], 30000);

			// The refetch is provoked *by* the event, not simultaneous
			// with it.
			await new Promise((r) => setTimeout(r, 4000));

			const calls = perf.since(t0)
				.filter((c) => !c.path.endsWith('ToggleDefaultPlaylistTrack'));
			const byPath = {};
			for (const c of calls) {
				byPath[c.path] ??= { calls: 0, ms: 0, bytes: 0 };
				byPath[c.path].calls++;
				byPath[c.path].ms += c.ms;
				byPath[c.path].bytes += Math.max(0, c.bytes);
			}

			// Put it back, so the next toggle starts where this one did
			// and adds rather than removes — after recording, so the
			// restore's own consequences are not counted.
			await ev.call('playlist.Service.ToggleDefaultPlaylistTrack', [victim], 30000)
				.catch(() => {});

			return {
				bindingCalls: calls.length,
				bindingBytes: calls.reduce((n, c) => n + Math.max(0, c.bytes), 0),
				longestTaskMs: perf.longtasks
					.filter((l) => l.start >= t0)
					.reduce((m, l) => Math.max(m, l.ms), 0),
				blockingMs: perf.longtasks
					.filter((l) => l.start >= t0)
					.reduce((n, l) => n + l.ms, 0),
				events: ev.names(),
				byPath,
			};
		};

		const closed = await toggleOnce();

		await navigate('playlists');
		const open = await toggleOnce();
		await navigate('tracks');

		return {
			playlists: count,
			tracksPerPlaylist: per,
			closed,
			open,
		};
	}, [PERF_PLAYLIST_PREFIX, PERF_PLAYLISTS, PERF_PLAYLIST_TRACKS]);

	if (result.error) return result;

	const tidy = (r) => ({
		...r,
		longestTaskMs: round(r.longestTaskMs),
		blockingMs: round(r.blockingMs),
		byPath: Object.fromEntries(
			Object.entries(r.byPath)
				.sort((a, b) => b[1].bytes - a[1].bytes)
				.slice(0, 8)
				.map(([k, v]) => [k, { ...v, ms: round(v.ms) }]),
		),
	});

	return {
		...result,
		closed: tidy(result.closed),
		open: tidy(result.open),
	};
}

/**
 * `perf.M10`: one 1.18 MB chunk containing all 27 views, every one of
 * them parsed and side-effect-evaluated before first paint.
 *
 * Two numbers, because splitting a bundle is a trade and reporting only
 * the first half of it would be dishonest: how much less is loaded up
 * front, and what the *first* navigation to each view now costs — a
 * split that halves startup by making every page load visibly slower
 * has not obviously helped anyone.
 */
async function measureViewOpen(page) {
	return page.evaluate(async ([views, TAGS]) => {
		const scripts = () => performance
			.getEntriesByType('resource')
			.filter((r) => r.name.endsWith('.js'));

		const timings = {};

		for (const view of views) {
			// Each view is measured on its *first* open, which is the only
			// time a chunk can be fetched. Re-measuring a warmed view
			// would report the cache and prove nothing.
			const t0 = performance.now();

			document.dispatchEvent(
				new CustomEvent('navigate', { detail: { view } }),
			);

			// Wait for *this* view's element to exist and have rendered.
			//
			// Not `#main-content > :not(.view-hidden)`: the outgoing view
			// stays visible until the incoming one is ready, so that
			// selector matches the previous page immediately and reports
			// 0 ms for everything.
			const tag = TAGS[view];
			const deadline = performance.now() + 10000;

			for (;;) {
				const el = document.querySelector(
					`#main-content > ${tag}:not(.view-hidden)`,
				);

				if (el && el.shadowRoot?.childElementCount) break;
				if (performance.now() > deadline) {
					timings[view] = null;
					break;
				}

				await new Promise((r) => requestAnimationFrame(r));
			}

			if (timings[view] === null) continue;

			timings[view] = Math.round(performance.now() - t0);
			await new Promise((r) => setTimeout(r, 200));
		}

		const all = scripts();

		return {
			totalScriptCount: all.length,
			totalScriptBytes: all.reduce(
				(n, r) => n + (r.encodedBodySize || 0), 0,
			),
			firstOpenMs: timings,
			worstFirstOpenMs: Math.max(
				...Object.values(timings).filter((v) => v !== null),
			),
		};
	}, [
		BROWSE_VIEWS.filter((v, i, a) => a.indexOf(v) === i),
		VIEW_ELEMENT_TAGS,
	]);
}

/**
 * `perf.M5`: the playlist and smart-playlist detail views render every
 * track with a plain `.map()` — no virtualizer.
 *
 * The seed has one empty playlist, against which the finding is
 * literally nothing, so this stages a big one idempotently by name (the
 * same discipline as PERF_PLAYLISTS above).  Two thousand tracks is the
 * audit's own figure and is a plausible "everything I like" playlist.
 *
 * Four numbers, because the audit names four mechanisms and they turn
 * out not to be equally real:
 *
 *   nodes      elements retained in the view's shadow root, pierced.
 *              This is the finding.  `<track-list>` holds ~520 for a
 *              *fifty thousand* track library because it virtualizes;
 *              anything proportional to the playlist length is the bug.
 *   images     `<img>` in the DOM and how many of them asked to be
 *              lazy.  Deliberately *not* the request count: running
 *              last means every cover is already in the HTTP cache, so
 *              that figure is zero on any build and is therefore not
 *              evidence.  It stays in the JSON and off the table.
 *              Elements and their `loading` attribute are true
 *              regardless of what ran before.
 *   passMs     what one update pass costs, and how many listeners it
 *              rebinds.  The audit predicts 10 000 rebinds per pass;
 *              this is here to check that rather than to assume it.
 *   heapMB     retained after the view has settled, post-GC.
 *
 * Scroll frames are recorded too, but a fully-materialised list scrolls
 * *well* — the DOM is already built.  The cost is building and holding
 * it, which is why the headline is nodes and heap.
 */
async function measurePlaylistOpen(page, client) {
	const gc = async () => {
		await client.send('HeapProfiler.collectGarbage');
		await page.waitForTimeout(400);
		const u = await client.send('Runtime.getHeapUsage');

		return round(u.usedSize / 1024 / 1024, 2);
	};

	const staged = await page.evaluate(async ([name, n]) => {
		const ev = window.__yjEvents;

		const existing = await ev.call('playlist.Service.GetAllPlaylists', [], 30000);
		let pl = (existing ?? []).find((p) => (p.Name ?? p.name) === name);

		if (!pl) {
			const tracks = await ev.call('library.Library.GetAllTracks', [], 60000);
			const paths = (tracks ?? []).map((t) => t.FilePath);

			if (paths.length < n) return { error: `library too small: ${paths.length}` };

			await ev.call(
				'playlist.Service.CreatePlaylistWithTracks',
				[name, paths.slice(0, n)],
				120000,
			);

			const after = await ev.call('playlist.Service.GetAllPlaylists', [], 30000);
			pl = (after ?? []).find((p) => (p.Name ?? p.name) === name);
		}

		return pl ? { id: pl.ID ?? pl.id } : { error: 'staged playlist not found' };
	}, [PERF_BIG_PLAYLIST, PERF_BIG_PLAYLIST_TRACKS]);

	if (staged.error) return staged;

	// Baseline from the tracks view, so the delta is the playlist's and
	// not the previous measurement's residue.
	await page.evaluate(() => document.dispatchEvent(
		new CustomEvent('navigate', { detail: { view: 'tracks' } }),
	));
	await page.waitForTimeout(2000);
	const heapBefore = await gc();

	const opened = await page.evaluate(async ([id, name, tracks]) => {
		// Counting has to pierce shadow roots: every row in this app lives
		// inside one, so a document-level count sees the wrapper and
		// nothing else.
		const countNodes = (root) => {
			let c = 0;

			for (const el of root.querySelectorAll('*')) {
				c++;
				if (el.shadowRoot) c += countNodes(el.shadowRoot);
			}

			return c;
		};

		// `addEventListener` is instrumented for the duration only. Lit's
		// EventPart is itself the listener (`handleEvent`), so a fresh
		// arrow function per render updates a stored value and never
		// touches the DOM — which is the audit's claim, stated as a number
		// that can disagree with it.
		const realAdd = EventTarget.prototype.addEventListener;
		const realRemove = EventTarget.prototype.removeEventListener;
		let added = 0;
		let removed = 0;

		EventTarget.prototype.addEventListener = function counted(...args) {
			added++;

			return realAdd.apply(this, args);
		};
		EventTarget.prototype.removeEventListener = function counted(...args) {
			removed++;

			return realRemove.apply(this, args);
		};

		const imagesBefore = performance.getEntriesByType('resource')
			.filter((r) => r.initiatorType === 'img').length;

		const t0 = performance.now();

		document.dispatchEvent(new CustomEvent('navigate', {
			detail: { view: 'playlist-details', playlistId: id, playlistName: name },
		}));

		// Wait for a row, not for the element: the element exists as soon
		// as its chunk resolves, and the finding is about what happens
		// after that.
		let el = null;
		let firstRowMs = null;
		const deadline = performance.now() + 30000;

		for (;;) {
			el = document.querySelector('playlist-details');

			if (el?.shadowRoot?.querySelector('.track-item')) {
				firstRowMs = performance.now() - t0;
				break;
			}

			if (performance.now() > deadline) break;

			await new Promise((r) => requestAnimationFrame(r));
		}

		if (firstRowMs === null) {
			EventTarget.prototype.addEventListener = realAdd;
			EventTarget.prototype.removeEventListener = realRemove;

			return { error: 'no playlist row rendered within 30 s' };
		}

		await new Promise((r) => setTimeout(r, 3000));

		const settledMs = performance.now() - t0;
		const addedOnOpen = added;
		const removedOnOpen = removed;

		const root = el.shadowRoot;
		const images = root.querySelectorAll('img');

		const result = {
			tracks,
			firstRowMs: Math.round(firstRowMs),
			settledMs: Math.round(settledMs),
			nodes: countNodes(root),
			rowsInDom: root.querySelectorAll('.track-item').length,
			imagesInDom: images.length,
			imagesEager: [...images].filter((i) => i.loading !== 'lazy').length,
			imageRequests: performance.getEntriesByType('resource')
				.filter((r) => r.initiatorType === 'img').length - imagesBefore,
			addedOnOpen,
			removedOnOpen,
		};

		// One update pass of the kind any player event provokes — this
		// component holds an unfiltered PlayerController subscription, so
		// it pays this on every state, track, volume and mute change.
		const passes = [];

		for (let i = 0; i < 5; i++) {
			added = 0;
			removed = 0;
			const p0 = performance.now();
			el.requestUpdate();
			await el.updateComplete;
			passes.push({
				ms: performance.now() - p0,
				added,
				removed,
			});
			await new Promise((r) => setTimeout(r, 100));
		}

		EventTarget.prototype.addEventListener = realAdd;
		EventTarget.prototype.removeEventListener = realRemove;

		const ms = passes.map((p) => p.ms).sort((a, b) => a - b);

		return {
			...result,
			passMedianMs: ms[Math.floor(ms.length / 2)],
			passWorstMs: ms[ms.length - 1],
			passListenersRebound: Math.max(...passes.map((p) => p.added + p.removed)),
		};
	}, [staged.id, PERF_BIG_PLAYLIST, PERF_BIG_PLAYLIST_TRACKS]);

	if (opened.error) return opened;

	const heapAfter = await gc();

	const scroll = await page.evaluate(async () => {
		const el = document.querySelector('playlist-details');
		const root = el?.shadowRoot;
		if (!root) return null;

		// Whichever element actually scrolls: `.content` before the fix,
		// the virtualizer's own scroller after it.
		const scroller = [...root.querySelectorAll('*')]
			.find((c) => c.scrollHeight > c.clientHeight + 100);

		if (!scroller) return null;

		const gaps = [];
		let last = performance.now();
		let running = true;
		const tick = () => {
			const now = performance.now();
			gaps.push(now - last);
			last = now;
			if (running) requestAnimationFrame(tick);
		};

		requestAnimationFrame(tick);

		for (let i = 0; i < 12; i++) {
			scroller.scrollTop += scroller.clientHeight;
			await new Promise((r) => setTimeout(r, 150));
		}

		running = false;
		gaps.sort((a, b) => a - b);

		return {
			frames: gaps.length,
			medianFrameMs: gaps[Math.floor(gaps.length / 2)],
			worstFrameMs: gaps[gaps.length - 1],
		};
	});

	await page.evaluate(() => document.dispatchEvent(
		new CustomEvent('navigate', { detail: { view: 'tracks' } }),
	));

	return {
		...opened,
		passMedianMs: round(opened.passMedianMs, 2),
		passWorstMs: round(opened.passWorstMs, 2),
		heapBeforeMB: heapBefore,
		heapAfterMB: heapAfter,
		heapDeltaMB: round(heapAfter - heapBefore, 2),
		scroll: scroll && {
			frames: scroll.frames,
			medianFrameMs: round(scroll.medianFrameMs),
			worstFrameMs: round(scroll.worstFrameMs),
		},
	};
}

/**
 * `perf.M6` / `H-14`: the index status was pushed every 3 s for the life
 * of the process, with an identical payload once the index was ready,
 * and `config-page` assigns it to a `@state` field — so a user who had
 * once visited Settings paid a full re-render of a 2 000-line template
 * every 3 s, forever, for no news.
 *
 * Measured as a *rate*: events received and component updates performed
 * while the page sits there doing nothing.  A duration long enough to
 * span several ticks, or a build that emits nothing either way is
 * indistinguishable from the fix.
 */
async function measureSettingsIdle(page, seconds = 15) {
	return page.evaluate(async (secs) => {
		const ev = window.__yjEvents;

		document.dispatchEvent(
			new CustomEvent('navigate', { detail: { view: 'settings' } }),
		);
		// Settling first, so the page's own load is not counted as idle
		// cost: arriving legitimately fetches and renders.
		await new Promise((r) => setTimeout(r, 3000));

		const el = document.querySelector('config-page');
		if (!el) return { error: 'config-page not mounted' };

		// Count renders on the instance. `update` is a prototype method,
		// so an own-property override intercepts without touching any
		// other component.
		let updates = 0;
		const original = el.update;
		el.update = function patched(...args) {
			updates++;

			return original.apply(this, args);
		};

		ev.reset();
		window.__yjPerf.longtasks.length = 0;
		const t0 = performance.now();

		await new Promise((r) => setTimeout(r, secs * 1000));

		delete el.update;

		const elapsed = (performance.now() - t0) / 1000;
		const names = ev.names();

		document.dispatchEvent(
			new CustomEvent('navigate', { detail: { view: 'tracks' } }),
		);

		return {
			seconds: Math.round(elapsed),
			indexStatusEvents: names.IndexStatusChanged ?? 0,
			renders: updates,
			blockingMs: window.__yjPerf.longtasks
				.reduce((n, l) => n + l.ms, 0),
			events: names,
		};
	}, seconds);
}

/**
 * `perf.M7` / `M8`: the Explore caches are never evicted.
 *
 * This finding failed to reproduce twice, in two separate sessions,
 * against `measureHeapAfterBrowse` — 37 → 38 MB either way.  The reason
 * is not that the caches are bounded: it is that the browse script
 * *visits* Explore and never types in it, and both caches are filled
 * only by a search.  A view that is opened and looked at populates
 * nothing.
 *
 * So this is a *session*, not a visit: a fixed list of queries typed
 * into the page's own search box, with long enough after each for the
 * thumbnail fetches it fires to stream back.  Heap is sampled post-GC
 * after each one, so the result is a curve rather than a single figure
 * — a monotonic climb is the finding, and a plateau is the fix.
 *
 * Two things make it repeatable enough to compare builds:
 *
 *   - The queries are fixed and well-known, so the same albums are
 *     fetched every run and the same art comes back.
 *   - Cover art is cached on disk by the backend after the first run,
 *     so every subsequent run is a disk read.  The *first* run against
 *     a cold `YJ_HOME` is slow and its timings mean nothing; its heap
 *     numbers are still valid, because what is retained does not depend
 *     on where the bytes came from.
 *
 * **The session must be longer than the caps it is testing.**  The
 * first version ran twelve searches, which cached 180 thumbnails
 * against a cap of 192 — so the bounded build evicted nothing, reported
 * a number identical to the unbounded one, and looked exactly like a
 * fix that did not work.  Twenty-four searches overruns both caps by a
 * comfortable margin, which is what makes the plateau visible.
 */
const EXPLORE_QUERIES = [
	'radiohead', 'the beatles', 'david bowie', 'miles davis',
	'nirvana', 'portishead', 'aphex twin', 'bjork',
	'massive attack', 'kraftwerk', 'joy division', 'pixies',
	'talking heads', 'brian eno', 'can', 'neu',
	'slowdive', 'mogwai', 'godspeed', 'burial',
	'autechre', 'boards of canada', 'stereolab', 'broadcast',
];

// Each search fires one thumbnail fetch per uncached release group, and
// they stream back individually.  Too short a wait measures a session
// that has not finished arriving, which understates retention on both
// builds and is therefore not even wrong in a useful direction.
const EXPLORE_SETTLE_MS = 6000;

/**
 * `perf.M3` / `M4`: what scrolling a long list costs.
 *
 * Two findings share one measurement because they are the same shape —
 * per-row work that only happens while the virtualizer is recycling
 * rows, and is therefore invisible to every other number here.
 *
 *   M3  The track list's Art column renders `CoverArtPath`, the
 *       *original* embedded artwork (commonly 1500×1500, several
 *       hundred kB) scaled by CSS into a 24 px box, with no
 *       `loading="lazy"` and no `decoding="async"` — while
 *       `CoverArtSmall` (100 px) sits unused on the same model.  So
 *       the honest number is **image bytes fetched per screen of
 *       scrolling**, and the decode cost behind it.
 *
 *       Except that the bulk library cannot show that, and it is worth
 *       knowing why before reading the row: `cmd/gentestdata` generates
 *       **300×300, ~3.7 kB** covers on purpose (a smooth gradient, so
 *       50 000 tracks come to 466 MB instead of 2 GB).  Its "original"
 *       is already thumbnail-sized, so the bytes saved by picking the
 *       right tier are 3.7 kB → 1.1 kB per cover instead of the
 *       hundreds of kB a real library would save.  The number that is
 *       *not* hostage to the fixture is **which tier was requested**,
 *       counted below — it goes to zero originals when the fix lands,
 *       on any library.
 *
 *   M4  `artists-view` falls back to scanning every cached album,
 *       lowercasing two strings per comparison, to find a cover for an
 *       artist with no image — inside `renderItem`, i.e. per card per
 *       frame.  The bulk seed has 440 artists, 4 988 albums and **zero**
 *       artist images, so every card takes the fallback.  The number is
 *       main-thread blocking during the same scroll.
 *
 * The Art column is off by default, so this stages it — idempotently,
 * through the real `SetTrackListColumns` binding rather than by writing
 * config — and puts it back afterwards.  Without that, M3 measures a
 * column nobody is rendering.
 */
const SCROLL_STEPS = 12;
const SCROLL_SETTLE_MS = 260;

async function measureScroll(page) {
	// -- M3: the track list, with the Art column staged on. --
	const priorColumns = await page.evaluate(async () => {
		const ev = window.__yjEvents;
		const prior = await ev.call('config.Config.GetTrackListColumns', [], 15000);

		await ev.call('config.Config.SetTrackListColumns', [
			[{ id: 'albumArt' }, { id: 'trackName' },
				{ id: 'artistName' }, { id: 'trackLength' }],
		], 15000);

		return (prior ?? []).map((c) => ({ id: c.id }));
	});

	const scrollView = async (view, tag) => {
		await page.evaluate((v) => document.dispatchEvent(
			new CustomEvent('navigate', { detail: { view: v } }),
		), view);
		await page.waitForTimeout(1200);

		return page.evaluate(async ([elementTag, steps, settleMs]) => {
			const host = document.querySelector(
				`#main-content > ${elementTag}`,
			);
			if (!host?.shadowRoot) return { error: `${elementTag} not mounted` };

			// The scroller is whichever descendant actually overflows.
			let scroller = null;
			for (const cand of host.shadowRoot.querySelectorAll('*')) {
				if (cand.scrollHeight > cand.clientHeight + 100) {
					scroller = cand;
					break;
				}
			}
			if (!scroller) return { error: `${elementTag}: no scroller` };

			const imgBytes = () => performance
				.getEntriesByType('resource')
				.filter((r) => r.initiatorType === 'img')
				.reduce((n, r) => n + (r.encodedBodySize || 0), 0);
			const imgCount = () => performance
				.getEntriesByType('resource')
				.filter((r) => r.initiatorType === 'img').length;
			// Cover art is written as `<hash>.jpg` (original) alongside
			// `<hash>_sm|_md|_lg.jpg`, so the tier a request asked for is
			// readable straight off the URL.
			const originals = () => performance
				.getEntriesByType('resource')
				.filter((r) => r.initiatorType === 'img'
					&& /\/covers\//.test(r.name)
					&& !/_(sm|md|lg)\.[a-z]+$/.test(r.name))
				.length;

			// Settle first: arriving at a view legitimately loads what is
			// on screen, and that is not a scrolling cost.
			scroller.scrollTop = 0;
			await new Promise((r) => setTimeout(r, 600));

			const bytes0 = imgBytes();
			const count0 = imgCount();
			const orig0 = originals();
			window.__yjPerf.longtasks.length = 0;

			const frames = [];
			const t0 = performance.now();

			for (let i = 0; i < steps; i++) {
				const s0 = performance.now();

				scroller.scrollTop += scroller.clientHeight;
				// One frame to lay out and paint the recycled rows.
				await new Promise((r) =>
					requestAnimationFrame(() => requestAnimationFrame(r)));
				frames.push(performance.now() - s0);

				await new Promise((r) => setTimeout(r, settleMs));
			}

			const totalMs = performance.now() - t0;
			const sorted = [...frames].sort((a, b) => a - b);

			return {
				steps,
				imageBytes: imgBytes() - bytes0,
				imageRequests: imgCount() - count0,
				originalTierRequests: originals() - orig0,
				medianFrameMs: sorted[Math.floor(sorted.length / 2)],
				worstFrameMs: sorted[sorted.length - 1],
				blockingMs: window.__yjPerf.longtasks
					.reduce((n, l) => n + l.ms, 0),
				totalMs,
			};
		}, [tag, SCROLL_STEPS, SCROLL_SETTLE_MS]);
	};

	const tracks = await scrollView('tracks', 'track-list');
	const artists = await scrollView('artists', 'artists-view');

	await page.evaluate(
		(cols) => window.__yjEvents.call(
			'config.Config.SetTrackListColumns', [cols], 15000,
		),
		priorColumns,
	);

	return {
		tracksWithArt: tracks,
		artists,
		imageBytesPerScreen: tracks.imageBytes
			? round(tracks.imageBytes / SCROLL_STEPS / 1024)
			: 0,
	};
}

async function measureExploreSession(page, client) {
	const sample = async () => {
		// GC before each sample: an un-collected heap measures allocation
		// rate, and retention is the finding.
		await client.send('HeapProfiler.collectGarbage');
		await page.waitForTimeout(400);
		const usage = await client.send('Runtime.getHeapUsage');
		const caches = await page.evaluate(
			() => window.__yjCacheStats?.() ?? {},
		);

		return {
			heapMB: round(usage.usedSize / 1024 / 1024, 2),
			caches,
		};
	};

	await page.evaluate(() => document.dispatchEvent(
		new CustomEvent('navigate', { detail: { view: 'explore' } }),
	));
	await page.waitForTimeout(1500);

	const baseline = await sample();
	const curve = [];

	for (const query of EXPLORE_QUERIES) {
		const typed = await page.evaluate((q) => {
			const el = document.querySelector('explore-view');
			const input = el?.shadowRoot?.querySelector('input');
			if (!input) return false;

			// The page's own input path, debounce and all — not the
			// binding underneath it, which would skip the caching the
			// component does on the way back.
			input.value = q;
			input.dispatchEvent(
				new Event('input', { bubbles: true, composed: true }),
			);

			return true;
		}, query);

		if (!typed) return { error: 'explore-view search input not found' };

		await page.waitForTimeout(EXPLORE_SETTLE_MS);
		const s = await sample();
		curve.push({ query, heapMB: s.heapMB });
	}

	const final = await sample();
	const totalChars = Object.values(final.caches)
		.reduce((n, c) => n + (c.chars ?? 0), 0);

	await page.evaluate(() => document.dispatchEvent(
		new CustomEvent('navigate', { detail: { view: 'tracks' } }),
	));

	return {
		queries: EXPLORE_QUERIES.length,
		heapBeforeMB: baseline.heapMB,
		heapAfterMB: final.heapMB,
		heapGrowthMB: round(final.heapMB - baseline.heapMB, 2),
		growthPerQueryMB: round(
			(final.heapMB - baseline.heapMB) / EXPLORE_QUERIES.length, 3,
		),
		cacheEntries: Object.fromEntries(
			Object.entries(final.caches).map(([k, v]) => [k, v.entries]),
		),
		cacheChars: Object.fromEntries(
			Object.entries(final.caches).map(([k, v]) => [k, v.chars]),
		),
		cacheLimits: Object.fromEntries(
			Object.entries(final.caches).map(([k, v]) => [k, v.limit]),
		),
		retainedCharsTotal: totalChars,
		curve,
	};
}

async function measureHeapAfterBrowse(page, client) {
	for (const view of BROWSE_VIEWS) {
		await page.evaluate((v) => document.dispatchEvent(
			new CustomEvent('navigate', { detail: { view: v } }),
		), view);
		await page.waitForTimeout(700);

		// Scroll whatever the view's scroller is, so a virtualizer
		// actually renders rows rather than reporting an idle one.
		await page.evaluate(async () => {
			const el = document.querySelector('#main-content > :not(.view-hidden)');
			const sc = el?.shadowRoot?.querySelector('*');
			for (const cand of el?.shadowRoot?.querySelectorAll('*') ?? []) {
				if (cand.scrollHeight > cand.clientHeight + 100) {
					for (let i = 0; i < 6; i++) {
						cand.scrollTop += cand.clientHeight;
						await new Promise((r) => setTimeout(r, 120));
					}
					return;
				}
			}
			void sc;
		});
	}

	// GC first: an un-collected heap measures allocation rate, not
	// retention, and retention is the finding.
	await client.send('HeapProfiler.collectGarbage');
	await page.waitForTimeout(500);
	const usage = await client.send('Runtime.getHeapUsage');

	// Must pierce shadow roots: every list in this app renders inside
	// one, so a document-level count reports 40 nodes for a page holding
	// several thousand.
	const nodes = await page.evaluate(() => {
		const count = (root) => {
			let n = 0;
			for (const el of root.querySelectorAll('*')) {
				n++;
				if (el.shadowRoot) n += count(el.shadowRoot);
			}

			return n;
		};

		return count(document);
	});

	return {
		heapUsedMB: round(usage.usedSize / 1024 / 1024, 2),
		heapTotalMB: round(usage.totalSize / 1024 / 1024, 2),
		documentNodes: nodes,
	};
}

/**
 * What the selection costs at 50 000 tracks (`perf.m6`).
 *
 * None of the other ten measurements selects anything, so the two
 * helpers the audit names were never on any measured path.
 *
 * Two numbers, because the audit makes two claims of very different
 * size. `getSelectedKeysOrdered()` walks the whole item list rather
 * than the selection, so *starting a drag of one row* pays a 50 000
 * iteration loop — real, and small. `openBatchTrackDetails` does
 * `filePaths.map(fp => tracks.find(...))`, and the audit predicts
 * "Select all → Edit tags" **hangs the renderer**. It does not hang, but
 * it blocks the main thread for six seconds, which is the same thing to
 * a user with a mouse in their hand.
 *
 * Both are driven through the component's own methods rather than a
 * reimplementation of them here — a measurement of a copy of the code
 * is evidence about the copy.
 */
async function measureSelection(page) {
	await page.evaluate(() => document.dispatchEvent(
		new CustomEvent('navigate', { detail: { view: 'tracks' } }),
	));
	await page.waitForTimeout(1200);

	const result = await page.evaluate(async () => {
		const tl = document.querySelector('#main-content > track-list');
		if (!tl?.selection) return { error: 'track-list not mounted' };

		const itemCount = tl.getItemCount();
		if (!itemCount) return { error: 'no tracks' };

		const time = async (fn) => {
			window.__yjPerf.longtasks.length = 0;
			const t0 = performance.now();
			const value = await fn();
			const ms = performance.now() - t0;
			// A `longtask` entry is delivered *after* the task that
			// produced it, so reading the buffer synchronously reports
			// 0 ms of blocking next to a six-second stall — which is
			// this phase's most-repeated broken measurement.
			await new Promise((r) => setTimeout(r, 250));

			return {
				ms,
				value,
				blockingMs: window.__yjPerf.longtasks
					.reduce((n, l) => n + l.ms, 0),
			};
		};

		// One row selected: the cost every `dragstart`, every
		// context-menu action and every favourite toggle pays.
		//
		// Both ends, on purpose. The helper walks the list looking for
		// the selection, so the *first* row is its best case and
		// reporting only that would be self-flattering — the row a user
		// drags is wherever they scrolled to.
		tl.selection.clear();
		tl.selection.handleContextMenu(tl.getItemKey(0));
		const one = await time(() => tl.selection.getSelectedKeysOrdered());

		tl.selection.clear();
		tl.selection.handleContextMenu(tl.getItemKey(itemCount - 1));
		const last = await time(() => tl.selection.getSelectedKeysOrdered());

		// Everything selected.
		tl.selection.selectAll();
		const all = await time(() => tl.selection.getSelectedKeysOrdered());

		// "Select all → Edit tags", through the real opener.
		const keys = tl.selection.getSelectedKeysOrdered();
		const batch = await time(() => tl.openBatchTrackDetails(keys));

		// Leave the app as it was found: a 50 000-row selection and an
		// open dialog would follow this into whatever runs next.
		const dialog = tl.shadowRoot
			?.querySelector('track-details')
			?.shadowRoot?.querySelector('wa-dialog');
		if (dialog) dialog.open = false;
		tl.selection.clear();
		await new Promise((r) => setTimeout(r, 300));

		return {
			itemCount,
			selectedForBatch: keys.length,
			orderedKeysFirstRowMs: one.ms,
			orderedKeysLastRowMs: last.ms,
			orderedKeysAllMs: all.ms,
			batchDetailsMs: batch.ms,
			batchDetailsBlockingMs: batch.blockingMs,
		};
	});

	if (result.error) return result;

	return {
		...result,
		orderedKeysFirstRowMs: round(result.orderedKeysFirstRowMs, 2),
		orderedKeysLastRowMs: round(result.orderedKeysLastRowMs, 2),
		orderedKeysAllMs: round(result.orderedKeysAllMs, 2),
		batchDetailsMs: round(result.batchDetailsMs),
		batchDetailsBlockingMs: round(result.batchDetailsBlockingMs),
	};
}

/**
 * `perf.m2`: "play these" resolves file paths with one binding call per
 * album (sequentially, inside a `for await`) or per genre.
 *
 * Measured through the components' own resolvers rather than through a
 * synthetic loop, so what is counted is the round trips and the bytes a
 * real context-menu Play provokes.  All three sites want file paths and
 * all three ask for whole track rows to get them, which the audit does
 * not mention and which is most of the bytes.
 */
async function measurePlayThese(page) {
	const artist = await page.evaluate(async () => {
		document.dispatchEvent(new CustomEvent('navigate', { detail: { view: 'artists' } }));
		await new Promise((r) => setTimeout(r, 2500));

		const el = document.querySelector('#main-content > artists-view');
		const perf = window.__yjPerf;

		if (!el?.getArtistFilePaths) return { error: 'artists-view not mounted' };

		const artists = el.artists ?? [];

		if (!artists.length) return { error: 'no artists' };

		// Deterministic given the same seed: the fattest artist in the
		// first forty, so the round-trip count is the finding's shape
		// rather than whatever happened to sort first.
		let best = artists[0];
		let bestAlbums = 0;

		for (const a of artists.slice(0, 40)) {
			const n = a.AlbumCount ?? a.albumCount ?? 0;

			if (n > bestAlbums) { bestAlbums = n; best = a; }
		}

		perf.reset();
		const t0 = performance.now();
		const paths = await el.getArtistFilePaths(best);
		const ms = performance.now() - t0;
		const calls = perf.since(t0 - 1);

		return {
			artist: best.Name,
			paths: paths.length,
			ms,
			bindingCalls: calls.length,
			bindingBytes: calls.reduce((n, c) => n + Math.max(0, c.bytes), 0),
		};
	});

	const albums = await page.evaluate(async () => {
		document.dispatchEvent(new CustomEvent('navigate', { detail: { view: 'albums' } }));
		await new Promise((r) => setTimeout(r, 2500));

		const el = document.querySelector('#main-content > cover-grid');
		const perf = window.__yjPerf;
		const mgr = el?.selMgr;

		if (!mgr?.getSelectedAlbumFilePaths) return { error: 'cover-grid not mounted' };

		const all = el.albums ?? [];

		if (all.length < 20) return { error: 'too few albums' };

		const ids = new Set(all.slice(0, 20).map((a) => a.ID));

		perf.reset();
		const t0 = performance.now();
		const paths = await mgr.getSelectedAlbumFilePaths(ids);
		const ms = performance.now() - t0;
		const calls = perf.since(t0 - 1);

		return {
			albums: ids.size,
			paths: paths.length,
			ms,
			bindingCalls: calls.length,
			bindingBytes: calls.reduce((n, c) => n + Math.max(0, c.bytes), 0),
		};
	});

	const genres = await page.evaluate(async () => {
		document.dispatchEvent(new CustomEvent('navigate', { detail: { view: 'genres' } }));
		await new Promise((r) => setTimeout(r, 2500));

		const el = document.querySelector('#main-content > genres-view');
		const perf = window.__yjPerf;

		if (!el?.getFilePathsForGenres) return { error: 'genres-view not mounted' };

		const names = (el.genres ?? []).slice(0, 5).map((g) => g.name);

		if (!names.length) return { error: 'no genres' };

		perf.reset();
		const t0 = performance.now();
		const paths = await el.getFilePathsForGenres(names);
		const ms = performance.now() - t0;
		const calls = perf.since(t0 - 1);

		document.dispatchEvent(new CustomEvent('navigate', { detail: { view: 'tracks' } }));

		return {
			genres: names.length,
			paths: paths.length,
			ms,
			bindingCalls: calls.length,
			bindingBytes: calls.reduce((n, c) => n + Math.max(0, c.bytes), 0),
		};
	});

	const tidy = (r) => (r.error ? r : { ...r, ms: round(r.ms) });

	return { artist: tidy(artist), albums: tidy(albums), genres: tidy(genres) };
}

/**
 * `perf.m4`: two drag interactions register document `mousemove` and
 * `mouseup` up front and only take them down again when the component
 * goes away, so every pointer move anywhere in the app runs a handler
 * that guards and returns.
 *
 * The audit already concedes the cost is small, so the honest number
 * here is the *listener census* — taken through CDP rather than by
 * patching `addEventListener`, because a global patch would perturb
 * every other measurement in the run and make the before and after
 * differ in two things.  The dispatch cost is measured too, and is
 * expected to be noise: a row saying so is worth more than an omitted
 * one.
 */
async function measurePointerListeners(page, client) {
	await page.evaluate(() => document.dispatchEvent(
		new CustomEvent('navigate', { detail: { view: 'tracks' } }),
	));
	await page.waitForTimeout(1200);

	const { result } = await client.send('Runtime.evaluate', { expression: 'document' });
	const { listeners } = await client.send('DOMDebugger.getEventListeners', {
		objectId: result.objectId,
	});

	const byType = {};

	for (const l of listeners) byType[l.type] = (byType[l.type] ?? 0) + 1;

	const dispatch = await page.evaluate(() => {
		const N = 2000;
		const ev = () => new MouseEvent('mousemove', {
			clientX: Math.random() * 800, clientY: Math.random() * 600, bubbles: true,
		});

		for (let i = 0; i < 200; i++) document.dispatchEvent(ev());

		const t0 = performance.now();

		for (let i = 0; i < N; i++) document.dispatchEvent(ev());

		const ms = performance.now() - t0;

		return { events: N, ms, perEventUs: (ms / N) * 1000 };
	});

	return {
		documentListeners: byType,
		mousemove: byType.mousemove ?? 0,
		mouseup: byType.mouseup ?? 0,
		dispatchMs: round(dispatch.ms, 2),
		dispatchEvents: dispatch.events,
		perEventUs: round(dispatch.perEventUs, 3),
	};
}

/**
 * `perf.m5`: `now-playing.updated()` does unconditional DOM work on
 * every update pass, and the player store notifies at 1 Hz while
 * playing — so this runs about once a second for the life of a track.
 *
 * None of the other eleven measurements sees layout cost, so this one
 * instruments it directly rather than sampling: the element's own
 * `updated()` is wrapped, and inside it `querySelector`, the
 * layout-forcing `scrollWidth`/`clientWidth` getters and
 * `style.setProperty` are counted.  `forcedLayouts` is the number of
 * layout reads taken while the DOM is dirty — i.e. the read/write
 * interleave the finding is actually about, since a read after a write
 * is what makes the engine flush layout again.
 *
 * Two numbers that have to agree, per this phase's most-repeated
 * lesson: the counts and the wall time inside `updated()`.  A guard
 * that drops one without the other is a broken measurement, not a win.
 *
 * Measured twice, because the work is conditional on the text
 * overflowing and only one of the two branches is the expensive one.
 * The panel is narrowed to its minimum to stage the overflowing case —
 * by the component's own resize path, and put back afterwards.
 */
async function measurePlayerBarPass(page) {
	const result = await page.evaluate(async () => {
		const ev = window.__yjEvents;
		const el = document.querySelector('now-playing');

		if (!el) return { error: 'now-playing not mounted' };

		// Stage a loaded track.  Deliberately the same first tracks the
		// track-change measurement already played, so this warms no cover
		// art that a later measurement counts requests for.
		const tracks = await ev.call('library.Library.GetAllTracks', [], 60000);
		const paths = (tracks ?? []).slice(0, 4).map((t) => t.FilePath);

		if (paths.length < 2) return { error: 'library too small to measure' };

		await ev.call(
			'queue.Queue.SetQueue',
			// The fourth argument is the queue's source; these are
			// ad-hoc tracks, so it is the empty one.  v3 rejects a
			// call with the wrong argument count where v2 filled the
			// gap with a zero value.
			[paths, 0, false, { type: '', id: 0, label: '' }],
			15000,
		);
		await ev.call('queue.Queue.PlayIndex', [0], 15000);
		await ev.call('player.Player.Pause', [], 5000).catch(() => {});
		await new Promise((r) => setTimeout(r, 600));

		/* ---- instrument -------------------------------------------- */

		const c = {
			on: false, dirty: false,
			qs: 0, reads: 0, writes: 0, flushes: 0, passes: 0, ms: 0,
		};

		const restore = [];

		for (const proto of [Element.prototype, DocumentFragment.prototype]) {
			const orig = proto.querySelector;
			proto.querySelector = function (...a) {
				if (c.on) c.qs++;

				return orig.apply(this, a);
			};
			restore.push(() => { proto.querySelector = orig; });
		}

		// `scrollWidth`/`clientWidth` live on Element, `offsetWidth` on
		// HTMLElement — asking the wrong prototype yields `undefined` and
		// `defineProperty` throws.
		for (const [proto, name] of [
			[Element.prototype, 'scrollWidth'],
			[Element.prototype, 'clientWidth'],
			[HTMLElement.prototype, 'offsetWidth'],
		]) {
			const d = Object.getOwnPropertyDescriptor(proto, name);
			Object.defineProperty(proto, name, {
				...d,
				get() {
					if (c.on) {
						c.reads++;
						if (c.dirty) { c.flushes++; c.dirty = false; }
					}

					return d.get.call(this);
				},
			});
			restore.push(() => Object.defineProperty(proto, name, d));
		}

		const setProp = CSSStyleDeclaration.prototype.setProperty;
		CSSStyleDeclaration.prototype.setProperty = function (...a) {
			if (c.on) { c.writes++; c.dirty = true; }

			return setProp.apply(this, a);
		};
		restore.push(() => { CSSStyleDeclaration.prototype.setProperty = setProp; });

		// Counting inside the element's own `updated()` rather than around
		// the pass: during real playback the seek bar updates in the same
		// frame, and its layout reads are not this finding.
		const protoUpdated = Object.getPrototypeOf(el).updated;
		el.updated = function (changed) {
			c.on = true;
			// A render mutated the DOM, so the first read of the pass
			// forces layout whatever else happens.
			c.dirty = true;
			const t0 = performance.now();

			try {
				protoUpdated.call(this, changed);
			} finally {
				c.ms += performance.now() - t0;
				c.passes++;
				c.on = false;
			}
		};
		restore.push(() => { delete el.updated; });

		const zero = () => {
			c.qs = 0; c.reads = 0; c.writes = 0;
			c.flushes = 0; c.passes = 0; c.ms = 0;
		};

		const PASSES = 30;

		const synthetic = async (mutate) => {
			// One warm pass so the first-of-a-build cost is not the median.
			el.requestUpdate();
			await el.updateComplete;
			zero();

			const wall = [];

			for (let i = 0; i < PASSES; i++) {
				if (mutate) mutate(i);
				const t0 = performance.now();
				el.requestUpdate();
				// eslint-disable-next-line no-await-in-loop
				await el.updateComplete;
				wall.push(performance.now() - t0);
			}

			wall.sort((a, b) => a - b);

			return {
				passes: c.passes,
				querySelectorsPerPass: c.qs / c.passes,
				layoutReadsPerPass: c.reads / c.passes,
				styleWritesPerPass: c.writes / c.passes,
				forcedLayoutsPerPass: c.flushes / c.passes,
				updatedMedianMs: c.ms / c.passes,
				passMedianMs: wall[Math.floor(wall.length / 2)],
			};
		};

		// Wide: the title fits, so the scroll-distance half is skipped.
		el.updateWidth(500);
		el.requestUpdate();
		await el.updateComplete;
		await new Promise((r) => setTimeout(r, 200));
		const wide = await synthetic();
		wide.titleOverflows = el.titleOverflows;

		// Narrow: the text overflows, which is the expensive branch.
		el.updateWidth(120);
		el.requestUpdate();
		await el.updateComplete;
		await new Promise((r) => setTimeout(r, 200));
		const narrow = await synthetic();
		narrow.titleOverflows = el.titleOverflows;

		// Narrow, and the DOM genuinely changes every pass.  The steady
		// state above does not: a 1 Hz position report changes nothing
		// this component renders, so its reads hit a clean layout and are
		// nearly free.  Hovering the title toggles a class, which is what
		// makes the read-after-write actually flush — the upper bound the
		// finding describes, and the number the guard has to move.
		const dirty = await synthetic(() => {
			el.titleHovered = !el.titleHovered;
		});
		dirty.titleOverflows = el.titleOverflows;
		el.titleHovered = false;
		await el.updateComplete;

		/* ---- and what a second of real playback costs --------------- */

		const SECONDS = 6;
		zero();
		await ev.call('player.Player.Play', [], 5000).catch(() => {});
		await new Promise((r) => setTimeout(r, SECONDS * 1000));
		await ev.call('player.Player.Pause', [], 5000).catch(() => {});

		const playback = {
			seconds: SECONDS,
			passes: c.passes,
			forcedLayouts: c.flushes,
			updatedMs: c.ms,
		};

		/* ---- leave it as it was found ------------------------------- */

		for (const undo of restore.reverse()) undo();
		el.updateWidth(320);
		await ev.call('queue.Queue.Clear', [], 10000).catch(() => {});

		return { wide, narrow, dirty, playback };
	});

	if (result.error) return result;

	const tidy = (p) => ({
		...p,
		querySelectorsPerPass: round(p.querySelectorsPerPass, 2),
		layoutReadsPerPass: round(p.layoutReadsPerPass, 2),
		styleWritesPerPass: round(p.styleWritesPerPass, 2),
		forcedLayoutsPerPass: round(p.forcedLayoutsPerPass, 2),
		updatedMedianMs: round(p.updatedMedianMs, 3),
		passMedianMs: round(p.passMedianMs, 3),
	});

	return {
		wide: tidy(result.wide),
		narrow: tidy(result.narrow),
		dirty: tidy(result.dirty),
		playback: { ...result.playback, updatedMs: round(result.playback.updatedMs, 2) },
	};
}

/* -------------------------------------------------------------------- */

function round(n, dp = 1) {
	if (typeof n !== 'number' || Number.isNaN(n)) return n;
	const f = 10 ** dp;

	return Math.round(n * f) / f;
}

async function run(label) {
	const browser = await chromium.launch();
	const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
	await context.addInitScript({ path: BRIDGE });
	await context.addInitScript(
		`(${INSTRUMENT})(${JSON.stringify(METHOD_NAMES)})`,
	);

	const page = await context.newPage();
	const client = await context.newCDPSession(page);
	await client.send('HeapProfiler.enable');

	const t0 = Date.now();
	await page.goto(BASE_URL, { waitUntil: 'load' });
	await page.evaluate(() => window.__yjEvents.ready(30000));
	// No second instrumentation pass.  The old one existed because
	// wrapping had to happen after `window.go` appeared, yet the long
	// task observer had to start before it; the bridge now records every
	// binding call from the initScript onward, so one pass does both.

	const report = {
		label,
		takenAt: new Date().toISOString(),
		url: BASE_URL,
		loadWallMs: Date.now() - t0,
	};

	console.log('  startup…');
	report.startup = await measureStartup(page);
	// Before anything else navigates: every view's first open has to be
	// its genuine first open, or the chunk is already warm.
	console.log('  view open…');
	report.viewOpen = await measureViewOpen(page);
	console.log('  keystroke…');
	report.keystroke = await measureKeystroke(page);
	console.log('  track change…');
	report.trackChange = await measureTrackChange(page);
	// Immediately after the track change, which has already played and
	// paused these same first tracks: staging costs nothing new and
	// warms no cover art a later measurement counts requests for.
	console.log('  player bar update pass…');
	report.playerBar = await measurePlayerBarPass(page);
	console.log('  favourite toggle…');
	report.favourite = await measureFavouriteToggle(page);
	console.log('  settings idle…');
	report.settingsIdle = await measureSettingsIdle(page);
	console.log('  scroll (art column, artist grid)…');
	report.scroll = await measureScroll(page);
	console.log('  browse + heap…');
	report.heap = await measureHeapAfterBrowse(page, client);
	// After the heap absolute, because it retains 50 000 keys and a
	// batch dialog holding 50 000 tracks while it runs.
	console.log('  selection (50 000 tracks)…');
	report.selection = await measureSelection(page);
	// Leaves the app on Tracks, which is where the column-resize
	// listeners live.
	console.log('  document pointer listeners…');
	report.pointer = await measurePointerListeners(page, client);
	console.log('  play this artist / these albums / these genres…');
	report.playThese = await measurePlayThese(page);
	// Deliberately last.  The Explore session retains ~9 MB on an
	// unbounded build, and running it first carries that into the browse
	// heap figure — which silently stops that row comparing against every
	// measurement taken before this one existed.  The session reports a
	// *delta*, so it does not care what preceded it; the browse figure is
	// an absolute, so it does.
	console.log(`  explore session (${EXPLORE_QUERIES.length} searches)…`);
	report.exploreSession = await measureExploreSession(page, client);
	// Also deliberately last, and for the mirror-image reason: opening a
	// 2 000-track playlist pulls ~90 cover images, which sit in the HTTP
	// cache and take the scroll measurement's request count to zero if
	// this runs first.  A measurement that warms something is a
	// measurement that has to go after everything reading it.
	console.log('  playlist open (2 000 tracks)…');
	report.playlistOpen = await measurePlaylistOpen(page, client);

	await browser.close();

	mkdirSync(OUT_DIR, { recursive: true });
	const out = resolve(OUT_DIR, `${label}.json`);
	writeFileSync(out, JSON.stringify(report, null, 2) + '\n');
	console.log(`\nwrote ${out}\n`);
	console.log(table([report]));

	return report;
}

const ROWS = [
	['First contentful paint', (r) => fmt(r.startup.firstContentfulPaintMs, 'ms')],
	['First track row', (r) => fmt(r.startup.firstRowAfterLoadMs, 'ms')],
	['JS transferred', (r) => fmt(round(r.startup.scriptBytes / 1024), 'kB')],
	['JS evaluated before first paint', (r) => fmt(round((r.startup.scriptBytesBeforePaint ?? 0) / 1024), 'kB')],
	['JS after visiting every view', (r) => fmt(round((r.viewOpen?.totalScriptBytes ?? 0) / 1024), 'kB')],
	['Chunks after visiting every view', (r) => String(r.viewOpen?.totalScriptCount ?? '—')],
	['Slowest first view open', (r) => fmt(r.viewOpen?.worstFirstOpenMs, 'ms')],
	['Cross-origin requests', (r) => String(r.startup.crossOriginRequests)],
	['Keystroke → paint (median)', (r) => fmt(r.keystroke.medianMs, 'ms')],
	['Keystroke blocking (median)', (r) => fmt(r.keystroke.medianBlockingMs, 'ms')],
	['Track change: binding calls', (r) => String(r.trackChange.bindingCalls ?? '—')],
	['Track change: bytes over IPC', (r) => fmt(round((r.trackChange.bindingBytes ?? 0) / 1024 / 1024, 2), 'MB')],
	['Track change: longest task', (r) => fmt(r.trackChange.longestTaskMs, 'ms')],
	['Favourite (Playlists never opened): bytes', (r) => fmt(round((r.favourite.closed.bindingBytes ?? 0) / 1024 / 1024, 2), 'MB')],
	['Favourite (Playlists open): binding calls', (r) => String(r.favourite.open.bindingCalls ?? '—')],
	['Favourite (Playlists open): bytes', (r) => fmt(round((r.favourite.open.bindingBytes ?? 0) / 1024 / 1024, 2), 'MB')],
	['Favourite (Playlists open): longest task', (r) => fmt(r.favourite.open.longestTaskMs, 'ms')],
	['Playlist open: DOM nodes', (r) => String(r.playlistOpen?.nodes ?? '—')],
	['Playlist open: rows in DOM', (r) => String(r.playlistOpen?.rowsInDom ?? '—')],
	['Playlist open: images in DOM', (r) => String(r.playlistOpen?.imagesInDom ?? '—')],
	['Playlist open: eager images', (r) => String(r.playlistOpen?.imagesEager ?? '—')],
	['Playlist open: first row', (r) => fmt(r.playlistOpen?.firstRowMs, 'ms')],
	['Playlist open: heap retained', (r) => fmt(r.playlistOpen?.heapDeltaMB, 'MB')],
	['Playlist open: update pass (median)', (r) => fmt(r.playlistOpen?.passMedianMs, 'ms')],
	['Playlist open: listeners rebound per pass', (r) => String(r.playlistOpen?.passListenersRebound ?? '—')],
	['Playlist open: worst scroll frame', (r) => fmt(r.playlistOpen?.scroll?.worstFrameMs, 'ms')],
	['Settings idle: status events', (r) => `${r.settingsIdle.indexStatusEvents} / ${r.settingsIdle.seconds} s`],
	['Settings idle: re-renders', (r) => String(r.settingsIdle.renders)],
	['Scroll tracks+art: image bytes', (r) => fmt(round((r.scroll?.tracksWithArt?.imageBytes ?? 0) / 1024 / 1024, 2), 'MB')],
	['Scroll tracks+art: per screen', (r) => fmt(r.scroll?.imageBytesPerScreen, 'kB')],
	['Scroll tracks+art: image requests', (r) => String(r.scroll?.tracksWithArt?.imageRequests ?? '—')],
	['Scroll tracks+art: full-size originals', (r) => String(r.scroll?.tracksWithArt?.originalTierRequests ?? '—')],
	['Scroll tracks+art: worst frame', (r) => fmt(round(r.scroll?.tracksWithArt?.worstFrameMs), 'ms')],
	['Scroll tracks+art: blocking', (r) => fmt(round(r.scroll?.tracksWithArt?.blockingMs), 'ms')],
	['Scroll artists: worst frame', (r) => fmt(round(r.scroll?.artists?.worstFrameMs), 'ms')],
	['Scroll artists: blocking', (r) => fmt(round(r.scroll?.artists?.blockingMs), 'ms')],
	['Explore session: heap growth', (r) => fmt(r.exploreSession?.heapGrowthMB, 'MB')],
	['Explore session: growth per search', (r) => fmt(r.exploreSession?.growthPerQueryMB, 'MB')],
	['Explore session: thumbnails cached', (r) => String(r.exploreSession?.cacheEntries?.['explore.thumbnails'] ?? '—')],
	['Explore session: artist images cached', (r) => String(r.exploreSession?.cacheEntries?.['explore.artistImages'] ?? '—')],
	['Explore session: retained chars', (r) => fmt(round((r.exploreSession?.retainedCharsTotal ?? 0) / 1e6, 2), 'M')],
	['Selection: ordered keys, first row', (r) => fmt(r.selection?.orderedKeysFirstRowMs, 'ms')],
	['Selection: ordered keys, last row', (r) => fmt(r.selection?.orderedKeysLastRowMs, 'ms')],
	['Selection: ordered keys, all selected', (r) => fmt(r.selection?.orderedKeysAllMs, 'ms')],
	['Selection: select all → edit tags', (r) => fmt(r.selection?.batchDetailsMs, 'ms')],
	['Selection: select all → edit tags, blocking', (r) => fmt(r.selection?.batchDetailsBlockingMs, 'ms')],
	['Player bar: querySelectors per pass', (r) => String(r.playerBar?.narrow?.querySelectorsPerPass ?? '—')],
	['Player bar: layout reads per pass', (r) => String(r.playerBar?.narrow?.layoutReadsPerPass ?? '—')],
	['Player bar: style writes per pass', (r) => String(r.playerBar?.narrow?.styleWritesPerPass ?? '—')],
	['Player bar: forced layouts per pass', (r) => String(r.playerBar?.narrow?.forcedLayoutsPerPass ?? '—')],
	['Player bar: updated() per pass', (r) => fmt(r.playerBar?.narrow?.updatedMedianMs, 'ms')],
	['Player bar: whole pass (median)', (r) => fmt(r.playerBar?.narrow?.passMedianMs, 'ms')],
	['Player bar (no overflow): forced layouts per pass', (r) => String(r.playerBar?.wide?.forcedLayoutsPerPass ?? '—')],
	['Player bar (no overflow): updated() per pass', (r) => fmt(r.playerBar?.wide?.updatedMedianMs, 'ms')],
	['Player bar (DOM changed): querySelectors per pass', (r) => String(r.playerBar?.dirty?.querySelectorsPerPass ?? '—')],
	['Player bar (DOM changed): forced layouts per pass', (r) => String(r.playerBar?.dirty?.forcedLayoutsPerPass ?? '—')],
	['Player bar (DOM changed): updated() per pass', (r) => fmt(r.playerBar?.dirty?.updatedMedianMs, 'ms')],
	['Player bar (DOM changed): whole pass (median)', (r) => fmt(r.playerBar?.dirty?.passMedianMs, 'ms')],
	['Player bar, 6 s of playback: passes', (r) => String(r.playerBar?.playback?.passes ?? '—')],
	['Player bar, 6 s of playback: forced layouts', (r) => String(r.playerBar?.playback?.forcedLayouts ?? '—')],
	['Player bar, 6 s of playback: updated() total', (r) => fmt(r.playerBar?.playback?.updatedMs, 'ms')],
	['Document mousemove listeners', (r) => String(r.pointer?.mousemove ?? '—')],
	['Document mouseup listeners', (r) => String(r.pointer?.mouseup ?? '—')],
	['2 000 mousemoves dispatched', (r) => fmt(r.pointer?.dispatchMs, 'ms')],
	['Per pointer move', (r) => fmt(r.pointer?.perEventUs, 'us')],
	['Play artist: binding calls', (r) => String(r.playThese?.artist?.bindingCalls ?? '—')],
	['Play artist: bytes over IPC', (r) => fmt(round((r.playThese?.artist?.bindingBytes ?? 0) / 1024, 1), 'kB')],
	['Play artist: wall time', (r) => fmt(r.playThese?.artist?.ms, 'ms')],
	['Play 20 albums: binding calls', (r) => String(r.playThese?.albums?.bindingCalls ?? '—')],
	['Play 20 albums: bytes over IPC', (r) => fmt(round((r.playThese?.albums?.bindingBytes ?? 0) / 1024, 1), 'kB')],
	['Play 20 albums: wall time', (r) => fmt(r.playThese?.albums?.ms, 'ms')],
	['Play 5 genres: binding calls', (r) => String(r.playThese?.genres?.bindingCalls ?? '—')],
	['Play 5 genres: bytes over IPC', (r) => fmt(round((r.playThese?.genres?.bindingBytes ?? 0) / 1024, 1), 'kB')],
	['Play 5 genres: wall time', (r) => fmt(r.playThese?.genres?.ms, 'ms')],
	['Heap after browse', (r) => fmt(r.heap.heapUsedMB, 'MB')],
	['DOM nodes after browse', (r) => String(r.heap.documentNodes)],
];

function fmt(v, unit) {
	return v === null || v === undefined ? '—' : `${v} ${unit}`;
}

function table(reports) {
	const head = ['Measurement', ...reports.map((r) => r.label)];
	const lines = [
		`| ${head.join(' | ')} |`,
		`|${head.map(() => '---').join('|')}|`,
	];

	for (const [name, get] of ROWS) {
		const cells = reports.map((r) => {
			try { return get(r); } catch { return '—'; }
		});
		lines.push(`| ${name} | ${cells.join(' | ')} |`);
	}

	return lines.join('\n');
}

function compare(a, b) {
	const load = (l) => {
		const p = resolve(OUT_DIR, `${l}.json`);
		if (!existsSync(p)) throw new Error(`no measurement labelled '${l}' at ${p}`);

		return JSON.parse(readFileSync(p, 'utf8'));
	};

	console.log(table([load(a), load(b)]));
}

const args = parseArgs(process.argv.slice(2));

if (args.compare) {
	compare(args.compare[0], args.compare[1]);
} else if (args.label) {
	await run(args.label);
} else {
	console.error('usage: measure.mjs --label <name> | --compare <a> <b>');
	process.exit(2);
}
