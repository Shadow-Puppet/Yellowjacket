/**
 * The keyboard shortcut service and the store behind it. This is the
 * test that most justifies running in a real browser: the service walks
 * shadow roots to find the deepest focused element, and no jsdom
 * approximation of that is worth trusting.
 */
import { describe, expect, it, beforeEach, afterEach } from 'vitest';

import { buildKeyString } from '../../src/services/keyboard-shortcut-service';
import '../../src/services/keyboard-shortcut-service';
import { shortcutsStore } from '@store/shortcuts-store';
import { Events } from '../../src/events';
import { emit, calls, lastArgs, stub } from '@test/support/harness';

/** Install a binding table, as the backend's config push does. */
function bindings(table: Record<string, string>): void {
  emit(Events.ShortcutsConfigChanged, table);
}

/** Send a keydown through the document, where the service listens. */
function press(
  key: string,
  modifiers: Partial<
    Record<'ctrlKey' | 'altKey' | 'shiftKey' | 'metaKey', boolean>
  > = {},
): KeyboardEvent {
  const event = new KeyboardEvent('keydown', {
    key,
    bubbles: true,
    cancelable: true,
    ...modifiers,
  });

  document.dispatchEvent(event);

  return event;
}

const mounted: HTMLElement[] = [];

/** Append an element to the body and remember to remove it. */
function mount<T extends HTMLElement>(el: T): T {
  document.body.append(el);
  mounted.push(el);

  return el;
}

afterEach(() => {
  while (mounted.length > 0) mounted.pop()?.remove();
  bindings({});
});

// ===================================================================

describe('buildKeyString', () => {
  it('uppercases a bare printable key', () => {
    expect(buildKeyString(new KeyboardEvent('keydown', { key: 'n' }))).toBe(
      'N',
    );
  });

  it('orders modifiers Ctrl, Alt, Shift regardless of press order', () => {
    const e = new KeyboardEvent('keydown', {
      key: 'f',
      shiftKey: true,
      altKey: true,
      ctrlKey: true,
    });

    expect(buildKeyString(e)).toBe('Ctrl+Alt+Shift+F');
  });

  it('folds Meta into Ctrl so macOS and Linux share one binding table', () => {
    const meta = new KeyboardEvent('keydown', { key: 'f', metaKey: true });
    const ctrl = new KeyboardEvent('keydown', { key: 'f', ctrlKey: true });

    expect(buildKeyString(meta)).toBe(buildKeyString(ctrl));
  });

  it('aliases arrows and space to their canonical names', () => {
    const names = ['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', ' '].map(
      (key) => buildKeyString(new KeyboardEvent('keydown', { key })),
    );

    expect(names).toEqual(['Up', 'Down', 'Left', 'Right', 'Space']);
  });

  it('leaves multi-character named keys alone', () => {
    expect(
      buildKeyString(new KeyboardEvent('keydown', { key: 'Escape' })),
    ).toBe('Escape');
  });

  it('returns nothing for a bare modifier press', () => {
    const bare = ['Control', 'Alt', 'Shift', 'Meta'].map((key) =>
      buildKeyString(new KeyboardEvent('keydown', { key })),
    );

    expect(bare).toEqual(['', '', '', '']);
  });
});

// ===================================================================

describe('shortcuts store: lookup', () => {
  beforeEach(() => {
    bindings({
      'player.playPause': 'Space',
      'player.next': 'Ctrl+Right',
      'tracklist.play': 'Enter',
      'tracklist.delete': 'Delete',
    });
  });

  it('resolves an action to its key', () => {
    expect(shortcutsStore.getKeyForAction('player.next')).toBe('Ctrl+Right');
  });

  it('reverse-resolves a key to its global action', () => {
    expect(shortcutsStore.getActionForKey('Space')).toBe('player.playPause');
  });

  it('does not resolve a panel binding from global scope', () => {
    // `tracklist.` is a panel prefix: Enter must do nothing unless the
    // track list has focus.
    expect(shortcutsStore.getActionForKey('Enter')).toBeUndefined();
  });

  it('resolves a panel binding when the matching scope is supplied', () => {
    expect(shortcutsStore.getActionForKey('Enter', 'panel:tracklist')).toBe(
      'tracklist.play',
    );
  });

  it('falls back to global inside a panel scope', () => {
    expect(shortcutsStore.getActionForKey('Space', 'panel:tracklist')).toBe(
      'player.playPause',
    );
  });

  it('reports a conflict, excluding the action being rebound', () => {
    expect(
      shortcutsStore.findConflict('Space', 'global', 'player.next'),
    ).toEqual({ action: 'player.playPause', key: 'Space' });
  });

  it('does not report an action conflicting with itself', () => {
    expect(
      shortcutsStore.findConflict('Space', 'global', 'player.playPause'),
    ).toBeNull();
  });
});

// ===================================================================

describe('shortcut dispatch: scope', () => {
  beforeEach(() => {
    bindings({
      'player.playPause': 'Space',
      'tracklist.play': 'Enter',
    });
  });

  it('dispatches from global scope', () => {
    press(' ');

    expect(calls('queue.Queue.Play')).toHaveLength(1);
  });

  it('preventDefaults a key it handled', () => {
    expect(press(' ').defaultPrevented).toBe(true);
  });

  it('leaves an unbound key alone', () => {
    expect(press('q').defaultPrevented).toBe(false);
  });

  it('suppresses shortcuts while a text input has focus', () => {
    const input = mount(document.createElement('input'));

    input.type = 'text';
    input.focus();
    press(' ');

    expect(calls('queue.Queue.Play')).toHaveLength(0);
  });

  it('suppresses shortcuts inside a contenteditable', () => {
    const div = mount(document.createElement('div'));

    div.contentEditable = 'true';
    div.tabIndex = 0;
    div.focus();
    press(' ');

    expect(calls('queue.Queue.Play')).toHaveLength(0);
  });

  it('lets a checkbox through — it is not a text input', () => {
    const input = mount(document.createElement('input'));

    input.type = 'checkbox';
    input.focus();
    press(' ');

    expect(calls('queue.Queue.Play')).toHaveLength(1);
  });

  it('blurs the input on Escape, and only on Escape', () => {
    const input = mount(document.createElement('input'));

    input.type = 'search';
    input.focus();
    press('Escape');

    expect(document.activeElement).not.toBe(input);
  });

  it('finds a text input nested in a shadow root', () => {
    // document.activeElement stops at the shadow host, so a service
    // that did not walk the chain would see <div> and fire the shortcut.
    const host = mount(document.createElement('div'));
    const root = host.attachShadow({ mode: 'open' });
    const input = document.createElement('input');

    input.type = 'text';
    root.append(input);
    input.focus();
    press(' ');

    expect(calls('queue.Queue.Play')).toHaveLength(0);
  });

  it('resolves a panel scope from a data-shortcut-scope ancestor', () => {
    const panel = mount(document.createElement('div'));
    const button = document.createElement('button');

    panel.dataset['shortcutScope'] = 'tracklist';
    panel.append(button);
    button.focus();

    let fired = 0;
    const listener = (): void => {
      fired += 1;
    };

    document.addEventListener('shortcut:tracklist-play', listener);
    press('Enter');
    document.removeEventListener('shortcut:tracklist-play', listener);

    expect(fired).toBe(1);
  });

  it('crosses a shadow boundary looking for the panel scope', () => {
    const panel = mount(document.createElement('div'));
    const inner = document.createElement('div');
    const root = inner.attachShadow({ mode: 'open' });
    const button = document.createElement('button');

    panel.dataset['shortcutScope'] = 'tracklist';
    panel.append(inner);
    root.append(button);
    button.focus();

    let fired = 0;
    const listener = (): void => {
      fired += 1;
    };

    document.addEventListener('shortcut:tracklist-play', listener);
    press('Enter');
    document.removeEventListener('shortcut:tracklist-play', listener);

    expect(fired).toBe(1);
  });
});

// ===================================================================

describe('shortcut dispatch: actions', () => {
  it('toggles between pause and play based on cached player state', () => {
    bindings({ 'player.playPause': 'Space' });

    emit(Events.PlaybackStateChanged, { state: 'playing' });
    press(' ');
    emit(Events.PlaybackStateChanged, { state: 'paused' });
    press(' ');

    expect(calls().map((c) => c.path)).toEqual([
      'player.Player.Pause',
      'queue.Queue.Play',
    ]);
  });

  it('steps the volume by a fixed amount in each direction', () => {
    bindings({ 'player.volumeUp': 'Up', 'player.volumeDown': 'Down' });

    press('ArrowUp');
    press('ArrowDown');

    expect(calls('player.Player.ChangeVolume').map((c) => c.args)).toEqual([
      [5],
      [-5],
    ]);
  });

  it('clamps a forward seek to the track length', async () => {
    bindings({ 'player.seekForward': 'Right' });
    stub('player.Player.CurrentPositionSeconds', 98);
    stub('player.Player.TrackLengthInSeconds', 100);

    press('ArrowRight');
    await new Promise<void>((r) => {
      setTimeout(r, 0);
    });

    expect(lastArgs('player.Player.Seek')).toEqual([100]);
  });

  it('clamps a backward seek at zero', async () => {
    bindings({ 'player.seekBack': 'Left' });
    stub('player.Player.CurrentPositionSeconds', 2);

    press('ArrowLeft');
    await new Promise<void>((r) => {
      setTimeout(r, 0);
    });

    expect(lastArgs('player.Player.Seek')).toEqual([0]);
  });

  it('toggles the queue panel open and closed', () => {
    bindings({ 'nav.queue': 'Q' });

    const panel = mount(document.createElement('div'));

    panel.id = 'queue-panel';

    press('q');
    const opened = panel.hasAttribute('open');

    press('q');

    expect([opened, panel.hasAttribute('open')]).toEqual([true, false]);
  });

  it('broadcasts select-all as a document event', () => {
    bindings({ 'app.selectAll': 'Ctrl+A' });

    let fired = 0;
    const listener = (): void => {
      fired += 1;
    };

    document.addEventListener('shortcut:select-all', listener);
    press('a', { ctrlKey: true });
    document.removeEventListener('shortcut:select-all', listener);

    expect(fired).toBe(1);
  });

  it('ignores an action name the dispatcher does not know', () => {
    bindings({ 'player.teleport': 'T' });

    press('t');

    expect(calls()).toEqual([]);
  });
});

// ===================================================================

describe('shortcuts store: writes', () => {
  it('sends a single rebind to the backend', async () => {
    await shortcutsStore.updateBinding('player.next', 'Ctrl+N');

    expect(lastArgs('config.Config.SetShortcut')).toEqual([
      'player.next',
      'Ctrl+N',
    ]);
  });

  it('sends a whole table at once', async () => {
    await shortcutsStore.setAll({ 'player.next': 'N' });

    expect(lastArgs('config.Config.SetShortcuts')).toEqual([
      { 'player.next': 'N' },
    ]);
  });

  it('does not update its cache optimistically', async () => {
    bindings({ 'player.next': 'Ctrl+Right' });
    await shortcutsStore.updateBinding('player.next', 'Ctrl+N');

    // The backend is the only writer; the cache waits for the
    // ShortcutsConfigChanged push.
    expect(shortcutsStore.getKeyForAction('player.next')).toBe('Ctrl+Right');
  });
});
