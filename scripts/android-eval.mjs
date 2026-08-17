/**
 * Evaluate an expression inside the app's WebView on a real device.
 *
 * The device tier could only ever *look* at the app (a screenshot) or
 * read what Go chose to log. This is the third thing: the page's own
 * answer, from the engine that is actually rendering it — which is how
 * "the icons are missing" stops being a guess about assets and becomes a
 * computed style.
 *
 * Two facts make it work at all. A `debuggable` build calls
 * `WebView.setWebContentsDebuggingEnabled(true)`, which opens an abstract
 * unix socket per process (`webview_devtools_remote_<pid>`); `make
 * android-inspect` forwards it to localhost. And **Playwright cannot use
 * it** — `connectOverCDP` immediately calls `Browser.setDownloadBehavior`,
 * which a WebView answers with "Browser context management is not
 * supported", so the connection dies before the first evaluate. Raw CDP
 * over Node's built-in WebSocket is a dozen lines and has no such
 * opinion.
 *
 * Usage: node scripts/android-eval.mjs '<js expression>'
 *        make android-eval EXPR='...'
 *
 * The expression is evaluated with `awaitPromise`, so an async probe is
 * fine. Return a string (`JSON.stringify(...)`) for anything structured:
 * `returnByValue` will not serialise a DOM node.
 */
const PORT = process.env.YJ_ANDROID_CDP_PORT ?? '9222';
const expression = process.argv[2];

if (!expression) {
    console.error("usage: node scripts/android-eval.mjs '<js expression>'");
    process.exit(2);
}

const endpoint = `http://localhost:${PORT}/json`;
let targets;

try {
    targets = await (await fetch(endpoint)).json();
} catch (err) {
    console.error(
        `android-eval: nothing on :${PORT} (${err.message})\n` +
            "  run 'make android-inspect' first, and check the phone is " +
            'awake -- wireless adb drops when the screen sleeps',
    );
    process.exit(1);
}

const page = targets.find((t) => t.type === 'page');

if (!page) {
    console.error('android-eval: no page target; is the app in the foreground?');
    process.exit(1);
}

const ws = new WebSocket(page.webSocketDebuggerUrl);

await new Promise((resolve, reject) => {
    ws.onopen = resolve;
    ws.onerror = () => reject(new Error('websocket refused'));
});

const answer = await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('evaluate timed out')), 20_000);

    ws.onmessage = (m) => {
        const msg = JSON.parse(m.data);

        if (msg.id !== 1) return;

        clearTimeout(timer);
        resolve(msg.result);
    };

    ws.send(
        JSON.stringify({
            id: 1,
            method: 'Runtime.evaluate',
            params: { expression, awaitPromise: true, returnByValue: true },
        }),
    );
});

ws.close();

if (answer.exceptionDetails) {
    console.error(
        'android-eval: threw:',
        answer.exceptionDetails.exception?.description ??
            answer.exceptionDetails.text,
    );
    process.exit(1);
}

const value = answer.result?.value;

console.log(typeof value === 'string' ? value : JSON.stringify(value, null, 2));
