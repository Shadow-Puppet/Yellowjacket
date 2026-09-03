/**
 * #175: the first-run wizard's dismissal follows the library existing,
 * not its own button being pressed.
 *
 * The wizard is a modal that blocks every pointer event, so a library
 * arriving by another route — Settings, a direct call — used to leave
 * it up over an app that was already set up. `LibraryAdded` is emitted
 * by `AddLibrary` whoever calls it, which is what makes one
 * subscription the whole fix.
 */
import { beforeEach, describe, expect, it } from 'vitest';

import { Events } from '../../src/events';
import { emit, stub } from '../support/harness';
import { fixture, shadow, shadowAll } from '../support/render';
import { wails } from '../support/wails-fake';

import '@components/first-run-wizard/first-run-wizard';

import type { FirstRunWizard } from '@components/first-run-wizard/first-run-wizard';

/** A library row, as `GetAllLibrariesWithTrackCounts` returns one. */
const aLibrary = {
    id: 1,
    name: 'Music',
    path: '/home/logan/Music',
    trackCount: 9,
};

/** Mount the wizard on a fresh install: no libraries yet. */
async function wizardOnAFreshInstall(): Promise<FirstRunWizard> {
    stub('library.Library.GetAllLibrariesWithTrackCounts', []);

    return fixture<FirstRunWizard>('first-run-wizard');
}

/** Whether the wizard is rendering its modal at all. */
function isShowing(el: FirstRunWizard): boolean {
    return shadow(el, 'wa-dialog') !== null;
}

beforeEach(() => {
    stub('library.Library.AddLibrary', aLibrary);
});

describe('first-run-wizard', () => {
    it('shows on a fresh install and stays up until a library exists', async () => {
        const el = await wizardOnAFreshInstall();

        expect(isShowing(el)).toBe(true);
    });

    it('stays hidden when a library is already configured', async () => {
        stub('library.Library.GetAllLibrariesWithTrackCounts', [aLibrary]);

        const el = await fixture<FirstRunWizard>('first-run-wizard');

        expect(isShowing(el)).toBe(false);
    });

    it('dismisses when a library appears by another route', async () => {
        const el = await wizardOnAFreshInstall();

        expect(isShowing(el)).toBe(true);

        emit(Events.LibraryAdded, aLibrary);
        await el.updateComplete;

        expect(isShowing(el)).toBe(false);
    });

    it('does not raise itself when a library arrives while it is asking', async () => {
        // The read is still in flight when the event lands, so its
        // answer — an empty list — is stale by the time it returns.
        let answer: (libraries: unknown[]) => void = () => {};

        stub(
            'library.Library.GetAllLibrariesWithTrackCounts',
            () =>
                new Promise((resolve) => {
                    answer = resolve;
                }),
        );

        const el = await fixture<FirstRunWizard>('first-run-wizard');

        emit(Events.LibraryAdded, aLibrary);
        answer([]);

        await el.updateComplete;
        await new Promise((r) => setTimeout(r, 0));
        await el.updateComplete;

        expect(isShowing(el)).toBe(false);
    });

    it('still dismisses through its own Get Started button', async () => {
        stub('frontendutil.FrontendUtil.HasNativeDirectoryPicker', true);
        stub('frontendutil.FrontendUtil.DirectoryPicker', '/home/logan/Music');

        const { resetDirectoryPickerCache } = await import(
            '@utils/pick-directory'
        );

        resetDirectoryPickerCache();

        const el = await wizardOnAFreshInstall();
        const [choose, finish] = shadowAll<HTMLButtonElement>(el, '.btn');

        choose?.click();
        await new Promise((r) => setTimeout(r, 0));
        await el.updateComplete;

        finish?.click();
        await new Promise((r) => setTimeout(r, 0));
        await el.updateComplete;

        expect(
            wails.calls.filter((c) => c.path === 'library.Library.AddLibrary'),
        ).toHaveLength(1);
        expect(isShowing(el)).toBe(false);
    });
});
