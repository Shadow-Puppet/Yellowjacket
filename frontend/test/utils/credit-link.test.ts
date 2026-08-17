/**
 * A track credited to more than one artist has one navigable artist in
 * this app and the rest are punctuation. `creditLink` is the fix: it
 * renders a credit as one link per credited artist with the join
 * phrases as plain text between them.
 *
 * The rule these tests exist to pin is that join phrases are
 * **assembly** instructions, not disassembly instructions — the credit
 * is built from its parts, never found by searching a name inside a
 * credit string. Measured on a real library, the stored credit text and
 * the catalog's parts disagree for about one in three multi-artist
 * credits, so a search would miss or match the wrong span.
 */
import { describe, expect, it } from 'vitest';
import { html, render } from 'lit';

import { creditLink, creditText, type CreditPart } from '@utils/explore-link';

const TUPAC = '11111111-1111-4111-8111-111111111111';
const SNOOP = '22222222-2222-4222-8222-222222222222';

const parts: CreditPart[] = [
    { creditedName: '2Pac', artistMbid: TUPAC, joinPhrase: ' feat. ' },
    { creditedName: 'Snoop Dogg', artistMbid: SNOOP, joinPhrase: '' },
];

function renderToEl(value: unknown): HTMLElement {
    const host = document.createElement('div');
    render(html`${value}`, host);

    return host;
}

describe('creditLink', () => {
    it('renders one link per credited artist', () => {
        const el = renderToEl(creditLink(parts, '2Pac feat. Snoop Dogg', TUPAC));
        const links = el.querySelectorAll('a.explore-link');

        expect(links).toHaveLength(2);
        expect(links[0]?.textContent).toBe('2Pac');
        expect(links[1]?.textContent).toBe('Snoop Dogg');
    });

    it('puts the join phrase between the links as plain text', () => {
        const el = renderToEl(creditLink(parts, '2Pac feat. Snoop Dogg', TUPAC));

        // The whole credit reads correctly...
        expect(el.textContent?.replace(/\s+/g, ' ').trim()).toBe(
            '2Pac feat. Snoop Dogg',
        );

        // ...and " feat. " is not inside either link, which is the
        // difference between a credit and a link with punctuation in it.
        for (const link of el.querySelectorAll('a.explore-link')) {
            expect(link.textContent).not.toMatch(/feat/);
        }
    });

    it('falls back to a single link when there are no parts', () => {
        const el = renderToEl(creditLink(undefined, 'Alina Baraz & Galimatias', TUPAC));
        const links = el.querySelectorAll('a.explore-link');

        expect(links).toHaveLength(1);
        expect(links[0]?.textContent).toBe('Alina Baraz & Galimatias');
    });

    it('does not split the fallback string on its separators', () => {
        // "&" and "with" appear inside real artist names — "Simon &
        // Garfunkel" is one artist — so a credit with no parts is one
        // link, always. This is the whole reason primaryArtist() does
        // not split on them either.
        const el = renderToEl(creditLink(undefined, 'Simon & Garfunkel', TUPAC));

        expect(el.querySelectorAll('a.explore-link')).toHaveLength(1);
    });

    it('treats a one-part credit as the single-link case', () => {
        // A zero- or one-part credit reaching the multi-artist branch
        // would render as nothing, or as a link with a dangling join
        // phrase after it.
        const one: CreditPart[] = [
            { creditedName: 'Solo', artistMbid: TUPAC, joinPhrase: '' },
        ];
        const el = renderToEl(creditLink(one, 'Solo', TUPAC));

        expect(el.querySelectorAll('a.explore-link')).toHaveLength(1);
        expect(el.textContent?.trim()).toBe('Solo');
    });

    it('renders the credited name, not the artist name', () => {
        // MusicBrainz credits "Snoop Dogg" on a track by the artist
        // called "Snoop Doggy Dogg". Display follows the credit;
        // navigation follows the MBID.
        const el = renderToEl(creditLink(parts, 'anything', TUPAC));

        expect(el.textContent).toContain('Snoop Dogg');
        expect(el.textContent).not.toContain('Snoop Doggy Dogg');
    });
});

describe('creditText', () => {
    it('reassembles the credit as a string', () => {
        expect(creditText(parts, 'ignored')).toBe('2Pac feat. Snoop Dogg');
    });

    it('is the fallback string when there are no parts', () => {
        expect(creditText(undefined, 'Alina Baraz & Galimatias')).toBe(
            'Alina Baraz & Galimatias',
        );
    });

    it('agrees with what creditLink renders', () => {
        // The tooltip and the links come from the same parts by the same
        // concatenation, so they cannot disagree.
        const el = renderToEl(creditLink(parts, 'ignored', TUPAC));

        expect(el.textContent?.replace(/\s+/g, ' ').trim()).toBe(
            creditText(parts, 'ignored'),
        );
    });
});
