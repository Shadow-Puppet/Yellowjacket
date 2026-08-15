import { readFileSync, readdirSync, statSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join, resolve } from 'node:path';

/**
 * Turns the method ids a binding call carries back into the names a
 * spec (or a measurement) wants to read.
 *
 * Plain JavaScript, not TypeScript, because `e2e/perf/measure.mjs` runs
 * under bare `node` and both it and the specs need exactly this map —
 * and one derivation with a `.mjs` extension is better than two that
 * can disagree.
 *
 * v2 put every bound method on `window.go`, so a harness could walk
 * that object to wrap or enumerate them.  v3 has no such surface: the
 * generated bindings are ordinary bundled modules calling
 * `$Call.ByID(<fnv hash of the fully-qualified name>)`, and what
 * reaches the wire — and therefore what `.playwright/init-events.js`
 * can record — is the number.
 *
 * Plan 009 phase 6b named two ways to get the list back.  This is the
 * preferred one: derive it from `frontend/bindings/`, which is a real
 * generated tree and is already gated by `make bindings-check`.  The
 * alternative — a hand-maintained list — goes stale silently, which is
 * the failure mode that check exists to prevent.
 *
 * Nothing here hashes anything.  The generated source carries the id as
 * a literal beside the function that sends it, so this reads the two
 * together rather than recomputing one from the other and hoping the
 * hash still matches.
 */

const here = dirname(fileURLToPath(import.meta.url));

const BINDINGS_ROOT = resolve(here, '../../frontend/bindings/yellowjacket');

/** `export function Name(…) { return $Call.ByID(123, …) }` */
const BOUND_METHOD = /export function (\w+)\([\s\S]*?\$Call\.ByID\((\d+)/g;

/** `import * as Library from "./library.js";` — the only place the Go
 *  type's casing survives; the file is `library.ts`, and
 *  `frontendutil.ts` cannot tell you it is `FrontendUtil`. */
const SERVICE_EXPORT = /import \* as (\w+) from "\.\/([\w.]+)\.js"/g;

function* walk(dir) {
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);

    if (statSync(path).isDirectory()) yield* walk(path);
    else if (entry.endsWith('.ts')) yield path;
  }
}

let cached;

/**
 * methodIDs maps a binding's method id to `pkg.Type.Method` — the same
 * short path `callBinding` takes, so a spec never has to know that the
 * backend calls it `yellowjacket/backend/queue.Queue.GetState`.
 */
export function methodIDs() {
  if (cached) return cached;

  const byID = new Map();

  for (const file of walk(BINDINGS_ROOT)) {
    if (!file.endsWith('index.ts')) continue;

    const dir = dirname(file);
    const pkg = dir.split('/').pop() ?? '';
    const index = readFileSync(file, 'utf8');

    for (const [, typeName, base] of index.matchAll(SERVICE_EXPORT)) {
      let source;

      try {
        source = readFileSync(join(dir, `${base}.ts`), 'utf8');
      } catch {
        continue; // a models-only re-export, which binds nothing
      }

      for (const [, method, id] of source.matchAll(BOUND_METHOD)) {
        byID.set(Number(id), `${pkg}.${typeName}.${method}`);
      }
    }
  }

  if (byID.size === 0) {
    throw new Error(
      `method-ids: no bound methods under ${BINDINGS_ROOT}; ` +
        `run 'make bindings'`,
    );
  }

  cached = byID;

  return byID;
}

/** Names one recorded binding call, or `#<id>` if the map has no entry
 *  — which fails the assertion naming it rather than passing quietly. */
export function nameOf(call) {
  if (call.methodName) {
    return call.methodName.replace(/^yellowjacket\/backend\//, '');
  }

  return call.methodID === null
    ? '#unknown'
    : (methodIDs().get(call.methodID) ?? `#${call.methodID}`);
}
