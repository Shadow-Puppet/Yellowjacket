/**
 * The surface itself: one component, four presentations.
 *
 * The assertions are on what a user (and Playwright) can see — the
 * sentence, the action, the dismiss — rather than on which element
 * happens to hold them, because the whole point of the shared notice is
 * that a caller does not choose the markup.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import '@components/notifications/notification-host';
import '@components/notifications/inline-notice';
import { notificationStore } from '@store/notification-store';
import { flush } from '@test/support/harness';
import { fixture, shadow, shadowAll, text } from '@test/support/render';
import type { LitElement } from 'lit';

/** Let the store's microtask notification reach the component. */
async function settle(el: LitElement): Promise<void> {
  await flush();
  await el.updateComplete;
}

describe('<notification-host>', () => {
  beforeEach(() => {
    notificationStore.clear();
  });

  it('says nothing when there is nothing to say', async () => {
    const el = await fixture('notification-host');

    expect(shadow(el, '[data-testid="notification-stack"]')).toBeNull();
  });

  it('renders a persistent failure with the action it offers', async () => {
    const el = await fixture<LitElement>('notification-host');

    notificationStore.persistent({
      text: 'The scan did not start.',
      action: { label: 'Try again', run: () => undefined },
    });
    await settle(el);

    expect([
      text(el, '[data-testid="notification"]'),
      text(el, '[data-testid="notification-action"]'),
    ]).toEqual([
      expect.stringContaining('The scan did not start.'),
      'Try again',
    ]);
  });

  it('stacks persistent above transient, so a toast never buries an answer', async () => {
    const el = await fixture<LitElement>('notification-host');

    notificationStore.transient({ text: 'That favourite was undone.' });
    notificationStore.persistent({ text: 'The scan did not start.' });
    await settle(el);

    expect(
      shadowAll(el, '[data-testid="notification"]').map((n) =>
        n.getAttribute('data-level'),
      ),
    ).toEqual(['persistent', 'transient']);
  });

  it('dismisses on request', async () => {
    const el = await fixture<LitElement>('notification-host');

    notificationStore.transient({ text: 'That favourite was undone.' });
    await settle(el);

    shadow<HTMLButtonElement>(el, '.notice-dismiss')?.click();
    await settle(el);

    expect(shadow(el, '[data-testid="notification"]')).toBeNull();
  });

  it('puts a blocking failure in a dialog, which has to be answered', async () => {
    const el = await fixture<LitElement>('notification-host');

    notificationStore.blocking({
      title: 'This folder was only partly retagged',
      text: '3 of 9 tracks were written.',
    });
    await settle(el);

    expect(shadow(el, '[data-testid="notification-blocking"]')).not.toBeNull();
  });

  it('leaves inline messages to the region that failed', async () => {
    const el = await fixture<LitElement>('notification-host');

    notificationStore.inline('player', { text: 'Could not seek.' });
    await settle(el);

    expect(shadow(el, '[data-testid="notification"]')).toBeNull();
  });
});

describe('<inline-notice>', () => {
  beforeEach(() => {
    notificationStore.clear();
  });

  it('renders only its own region', async () => {
    const el = await fixture<LitElement>('inline-notice', { region: 'player' });

    notificationStore.inline('explore', { text: 'The search did not answer.' });
    notificationStore.inline('player', { text: 'Could not seek.' });
    await settle(el);

    expect(text(el, '[data-testid="notification"]')).toContain(
      'Could not seek.',
    );
  });

  it('is a live region, because nothing moved focus to it', async () => {
    const el = await fixture<LitElement>('inline-notice', { region: 'player' });

    notificationStore.inline('player', { text: 'Could not seek.' });
    await settle(el);

    expect(shadow(el, '[aria-live="polite"]')).not.toBeNull();
  });
});
