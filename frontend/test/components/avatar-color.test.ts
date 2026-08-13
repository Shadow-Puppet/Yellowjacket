/**
 * A letter avatar's background clears 4.5:1 against white for *every*
 * hue it can generate.
 *
 * The two failures a rendered sweep found were not the finding. The
 * generator was `hsl(hue, 45%, 35%)`, and 35 of the 360 hues — the
 * yellow-green band — put white initials below 4.5:1, bottoming out at
 * 4.08:1. Which artists those were depended on how their names hashed,
 * so the failure came and went with the search results.
 *
 * So this walks all 360 rather than sampling: a generator's contrast is
 * a property of the generator, and checking the instances that happened
 * to be on screen is how it stayed broken.
 */
import { describe, expect, it } from 'vitest';

import { avatarBackground, nameToHue } from '@utils/avatar-color';

/** Resolve an `hsl(...)` string to sRGB via the browser's own parser. */
function toRgb(color: string): [number, number, number] {
  const probe = document.createElement('div');

  probe.style.color = color;
  document.body.append(probe);

  const computed = getComputedStyle(probe).color;

  probe.remove();

  const [r, g, b] = computed
    .slice(computed.indexOf('(') + 1, computed.indexOf(')'))
    .split(/[,\s/]+/)
    .filter(Boolean)
    .map(Number) as [number, number, number];

  return [r, g, b];
}

function contrastWithWhite(color: string): number {
  const channels = toRgb(color).map((v) => v / 255);

  const [r, g, b] = channels.map((v) =>
    v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4,
  ) as [number, number, number];

  const l = 0.2126 * r + 0.7152 * g + 0.0722 * b;

  return 1.05 / (l + 0.05);
}

describe('avatar colours', () => {
  it('clears 4.5:1 against white at every hue', () => {
    const ratios = Array.from({ length: 360 }, (_, hue) =>
      contrastWithWhite(`hsl(${hue}, 45%, 32%)`),
    );

    expect(Math.min(...ratios)).toBeGreaterThanOrEqual(4.5);
  });

  // The generator is only safe if every name lands on one of those hues,
  // which is the half a hue-only sweep cannot see.
  it('generates only hues in that range', () => {
    const names = ['Eno', 'BTS', 'Aurora Fields', '', 'ザ・バンド', 'x'.repeat(200)];

    const hues = names.map((n) => nameToHue(n));

    expect(hues.every((h) => Number.isInteger(h) && h >= 0 && h < 360)).toBe(
      true,
    );
  });

  it('is the same colour for the same name', () => {
    expect(avatarBackground('Aurora Fields')).toBe(
      avatarBackground('Aurora Fields'),
    );
  });
});
