/**
 * Two Web Awesome controls put the role somewhere the host's
 * `aria-label` cannot reach it, and this pins the way out of each.
 *
 * `a11y.md` lists `seek-bar` and `volume-control` under **what is
 * already correct** ("`wa-slider` with `aria-label`"). Measured against
 * the running app with `Accessibility.getFullAXTree`, both sliders
 * computed a name of `""` on all eleven views: the role is on a
 * `<div id="slider" aria-labelledby="label">` inside `wa-slider`'s own
 * shadow root, and an `aria-labelledby` pointing at an empty internal
 * `<label>` outranks the host's `aria-label`. `volume-control` did not
 * have the `aria-label` the audit credits it with in the first place.
 *
 * `a11y.25` is the same family, and the fix is the library's own API in
 * both cases — `label` — which for a progress bar is invisible and for a
 * slider is not, hence `styles/wa-slider-label.css.ts`.
 *
 * What this tier checks is the *wiring*: that the internal label carries
 * the text, that the IDREF still points at it, and that hiding it does
 * not move the control. Computing an accessible name is Playwright's
 * job, and `e2e/specs/control-names.spec.ts` does it there.
 */
import { describe, expect, it } from 'vitest';

import '@awesome.me/webawesome/dist/components/slider/slider.js';
import '@awesome.me/webawesome/dist/components/progress-bar/progress-bar.js';
import '@components/audio-player/seekbar/seek-bar';
import '@components/audio-player/volume-control/volume-control';
import '@components/jobs/job-row';
import { fixture, shadow } from '@test/support/render';

/** The element Web Awesome puts `role="slider"` on, and its name source. */
function sliderName(wa: Element | null): string | null {
  const inner = wa?.shadowRoot?.querySelector('[role="slider"]');
  const id = inner?.getAttribute('aria-labelledby');

  if (!id) return null;

  return wa?.shadowRoot?.getElementById(id)?.textContent?.trim() ?? null;
}

describe('naming a wa-slider', () => {
  it('names the seek bar through the internal label, not the host', async () => {
    const el = await fixture('seek-bar');
    const wa = shadow(el, 'wa-slider');

    await (wa as HTMLElement & { updateComplete: Promise<unknown> })
      .updateComplete;

    // The positive case: the name a screen reader would compute is
    // reachable from the element that carries the role.
    expect(sliderName(wa)).toBe('Seek');
  });

  it('leaves the seek bar the height it was without a label', async () => {
    const el = await fixture('seek-bar');
    const wa = shadow(el, 'wa-slider') as HTMLElement;

    await (wa as HTMLElement & { updateComplete: Promise<unknown> })
      .updateComplete;

    // `#slider` takes an 8px margin-block-start as soon as a label
    // exists, so hiding the label alone still grows the control from
    // 6px to 14px and moves the transport bar with it.
    const label = wa.shadowRoot?.querySelector('[part~="label"]') as HTMLElement;

    expect(getComputedStyle(label).display).toBe('none');
    expect(wa.getBoundingClientRect().height).toBeLessThan(10);
  });

  it('names the volume slider, which had no name of any kind', async () => {
    const el = await fixture('volume-control');
    const trigger = shadow<HTMLElement>(el, 'button');

    trigger?.click();
    await (el as HTMLElement & { updateComplete: Promise<unknown> })
      .updateComplete;

    const wa = shadow(el, 'wa-slider');

    await (wa as HTMLElement & { updateComplete: Promise<unknown> })
      .updateComplete;

    expect(sliderName(wa)).toBe('Volume');
  });
});

describe('naming a wa-progress-bar', () => {
  it('says what is progressing rather than "progress"', async () => {
    const el = await fixture('job-row', {
      job: {
        id: 'j1',
        title: 'Scanning Music',
        kind: 'library-scan',
        state: 'running',
        current: 45,
        total: 100,
        caps: { pausable: false, cancellable: false },
        stages: [],
        stats: [],
        startedAt: 0,
        updatedAt: 0,
        logCount: 0,
        warnCount: 0,
        errorCount: 0,
      },
    });

    const bar = shadow(el, 'wa-progress-bar');

    await (bar as HTMLElement & { updateComplete: Promise<unknown> })
      .updateComplete;

    // Web Awesome maps `label` onto the inner role="progressbar"'s
    // aria-label, falling back to the localised word "progress" — so
    // this was never *unnamed*, it was named after the widget instead
    // of after the work.
    const inner = bar?.shadowRoot?.querySelector('[role="progressbar"]');

    expect(inner?.getAttribute('aria-label')).toBe('Scanning Music');
  });
});
