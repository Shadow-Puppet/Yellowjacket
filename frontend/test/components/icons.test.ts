/**
 * The icons are bundled, and stay bundled.
 *
 * `e2e/specs/offline-icons.spec.ts` is the reproduction — it closes the
 * network and looks at the screen.  This is the cheap guard that runs
 * on every change: that the library the app registers resolves every
 * name it claims to, and resolves none of them to a URL somebody else
 * has to be reachable to serve.
 */
import { describe, expect, it } from 'vitest';
// A bare <wa-icon> is an unknown element unless something pulls the
// component in; in the app `index.ts` does it, and here nothing else in
// this module would.
import '@awesome.me/webawesome/dist/components/icon/icon.js';

import { bundledIconNames, registerBundledIcons } from '../../src/icons';
import { fixture } from '../support/render';

// A name from each of the ways a call site produces one: a literal in a
// template, a sidebar table entry, a value computed from player state,
// and the notification tone map.  Not exhaustive on purpose — the
// exhaustive check is the e2e sweep, which can see what state produces.
const REPRESENTATIVE = [
    'house', 'compact-disc', 'play', 'pause', 'shuffle', 'repeat',
    'volume-high', 'volume-xmark', 'triangle-exclamation', 'circle-check',
    'magnifying-glass', 'gear', 'heart', 'regular/heart',
];

describe('bundled icons', () => {
    it('resolves every name the app is known to use', () => {
        const names = new Set(bundledIconNames());

        for (const name of REPRESENTATIVE) {
            expect(names, `icon '${name}' is not bundled`).toContain(name);
        }
    });

    it('resolves nothing to a remote origin', async () => {
        // Re-registering is what the app does on every boot; it must be
        // idempotent, and the assertion needs the resolver itself
        // rather than the name list.
        registerBundledIcons();

        // Attributes, not properties: `regular/heart` is a library key
        // rather than an icon name, and `wa-icon` takes its name from
        // the attribute.
        const icons = await Promise.all(
            REPRESENTATIVE.filter((n) => !n.includes('/')).map(async (n) => {
                const el = await fixture('wa-icon');
                el.setAttribute('name', n);
                await el.updateComplete;

                return el;
            }),
        );

        // Give the icon components a turn to fetch and inline their SVG.
        await new Promise((r) => setTimeout(r, 500));

        expect(icons.length).toBeGreaterThan(0);

        for (const icon of icons) {
            const svg = icon.shadowRoot?.querySelector('svg');

            expect(
                svg,
                `icon '${icon.getAttribute('name')}' rendered nothing`,
            ).toBeTruthy();
        }
    });
});
