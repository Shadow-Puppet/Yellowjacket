import { test, expect, LONG_TRACK } from '../support/fixtures.js';

/**
 * The phone shell (plan 016 B2, phase 1).
 *
 * This is the tier that can actually answer the question. Wails v3's
 * server mode serves the real frontend, so a Chromium at 390×844 is the
 * same document an Android WebView renders — the only thing a device
 * adds here is the WebView's own quirks, and CI runs the WebKit half
 * for exactly that reason.
 *
 * The assertions are the three things B2 is *for*: the eleven-item
 * sidebar is gone, the four destinations plan 016 committed to are
 * reachable with a thumb, and nothing scrolls sideways. The last one is
 * the one that hides: `overflow-x: auto` on `body` means a shell that
 * does not fit produces a scrollbar rather than a broken layout, which
 * looks survivable in a screenshot and is not.
 */

/** A common small phone. Narrower than any device this is likely to meet. */
const PHONE = { width: 390, height: 844 };

/** The narrowest thing still sold, near enough. */
const SMALL_PHONE = { width: 360, height: 780 };

const horizontalOverflow = (page: import('@playwright/test').Page) =>
  page.evaluate(() => ({
    scrollWidth: document.body.scrollWidth,
    clientWidth: document.body.clientWidth,
  }));

test.describe('the shell on a phone', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize(PHONE);
  });

  test('replaces the sidebar with a bottom tab bar', async ({ app }) => {
    await expect(app.locator('div.sidebar')).toBeHidden();

    const nav = app.locator('bottom-nav');

    await expect(nav).toBeVisible();

    // Four tabs and a way to everything else, which is the shape the
    // plan argues for: a tab bar is 3-5 items before the targets stop
    // being thumb-sized.
    for (const id of ['home', 'albums', 'tracks', 'playlists', 'more']) {
      await expect(app.getByTestId(`tab-${id}`)).toBeVisible();
    }
  });

  test('navigates from a tab', async ({ app }) => {
    await app.getByTestId('tab-albums').click();

    await expect(app.getByTestId('main-content'))
      .toHaveAttribute('data-active-view', 'albums');

    await app.getByTestId('tab-home').click();

    await expect(app.getByTestId('main-content'))
      .toHaveAttribute('data-active-view', 'home');
  });

  test('reaches the views with no tab through the drawer', async ({ app }) => {
    await app.getByTestId('tab-more').click();

    // Scoped to the drawer: the desktop sidebar is still in the DOM
    // (hidden by the media query, not removed), so an unscoped testid
    // matches two elements and Playwright's strict mode refuses --
    // which is the right complaint, since the two really are different
    // buttons.
    //
    // The drawer holds the *same* sidebar the desktop uses, so Settings
    // -- which a phone still needs occasionally -- is reachable without
    // a second list of destinations to keep in step.
    const settings = app
      .getByTestId('nav-drawer')
      .getByTestId('nav-settings');

    await expect(settings).toBeVisible();
    await settings.click();

    await expect(app.getByTestId('main-content'))
      .toHaveAttribute('data-active-view', 'settings');

    // And the drawer gets out of the way once it has done its job.
    await expect(app.getByTestId('nav-drawer')).toBeHidden();
  });

  test('has a named drawer', async ({ app }) => {
    await app.getByTestId('tab-more').click();

    // The a11y snapshot never prints a dialog's name, so this asks for
    // the role and the name together -- which is the check that caught
    // eleven unnamed dialogs.
    await expect(
      app.getByRole('dialog', { name: 'All views' }),
    ).toBeVisible();
  });

  test('draws "More" as a sheet on the bottom edge (#71)', async ({ app }) => {
    await app.getByTestId('tab-more').click();
    await expect(app.getByTestId('nav-drawer').locator('app-sidebar'))
      .toBeVisible();

    // What the report is about is geometry, and geometry is what no
    // other assertion here can see: the side drawer was a 200px column
    // opening away from the thumb that asked for it, with the rest of
    // its 400px band empty. Measured rather than screenshotted, since
    // the failure is a number.
    //
    // Polled, because a sheet *arrives*: the drawer's show animation
    // translates it a full height below the fold, so a measurement
    // taken the moment its content is visible reports a box hanging
    // 412px off the bottom of the screen. Asking for the settled
    // number is the assertion; asking once is a race.
    const measure = () => app.evaluate(() => {
      const nav = document.querySelector('bottom-nav');
      const drawer = nav?.shadowRoot?.querySelector('wa-drawer');
      const dialog = drawer?.shadowRoot?.querySelector('[part~="dialog"]');
      const sidebar = nav?.shadowRoot?.querySelector('app-sidebar');
      const row = sidebar?.shadowRoot?.querySelector('li button');
      const box = dialog?.getBoundingClientRect();

      return {
        left: Math.round(box?.left ?? -1),
        right: Math.round(box?.right ?? -1),
        bottom: Math.round(box?.bottom ?? -1),
        height: Math.round(box?.height ?? -1),
        row: Math.round(row?.getBoundingClientRect().height ?? -1),
        viewport: [window.innerWidth, window.innerHeight],
      };
    });

    await expect
      .poll(async () => (await measure()).bottom)
      .toBe(PHONE.height);

    const sheet = await measure();

    expect(sheet.left).toBe(0);
    expect(sheet.right).toBe(sheet.viewport[0]);

    // A surface covering the whole screen is a page, not a sheet --
    // which is also what leaves an outside to tap on, the only pointer
    // route out of it (#171 is the same question one surface over).
    expect(sheet.height).toBeLessThan(sheet.viewport[1]);

    // 48px rows, from #186's touch floor and #60's context sheet.
    expect(sheet.row).toBeGreaterThanOrEqual(48);
  });

  for (const vp of [PHONE, SMALL_PHONE]) {
    test(`does not scroll sideways at ${vp.width}×${vp.height}`, async ({ app }) => {
      await app.setViewportSize(vp);
      await app.getByTestId('tab-tracks').click();
      await expect(app.getByTestId('main-content'))
        .toHaveAttribute('data-active-view', 'tracks');

      const { scrollWidth, clientWidth } = await horizontalOverflow(app);

      expect(scrollWidth, `body overflows by ${scrollWidth - clientWidth}px`)
        .toBeLessThanOrEqual(clientWidth);
    });
  }

  test('opens the full-screen now playing, and comes back', async ({ app }) => {
    // Something has to be playing for the mini player to be a way in.
    await app.getByTestId('tab-tracks').click();
    await expect(app.getByTestId('main-content'))
      .toHaveAttribute('data-active-view', 'tracks');

    await app.locator('track-list .track-row').first().dblclick();
    await expect(app.getByTestId('now-playing-title')).not.toBeEmpty();

    await app.getByTestId('open-now-playing').click();

    await expect(app.getByTestId('main-content'))
      .toHaveAttribute('data-active-view', 'now-playing');

    // The seek bar and volume that phase 1 took out of the bottom bar
    // are here, and they are the *same* components -- this view
    // composes the transport rather than reimplementing it.
    await expect(app.locator('now-playing-view seek-bar')).toBeVisible();

    // Volume is here **because the player says there is one** (#64),
    // not because this is a phone. This tier is the platform that owns
    // its own volume, so what it can assert is that the control's
    // presence follows that answer -- an inverted polarity in
    // `volume-style-store` fails here and in `bottom-bar.spec.ts`, and
    // the *absent* branch is checked in the component tier, where the
    // binding can be stubbed. Nothing here can reach the Android side.
    const systemOwns = await app.evaluate(
      async () =>
        (await window.__yjEvents.call(
          'player.Player.SystemOwnsVolume',
          [],
          5_000,
        )) as boolean,
    );

    expect(systemOwns, 'this platform should own its own volume').toBe(false);

    await expect(app.locator('now-playing-view volume-control')).toBeVisible();

    // Back goes where the user came from, through the nav stack.
    await app.getByTestId('npv-back').click();
    await expect(app.getByTestId('main-content'))
      .toHaveAttribute('data-active-view', 'tracks');
  });

  /**
   * The same journey with a track that has **no cover art** (#150).
   *
   * The test above starts the *first* row of the track list, so which
   * track it plays is the order the scan inserted them in — and the
   * answer decided whether it passed. A track with artwork renders an
   * `<img>`, which is no obstacle; one without renders a placeholder
   * `wa-icon`, which took every click aimed at the button beneath it,
   * because that button is absolutely positioned with `z-index: auto`
   * and the art is a *later* sibling. They tied, and the later one won.
   *
   * So this picks a track *for* the property that broke it, which is
   * the only way the assertion means anything: the version above passes
   * on a broken build roughly two runs in three, which is exactly how
   * it came to cost three CI cycles across two branches that could not
   * have caused it.
   */
  test('opens the full-screen now playing for a track with no art', async ({
    app,
  }) => {
    // `LONG_TRACK` by name, and not "the first track with no
    // CoverArt": the *library* model reports that field empty for
    // every row in this fixture (31 of 31), so filtering on it selects
    // nothing in particular and picked a 2-second track, which had
    // finished before the assertions ran. The placeholder check below
    // is what actually holds the property this test needs.
    const started = await app.evaluate(async (longTitle) => {
      const tracks = (await window.__yjEvents.call(
        'library.Library.GetTracks',
        [0],
        10_000,
      )) as { FilePath: string; TrackName: string }[];

      const bare = tracks.find((t) => t.TrackName === longTitle);

      if (!bare) return null;

      await window.__yjEvents.call(
        'queue.Queue.SetQueue',
        [[bare.FilePath], 0, false, { type: '', id: 0, label: '' }],
        10_000,
      );
      await window.__yjEvents.call('queue.Queue.Play', [], 5_000);

      return bare.TrackName;
    }, LONG_TRACK);

    expect(started).toBe(LONG_TRACK);

    await expect(app.getByTestId('now-playing-title')).not.toBeEmpty();

    // **The placeholder is the whole point**, so it is asserted rather
    // than assumed: this test is about the thing that renders when
    // there is no artwork. If the fixture ever gives this album a
    // cover, this fails and says so instead of passing while measuring
    // the easy case.
    //
    // One selector rather than a chain from the host: Playwright's CSS
    // engine pierces an open shadow root, and chaining from the host
    // element does not reach into it.
    await expect(
      app.locator('now-playing .cover-placeholder'),
    ).toBeAttached();

    await app.getByTestId('open-now-playing').click();

    await expect(app.getByTestId('main-content'))
      .toHaveAttribute('data-active-view', 'now-playing');

    await app.getByTestId('npv-back').click();
  });

  test('offers no way in on a desktop, where the bar is whole', async ({ app }) => {
    await app.setViewportSize({ width: 1440, height: 900 });

    // The button exists in the markup at every size; CSS decides. If
    // this becomes visible on a desktop it is a 48px hit target over
    // the cover art, swallowing the clicks that open the preview.
    await expect(app.getByTestId('open-now-playing')).toBeHidden();
  });

  test('keeps the transport, minus what a thumb cannot use', async ({ app }) => {
    // The player bar stays: this is a music player, and what is playing
    // has to be visible and pausable from every view.
    await expect(app.locator('audio-player')).toBeVisible();
    await expect(app.locator('now-playing')).toBeVisible();

    // Volume is the hardware keys' job on a phone, and a 4px seek bar
    // is not a thumb target -- both belong to the full-screen
    // now-playing view.
    //
    // `.bottom-bar volume-control`, not `audio-player volume-control`:
    // #42 moved the control out of that component and into the bar, and
    // **the old locator would have kept passing** — `toBeHidden()` is
    // satisfied by an element that does not exist, so this assertion
    // would have gone on reporting success about nothing. Its partner
    // below is what makes this one mean something.
    await expect(app.locator('.bottom-bar volume-control')).toBeHidden();

    // The element is there and hidden, rather than absent: the check
    // above cannot tell those apart on its own.
    await expect(app.locator('.bottom-bar volume-control')).toHaveCount(1);

    // And the seek bar is still inside the transport, where it stands
    // down by its own media query.
    await expect(
      app.locator('audio-player').locator('seek-bar'),
    ).toBeHidden();
  });
});

test.describe('the desktop shell is unchanged', () => {
  test('keeps the sidebar and hides the tab bar', async ({ app }) => {
    await app.setViewportSize({ width: 1440, height: 900 });

    await expect(app.locator('div.sidebar')).toBeVisible();
    await expect(app.locator('bottom-nav')).toBeHidden();
  });
});
