/**
 * Every `wa-dialog` in this app was an unnamed dialog.
 *
 * `a11y.md` lists all of them under "what is already correct" and notes
 * that every call site passes a `label`. Both true, and the label never
 * reached the accessibility tree: Web Awesome renders it into an
 * `<h2 id="title">` in the same shadow root as the native `<dialog>` and
 * never points `aria-labelledby` at it.
 *
 * What this tier can and cannot check is worth stating, because the
 * distinction is the whole reason the e2e spec also exists. It queries
 * shadow roots directly, so it can assert the *wiring* — the IDREF, and
 * that it resolves to a heading carrying the label. It cannot compute an
 * accessible name; `getByRole('dialog', {name})` is Playwright's job,
 * and `e2e/specs/dialog-names.spec.ts` does it there.
 */
import { describe, expect, it } from 'vitest';

import '@awesome.me/webawesome/dist/components/dialog/dialog.js';
import { confirmAction } from '@components/confirm-dialog/confirm-dialog';
import '@components/shortcuts-overlay/shortcuts-overlay';
import { fixture, shadow } from '@test/support/render';
import { nameDialog, nameDialogsIn } from '@utils/name-dialog';

/** The `<dialog>` Web Awesome renders inside a `<wa-dialog>`. */
function native(host: Element | null): HTMLElement | null {
  return host?.shadowRoot?.querySelector<HTMLElement>('dialog[part~="dialog"]')
    ?? null;
}

/** What an IDREF on that dialog actually resolves to, in its own root. */
function labelledByText(host: Element | null): string | null {
  const dialog = native(host);
  const id = dialog?.getAttribute('aria-labelledby');

  if (!id) return null;

  return host?.shadowRoot?.getElementById(id)?.textContent?.trim() ?? null;
}

/** Mount a bare `wa-dialog`, name it, and wait for both updates. */
async function namedDialog(
  attrs: Record<string, string>,
): Promise<HTMLElement> {
  const el = document.createElement('wa-dialog');

  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);

  document.body.append(el);
  await (el as HTMLElement & { updateComplete: Promise<unknown> })
    .updateComplete;

  nameDialog(el);
  await (el as HTMLElement & { updateComplete: Promise<unknown> })
    .updateComplete;

  return el;
}

describe('naming a wa-dialog', () => {
  it('is unnamed until something names it — which is the bug', async () => {
    const el = document.createElement('wa-dialog');

    el.setAttribute('label', 'Nobody announces this');
    document.body.append(el);
    await (el as HTMLElement & { updateComplete: Promise<unknown> })
      .updateComplete;

    const dialog = native(el);

    expect(dialog).toBeTruthy();
    expect(dialog!.getAttribute('aria-labelledby')).toBeNull();
    expect(dialog!.getAttribute('aria-label')).toBeNull();

    el.remove();
  });

  it('points the dialog at the heading carrying the label', async () => {
    const el = await namedDialog({ label: 'Duplicate Tracks Found' });

    expect(labelledByText(el)).toBe('Duplicate Tracks Found');

    el.remove();
  });

  it('falls back to aria-label when there is no header to point at', async () => {
    // `first-run-wizard` is the one caller using `without-header`, so
    // the IDREF path has no `<h2>` to name and the string is copied.
    const el = await namedDialog({
      label: 'Welcome to YellowJacket',
      'without-header': '',
    });

    expect(native(el)!.getAttribute('aria-labelledby')).toBeNull();
    expect(native(el)!.getAttribute('aria-label')).toBe(
      'Welcome to YellowJacket',
    );

    el.remove();
  });

  it('is a no-op on anything that is not a wa-dialog', () => {
    // The bounded failure mode: if Web Awesome moves this structure the
    // query misses, nothing is written, and the dialog is exactly as
    // unnamed as it is today.
    expect(() => nameDialog(document.createElement('div'))).not.toThrow();
    expect(() => nameDialogsIn(null)).not.toThrow();
  });
});

describe('the dialogs the app actually opens', () => {
  it('names confirm-dialog from the title the caller passed', async () => {
    const answer = confirmAction({
      title: 'Remove “Live Sessions”?',
      message: 'The library entry is removed; the files are not.',
    });

    const el = document.querySelector('confirm-dialog')!;

    await (el as HTMLElement & { updateComplete: Promise<unknown> })
      .updateComplete;

    const dialog = shadow(el, 'wa-dialog');

    await (dialog as HTMLElement & { updateComplete: Promise<unknown> })
      .updateComplete;

    expect(labelledByText(dialog)).toBe('Remove “Live Sessions”?');

    el.shadowRoot
      ?.querySelector<HTMLButtonElement>('[data-testid="confirm-cancel"]')
      ?.click();
    await answer;
  });

  it('names the shortcuts overlay, which renders its dialog on demand', async () => {
    const el = await fixture('shortcuts-overlay');

    document.dispatchEvent(new CustomEvent('shortcut:app-shortcuts'));
    await (el as HTMLElement & { updateComplete: Promise<unknown> })
      .updateComplete;

    const dialog = shadow(el, 'wa-dialog');

    await (dialog as HTMLElement & { updateComplete: Promise<unknown> })
      .updateComplete;

    expect(labelledByText(dialog)).toBe('Keyboard Shortcuts');
  });
});
