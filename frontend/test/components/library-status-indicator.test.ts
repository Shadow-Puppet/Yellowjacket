/**
 * The badge on cards, rows and the album title.
 *
 * The `partial` state was added so an album you hold nine tracks of
 * looks different from one you hold all twelve of. The risk it carries
 * is that a ring is a *claim about a total*, and most of an untagged
 * library has no total — so the rules under test are that the arc
 * reflects the real fraction, that extras do not overfill it, and that
 * the count reaches a screen reader rather than only an eye.
 */
import { describe, expect, it } from 'vitest';

import '@components/library-status-indicator/library-status-indicator';
import { fixture, shadow } from '@test/support/render';

/** The stroke-dashoffset the arc was drawn with, as a fraction filled. */
function filledFraction(el: Element): number {
  const arc = shadow(el, '.ring-fill');
  const dash = Number(arc?.getAttribute('stroke-dasharray'));
  const offset = Number(arc?.getAttribute('stroke-dashoffset'));

  return (dash - offset) / dash;
}

describe('the library status badge', () => {
  it('draws no ring unless it is partial', async () => {
    const el = await fixture('library-status-indicator', {
      status: 'in-library',
    });

    expect(shadow(el, '.ring-fill')).toBeNull();
    expect(shadow(el, 'wa-icon')?.getAttribute('name')).toBe('check');
  });

  it('fills the arc to the held fraction', async () => {
    const el = await fixture('library-status-indicator', {
      status: 'partial',
      owned: 9,
      expected: 12,
    });

    expect(filledFraction(el)).toBeCloseTo(0.75, 5);
  });

  it('does not overfill on bonus tracks', async () => {
    const el = await fixture('library-status-indicator', {
      status: 'partial',
      owned: 13,
      expected: 12,
    });

    expect(filledFraction(el)).toBeCloseTo(1, 5);
  });

  it('does not divide by a total it was never given', async () => {
    const el = await fixture('library-status-indicator', {
      status: 'partial',
      owned: 3,
      expected: 0,
    });

    expect(filledFraction(el)).toBe(0);
  });

  it('says the count, not just the shape', async () => {
    const el = await fixture('library-status-indicator', {
      status: 'partial',
      owned: 9,
      expected: 12,
      entityType: 'album',
      label: 'Glass Harbour',
    });

    const name = shadow(el, '.badge')?.getAttribute('aria-label') ?? '';

    expect(name).toContain('9 of 12');
    expect(name).toContain('Glass Harbour');
  });
});
