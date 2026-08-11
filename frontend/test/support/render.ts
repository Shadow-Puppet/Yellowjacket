/**
 * Mounting helpers for component tests.
 *
 * Components are mounted into a real document and queried through their
 * real (open) shadow roots — nothing here approximates the DOM, which
 * is the whole reason this tier runs in a browser.
 */
import type { LitElement } from 'lit';
import { expect } from 'vitest';

const mounted: HTMLElement[] = [];

/**
 * Create an element, apply properties, mount it and wait for Lit's
 * first render. Properties are set as *properties*, not attributes, so
 * non-string values survive.
 */
export async function fixture<T extends LitElement>(
  tag: string,
  props: Record<string, unknown> = {},
): Promise<T> {
  const el = document.createElement(tag) as T;

  Object.assign(el, props);
  document.body.append(el);
  mounted.push(el);

  await el.updateComplete;

  return el;
}

/** Apply properties to a mounted element and wait for the re-render. */
export async function update<T extends LitElement>(
  el: T,
  props: Record<string, unknown>,
): Promise<T> {
  Object.assign(el, props);
  el.requestUpdate();
  await el.updateComplete;

  return el;
}

/** Remove everything mounted by this module. Called from setup. */
export function cleanupFixtures(): void {
  while (mounted.length > 0) mounted.pop()?.remove();
}

// ===================================================================
// SHADOW DOM QUERIES
// ===================================================================

/** Query one element inside a component's shadow root. */
export function shadow<E extends Element = Element>(
  host: Element,
  selector: string,
): E | null {
  return host.shadowRoot?.querySelector<E>(selector) ?? null;
}

/** Query all matching elements inside a component's shadow root. */
export function shadowAll<E extends Element = Element>(
  host: Element,
  selector: string,
): E[] {
  return [...(host.shadowRoot?.querySelectorAll<E>(selector) ?? [])];
}

/** Trimmed text content of the first match, or null if absent. */
export function text(host: Element, selector: string): string | null {
  return shadow(host, selector)?.textContent?.trim() ?? null;
}

/** Trimmed text content of every match. */
export function texts(host: Element, selector: string): string[] {
  return shadowAll(host, selector).map((el) => el.textContent?.trim() ?? '');
}

/** The accessible names of every match, for assertions that mirror
 *  what a screen reader — and a Playwright selector — would see. */
export function labels(host: Element, selector: string): string[] {
  return shadowAll(host, selector).map(
    (el) => el.getAttribute('aria-label') ?? '',
  );
}

/** Click something inside a shadow root and let the update settle. */
export async function click(
  host: LitElement,
  selector: string,
): Promise<void> {
  const target = shadow<HTMLElement>(host, selector);

  if (!target) throw new Error(`no element matching ${selector}`);

  target.click();
  await host.updateComplete;
}

// ===================================================================
// VISUAL REGRESSION
// ===================================================================

/**
 * Visual regression is opt-in: `toMatchScreenshot` baselines depend on
 * font hinting and compositing, so a baseline taken on one machine
 * fails on another for reasons that have nothing to do with the
 * component. `make ui-visual` sets YJ_VISUAL=1; the default run
 * asserts behaviour only.
 */
export const visualEnabled = import.meta.env['YJ_VISUAL'] === '1';

/**
 * Screenshot a component against its baseline, when visual regression
 * is enabled. A no-op otherwise — deliberately not a skipped test, so
 * the behavioural assertions around it still run.
 */
export async function visual(el: Element, name: string): Promise<void> {
  if (!visualEnabled) return;

  await expect(el).toMatchScreenshot(name);
}
