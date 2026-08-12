/**
 * The key story, told once.
 *
 * Decision 1 keeps the unmodified single-key bindings, and Settings was
 * the only place they were written down — three of the four categories
 * of them, so the autotag keys were written down nowhere at all.
 */
import { describe, expect, it } from 'vitest';

import '@components/shortcuts-overlay/shortcuts-overlay';
import { emit } from '@test/support/harness';
import { Events } from '../../src/events';
import { fixture, shadow, shadowAll } from '@test/support/render';

/** Open it the way the shortcut service does. */
async function openOverlay(el: HTMLElement): Promise<void> {
  document.dispatchEvent(new CustomEvent('shortcut:app-shortcuts'));
  await (el as HTMLElement & { updateComplete: Promise<boolean> })
    .updateComplete;
}

describe('<shortcuts-overlay>', () => {
  it('renders nothing until it is asked for', async () => {
    const el = await fixture('shortcuts-overlay');

    expect(shadow(el, 'wa-dialog')).toBeNull();
  });

  it('opens on the shortcut event, and stays open on a second one', async () => {
    const el = await fixture('shortcuts-overlay');

    await openOverlay(el);
    expect(shadow(el, '[data-testid="shortcuts-overlay"]')).toBeTruthy();

    // Deliberately not a toggle: a dialog owns every unmodified key
    // while it is up, so a second `?` never reaches the service and a
    // toggle would be a promise the shortcut layer cannot keep.
    await openOverlay(el);
    expect(shadow(el, '[data-testid="shortcuts-overlay"]')).toBeTruthy();
  });

  it('lists the autotag keys, which Settings never showed', async () => {
    const el = await fixture('shortcuts-overlay');

    await openOverlay(el);

    const headings = shadowAll(el, 'h3').map((h) =>
      h.textContent!.trim().split(' ')[0],
    );

    expect(headings).toContain('Autotag');
  });

  it('shows the bound key, not the default it shipped with', async () => {
    const el = await fixture('shortcuts-overlay');

    emit(Events.ShortcutsConfigChanged, { 'player.playPause': 'K' });
    await openOverlay(el);

    const playPause = shadowAll(el, '.row').find((row) =>
      row.textContent!.includes('Play / Pause'),
    );

    expect(playPause?.querySelector('kbd')?.textContent).toBe('K');
  });

  it('splits a combination into one key each', async () => {
    const el = await fixture('shortcuts-overlay');

    emit(Events.ShortcutsConfigChanged, { 'app.selectAll': 'Ctrl+A' });
    await openOverlay(el);

    const selectAll = shadowAll(el, '.row').find((row) =>
      row.textContent!.includes('Select All'),
    );

    expect(
      [...selectAll!.querySelectorAll('kbd')].map((k) => k.textContent),
    ).toEqual(['Ctrl', 'A']);
  });
});
