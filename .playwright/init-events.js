/*
 * YellowJacket harness bridge — installed as a Playwright initScript, so
 * it runs in every page *before* any application script.
 *
 * Why this file exists: half of what this app does is push-driven.  Scan
 * progress, job updates, download progress, WantedListChanged and 40-odd
 * other events arrive from Go whenever they arrive.  An assertion that
 * sleeps and hopes is flaky; an assertion that awaits the event is not.
 *
 * Four things it provides on `window.__yjEvents`:
 *
 *   record   every backend -> frontend event, in order, with payloads
 *   wait     a promise that settles on a matching event (or rejects
 *            with the list of events that *did* arrive, which is the
 *            single most useful failure message this harness can give)
 *   call     a bound Go method, by name, over the runtime's own HTTP
 *            endpoint — no dependence on the app's bundle
 *   bindings every binding call the *app* made, which is what turns
 *            "did that refetch the library" from an inference into a
 *            fact (e2e/perf/measure.mjs labels and reads these)
 *
 * WHERE IT HOOKS.  Two places, and neither is `EventsOn`.
 *
 * Inbound, `window._wails.dispatchWailsEvent`: v3's runtime assigns it
 * at module scope and it is the single point every backend event enters
 * the page through, so wrapping it captures all 46 whether or not the
 * app subscribes to them.  The runtime does
 * `window._wails = window._wails || {}`, so this script creates that
 * object first and puts an accessor on the *property*, wrapping at
 * assignment time — v2 needed the accessor on `window` itself, because
 * there the whole object was replaced.
 *
 * Outbound, `fetch`: v3 routes every runtime call — binding calls, event
 * emits, window and dialog calls — through one POST to /wails/runtime.
 * There is no global to wrap the way v2's `window.runtime` could be, and
 * this is better anyway: it sees calls from any module, needs no walk of
 * an object graph, and cannot miss one made before the harness looked.
 *
 * INSTALL EXACTLY ONCE.  Listeners registered by one `eval` survive into
 * the next, so a recorder that re-registers double-counts.  Tests call
 * `__yjEvents.reset()`; they never re-install.
 */
(() => {
	if (window.__yjEvents) {
		return;
	}

	const LIMIT = 2000;

	// Every bound service in this app lives under this Go module path,
	// so specs name a binding the short way — 'queue.Queue.GetState' —
	// and this is what makes that the same thing the backend calls
	// 'yellowjacket/backend/queue.Queue.GetState'.
	const FQN_PREFIX = "yellowjacket/backend/";

	// The runtime's own object and method ids (objectNames in
	// @wailsio/runtime): 0 is Call, 3 is Events, and method 0 on each is
	// CallBinding and Emit respectively.
	const OBJECT_CALL = 0;
	const OBJECT_EVENTS = 3;

	// Captured before the wrap below, and used for the harness's own
	// calls: `__yjEvents.call` is this file talking to the backend, not
	// the app, and counting it would make "did that action refetch the
	// library" answer for the question as well as the app.
	const nativeFetch = window.fetch.bind(window);

	let seq = 0;
	const log = [];
	const bindings = [];
	const waiters = new Set();

	const summarize = () => {
		const counts = {};
		for (const e of log) {
			counts[e.name] = (counts[e.name] || 0) + 1;
		}
		return counts;
	};

	/*
	 * `data` is recorded as the argument list Go emitted, which is the
	 * shape every spec reads (`ev.data[0]`).
	 *
	 * v3's EventManager.Emit packs a variadic call into one field: no
	 * arguments is null, one is the value itself, more than one is the
	 * slice.  Unpacking that back into a list is exact except for a
	 * single argument that is itself an array, which is indistinguishable
	 * from several arguments — an ambiguity v3 introduced and no
	 * assertion here depends on, since nothing in backend/events emits
	 * more than one value.
	 */
	const argsOf = (data) => {
		if (data === null || data === undefined) {
			return [];
		}
		return Array.isArray(data) ? data : [data];
	};

	const record = (name, data, dir) => {
		const entry = { seq: ++seq, name, data, dir, t: Date.now() };
		log.push(entry);
		if (log.length > LIMIT) {
			log.splice(0, log.length - LIMIT);
		}
		for (const w of Array.from(waiters)) {
			let hit = false;
			try {
				hit = w.test(entry);
			} catch {
				hit = false;
			}
			if (hit) {
				waiters.delete(w);
				clearTimeout(w.timer);
				w.resolve(entry);
			}
		}
		return entry;
	};

	// `name` is a string, or "*" for any event.  `match` is an optional
	// predicate over (data, entry) — only usable from an eval'd function,
	// which is how every harness call is written anyway.
	const makeTest = (name, match) => (entry) => {
		if (name && name !== "*" && entry.name !== name) {
			return false;
		}
		return match ? !!match(entry.data, entry) : true;
	};

	const api = {
		version: 2,

		/** Every recorded event, oldest first. */
		get log() {
			return log.slice();
		},

		/** The sequence number of the most recent event. */
		get seq() {
			return seq;
		},

		/**
		 * Every binding call the app made, oldest first.  Each is
		 * { methodID, methodName, start, ms, bytes } — the id is what the
		 * generated bindings send, and turning it back into a name is
		 * e2e/perf/measure.mjs's job, which derives the map from
		 * frontend/bindings/.
		 */
		get bindings() {
			return bindings.slice();
		},

		/**
		 * Read the size of every binding response.  Off by default: it
		 * costs a clone-and-read of each body, which only a measurement
		 * wants to pay.  With it off, `bytes` is the Content-Length when
		 * the server sent one and -1 otherwise.
		 */
		measureBytes: false,

		/** Drop the buffers.  Does NOT touch the recorder or waiters. */
		reset() {
			const n = log.length;
			log.length = 0;
			bindings.length = 0;
			return n;
		},

		/** Every recorded event, optionally filtered by name. */
		all(name) {
			return name ? log.filter((e) => e.name === name) : log.slice();
		},

		/** How many of `name` (or of everything) have arrived. */
		count(name) {
			return this.all(name).length;
		},

		/** The most recent matching event, or null. */
		last(name) {
			const hits = this.all(name);
			return hits.length ? hits[hits.length - 1] : null;
		},

		/** Everything after a sequence number — pairs with `.seq`. */
		since(n) {
			return log.filter((e) => e.seq > n);
		},

		/** name -> count, for "what actually happened?" */
		names() {
			return summarize();
		},

		/**
		 * Settle on the next (or already-buffered) matching event.
		 *
		 *   await __yjEvents.wait('LibraryScanComplete', { timeoutMs: 60000 })
		 *   await __yjEvents.wait('JobsChanged', { match: (d) => d.length > 0 })
		 *
		 * Rejects on timeout with the names that did arrive, because
		 * "timed out waiting for X" without that list is a dead end.
		 */
		wait(name, opts) {
			const o = opts || {};
			const test = makeTest(name, o.match);
			const since = o.since || 0;

			for (const entry of log) {
				if (entry.seq > since && test(entry)) {
					return Promise.resolve(entry);
				}
			}

			return new Promise((resolve, reject) => {
				const w = { test, resolve };
				w.timer = setTimeout(() => {
					waiters.delete(w);
					reject(
						new Error(
							`__yjEvents.wait(${JSON.stringify(name)}) timed out after ` +
								`${o.timeoutMs || 5000}ms; events seen: ` +
								JSON.stringify(summarize()),
						),
					);
				}, o.timeoutMs || 5000);
				waiters.add(w);
			});
		},

		/**
		 * Resolve when the backend is actually answering calls — not
		 * when the DOM is ready, which is earlier and lies.
		 */
		async ready(timeoutMs) {
			const deadline = Date.now() + (timeoutMs || 15000);
			for (;;) {
				try {
					await api.call("queue.Queue.GetState", [], 2000);
					return true;
				} catch {
					/* backend not up yet */
				}
				if (Date.now() > deadline) {
					throw new Error("__yjEvents.ready timed out");
				}
				await new Promise((r) => setTimeout(r, 100));
			}
		},

		/**
		 * Call a bound Go method by dotted path.
		 *
		 *   await __yjEvents.call('player.Player.SetVolume', [42])
		 *
		 * This posts to the runtime's own endpoint rather than reaching
		 * into the page for a binding function, because v3 has no
		 * `window.go` and the generated bindings are ordinary bundled
		 * modules an initScript cannot import.  It calls *by name*, which
		 * the backend resolves the same way it resolves the id the
		 * bundle sends.
		 *
		 * v3 rejects a bad call rather than silently never firing its
		 * callback the way v2 did — wrong argument types come back as a
		 * TypeError naming the argument, an unknown method as a
		 * ReferenceError.  The timeout below is therefore a backstop for
		 * a genuinely hung request, not the mechanism that makes a
		 * mistake visible.
		 */
		call(path, args, timeoutMs) {
			const request = nativeFetch("/wails/runtime", {
				method: "POST",
				headers: {
					"Content-Type": "application/json",
					"x-wails-client-id": window._wails?.clientId ?? "",
				},
				body: JSON.stringify({
					object: OBJECT_CALL,
					method: 0,
					args: {
						"call-id": `yj-${Math.random().toString(36).slice(2)}`,
						methodName: FQN_PREFIX + String(path),
						args: args || [],
					},
				}),
			}).then(async (res) => {
				const type = res.headers.get("Content-Type") || "";
				const json = type.includes("application/json");

				if (!res.ok) {
					const body = json ? await res.json() : { message: await res.text() };
					throw new Error(
						`__yjEvents.call(${path}) failed: ` +
							`${body.kind || "Error"}: ${body.message}`,
					);
				}

				return json ? res.json() : res.text();
			});

			return Promise.race([
				request,
				new Promise((_, reject) =>
					setTimeout(
						() =>
							reject(
								new Error(
									`__yjEvents.call(${path}) did not settle in ` +
										`${timeoutMs || 10000}ms — the runtime endpoint ` +
										`hung, which is not how a bad argument fails; ` +
										`check .dev/app.log`,
								),
							),
						timeoutMs || 10000,
					),
				),
			]);
		},
	};

	Object.defineProperty(window, "__yjEvents", {
		value: api,
		configurable: false,
		enumerable: false,
		writable: false,
	});

	// ── Inbound ──────────────────────────────────────────────────────
	//
	// The runtime keeps whatever `window._wails` already is, so creating
	// it here and defining an accessor on the one property we care about
	// means the wrap happens the moment the runtime module is evaluated.
	window._wails = window._wails || {};

	let dispatch;

	Object.defineProperty(window._wails, "dispatchWailsEvent", {
		configurable: true,
		enumerable: true,
		get: () => dispatch,
		set: (fn) => {
			dispatch = function (event) {
				try {
					record(event?.name, argsOf(event?.data), "in");
				} catch {
					/* a broken recorder must never break the app */
				}
				return fn.apply(this, arguments);
			};
		},
	});

	// ── Outbound ─────────────────────────────────────────────────────
	//
	// One POST per runtime call.  Only two of the thirteen object ids
	// are interesting here; the rest (window, dialogs, clipboard) pass
	// through untouched and unrecorded.
	window.fetch = function (input, init) {
		let call = null;

		try {
			// The runtime passes a **URL object**, not a string — it
			// builds `new URL(runtimeURL())` — and a URL has no `.url`,
			// only a Request does.  Reading the wrong one matched
			// nothing and recorded no calls at all, which looks
			// identical to an app that made none.
			const url =
				input && typeof input === "object" && "url" in input
					? input.url
					: String(input ?? "");

			if (
				url.includes("/wails/runtime") &&
				init?.method === "POST" &&
				typeof init.body === "string"
			) {
				const body = JSON.parse(init.body);

				if (body.object === OBJECT_EVENTS && body.method === 0) {
					record(body.args?.name, argsOf(body.args?.data), "out");
				} else if (body.object === OBJECT_CALL && body.method === 0) {
					call = {
						methodID: body.args?.methodID ?? null,
						methodName: body.args?.methodName ?? null,
						start: performance.now(),
					};
				}
			}
		} catch {
			/* ditto */
		}

		const response = nativeFetch(input, init);

		if (!call) {
			return response;
		}

		return response.then(async (res) => {
			try {
				call.ms = performance.now() - call.start;
				call.bytes = api.measureBytes
					? (await res.clone().text()).length
					: Number(res.headers.get("Content-Length") ?? -1);
				bindings.push(call);
				if (bindings.length > LIMIT) {
					bindings.splice(0, bindings.length - LIMIT);
				}
			} catch {
				/* ditto */
			}

			return res;
		});
	};
})();
