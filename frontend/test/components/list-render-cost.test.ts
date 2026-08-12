/**
 * `perf.M3` and `perf.M4` are both per-row costs paid only while a
 * virtualizer is recycling rows, which makes them invisible to every
 * other tier: nothing renders differently, nothing fails, and the app
 * is merely slower to scroll.
 *
 * The measurement that found them lives in `e2e/perf/measure.mjs` and
 * needs a 50 000-track library. These are the cheap guards that keep
 * the fixes from being undone by someone reading the call site alone,
 * and they assert the *mechanism* rather than a duration — a timing
 * assertion in a component test is a flake, not a regression test.
 */
import { describe, expect, it } from 'vitest';

import { COLUMN_DEFS } from '@components/track-list/columns';
import { render } from 'lit';

/** Render a column's cell into a detached element and read the HTML. */
function cell(columnId: string, track: Record<string, unknown>): HTMLElement {
  const host = document.createElement('div');

  render(COLUMN_DEFS[columnId]?.renderCell?.(track as never), host);

  return host;
}

describe('the track list Art column', () => {
  const track = {
    CoverArtPath: '/covers/abc.jpg',
    CoverArtSmall: '/covers/abc_sm.jpg',
    CoverArtMedium: '/covers/abc_md.jpg',
  };

  it('asks for the 100 px tier, not the original, for a 24 px box', () => {
    // The original is commonly 1500×1500 and several hundred kB, and was
    // being decoded in full to draw 576 pixels.
    const img = cell('albumArt', track).querySelector('img');

    expect(img?.getAttribute('src')).toBe('/covers/abc_sm.jpg');
  });

  it('keeps the decode off the scroll path', () => {
    const img = cell('albumArt', track).querySelector('img');

    expect([
      img?.getAttribute('loading'),
      img?.getAttribute('decoding'),
    ]).toEqual(['lazy', 'async']);
  });

  it('falls back through the tiers rather than rendering nothing', () => {
    const onlyOriginal = cell('albumArt', {
      CoverArtPath: '/covers/abc.jpg',
      CoverArtSmall: '',
      CoverArtMedium: '',
    }).querySelector('img');

    expect(onlyOriginal?.getAttribute('src')).toBe('/covers/abc.jpg');
  });

  it('renders nothing at all when there is no art', () => {
    const none = cell('albumArt', {
      CoverArtPath: '',
      CoverArtSmall: '',
      CoverArtMedium: '',
    });

    expect(none.querySelector('img')).toBeNull();
  });
});
