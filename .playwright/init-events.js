/*
 * YellowJacket harness bridge — installed as a Playwright initScript, so
 * it runs in every page *before* any application script.
 *
 * Why this file exists: half of what this app does is push-driven.  Scan
 * progress, job updates, download progress, WantedListChanged and 40-odd
 * other events arrive from Go whenever they arrive.  An assertion that
 * sleeps and hopes is flaky; an assertion that awaits the event is not.
 *
 * Three things it provides on `window.__yjEvents`:
 *
 *   record   every backend -> frontend event, in order, with payloads
 *   wait     a promise that settles on a matching event (or rejects
 *            with the list of events that *did* arrive, which is the
 *            single most useful failure message this harness can give)
 *   call     a bound Go method that is guaranteed to settle: a binding
 *            invoked with wrong argument types makes the backend log
 *            "error parsing arguments" and never fire the callback, so
 *            the in-page promise hangs forever.  Timing out here fixes
 *            that once instead of in every eval.
 *
 * WHERE IT HOOKS.  Not EventsOn.  Every backend event enters the page
 * at exactly one place — wails' ipc_websocket.js does
 *
 *     case "n": window.wails.EventsNotify(message)
 *
 * and EventsNotify fans out to listeners from there.  Wrapping that
 * single choke point captures all 46 events whether or not the app
 * subscribes to them, and needs one wrap rather than 46.
 *
 * `window.wails` does not exist yet when this script runs, so we install
 * an accessor on `window` and wrap at assignment time (wails' main.js
 * does a plain `window.wails = {...}`), then collapse the accessor back
 * to a data property so nothing downstream can tell.
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

	let seq = 0;
	const log = [];
	const waiters = new Set();

	const summarize = () => {
		const counts = {};
		for (const e of log) {
			counts[e.name] = (counts[e.name] || 0) + 1;
		}
		return counts;
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
		version: 1,

		/** Every recorded event, oldest first. */
		get log() {
			return log.slice();
		},

		/** The sequence number of the most recent event. */
		get seq() {
			return seq;
		},

		/** Drop the buffer.  Does NOT touch the recorder or waiters. */
		reset() {
			const n = log.length;
			log.length = 0;
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
				if (window.go?.queue?.Queue?.GetState) {
					try {
						await api.call("queue.Queue.GetState", [], 2000);
						return true;
					} catch {
						/* backend not up yet */
					}
				}
				if (Date.now() > deadline) {
					throw new Error("__yjEvents.ready timed out");
				}
				await new Promise((r) => setTimeout(r, 100));
			}
		},

		/**
		 * Call a bound Go method by dotted path, with a timeout.
		 *
		 *   await __yjEvents.call('player.Player.SetVolume', [42])
		 *
		 * A binding called with the wrong argument types never fires its
		 * callback — the reason appears only in .dev/app.log.  Without a
		 * timeout the caller waits forever; with one it gets told where
		 * to look.
		 */
		call(path, args, timeoutMs) {
			const parts = String(path).split(".");
			let fn = window.go;
			for (const p of parts) {
				fn = fn?.[p];
			}
			if (typeof fn !== "function") {
				return Promise.reject(
					new Error(`__yjEvents.call: no such binding: ${path}`),
				);
			}

			return Promise.race([
				Promise.resolve(fn(...(args || []))),
				new Promise((_, reject) =>
					setTimeout(
						() =>
							reject(
								new Error(
									`__yjEvents.call(${path}) did not settle in ` +
										`${timeoutMs || 10000}ms — almost always wrong ` +
										`argument types; check .dev/app.log for ` +
										`"error parsing arguments"`,
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

	// Wrap `obj[method]` once, routing every invocation through `tap`.
	const wrap = (obj, method, tap) => {
		const original = obj[method];
		if (typeof original !== "function" || original.__yjWrapped) {
			return;
		}
		const wrapped = function (...args) {
			try {
				tap(args);
			} catch {
				/* a broken recorder must never break the app */
			}
			return original.apply(this, args);
		};
		wrapped.__yjWrapped = true;
		obj[method] = wrapped;
	};

	// Install an accessor that wraps on first assignment, then collapses
	// back into an ordinary property.
	const hookOnAssign = (name, onAssign) => {
		let value;
		Object.defineProperty(window, name, {
			configurable: true,
			enumerable: true,
			get: () => value,
			set: (v) => {
				value = v;
				try {
					onAssign(v);
				} catch {
					/* ditto */
				}
				Object.defineProperty(window, name, {
					value: v,
					configurable: true,
					enumerable: true,
					writable: true,
				});
			},
		});
	};

	// Inbound: every backend -> frontend event.
	hookOnAssign("wails", (w) => {
		wrap(w, "EventsNotify", ([message]) => {
			const parsed = JSON.parse(message);
			record(parsed.name, parsed.data, "in");
		});
	});

	// Outbound: events the frontend emits, so a flow that round-trips
	// through Go is legible from one buffer.
	hookOnAssign("runtime", (r) => {
		wrap(r, "EventsEmit", (args) => {
			record(args[0], args.slice(1), "out");
		});
	});
})();
