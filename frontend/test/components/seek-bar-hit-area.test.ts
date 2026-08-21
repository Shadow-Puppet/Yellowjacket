/**
 * The seek bar's painted track and the thing you can hit are allowed to
 * differ, and a slider is the clearest case where they should.
 *
 * On `now-playing-view` — the screen that exists so a phone has
 * somewhere to seek from — the slider measured 261x6 on the reference
 * device (#187). Six pixels is the whole of the drag target on the
 * app's primary seeking affordance, against a 44px floor the app set
 * for itself in #56 and holds to in the queue panel.
 *
 * Two separate faults, and the first is why the second was not obvious.
 *
 * **The phone rule had never applied.** `seek-bar`'s stylesheet asked
 * for a 12px track below 599px and then set 6px in a plain `wa-slider`
 * rule *written after it*. A media query adds no specificity, so the
 * plain rule won at every width — which is `index.css`'s documented
 * rule ("the phone section is last on purpose") reproduced inside a
 * component's own stylesheet. The source said 12 and the device said 6.
 *
 * **And 12px would still be under the floor**, so the target is built
 * around the track rather than by thickening it: padding on the part
 * that carries the gesture, with margins cancelling it so the row does
 * not grow.
 *
 * This is asserted against the *parsed stylesheet*, on
 * `hover-affordance.test.ts`'s precedent and with the same limitation
 * stated rather than hidden: no tier here renders at a phone width with
 * a real `wa-slider` laid out, so what can be checked is the shape the
 * browser built from the css`` literal. The pixel measurements that
 * chose these numbers were taken on the device and are recorded on
 * #187 and in the stylesheet's own comment — a number measured on a
 * phone is not a number CI can assert.
 *
 * Which is the regression worth catching anyway. Both failures are
 * invisible on a desktop: hoisting the block back above the plain rule
 * renders identically at every width CI runs at, and it is exactly what
 * a tidy-up does.
 */
import { describe, expect, it } from 'vitest';

import '@components/audio-player/seekbar/seek-bar';
import { fixture } from '@test/support/render';

/** The app's touch floor, from #56. */
const TOUCH_FLOOR = 44;

/** The width below which the phone's rules apply. */
const PHONE_QUERY = /max-width:\s*599px/;

type Rule = { text: string; condition: string | null };

/**
 * Every rule in the element's own adopted stylesheets, flattened **in
 * order**, which is the whole point here: the fault being guarded is a
 * rule sitting in the wrong place, not a rule being absent.
 */
function rulesOf(host: Element): Rule[] {
  const sheets = host.shadowRoot?.adoptedStyleSheets ?? [];
  const out: Rule[] = [];

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

/**
 * The two px numbers of a `*-block` declaration, as [start, end].
 *
 * A symmetric pair is **serialised back as one value** — `padding-block:
 * 16px 16px` reads as `padding-block: 16px` — so a naive pair-reader
 * fails on the shorthand rather than on the thing it is checking, and
 * says the wrong thing about why. That is not hypothetical: it is what
 * the symmetric-padding reversion did while this test was being
 * proved.
 */
function blockPair(text: string, property: string): [number, number] | null {
  const declaration = new RegExp(`${property}:\\s*([^;]+)`).exec(text)?.[1];

  if (declaration === undefined) {
    return null;
  }

  const values = [...declaration.matchAll(/(-?[\d.]+)px/g)].map((m) =>
    Number(m[1]),
  );

  const [start, end] = values;

  if (start === undefined) {
    return null;
  }

  return [start, end ?? start];
}

describe("the seek bar's phone rules", () => {
  it('are last, so they are not silently overridden', async () => {
    const el = await fixture('seek-bar', {});
    const rules = rulesOf(el);

    // A sweep that read nothing passes vacuously — the same first
    // assertion icon-language.test.ts makes, for the same reason.
    expect(rules.length).toBeGreaterThan(0);

    const declaresTrackSize = (r: Rule) => /--track-size:/.test(r.text);

    const lastUnconditional = rules.findLastIndex(
      (r) => r.condition === null && declaresTrackSize(r),
    );
    const phoneOverride = rules.findLastIndex(
      (r) => r.condition !== null && PHONE_QUERY.test(r.condition)
        && declaresTrackSize(r),
    );

    expect(lastUnconditional).toBeGreaterThanOrEqual(0);
    expect(phoneOverride).toBeGreaterThanOrEqual(0);

    // A media query adds no specificity. Written first, it loses.
    expect(phoneOverride).toBeGreaterThan(lastUnconditional);
  });

  it('give the slider a pointer target of at least the touch floor', async () => {
    const el = await fixture('seek-bar', {});
    const rules = rulesOf(el);

    const track = rules.find(
      (r) => r.condition !== null && PHONE_QUERY.test(r.condition)
        && /--track-size:/.test(r.text),
    );
    const target = rules.find(
      (r) => r.condition !== null && PHONE_QUERY.test(r.condition)
        && r.text.includes('::part(slider)'),
    );

    expect(track).toBeDefined();
    expect(target).toBeDefined();

    const trackSize = Number(
      /--track-size:\s*(-?[\d.]+)px/.exec(track!.text)?.[1],
    );
    const padding = blockPair(target!.text, 'padding-block');

    expect(padding).not.toBeNull();

    // The padding is on ::part(slider) rather than on the host because
    // that inner div is what carries the gesture: it has the listener
    // and the touch-action, and it is exactly the host's size, so
    // padding the host grows a box that does not take the press.
    const hitArea = trackSize + padding![0] + padding![1];

    expect(hitArea).toBeGreaterThanOrEqual(TOUCH_FLOOR);
  });

  it('do not grow the row they sit in', async () => {
    const el = await fixture('seek-bar', {});

    const target = rulesOf(el).find(
      (r) => r.condition !== null && PHONE_QUERY.test(r.condition)
        && r.text.includes('::part(slider)'),
    );

    expect(target).toBeDefined();

    const padding = blockPair(target!.text, 'padding-block');
    const margin = blockPair(target!.text, 'margin-block');

    expect(padding).not.toBeNull();
    expect(margin).not.toBeNull();

    // now-playing-view's vertical budget is fixed and #51 measured
    // every pixel of it: letting the row grow by the difference cost
    // the album art 25px of 143 when it was tried on the device.
    expect(margin![0]).toBe(-padding![0]);
    expect(margin![1]).toBe(-padding![1]);
  });

  it('take the space above, because what is below is the transport', async () => {
    const el = await fixture('seek-bar', {});

    const target = rulesOf(el).find(
      (r) => r.condition !== null && PHONE_QUERY.test(r.condition)
        && r.text.includes('::part(slider)'),
    );

    expect(target).toBeDefined();

    const pair = blockPair(target!.text, 'padding-block');

    expect(pair).not.toBeNull();

    const [above, below] = pair!;

    // Measured at 424x439: the seek row is 19px and the play button's
    // top edge is 8px below it, while `.art` above is a non-interactive
    // div. A symmetric target would reach into the play button — the
    // most important control on the screen — so the growth is upward.
    expect(above).toBeGreaterThan(below);
  });
});
