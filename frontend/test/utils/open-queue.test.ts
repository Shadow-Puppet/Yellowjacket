/**
 * Opening the queue is a *navigation* where the queue is a screen, and
 * an *attribute* where it is a column (#55).
 *
 * This is the one decision in that change, so it is pinned at the tier
 * that can state it without a shell: the mode is read off the panel's
 * own `overlay` attribute — which #24 computes from the measured widths
 * — and never from a viewport breakpoint. A breakpoint would silently
 * assume the default 320px panel and be wrong by up to 180px for a user
 * who has dragged it wide, in the direction that hurts.
 *
 * What this tier cannot see is the other half: that the entry is
 * unwound when the panel closes, which lives in the shell's mutation
 * observer. `e2e/specs/queue-as-a-screen.spec.ts` is where that is
 * asserted, and it is asserted as *two* back presses rather than one.
 */
import { afterEach, describe, expect, it } from 'vitest';

import { openQueue, queueIsAScreen } from '@utils/open-queue';

function panel(overlay: boolean): HTMLElement {
    const el = document.createElement('div');

    el.id = 'queue-panel';
    if (overlay) el.setAttribute('overlay', '');
    document.body.appendChild(el);

    return el;
}

function recordNavigations(): string[] {
    const seen: string[] = [];
    const listener = (e: Event) => {
        seen.push((e as CustomEvent).detail.view);
    };

    document.addEventListener('navigate', listener);
    cleanup.push(() => document.removeEventListener('navigate', listener));

    return seen;
}

const cleanup: Array<() => void> = [];

afterEach(() => {
    while (cleanup.length) cleanup.pop()!();
    document.getElementById('queue-panel')?.remove();
});

describe('opening the queue', () => {
    it('navigates where the queue covers the content', () => {
        const el = panel(true);
        const seen = recordNavigations();

        expect(queueIsAScreen()).toBe(true);

        openQueue();

        expect(seen).toEqual(['queue']);
        // The shell answers the navigation by setting the attribute, so
        // the helper deliberately does *not* set it as well: two
        // mechanisms for one fact is two things to keep in step, which
        // is what `now-playing-view`'s copy of this button was.
        expect(el.hasAttribute('open')).toBe(false);
    });

    it('sets the attribute where the queue is a column', () => {
        const el = panel(false);
        const seen = recordNavigations();

        expect(queueIsAScreen()).toBe(false);

        openQueue();

        // A column is a thing the user docked. Back must not undock it,
        // so it is not a history entry and therefore not a navigation.
        expect(seen).toEqual([]);
        expect(el.hasAttribute('open')).toBe(true);
    });

    it('follows the panel rather than the viewport', () => {
        const el = panel(false);
        const seen = recordNavigations();

        openQueue();
        expect(seen).toEqual([]);

        // Nothing about the window changed; the panel got wider, which
        // is exactly the case a media query cannot express.
        el.removeAttribute('open');
        el.setAttribute('overlay', '');

        openQueue();
        expect(seen).toEqual(['queue']);
    });

    it('says the queue is not a screen when there is no panel at all', () => {
        expect(queueIsAScreen()).toBe(false);
        expect(() => openQueue()).not.toThrow();
    });
});
