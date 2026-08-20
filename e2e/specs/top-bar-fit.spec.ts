import { test, expect } from '../support/fixtures.js';

/**
 * The top bar fits the window it is in (#143).
 *
 * **This is measured per child, not on the shell**, which is #69's
 * lesson repeated one component over: `layout-overflow.spec.ts` asserts
 * the *document* needs no sideways scrolling, and clipping inside a
 * component is invisible to it — which is exactly why that spec was
 * green throughout this defect. What a user sees is a control rendered
 * past the edge of the bar it belongs to, so that is what is asserted.
 *
 * **And it is measured with a job running**, which is the half the
 * original report missed. `job-indicator` is `hidden` while idle and up
 * to 235px wide when it is not, so the bar was 611px inside 600 sitting
 * still and 862px during a scan — 171 to 262px of overflow, arriving
 * exactly when a user has reason to look at that bar. Nothing else in
 * this suite has ever measured a layout with work in flight;
 * `/__test/emit` stages it without staging the scan.
 */
type Page = import('@playwright/test').Page;

/**
 * The widths this asks about.
 *
 * 600 is the bottom of the Compact band (#24) and where the defect
 * lands; 899 and 900 straddle `nav-history` appearing (68px more to
 * find, at the width that just gained the sidebar's labels); 800 is the
 * enforced minimum; 390 is a phone, where the answer must be that
 * nothing collapses because the media queries already did the work.
 */
const WIDTHS = [390, 600, 800, 899, 900, 1440];

/**
 * A scan whose title is as long as a real one gets. The label is capped
 * at 12rem by the component, so this is the widest the indicator can
 * be — measuring with "Scanning" instead reports a bar that fits and a
 * defect that is 100px smaller than it is.
 */
const LONG_JOB = {
  id: 'top-bar-fit',
  kind: 'library-scan',
  state: 'running',
  title: 'Scanning Music from the external drive',
  current: 40,
  total: 100,
};

/**
 * Every child's right edge against the bar's own content box.
 *
 * The content box, not `clientWidth`: the bar has a 2em right gutter,
 * and a control sitting in the padding is already the failure — it is
 * simply one that `scrollWidth` under-reports, because `scrollWidth`
 * counts the left padding and not the right.
 */
const overflowingChildren = (page: Page) =>
  page.evaluate(() => {
    const bar = document.querySelector<HTMLElement>('header.top-bar')!;
    const style = getComputedStyle(bar);
    const box = bar.getBoundingClientRect();
    const left = box.left + parseFloat(style.paddingLeft);
    const right = box.right - parseFloat(style.paddingRight);

    return [...bar.children]
      .filter((child) => {
        const cs = getComputedStyle(child);

        // Out of flow is out of the question: a collapsed wordmark is
        // `position: absolute` and 1px wide precisely so it costs the
        // row nothing.
        if (cs.display === 'none' || cs.position === 'absolute') return false;

        const r = child.getBoundingClientRect();

        return r.width > 0 && (r.right > right + 0.5 || r.left < left - 0.5);
      })
      .map((child) => {
        const r = child.getBoundingClientRect();

        return `${child.tagName.toLowerCase()}: ${Math.round(r.left)}..${Math.round(r.right)} outside ${Math.round(left)}..${Math.round(right)}`;
      });
  });

/** What the fit pass gave up, read back off the DOM it changed. */
const collapsed = (page: Page) =>
  page.evaluate(() => ({
    wordmark: !!document.querySelector('header.top-bar hgroup.yj-collapsed'),
    jobLabel: !!document.querySelector('job-indicator[compact]'),
  }));

test.describe('the top bar fits the window', () => {
  for (const width of WIDTHS) {
    test(`no control sits outside the bar at ${width}px, idle`, async ({
      app,
    }) => {
      await app.setViewportSize({ width, height: 600 });

      await expect.poll(() => overflowingChildren(app)).toEqual([]);
    });

    test(`no control sits outside the bar at ${width}px, with a job running`, async ({
      app,
      testctl,
    }) => {
      await app.setViewportSize({ width, height: 600 });
      await testctl.emit('JobsChanged', [LONG_JOB]);

      // The indicator has to actually be up, or this test passes by
      // measuring the idle case under another name.
      await expect(app.locator('job-indicator')).toBeVisible();

      await expect.poll(() => overflowingChildren(app)).toEqual([]);
    });
  }

  /**
   * The other half of "measured, never breakpointed": a rule that
   * collapses defensively at every narrow width fits just as well and
   * is a worse app. 1440 is roomy at any job title; 899 was measured to
   * fit with the longest one, because `nav-history` is not there yet.
   */
  test('nothing is given up where there is room for it', async ({
    app,
    testctl,
  }) => {
    await app.setViewportSize({ width: 1440, height: 900 });
    await testctl.emit('JobsChanged', [LONG_JOB]);
    await expect(app.locator('job-indicator')).toBeVisible();

    await expect.poll(() => collapsed(app)).toEqual({
      wordmark: false,
      jobLabel: false,
    });
  });

  /**
   * And it gives them back. The pass starts from all-visible every
   * time, so this is the property that a rule which only ever *added*
   * to the collapsed set would fail — the wordmark would be gone for
   * the rest of the session after one narrow moment.
   */
  test('the wordmark comes back when the window does', async ({
    app,
    testctl,
  }) => {
    await testctl.emit('JobsChanged', [LONG_JOB]);
    await app.setViewportSize({ width: 600, height: 600 });

    await expect.poll(() => collapsed(app)).toEqual({
      wordmark: true,
      jobLabel: true,
    });

    await app.setViewportSize({ width: 1440, height: 900 });

    await expect.poll(() => collapsed(app)).toEqual({
      wordmark: false,
      jobLabel: false,
    });
  });

  /**
   * The wordmark yields its width and not its existence: `display:
   * none` would take the document from one top-level heading to none.
   */
  test('the collapsed wordmark is still the document heading', async ({
    app,
    testctl,
  }) => {
    await testctl.emit('JobsChanged', [LONG_JOB]);
    await app.setViewportSize({ width: 600, height: 600 });

    await expect.poll(() => collapsed(app)).toMatchObject({ wordmark: true });

    await expect(
      app.getByRole('heading', { name: 'YellowJacket', level: 1 }),
    ).toHaveCount(1);
  });

  /**
   * And the indicator keeps saying what it is doing after its visible
   * label goes — the `sr-only` live region is what announces the state,
   * which is the same argument the phone's own rule was written on.
   */
  test('the job indicator still announces its state without its label', async ({
    app,
    testctl,
  }) => {
    await testctl.emit('JobsChanged', [LONG_JOB]);
    await app.setViewportSize({ width: 600, height: 600 });

    await expect.poll(() => collapsed(app)).toMatchObject({ jobLabel: true });

    const spoken = await app
      .locator('job-indicator')
      .evaluate(
        (el) =>
          el.shadowRoot?.querySelector('[aria-live]')?.textContent?.trim() ?? '',
      );

    expect(spoken).toContain('Scanning Music from the external drive');

    // Leave the app as the next spec expects to find it.
    await app.setViewportSize({ width: 1440, height: 900 });
  });
});
