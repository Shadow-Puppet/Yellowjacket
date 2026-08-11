import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

import { test as base, expect, type Page } from '@playwright/test';

const here = dirname(fileURLToPath(import.meta.url));

/** The same bridge `playwright-cli` loads, so an exploratory session and
 *  a committed spec see an identical page. */
const INIT_SCRIPT = resolve(here, '../../.playwright/init-events.js');

/**
 * The 90-second fixture track (`cmd/gentestdata`, case `edge-lengths`).
 *
 * Every other fixture is 2–6 seconds, which is shorter than the time a
 * spec takes to click something — a "pause it" test against one of
 * those races the track finishing and fails on a UI that is correct.
 */
export const LONG_TRACK = 'Long Player';

/** Shape of the recorder installed by .playwright/init-events.js. */
export type YjEvent = {
  seq: number;
  name: string;
  data: unknown[];
  dir: 'in' | 'out';
  t: number;
};

/**
 * Await a backend event instead of a timeout.
 *
 * Half of this app is push-driven, and the events that matter
 * (scan progress, job updates, playback state) arrive whenever the
 * backend gets to them.  `waitForEvent` resolves against events already
 * buffered as well as future ones, so there is no race between doing
 * the thing and starting to listen.
 */
export async function waitForEvent(
  page: Page,
  name: string,
  opts: { timeoutMs?: number; since?: number } = {},
): Promise<YjEvent> {
  return page.evaluate(
    ([n, o]) => window.__yjEvents.wait(n as string, o as object),
    [name, { timeoutMs: 10_000, ...opts }] as const,
  ) as Promise<YjEvent>;
}

/** Drop the event buffer.  Never re-register a recorder: listeners
 *  survive across evaluate calls and a second recorder double-counts. */
export async function resetEvents(page: Page): Promise<void> {
  await page.evaluate(() => void window.__yjEvents.reset());
}

/** name -> count, for asserting on (or debugging) what actually fired. */
export async function eventNames(
  page: Page,
): Promise<Record<string, number>> {
  return page.evaluate(() => window.__yjEvents.names());
}

/**
 * Call a bound Go method with a timeout.
 *
 * Wrong argument types make the backend log "error parsing arguments"
 * and never fire the callback, so an unguarded call hangs until the
 * whole spec times out with no clue why.  This fails in seconds and
 * says where to look.
 */
export async function callBinding<T = unknown>(
  page: Page,
  path: string,
  args: unknown[] = [],
  timeoutMs = 10_000,
): Promise<T> {
  return page.evaluate(
    ([p, a, t]) =>
      window.__yjEvents.call(p as string, a as unknown[], t as number),
    [path, args, timeoutMs] as const,
  ) as Promise<T>;
}

/** Thin client for the dev-only /__test/ surface (backend/testctl). */
export class TestCtl {
  constructor(private readonly baseURL: string) {}

  private async req(path: string, init?: RequestInit) {
    const res = await fetch(`${this.baseURL}${path}`, {
      signal: AbortSignal.timeout(120_000),
      ...init,
    });
    const body = await res.json();

    if (!res.ok) {
      throw new Error(`testctl ${path}: ${body.error ?? res.status}`);
    }

    return body;
  }

  health() {
    return this.req('/__test/health');
  }

  snapshot(name: string) {
    return this.req(`/__test/db/snapshot?name=${name}`, { method: 'POST' });
  }

  restore(name: string) {
    return this.req(`/__test/db/restore?name=${name}`, { method: 'POST' });
  }

  emit(name: string, ...data: unknown[]) {
    return this.req('/__test/emit', {
      method: 'POST',
      body: JSON.stringify({ name, data }),
    });
  }

  sql(sql: string, args: unknown[] = []) {
    return this.req('/__test/sql', {
      method: 'POST',
      body: JSON.stringify({ sql, args }),
    });
  }
}

export const test = base.extend<{ app: Page; testctl: TestCtl }>({
  /** A page with the event bridge installed and the app loaded and
   *  actually talking to the backend — not merely DOM-ready, which is
   *  earlier and lies. */
  app: async ({ page, baseURL }, use) => {
    await page.addInitScript({ path: INIT_SCRIPT });
    await page.goto(baseURL!);
    await page.evaluate(() => window.__yjEvents.ready(20_000));
    await use(page);
  },

  testctl: async ({ baseURL }, use) => {
    await use(new TestCtl(baseURL!));
  },
});

export { expect };

declare global {
  interface Window {
    __yjEvents: {
      version: number;
      seq: number;
      log: YjEvent[];
      reset(): number;
      all(name?: string): YjEvent[];
      count(name?: string): number;
      last(name?: string): YjEvent | null;
      since(seq: number): YjEvent[];
      names(): Record<string, number>;
      wait(
        name: string,
        opts?: {
          timeoutMs?: number;
          since?: number;
          match?: (data: unknown[], entry: YjEvent) => boolean;
        },
      ): Promise<YjEvent>;
      ready(timeoutMs?: number): Promise<boolean>;
      call(path: string, args?: unknown[], timeoutMs?: number): Promise<any>;
    };
    go: Record<string, Record<string, Record<string, (...a: any[]) => any>>>;
  }
}
