/**
 * The context menu has a keyboard model, and it is one model.
 *
 * `a11y.3`: the panel is a bare `wa-popup` holding `wa-dropdown-item`s.
 * Web Awesome gives each item `role="menuitem"`, but nothing gave the
 * container `role="menu"`, nothing moved focus into it, nothing handled
 * Arrow/Escape, and nothing restored focus. Play, Add to Queue, Play
 * Next, Add to Playlist, Favourite and Track Details — most of which
 * have no other route — were mouse-only.
 *
 * `MenuKeyboard` is that model, standalone rather than part of
 * `ContextMenuController`, because `playlist-view` renders a menu
 * without the controller and two menus with two keyboard models is the
 * thing this is meant to prevent.
 *
 * The two non-obvious parts are pinned below, both of which cost a cycle
 * when they were wrong:
 *
 * - The items are not items yet when the host's `updateComplete`
 *   resolves. `wa-dropdown-item` sets its `role` in its *own* first
 *   update, so a `[role^="menuitem"]` query at that moment finds
 *   nothing and the menu opens without taking focus.
 * - Focus is only taken back on close if the menu had it. A click
 *   elsewhere closes the menu too, and pulling focus to the row the
 *   user right-clicked a moment ago would be worse than leaving it.
 */
import { describe, expect, it, beforeEach, afterEach } from 'vitest';

import { MenuKeyboard, isContextMenuKey } from '@utils/context-menu-controller';

/** A panel of plain elements carrying the roles Web Awesome would set. */
function panelWith(labels: string[]): HTMLElement {
  const panel = document.createElement('div');

  panel.className = 'context-menu-panel';
  panel.setAttribute('role', 'menu');

  for (const label of labels) {
    const item = document.createElement('div');

    item.setAttribute('role', 'menuitem');
    item.textContent = label;
    panel.append(item);
  }

  document.body.append(panel);

  return panel;
}

function press(target: EventTarget, key: string): void {
  target.dispatchEvent(
    new KeyboardEvent('keydown', { key, bubbles: true, composed: true }),
  );
}

/** The focused element, resolved the way the app resolves it. */
function active(): Element | null {
  let el = document.activeElement;

  while (el?.shadowRoot?.activeElement) el = el.shadowRoot.activeElement;

  return el;
}

describe('the context menu key', () => {
  it('is Shift+F10 and the ContextMenu key, and nothing else', () => {
    expect(isContextMenuKey(new KeyboardEvent('keydown', { key: 'ContextMenu' }))).toBe(
      true,
    );
    expect(
      isContextMenuKey(new KeyboardEvent('keydown', { key: 'F10', shiftKey: true })),
    ).toBe(true);
    // F10 alone is a menu-bar convention we do not own.
    expect(isContextMenuKey(new KeyboardEvent('keydown', { key: 'F10' }))).toBe(false);
    expect(isContextMenuKey(new KeyboardEvent('keydown', { key: 'Enter' }))).toBe(false);
  });
});

describe('MenuKeyboard', () => {
  let panel: HTMLElement;
  let opener: HTMLButtonElement;
  let closed: number;
  let keyboard: MenuKeyboard;

  beforeEach(async () => {
    closed = 0;
    opener = document.createElement('button');
    opener.textContent = 'The row';
    document.body.append(opener);
    opener.focus();

    panel = panelWith(['Play', 'Add to Queue', 'Track Details']);
    keyboard = new MenuKeyboard(() => {
      closed++;
      keyboard.close();
    });
    keyboard.open(panel, opener);

    // Focus is taken across at least one frame, because a popup that has
    // not positioned itself yet cannot be focused.
    await new Promise((r) => requestAnimationFrame(r));
  });

  afterEach(() => {
    keyboard.close();
    panel.remove();
    opener.remove();
  });

  it('focuses the first item on open', () => {
    expect(active()?.textContent).toBe('Play');
  });

  it('moves with the arrows and wraps', () => {
    press(active()!, 'ArrowDown');
    expect(active()?.textContent).toBe('Add to Queue');

    press(active()!, 'ArrowUp');
    expect(active()?.textContent).toBe('Play');

    // Up from the first item wraps to the last, which is what a menu
    // does and what a listbox does not.
    press(active()!, 'ArrowUp');
    expect(active()?.textContent).toBe('Track Details');
  });

  it('goes to the ends with Home and End', () => {
    press(active()!, 'End');
    expect(active()?.textContent).toBe('Track Details');

    press(active()!, 'Home');
    expect(active()?.textContent).toBe('Play');
  });

  it('activates the focused item with Enter', () => {
    let clicked = '';

    for (const item of panel.querySelectorAll('[role="menuitem"]')) {
      item.addEventListener('click', () => {
        clicked = item.textContent ?? '';
      });
    }

    press(active()!, 'ArrowDown');
    press(active()!, 'Enter');

    expect(clicked).toBe('Add to Queue');
  });

  it('closes on Escape and gives focus back to the opener', () => {
    press(active()!, 'Escape');

    expect(closed).toBe(1);
    expect(active()).toBe(opener);
  });

  it('closes on Tab rather than letting focus escape the panel', () => {
    press(active()!, 'Tab');

    expect(closed).toBe(1);
    expect(active()).toBe(opener);
  });

  it('leaves focus alone when the menu did not have it', () => {
    const elsewhere = document.createElement('button');

    document.body.append(elsewhere);
    elsewhere.focus();

    keyboard.close();

    expect(active()).toBe(elsewhere);
    elsewhere.remove();
  });
});
