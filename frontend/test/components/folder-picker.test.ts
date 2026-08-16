import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { wails } from '../support/wails-fake';

import type { FolderPicker } from '@components/folder-picker/folder-picker';

const listings: Record<string, unknown> = {
  '/storage/emulated/0': {
    path: '/storage/emulated/0',
    parent: '/storage/emulated',
    entries: [
      { name: 'Music', path: '/storage/emulated/0/Music' },
      { name: 'Podcasts', path: '/storage/emulated/0/Podcasts' },
    ],
  },
  '/storage/emulated/0/Music': {
    path: '/storage/emulated/0/Music',
    parent: '/storage/emulated/0',
    entries: [],
  },
};

/**
 * `pickDirectory` imports the picker's chunk before it can create the
 * element, so the element does not exist on the turn the call is made.
 * That is deliberate -- mounting a dialog and calling showModal() in
 * one update is the trap `index.ts` documents -- so the test waits for
 * it rather than assuming it is synchronous.
 */
async function host(): Promise<FolderPicker> {
  for (let i = 0; i < 50; i++) {
    const el = document.querySelector<FolderPicker>('folder-picker');

    if (el) {
      await el.updateComplete;

      return el;
    }

    await new Promise((r) => setTimeout(r, 10));
  }

  throw new Error('folder-picker did not mount itself');
}

function click(el: FolderPicker, testid: string): void {
  el.shadowRoot
    ?.querySelector<HTMLButtonElement>(`[data-testid="${testid}"]`)
    ?.click();
}

beforeEach(() => {
  wails.stub('frontendutil.FrontendUtil.HasNativeDirectoryPicker', true);
  wails.stub('frontendutil.FrontendUtil.DirectoryPicker', '/home/logan/Music');
  wails.stub('frontendutil.FrontendUtil.DefaultBrowseRoot', '/storage/emulated/0');
  wails.stub('frontendutil.FrontendUtil.ListDirectories', (path: string) => {
    const listing = listings[path || '/storage/emulated/0'];

    if (!listing) throw new Error('permission denied');

    return listing;
  });
});

afterEach(() => {
  document.querySelector('folder-picker')?.remove();
  wails.reset();
});

describe('pickDirectory', () => {
  it('uses the platform dialog off Android', async () => {
    const { pickDirectory, resetDirectoryPickerCache } = await import(
      '@utils/pick-directory'
    );

    resetDirectoryPickerCache();

    await expect(pickDirectory()).resolves.toBe('/home/logan/Music');
    expect(document.querySelector('folder-picker')).toBeNull();
  });

  it('normalises the desktop dialog\u2019s empty string to null', async () => {
    wails.stub('frontendutil.FrontendUtil.DirectoryPicker', '');

    const { pickDirectory, resetDirectoryPickerCache } = await import(
      '@utils/pick-directory'
    );

    resetDirectoryPickerCache();

    await expect(pickDirectory()).resolves.toBeNull();
  });

  it('browses in-app on Android, and never opens the platform dialog', async () => {
    wails.stub('frontendutil.FrontendUtil.HasNativeDirectoryPicker', false);

    const { pickDirectory, resetDirectoryPickerCache } = await import(
      '@utils/pick-directory'
    );

    resetDirectoryPickerCache();
    const answer = pickDirectory();

    const el = await host();

    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    click(el, 'folder-picker-select');

    await expect(answer).resolves.toBe('/storage/emulated/0');
  });

  it('resolves null when the browser is cancelled', async () => {
    wails.stub('frontendutil.FrontendUtil.HasNativeDirectoryPicker', false);

    const { pickDirectory, resetDirectoryPickerCache } = await import(
      '@utils/pick-directory'
    );

    resetDirectoryPickerCache();
    const answer = pickDirectory();

    const el = await host();

    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    el.shadowRoot
      ?.querySelectorAll<HTMLButtonElement>('.actions button')[0]
      ?.click();

    await expect(answer).resolves.toBeNull();
  });

  it('descends into a folder and returns the one it is showing', async () => {
    wails.stub('frontendutil.FrontendUtil.HasNativeDirectoryPicker', false);

    const { pickDirectory, resetDirectoryPickerCache } = await import(
      '@utils/pick-directory'
    );

    resetDirectoryPickerCache();
    const answer = pickDirectory();

    const el = await host();

    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    const music = [
      ...(el.shadowRoot?.querySelectorAll<HTMLButtonElement>(
        '[data-testid="folder-picker-list"] button',
      ) ?? []),
    ].find((b) => b.textContent?.includes('Music'));

    music?.click();
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    click(el, 'folder-picker-select');

    await expect(answer).resolves.toBe('/storage/emulated/0/Music');
  });

  /**
   * A directory that cannot be read is not a failed picker. Android's
   * storage root holds directories no app may enter, and stranding the
   * user in an empty dialog with no way back is worse than saying so.
   */
  it('stays put and explains when a folder cannot be opened', async () => {
    wails.stub('frontendutil.FrontendUtil.HasNativeDirectoryPicker', false);

    const { pickDirectory, resetDirectoryPickerCache } = await import(
      '@utils/pick-directory'
    );

    resetDirectoryPickerCache();
    const answer = pickDirectory();

    const el = await host();

    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    // 'Podcasts' has no listing, so ListDirectories rejects.
    const bad = [
      ...(el.shadowRoot?.querySelectorAll<HTMLButtonElement>(
        '[data-testid="folder-picker-list"] button',
      ) ?? []),
    ].find((b) => b.textContent?.includes('Podcasts'));

    bad?.click();
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    expect(el.shadowRoot?.querySelector('[role="alert"]')).toBeTruthy();
    expect(
      el.shadowRoot?.querySelector('[data-testid="folder-picker-path"]')
        ?.textContent,
    ).toContain('/storage/emulated/0');

    click(el, 'folder-picker-select');
    await expect(answer).resolves.toBe('/storage/emulated/0');
  });
});
