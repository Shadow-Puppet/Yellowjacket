/**
 * Every control in Settings is at least 44px (#186, second pass).
 *
 * The header pass covered the five controls a user meets on every
 * screen. Settings is the other half and is much the larger one: swept
 * on the reference device (TLP301, 424x439) with all eleven
 * `config-section`s expanded, **120 controls** were under the floor,
 * not the 93 the issue's first table implies, and `config-field` — the
 * row shape the issue names — is eight of them. The bulk is behind the
 * disclosures:
 *
 * | control | size | count |
 * |---|---|---|
 * | `.column-arrow-btn` | **16x14** | 36 |
 * | `.column-toggle` | 16x16 | 29 |
 * | `shortcut-capture` button | 80x**25** | 26 |
 * | download format checkbox | 16x16 | 8 |
 * | `config-field` select | 335x**30** | 7 |
 * | `wa-input` / `wa-button` | 204x**20**, 185x**21** | 6 |
 * | `library-filter` select | 120x**32** | 1 |
 * | `.overflow-btn` | 31x31 | 1 |
 *
 * **This tier can measure it, unlike #187's seek bar**, for the reason
 * the header pass gives: the controls are real elements and the rules
 * are min-sizes, so a real Chromium rendering a real component gives
 * the actual answer at any width. And unlike the header there is no
 * overflow fit on this page, so nothing here needs the negative-margin
 * treatment — height is free and the two square controls can simply be
 * square.
 *
 * **What the sweep cannot see is written down here as a test rather
 * than as a comment**, because it is the trap this whole issue keeps
 * setting. Two controls are invisible to a walk of `button, select,
 * input`: `config-field`'s toggle, whose `<input>` is
 * `opacity: 0; width: 0; height: 0` so the thing a finger hits is the
 * `<label>` around it (34x19, smaller than anything in either of the
 * issue's tables), and `shortcut-capture`'s reset button, which renders
 * only for a shortcut somebody has already rebound. Both are asserted
 * by name below.
 */
import { beforeEach, describe, expect, it } from 'vitest';

import '@components/config-page/config-field';
import '@components/config-page/config-page';
import '@components/config-page/download-clients';
import '@components/config-page/shortcut-capture';
import '@components/library-filter/library-filter';

import { flush, stub } from '@test/support/harness';
import { fixture, shadow, shadowAll } from '@test/support/render';

/** The app's touch floor, from #56. */
const FLOOR = 44;

/**
 * Everything a finger can hit, through every shadow root under `root`.
 *
 * It descends rather than querying one root because Settings is a tree
 * of components — `config-page` renders `config-section`s holding
 * `config-field`s and `shortcut-capture`s — and the defect was
 * distributed across all of them. This is the device sweep, run here.
 */
function controlsUnder(root: Document | ShadowRoot | Element): { name: string; el: HTMLElement }[] {
  const SELECTOR = 'button, select, input, [role="button"], [role="tab"], [role="switch"]';
  const found: { name: string; el: HTMLElement }[] = [];
  const seen = new Set<Element>();

  const walk = (node: ParentNode, depth: number): void => {
    if (depth > 20) return;

    for (const el of Array.from(node.querySelectorAll('*'))) {
      if (el.matches(SELECTOR) && !seen.has(el)) {
        seen.add(el);

        const box = el.getBoundingClientRect();
        const style = getComputedStyle(el);
        const rendered =
          (box.width > 0 || box.height > 0) &&
          style.visibility !== 'hidden' &&
          style.display !== 'none';

        if (rendered) {
          const host = (el.getRootNode() as ShadowRoot).host;

          found.push({
            name: `${host ? host.tagName.toLowerCase() : 'root'} ${
              (typeof el.className === 'string' && el.className) || el.tagName.toLowerCase()
            }`,
            el: el as HTMLElement,
          });
        }
      }

      if (el.shadowRoot) walk(el.shadowRoot, depth + 1);
    }
  };

  walk(root as ParentNode, 0);

  return found;
}

/**
 * What a finger actually hits for `el`.
 *
 * For everything in this app that is one element, that is the element.
 * A native checkbox is the exception and is why this function exists:
 * it cannot grow its hit area without growing its paint, and a 44px
 * checkbox is not what anyone wants — so a checkbox that has a label
 * is targeted *through* the label, which is the fix the column lists
 * and the download formats both use.
 *
 * The fallback is the checkbox itself, deliberately: a checkbox with
 * no label is a 16px target and this must still say so.
 */
function hitTarget(el: HTMLElement): HTMLElement {
  const input = el as HTMLInputElement;

  if (input.type !== 'checkbox' && input.type !== 'radio') return el;

  const wrapping = el.closest('label');
  const root = el.getRootNode() as ShadowRoot | Document;
  const associated = input.id
    ? root.querySelector<HTMLLabelElement>(`label[for="${CSS.escape(input.id)}"]`)
    : null;

  return wrapping ?? associated ?? el;
}

/** The ones that miss the floor, reported with the numbers. */
function tooSmall(controls: { name: string; el: HTMLElement }[]): string[] {
  return controls
    .map(({ name, el }) => {
      const box = hitTarget(el).getBoundingClientRect();

      return { name, w: Math.round(box.width), h: Math.round(box.height) };
    })
    .filter((c) => c.w < FLOOR || c.h < FLOOR)
    .map((c) => `${c.name} ${c.w}x${c.h}`);
}

/**
 * Open every disclosure under `host`, so the sweep can see the page.
 *
 * A collapsed `config-section` renders its body with `hidden`, so its
 * controls measure 0x0 — which is exactly why the issue's first table
 * lists seven Settings controls and the real count is 120.
 */
async function expandEverySection(host: HTMLElement & { updateComplete: Promise<unknown> }) {
  const sections = shadowAll<HTMLElement>(host, 'config-section');

  expect(sections.length, 'the page renders disclosures to open').toBeGreaterThan(0);

  for (const section of sections) {
    section.shadowRoot
      ?.querySelector<HTMLButtonElement>('button[aria-expanded="false"]')
      ?.click();
  }

  await flush();
  await host.updateComplete;

  for (const section of sections) {
    await (section as HTMLElement & { updateComplete?: Promise<unknown> }).updateComplete;
  }

  return sections;
}

/** A control's own box, named so a failure says which and how small. */
function boxOf(el: Element | null | undefined): string {
  if (!el) return 'missing';

  const box = el.getBoundingClientRect();

  return `${Math.round(box.width)}x${Math.round(box.height)}`;
}

function meetsFloor(el: Element | null | undefined): boolean {
  if (!el) return false;

  const box = el.getBoundingClientRect();

  return Math.round(box.width) >= FLOOR && Math.round(box.height) >= FLOOR;
}

describe('a config field is the shape every Settings row uses', () => {
  it.each(['text', 'number', 'select', 'directory', 'color'] as const)(
    'a %s field meets the floor',
    async (type) => {
      const el = await fixture('config-field', {
        schema: {
          key: 'k',
          label: 'Music folder',
          type,
          options: [{ value: 'dark', label: 'Dark' }],
        },
        value: type === 'color' ? '#ffd43b' : '',
      });

      const controls = controlsUnder(el.shadowRoot!);

      // A sweep that found nothing passes vacuously.
      expect(controls.length).toBeGreaterThan(0);
      expect(tooSmall(controls)).toEqual([]);
    },
  );

  it('grows the toggle, which no sweep of inputs can see', async () => {
    // The `<input>` is opacity: 0; width: 0; height: 0, so the walk
    // above skips it as a zero-sized node -- and the thing a finger
    // hits is the styling <label> around it, which measured 34x19 on
    // the device. It is absent from #186's tables for exactly that
    // reason, and it is smaller than everything in them.
    const el = await fixture('config-field', {
      schema: { key: 'x', label: 'Scan on startup', type: 'toggle' },
      value: true,
    });

    const target = shadow(el, '.toggle-switch');

    expect(boxOf(target)).toBe('44x44');
  });

  it('keeps the toggle painted at its old size, in its old place', async () => {
    // A 44px pill is not what a switch should look like. The box is
    // 44px and the paint is not: the slider is a child centred in it,
    // and negative inline margins hand the extra width back so the
    // pill stays flush with the inputs in the rows above.
    const el = await fixture('config-field', {
      schema: { key: 'x', label: 'Scan on startup', type: 'toggle' },
      value: true,
    });

    const target = shadow(el, '.toggle-switch') as HTMLElement;
    const slider = shadow(el, '.toggle-slider');

    expect(slider!.getBoundingClientRect().height).toBeLessThan(FLOOR);

    const style = getComputedStyle(target);
    const handedBack =
      parseFloat(style.marginInlineStart) + parseFloat(style.marginInlineEnd);

    expect(handedBack).toBeLessThan(0);
  });
});

describe('the shortcut editor', () => {
  it('meets the floor', async () => {
    const el = await fixture('shortcut-capture', {
      action: 'play.toggle',
      label: 'Play/pause',
      currentKey: 'Space',
      defaultKey: 'Space',
    });

    expect(meetsFloor(shadow(el, 'button'))).toBe(true);
  });

  it('grows the reset button, which only a rebound shortcut renders', async () => {
    // Not in either of #186's tables, and it cannot be: a sweep of a
    // freshly-installed app never sees it. It appears the moment
    // anybody uses the feature.
    const el = await fixture('shortcut-capture', {
      action: 'play.toggle',
      label: 'Play/pause',
      currentKey: 'K',
      defaultKey: 'Space',
    });

    const reset = shadow(el, '.reset-btn');

    expect(reset, 'a rebound shortcut renders a reset button').toBeTruthy();
    expect(meetsFloor(reset)).toBe(true);
  });
});

describe('the library filter', () => {
  it('meets the floor in both of its placements', async () => {
    // One component, two mount points since #57 -- the desktop top bar
    // and Settings -> Libraries -- so it reaches the floor once.
    const el = await fixture('library-filter');

    const select = shadow(el, 'select');

    expect(select, 'the filter renders a select').toBeTruthy();
    expect(Math.round(select!.getBoundingClientRect().height)).toBeGreaterThanOrEqual(FLOOR);
  });
});

describe('download clients', () => {
  beforeEach(() => {
    stub('download.Service.ListProviders', []);
    stub('download.Service.ProviderKinds', []);
    stub('config.Config.GetDownloadPreferences', {});
  });

  it('gives Web Awesome form controls the floor through the library API', async () => {
    // wa-input's control is inside somebody else's shadow root, so the
    // height comes from --wa-form-control-height rather than from a
    // rule of ours reaching in. A custom property inherits through a
    // shadow boundary, which is what makes a :host declaration reach
    // it -- and what makes it measurable here.
    const el = await fixture('download-clients');

    await flush();
    await expandEverySection(el);

    expect(getComputedStyle(el).getPropertyValue('--wa-form-control-height').trim()).toBe(
      '44px',
    );

    // And the outcome, not only the mechanism. A sweep of `input`
    // reports a wa-input at 204x**42** even when this is right,
    // because the inner input sits *inside* the control's own 1px
    // border -- Web Awesome sizes it
    // `calc(--wa-form-control-height - border-width * 2)`. What a
    // finger hits is `part=base`, measured at 238x44 on the device.
    //
    // This reaches into another library's shadow root, which
    // `name-dialog.ts` only permits where the failure is bounded. It
    // is bounded the other way here: this is a test, so a renamed
    // part fails loudly rather than silently passing, which is the
    // direction that costs nobody a device session.
    const input = shadowAll<HTMLElement>(el, 'wa-input').find(
      (w) => w.getBoundingClientRect().height > 0,
    );

    expect(input, 'the add form renders a wa-input').toBeTruthy();

    const base = input!.shadowRoot?.querySelector('[part~="base"]');

    expect(base, 'wa-input still calls its control box "base"').toBeTruthy();
    expect(Math.round(base!.getBoundingClientRect().height)).toBeGreaterThanOrEqual(FLOOR);
  });

  it('makes each allowed-format checkbox label a target', async () => {
    const el = await fixture('download-clients');

    await flush();
    await el.updateComplete;

    // Every section starts collapsed, and a collapsed body is `hidden`
    // — so its controls measure 0x0 and a sweep of an unexpanded page
    // reports them all as fine. That is how the issue's first table
    // came to list seven Settings controls when there are 120.
    await expandEverySection(el);

    const options = shadowAll<HTMLElement>(el, '.format-option');

    expect(options.length).toBeGreaterThan(0);

    const short = options
      .map((o) => ({ label: o.textContent?.trim(), h: Math.round(o.getBoundingClientRect().height) }))
      .filter((o) => o.h < FLOOR);

    expect(short).toEqual([]);
  });
});

describe('the whole Settings page', () => {
  beforeEach(() => {
    for (const path of [
      'library.Library.GetAllLibrariesWithTrackCounts',
      'jobs.Service.GetJobs',
      'download.Service.ListProviders',
      'download.Service.ProviderKinds',
    ]) {
      stub(path, []);
    }

    stub('config.Config.GetShortcuts', {});
    stub('config.Config.GetDownloadPreferences', {});
    stub('config.Config.GetThemeAccentColor', '#ffd43b');
    stub('config.Config.GetThemeBackgroundShade', 'dark');
  });

  it('has no control under the floor with every section expanded', async () => {
    // The device sweep, run here: eleven collapsed sections is what
    // made the first table look like seven controls. The density is
    // behind the disclosures.
    const el = await fixture('config-page');

    await flush();
    await el.updateComplete;
    await expandEverySection(el);

    const controls = controlsUnder(el.shadowRoot!);

    expect(controls.length).toBeGreaterThan(0);
    expect(tooSmall(controls)).toEqual([]);
  });

  it('makes a column row a target by naming it, not by growing the checkbox', async () => {
    // A native checkbox cannot grow its hit area without growing its
    // paint. The label is the target instead -- which is also the
    // argument config-field already makes for its own labels, and it
    // is behaviour rather than annotation: the column's name is now a
    // click target for its checkbox.
    const el = await fixture('config-page');

    await flush();
    await el.updateComplete;
    await expandEverySection(el);

    const labels = shadowAll<HTMLLabelElement>(el, 'label.column-label');

    expect(labels.length).toBeGreaterThan(0);

    for (const label of labels) {
      const target = label.htmlFor
        ? el.shadowRoot!.getElementById(label.htmlFor)
        : null;

      expect(
        (target as HTMLInputElement | null)?.type,
        `${label.textContent?.trim()} names its checkbox`,
      ).toBe('checkbox');
      expect(
        Math.round(label.getBoundingClientRect().height),
        `${label.textContent?.trim()} is a target`,
      ).toBeGreaterThanOrEqual(FLOOR);
    }
  });
});
