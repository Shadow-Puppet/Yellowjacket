/**
 * The one "are you sure?".
 *
 * Three destructive actions had none at all — including a multi-select
 * loop that deleted N playlists on one click (errors.M6, M7, m4) — so
 * what matters here is that the promise a call site awaits cannot
 * resolve true unless somebody said so.
 */
import { describe, expect, it } from 'vitest';

import {
  confirmAction,
  type ConfirmDialog,
} from '@components/confirm-dialog/confirm-dialog';

/** The singleton the helper attaches to the document on first use. */
function host(): ConfirmDialog {
  const el = document.querySelector<ConfirmDialog>('confirm-dialog');

  if (!el) throw new Error('confirm-dialog did not mount itself');

  return el;
}

async function press(testid: string): Promise<void> {
  const el = host();

  await el.updateComplete;
  el.shadowRoot?.querySelector<HTMLButtonElement>(`[data-testid="${testid}"]`)
    ?.click();
  await el.updateComplete;
}

describe('confirmAction', () => {
  it('resolves true only when the user accepts', async () => {
    const answer = confirmAction({
      title: 'Delete “Chill”?',
      message: 'The playlist is deleted; the audio files are not.',
      confirmLabel: 'Delete playlist',
      danger: true,
    });

    await press('confirm-accept');

    await expect(answer).resolves.toBe(true);
  });

  it('resolves false on cancel, which is the default answer', async () => {
    const answer = confirmAction({ title: 'Delete?', message: 'Gone for good.' });

    await press('confirm-cancel');

    await expect(answer).resolves.toBe(false);
  });

  it('says what will happen before asking', async () => {
    const answer = confirmAction({
      title: 'Remove “Lidarr”?',
      message: 'YellowJacket will stop using this client.',
      impact: 'Its stored credentials are deleted and cannot be recovered.',
    });
    const el = host();

    await el.updateComplete;

    expect(el.shadowRoot?.textContent).toContain('cannot be recovered');

    await press('confirm-cancel');
    await answer;
  });

  it('does not leave a second question hanging', async () => {
    const first = confirmAction({ title: 'One?', message: 'First.' });
    const second = confirmAction({ title: 'Two?', message: 'Second.' });

    await press('confirm-accept');

    // The first was superseded and answered "no" rather than never
    // settling — a call site awaiting it would otherwise hang forever.
    await expect(Promise.all([first, second])).resolves.toEqual([false, true]);
  });
});
