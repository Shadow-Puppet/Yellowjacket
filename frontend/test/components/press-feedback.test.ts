/**
 * What a tap looks like now that the web view's own highlight is gone
 * (#54).
 *
 * `index.css` sets `-webkit-tap-highlight-color: transparent` on
 * `html`, which — the property being inherited — reaches every shadow
 * root in the app. That takes away the grey box a phone drew over the
 * bounding rect of whatever was tapped, and with it the only touch
 * feedback the rows, the tab bar, the sidebar's destinations and the
 * shared menu items had. So the press states below are not decoration:
 * without them this change trades wrong feedback for none.
 *
 * **Asserted against the parsed stylesheet**, on `hover-affordance`'s
 * precedent and with the same limitation stated out loud: CDP's
 * `Emulation.setEmulatedMedia` does not reach this tier's iframe, so
 * there is no way here to render a component as a phone would, and
 * `:active` cannot be forced from a test either. What the browser will
 * answer is the shape it built from the `css` literal — which rule sits
 * inside which media query, and what the press selector actually is.
 *
 * Two regressions are worth catching that way, and both are silent on a
 * desktop:
 *
 * - someone hoisting a hover tint back out of its query as a tidy-up,
 *   which on a phone is a highlight that arrives because a finger
 *   touched the row and stays after it has gone;
 * - someone simplifying the press selector to a bare `:active`, which
 *   is one class short of `.selected` / `.active` and so does nothing
 *   on the row a phone is most likely to press — the one it has just
 *   selected.
 *
 * The pixels are the Android tier's, and the tap highlight itself is
 * `e2e/specs/native-touch-feel.spec.ts`, since only the real app loads
 * `index.css` at all.
 */
import { describe, expect, it } from 'vitest';

import '@components/track-list/track-list';
import '@components/queue-panel/queue-panel';
import '@components/playlist-details/playlist-details';
import '@components/smart-playlist-details/smart-playlist-details';
import '@components/bottom-nav/bottom-nav';
import '@components/sidebar/app-sidebar';
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

/** The four lists, their row selector, and the tag that draws them. */
const LISTS: Array<[string, string]> = [
  ['track-list', '.track-row'],
  ['queue-panel', '.track-item'],
  ['playlist-details', '.track-item'],
  ['smart-playlist-details', '.track-item'],
];

describe('a row says it is being pressed', () => {
  for (const [tag, row] of LISTS) {
    it(`${tag} draws a press state that survives its state classes`, async () => {
      const el = await fixture(tag, {});
      const rules = rulesOf(el);

      // Worth nothing if it read no rules at all — the first assertion
      // icon-language.test.ts makes, for the same reason.
      expect(rules.length).toBeGreaterThan(0);

      const press = rules.filter(
        (r) => r.text.includes(`${row}:active`) && r.text.includes('background-color'),
      );

      expect(press.length).toBeGreaterThan(0);

      for (const rule of press) {
        // A press is not a hover: it is the one thing a touch device
        // can say, so it must not sit behind a pointer query.
        expect(rule.condition).toBeNull();
        expect(rule.text).toContain('--yj-press-overlay');
      }

      // The load-bearing half: the selector carries a state class, or
      // it loses to `.selected` / `.selected.active` and the press is
      // invisible on a selected or playing row.
      expect(press.some((r) => r.text.includes(`${row}.selected:active`))).toBe(true);
    });

    it(`${tag} keeps its hover tint for devices that hover`, async () => {
      const el = await fixture(tag, {});
      const rules = rulesOf(el);

      expect(rules.length).toBeGreaterThan(0);

      const hover = rules.filter(
        (r) =>
          r.text.includes(`${row}:hover`) &&
          r.text.includes('--yj-hover-overlay'),
      );

      expect(hover.length).toBeGreaterThan(0);

      for (const rule of hover) {
        expect(rule.condition).toMatch(/hover:\s*hover/);
        expect(rule.condition).toMatch(/pointer:\s*fine/);
      }
    });
  }
});

describe('the two navigations say they are being pressed', () => {
  it('the phone tab bar, which had no state of its own at all', async () => {
    const el = await fixture('bottom-nav', {});
    const rules = rulesOf(el);

    expect(rules.length).toBeGreaterThan(0);

    const press = rules.filter((r) => r.text.startsWith('button:active'));

    expect(press.length).toBe(1);
    expect(press[0]!.condition).toBeNull();
    expect(press[0]!.text).toContain('--yj-press-overlay');
  });

  it("the sidebar, which is also the phone's More sheet", async () => {
    const el = await fixture('app-sidebar', {});
    const rules = rulesOf(el);

    expect(rules.length).toBeGreaterThan(0);

    const press = rules.filter((r) => r.text.startsWith('li button:active'));

    expect(press.length).toBe(1);
    expect(press[0]!.condition).toBeNull();
    expect(press[0]!.text).toContain('--yj-press-overlay');

    // Its hover tint is a destination looking picked, if it is left to
    // a synthesised hover inside the More sheet.
    const hover = rules.filter((r) => r.text.startsWith('li button:hover'));

    expect(hover.length).toBeGreaterThan(0);

    for (const rule of hover) {
      expect(rule.condition).toMatch(/hover:\s*hover/);
    }
  });
});

describe('the shared context menu', () => {
  // One stylesheet, fourteen menus — the same reason the sheet's row
  // height lives there rather than in each host.
  it('presses its items, in the one place every menu includes', async () => {
    const el = await fixture('queue-panel', {});
    const rules = rulesOf(el);

    const press = rules.filter((r) =>
      r.text.startsWith('.context-menu-panel wa-dropdown-item:active'),
    );

    expect(press.length).toBe(1);
    expect(press[0]!.condition).toBeNull();
    expect(press[0]!.text).toContain('--yj-press-overlay');

    const hover = rules.filter((r) =>
      r.text.startsWith('.context-menu-panel wa-dropdown-item:hover'),
    );

    expect(hover.length).toBeGreaterThan(0);

    for (const rule of hover) {
      expect(rule.condition).toMatch(/hover:\s*hover/);
      expect(rule.condition).toMatch(/pointer:\s*fine/);
    }
  });
});
