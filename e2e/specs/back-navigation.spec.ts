import { test, expect } from '../support/fixtures.js';

/**
 * Back is the platform's, and the app has to have somewhere for it to
 * go (reported from a device: "the Android back button does not
 * navigate back in the app").
 *
 * The scaffold's `MainActivity.onBackPressed` asks `webView.canGoBack()`
 * and finishes the activity otherwise. This app never touched
 * `history`, so that was always false and back quit from any depth. A
 * navigation is a history entry now, which is why this is assertable
 * here at all: `page.goBack()` is the same `popstate` the phone's
 * gesture produces, so the browser tier can answer a question that
 * otherwise needs a device.
 *
 * What it cannot answer is whether Android's *gesture* reaches the
 * WebView, which is between the OS and the scaffold.
 *
 * **And `data-active-view` is not the behaviour.** Every assertion here
 * used to be that attribute, which the shell sets on every path
 * including `_isBack` — so this file was green throughout #72, in
 * which both navs highlighted the view the user had just *left*. The
 * shell's own bookkeeping was the one thing that was already right;
 * what a person sees is `aria-current`, and that is asserted below as
 * well. This is the same trap `layout-overflow.spec.ts` set for #69: a
 * spec named for the behaviour, measuring the plumbing.
 */
type Page = import('@playwright/test').Page;

const activeView = (page: Page) =>
  page.getByTestId('main-content');

/** A common phone, where the bottom bar is the primary navigation. */
const PHONE = { width: 390, height: 844 };

/**
 * The nav item for a destination, in whichever navigation is on screen.
 *
 * Both navs carry a button named `Albums`, and only one of them is ever
 * in the accessibility tree — the other is `display: none` — so the
 * role query resolves to the one the user can see at this viewport.
 * That is the point: the highlight has to be right in both, and #72 was
 * two different-looking symptoms of one cause.
 */
const navItem = (page: Page, label: string) =>
  page.getByRole('button', { name: label, exact: true });

/**
 * `aria-current="page"` is the accessible fact and the assertion worth
 * making; `.active` is a class and could be restyled without breaking
 * anything real.
 */
async function expectHighlighted(page: Page, label: string): Promise<void> {
  await expect(navItem(page, label)).toHaveAttribute('aria-current', 'page');
}

async function expectNotHighlighted(page: Page, label: string): Promise<void> {
  await expect(navItem(page, label)).toHaveAttribute('aria-current', 'false');
}

/**
 * Open an artist's detail view, which is the deepest ordinary route.
 *
 * A library artist opens `explore-artist-details` -- the catalog panel
 * standing in for a library one, as `explore-link.ts` describes -- and
 * the view name follows the component, not the source of the click.
 */
async function openAnArtist(app: Page): Promise<void> {
  await app.getByTestId('nav-artists').click();
  await expect(activeView(app)).toHaveAttribute('data-active-view', 'artists');

  // A card, by the name on it: the grid is virtualized and positioned
  // by transform, so a click at coordinates is a click at whatever
  // happens to be there.
  await app.locator('artists-view').getByText('Aurora Fields').first().click();
  await expect(activeView(app)).toHaveAttribute(
    'data-active-view',
    'explore-artist-details',
  );
}

test.describe('the back gesture', () => {
  test('leaves a detail view for the view it was opened from', async ({
    app,
  }) => {
    await openAnArtist(app);

    await app.goBack();

    await expect(activeView(app)).toHaveAttribute('data-active-view', 'artists');
  });

  test('walks back through primary views, one press per navigation', async ({
    app,
  }) => {
    await app.getByTestId('nav-tracks').click();
    await expect(activeView(app)).toHaveAttribute('data-active-view', 'tracks');

    await app.getByTestId('nav-albums').click();
    await expect(activeView(app)).toHaveAttribute('data-active-view', 'albums');

    await app.goBack();
    await expect(activeView(app)).toHaveAttribute('data-active-view', 'tracks');

    // Forward is free once back works, and it is what proves the entry
    // was restored rather than the view merely re-rendered.
    await app.goForward();
    await expect(activeView(app)).toHaveAttribute('data-active-view', 'albums');
  });

  test('leaves the nav highlighting the view it landed on, not the one it left', async ({
    app,
  }) => {
    await app.getByTestId('nav-albums').click();
    await expectHighlighted(app, 'Albums');

    await app.getByTestId('nav-tracks').click();
    await expectHighlighted(app, 'Tracks');

    await app.goBack();

    // #72, and the half of it the report did not describe: this is
    // desktop, and before the shell published the active view *both*
    // navs stayed on Tracks. An absent highlight reads as a glitch; a
    // confident wrong one is worse, and any back across two primary
    // views produced it.
    await expect(activeView(app)).toHaveAttribute('data-active-view', 'albums');
    await expectHighlighted(app, 'Albums');
    await expectNotHighlighted(app, 'Tracks');
  });

  test('keeps the parent destination lit while a detail view is open', async ({
    app,
  }) => {
    await app.getByTestId('nav-artists').click();
    await expectHighlighted(app, 'Artists');

    await openAnArtist(app);

    // A detail view is not a destination in either nav, and the user is
    // still inside Artists. `app-sidebar` did this by accident -- it
    // guarded on its own item list, so an unmatched name left the
    // highlight alone -- and that accident is why the sidebar looked
    // right on a detail view while the tab bar lit nothing. This test
    // therefore passed before the fix and is here to keep the rule from
    // being lost while the others are made to pass; the *tab bar's*
    // half of it is the phone test below, which did not.
    await expectHighlighted(app, 'Artists');

    await app.goBack();

    await expectHighlighted(app, 'Artists');
  });

  test('the tab bar survives the same journey on a phone', async ({ app }) => {
    await app.setViewportSize(PHONE);

    // The reported shape: Albums, open an album, press back. The tab
    // bar had a highlight, then no highlight at all, and never got it
    // back — `bottom-nav` took the detail view's name, matched it
    // against no tab, and lit nothing.
    await navItem(app, 'Albums').click();
    await expectHighlighted(app, 'Albums');

    await app.locator('cover-grid').getByText('Glass Harbour').first().click();
    await expect(activeView(app)).toHaveAttribute(
      'data-active-view',
      'explore-album-details',
    );
    await expectHighlighted(app, 'Albums');

    await app.goBack();

    await expect(activeView(app)).toHaveAttribute('data-active-view', 'albums');
    await expectHighlighted(app, 'Albums');
  });

  test('the drawer sidebar opens on the page you are standing on', async ({
    app,
  }) => {
    await app.setViewportSize(PHONE);

    await navItem(app, 'Tracks').click();
    await expectHighlighted(app, 'Tracks');

    // A third symptom of the same cause, found while measuring #72 and
    // not in the report: `bottom-nav` mounts its `<app-sidebar>` when
    // the drawer opens, so that copy had heard no `navigate` at all and
    // showed its own default — Home, from any page in the app. An event
    // has no answer for a listener that was not there; a store does.
    await navItem(app, 'More').click();

    // The element carrying the testid is the `wa-drawer` host, which
    // always reports hidden -- what is visible is the `<dialog>` in its
    // shadow root -- so the drawer being open is asserted of the
    // sidebar it holds rather than of itself.
    const drawer = app.getByTestId('nav-drawer');

    await expect(drawer.locator('app-sidebar')).toBeVisible();
    await expect(drawer.getByTestId('nav-tracks')).toHaveAttribute(
      'aria-current',
      'page',
    );
    await expect(drawer.getByTestId('nav-home')).toHaveAttribute(
      'aria-current',
      'false',
    );
  });

  test('an in-app back button consumes exactly one entry', async ({ app }) => {
    await app.getByTestId('nav-tracks').click();
    await openAnArtist(app);

    // The detail view's own back button and the phone's gesture are the
    // same press: if each popped its own stack, this would land two
    // navigations back instead of one.
    await app
      .locator('explore-artist-details')
      .getByRole('button', { name: 'Back to explore' })
      .click();

    await expect(activeView(app)).toHaveAttribute('data-active-view', 'artists');

    await app.goBack();

    await expect(activeView(app)).toHaveAttribute('data-active-view', 'tracks');
  });
});
