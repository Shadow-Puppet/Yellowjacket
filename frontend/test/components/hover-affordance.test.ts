/**
 * A hover affordance is gated on the device having hover.
 *
 * The home page's cover cards reveal a play button on :hover. A touch
 * long-press synthesises a hover state in the WebView, so on a phone
 * that button flashed into view during the 500ms hold that
 * utils/long-press.ts is measuring for a context menu — a control
 * appearing because the user was reaching for a different one.
 *
 * This is asserted against the *parsed stylesheet* rather than by
 * emulating a touch device, and that is a limitation worth stating
 * rather than hiding. CDP's Emulation.setEmulatedMedia does not reach
 * this tier's iframe — matchMedia still answers `hover: hover` after it
 * is set — so there is no way here to render the component as a phone
 * would and read the computed style. What can be checked is the shape
 * the browser actually built from the css`` literal: that the reveal
 * lives inside a hover media query and that the default is display:none.
 *
 * Which is the regression worth catching anyway. The failure mode is
 * someone hoisting the rule back out of the query for a one-line tidy —
 * a change nothing renders differently on a desktop, so every other
 * assertion in this repo passes and the phone silently regresses.
 */
import { describe, expect, it } from 'vitest';

import '@components/home-view/home-view';
import { fixture } from '@test/support/render';

/** Every rule in the element's own adopted stylesheets, flattened. */
function rulesOf(host: Element): { text: string; condition: string | null }[] {
  const sheets = host.shadowRoot?.adoptedStyleSheets ?? [];
  const out: { text: string; condition: string | null }[] = [];

  for (const sheet of sheets) {
    for (const rule of Array.from(sheet.cssRules)) {
      if (rule instanceof CSSMediaRule) {
        for (const inner of Array.from(rule.cssRules)) {
          out.push({ text: inner.cssText, condition: rule.conditionText });
        }

        continue;
      }

      out.push({ text: rule.cssText, condition: null });
    }
  }

  return out;
}

describe('the home card play button', () => {
  it('reveals itself only where the device has hover', async () => {
    const el = await fixture('home-view', {});
    const rules = rulesOf(el);

    // The sweep is worth nothing if it read no rules at all — the same
    // first assertion icon-language.test.ts makes for the same reason.
    expect(rules.length).toBeGreaterThan(0);

    const reveals = rules.filter(
      (r) => r.text.includes('.play') && /opacity:\s*1/.test(r.text),
    );

    expect(reveals.length).toBeGreaterThan(0);

    for (const rule of reveals) {
      expect(rule.condition).toMatch(/hover:\s*hover/);
      expect(rule.condition).toMatch(/pointer:\s*fine/);
    }
  });

  it('is display:none rather than transparent where it is absent', async () => {
    const el = await fixture('home-view', {});

    // opacity:0 alone would leave a button that still takes taps and is
    // still in the accessibility tree, so a phone would keep the hit
    // area for a control it can never see.
    const unconditional = rulesOf(el).filter(
      (r) => r.condition === null && r.text.startsWith('.play'),
    );

    expect(unconditional.length).toBeGreaterThan(0);
    expect(unconditional.some((r) => /display:\s*none/.test(r.text))).toBe(true);
  });
});
