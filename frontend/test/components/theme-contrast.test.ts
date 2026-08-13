/**
 * Every text colour clears WCAG AA against every surface it can sit on,
 * on all three background ramps.
 *
 * `a11y.md` flagged one pair as "borderline (≈4.1:1) but that needs a
 * real measurement", and plan 007 parked it as "worth measuring before
 * planning". Measured: it failed in **nine of twelve** combinations,
 * as low as 2.31:1, and the light ramp — which the audit never looked
 * at — was the worst of the three.
 *
 * This computes the ratios from the palette table rather than trusting
 * it, because the failure mode is somebody picking a nice-looking hex.
 * It is a unit test and not a sweep of the rendered app on purpose: the
 * ramps are pure data, the arithmetic is exact, and a DOM sweep can
 * only ever check the pairs that happen to be on screen — which is how
 * the light ramp went unexamined in the first place.
 */
import { describe, expect, it } from 'vitest';

import { SHADE_PALETTES } from '@store/theme-store';
import type { ShadePalette } from '@store/theme-store';

/** WCAG 2.1 relative luminance. */
function luminance(hex: string): number {
  const h = hex.replace('#', '');
  const channels = [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16) / 255);

  const [r, g, b] = channels.map((v) =>
    v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4,
  ) as [number, number, number];

  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [
    number,
    number,
  ];

  return (hi + 0.05) / (lo + 0.05);
}

const TEXT = [
  'textPrimary',
  'textSecondary',
  'textTertiary',
  'successText',
  'warningText',
  'errorText',
  'infoText',
] as const;

/**
 * The semantic *fills*, which are fixed across ramps because a danger
 * button is red in every theme. What varies is the foreground, and it
 * is computed rather than written down because the accent is a colour
 * picker: white on the default `#ffd43b` is 1.43:1.
 */
const FILLS = ['#2f9e44', '#e8590c', '#e03131', '#4263eb', '#ffd43b'];

/**
 * `bgOverlay` is deliberately absent for tertiary on the dark ramp.
 * Clearing 4.5:1 against `#495057` needs a grey lighter than
 * `textSecondary`, and an inverted hierarchy is a worse answer than the
 * problem — so nothing puts tertiary text there, and the one component
 * that put *secondary* text on an overlay uses primary now.
 */
const SURFACES: Record<(typeof TEXT)[number], (keyof ShadePalette)[]> = {
  textPrimary: ['bgBase', 'bgSurface', 'bgElevated', 'bgOverlay'],
  textSecondary: ['bgBase', 'bgSurface', 'bgElevated'],
  textTertiary: ['bgBase', 'bgSurface', 'bgElevated'],
  successText: ['bgBase', 'bgSurface', 'bgElevated'],
  warningText: ['bgBase', 'bgSurface', 'bgElevated'],
  errorText: ['bgBase', 'bgSurface', 'bgElevated'],
  infoText: ['bgBase', 'bgSurface', 'bgElevated'],
};

/** Black or white, whichever reads on a fill — `readableOn`'s rule. */
function readable(fill: string): string {
  return contrast(fill, '#ffffff') >= 4.5 ? '#ffffff' : '#000000';
}

describe('theme contrast', () => {
  const cases = Object.entries(SHADE_PALETTES).flatMap(([shade, palette]) =>
    TEXT.flatMap((text) =>
      SURFACES[text].map((surface) => ({
        shade,
        text,
        surface,
        ratio: contrast(palette[text], palette[surface]),
      })),
    ),
  );

  it.each(cases)('$shade: $text on $surface clears 4.5:1', ({ ratio }) => {
    expect(ratio).toBeGreaterThanOrEqual(4.5);
  });

  // Every fill in the app has a foreground that reads on it, for any
  // accent a user can pick — which is the half a fixed `color: #000`
  // got right only for the current default, and `var(--yj-bg-base)`
  // got backwards on the light ramp (white on yellow, 1.43:1).
  it.each(FILLS)('%s carries a readable foreground', (fill) => {
    expect(contrast(fill, readable(fill))).toBeGreaterThanOrEqual(4.5);
  });

  // Sizing tertiary to clear 4.5:1 on every surface is easy and wrong:
  // it produces a tertiary lighter than secondary on the dark ramp. The
  // ramp has to stay a ramp, or "tertiary" stops meaning anything.
  it.each(Object.entries(SHADE_PALETTES))(
    '%s keeps the text ramp ordered',
    (_shade, palette) => {
      // The greyscale ramp only. The semantic text colours are not a
      // ramp — they are four hues that each have to clear the same bar,
      // and ordering them against each other means nothing.
      const greyscale = [
        'textPrimary',
        'textSecondary',
        'textTertiary',
      ] as const;

      const steps = greyscale.map((t) =>
        contrast(palette[t], palette.bgSurface),
      );

      expect(steps).toEqual([...steps].sort((a, b) => b - a));
    },
  );
});
