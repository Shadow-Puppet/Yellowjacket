/**
 * A hover affordance is gated on the device having hover — in whichever
 * direction keeps the action reachable.
 *
 * The home page's cover cards reveal a play button on :hover. A touch
 * long-press synthesises a hover state in the WebView, so on a phone
 * that button flashed into view during the 500ms hold that
 * utils/long-press.ts is measuring for a context menu — a control
 * appearing because the user was reaching for a different one.
 *
 * #137 is the same sweep with the opposite answer for two of its three
 * cases. Where the revealed control is the *only* route to its action,
 * hiding it removes the action, so it is always visible where there is
 * no hover: `track-details`'s cover-art overlay and remove, and
 * `shortcut-capture`'s reset. The queue's per-row remove is the third,
 * and is the redundant kind — since #60 the row's context menu is a
 * bottom sheet carrying "Remove from Queue" — so it takes #68's
 * treatment here.
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
import '@components/queue-panel/queue-panel';
import '@components/track-details/track-details';
import '@components/config-page/shortcut-capture';
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

describe("the queue row's remove button", () => {
  it('is absent where the device has no hover, the menu carrying the action', async () => {
    const el = await fixture('queue-panel', {});
    const rules = rulesOf(el);

    expect(rules.length).toBeGreaterThan(0);

    // visibility:hidden alone would leave an invisible button holding
    // its hit area on a phone, which is the trap #68's commit names.
    const unconditional = rules.filter(
      (r) => r.condition === null && r.text.startsWith('.remove-button'),
    );

    expect(unconditional.length).toBeGreaterThan(0);
    expect(unconditional.some((r) => /display:\s*none/.test(r.text))).toBe(true);

    const reveals = rules.filter(
      (r) =>
        r.text.includes('.remove-button') && /visibility:\s*visible/.test(r.text),
    );

    expect(reveals.length).toBeGreaterThan(0);

    for (const rule of reveals) {
      expect(rule.condition).toMatch(/hover:\s*hover/);
      expect(rule.condition).toMatch(/pointer:\s*fine/);
    }
  });
});

/**
 * The two affordances that are the only route to their action.
 *
 * Asserted as "there is a rule showing it, and its condition is a
 * *negated* hover query" — the same stylesheet reading as above, for
 * the same reason: this tier's iframe cannot be emulated as a touch
 * device, and the regression worth catching is someone folding the rule
 * away as redundant on the desktop it does nothing on.
 */
describe('an affordance with no other route', () => {
  const cases: Array<[string, string, string[]]> = [
    ['track-details', 'track-details', ['.cover-art-overlay', '.cover-art-remove']],
    ['shortcut-capture', 'shortcut-capture', ['.reset-btn']],
  ];

  for (const [name, tag, selectors] of cases) {
    it(`${name} shows it where the device has no hover`, async () => {
      const el = await fixture(tag, {});
      const rules = rulesOf(el);

      expect(rules.length).toBeGreaterThan(0);

      for (const selector of selectors) {
        const shown = rules.filter(
          (r) =>
            r.condition !== null &&
            r.text.includes(selector) &&
            /opacity:\s*1/.test(r.text),
        );

        const touch = shown.filter((r) => /not[\s\S]*hover:\s*hover/.test(r.condition!));

        expect(touch.length).toBeGreaterThan(0);
      }
    });
  }

  // The one half this tier can measure rather than read: the query is
  // negated, so on the hover-capable browser running these tests the
  // control must still be revealed by hover and by nothing else. A rule
  // written without the `not` would show it here, permanently, on every
  // desktop.
  it('leaves the desktop reveal alone, where the device does have hover', async () => {
    expect(matchMedia('(hover: hover)').matches).toBe(true);

    const el = await fixture('shortcut-capture', {
      action: 'player.next',
      label: 'Next Track',
      currentKey: 'X',
      defaultKey: 'N',
    });
    const btn = el.shadowRoot?.querySelector('.reset-btn');

    expect(btn).not.toBeNull();
    expect(getComputedStyle(btn!).opacity).toBe('0');
  });
});
