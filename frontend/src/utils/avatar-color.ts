/**
 * The background for a letter avatar, derived from a name.
 *
 * Three call sites drew `hsl(nameToHue(name), 45%, 35%)` behind white
 * initials, from two copies of the same hash function. Measured across
 * all 360 hues: **35 of them** — the yellow-green band from about 53°
 * to 88° — put white text below 4.5:1, bottoming out at 4.08:1. Which
 * artists those were depended entirely on how their names hashed, so
 * the app had a contrast failure that came and went with the search
 * results, and the two instances that turned up in a sweep were not the
 * finding.
 *
 * 32% lightness clears every hue with a floor of 4.75:1, so the fix is
 * a property of the generator rather than of any colour it generates.
 * `avatar-color.test.ts` walks all 360.
 */

/** Hash a string to a hue value 0–360. */
export function nameToHue(name: string): number {
    let hash = 0;

    for (let i = 0; i < name.length; i++) {
        hash = name.charCodeAt(i) + ((hash << 5) - hash);
    }

    return Math.abs(hash) % 360;
}

/** Saturation and lightness are shared so the floor above holds. */
export const AVATAR_SATURATION = 45;
export const AVATAR_LIGHTNESS = 32;

/** The `background` value for a name's avatar. */
export function avatarBackground(name: string): string {
    return `hsl(${nameToHue(name)}, ${AVATAR_SATURATION}%, ${AVATAR_LIGHTNESS}%)`;
}
