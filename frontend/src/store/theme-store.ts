import { EventsOn } from '@runtime/runtime';
import {
    GetThemeAccentColor,
    GetThemeBackgroundShade,
    SetThemeAccentColor,
    SetThemeBackgroundShade,
} from '@go/config/config.js';
import { Events } from '../events';

export type BackgroundShade = 'darker' | 'dark' | 'light';

export interface ThemeState {
    accentColor: string;
    backgroundShade: BackgroundShade;
}

type Subscriber = () => void;

/**
 * Shade palettes keyed by BackgroundShade.
 * Each defines the base grayscale ramp used throughout the UI.
 */
export interface ShadePalette {
    bgBase: string;
    bgSurface: string;
    bgElevated: string;
    bgOverlay: string;
    textPrimary: string;
    textSecondary: string;
    textTertiary: string;
    /**
     * Semantic colours *as text on this ramp's surfaces*.
     *
     * Separate from the fills below because they answer a different
     * question. A fill is "what colour is a danger button", which is
     * red in every theme; this is "what colour is the word `failed` on
     * this background", which cannot be one value — a single fixed
     * colour cannot clear 4.5:1 against both a near-black and a
     * near-white surface, and the old fixed set measured 2.31–4.28:1
     * on nearly all of them.
     */
    successText: string;
    warningText: string;
    errorText: string;
    infoText: string;
    border: string;
    borderSubtle: string;
    hoverOverlay: string;
    selectionBg: string;
}

/**
 * The grayscale ramps, and the one rule that constrains them.
 *
 * `textTertiary` used to be `#888888` on both dark ramps and `#868e96`
 * on the light one, which `a11y.md` flagged as "borderline" from a hand
 * calculation and parked as "worth measuring before planning".
 * Measured, against the rendered app and then across all three ramps:
 * it failed WCAG AA in **nine of twelve** text/surface combinations,
 * as low as 2.31:1 on dark's overlay and 2.55:1 on light's. Not
 * borderline — the app's most-used secondary text colour, failing on
 * every view, and the light ramp (which the audit never considered) was
 * the worst of the three.
 *
 * So: **every text colour clears 4.5:1 against every surface it can sit
 * on**, and `theme-contrast.test.ts` computes that from this table
 * rather than trusting it. Two things decided the values.
 *
 * `bgOverlay` is not a text surface on the dark ramp. Sizing tertiary
 * to clear 4.5 against `#495057` needs `#c0c0c0`, which is *lighter
 * than secondary* — an inverted hierarchy is a worse answer than the
 * problem. Tertiary is sized to `bgElevated` there, and the one place
 * that did put text on the overlay (the downloads notice) uses
 * `textPrimary`, which clears 8.18:1.
 *
 * And the hue is kept. The light ramp's tertiary is a blue-grey, so it
 * darkens along its own hue to `#5c636a` rather than flattening to a
 * neutral that would have passed just as well and looked like a
 * different palette.
 */
export const SHADE_PALETTES: Record<BackgroundShade, ShadePalette> = {
    darker: {
        bgBase: '#000000',
        bgSurface: '#121212',
        bgElevated: '#1e1e1e',
        bgOverlay: '#2a2a2a',
        textPrimary: '#ffffff',
        textSecondary: '#b3b3b3',
        // 4.05:1 on bgOverlay at #888888.
        textTertiary: '#949494',
        successText: '#51cf66',
        warningText: '#ffa94d',
        errorText: '#ff8787',
        infoText: '#91a7ff',
        border: '#333333',
        borderSubtle: '#222222',
        hoverOverlay: 'rgba(255, 255, 255, 0.05)',
        selectionBg: 'rgba(100, 160, 255, 0.15)',
    },
    dark: {
        bgBase: '#000000',
        bgSurface: '#212529',
        bgElevated: '#343a40',
        bgOverlay: '#495057',
        textPrimary: '#ffffff',
        textSecondary: '#b3b3b3',
        // 4.35:1 on bgSurface and 3.25:1 on bgElevated at #888888 — the
        // measured version of the audit's estimate, on every view.
        textTertiary: '#a6a6a6',
        successText: '#51cf66',
        warningText: '#ffa94d',
        errorText: '#ff8787',
        infoText: '#91a7ff',
        border: '#444444',
        borderSubtle: '#333333',
        hoverOverlay: 'rgba(255, 255, 255, 0.05)',
        selectionBg: 'rgba(100, 160, 255, 0.15)',
    },
    light: {
        bgBase: '#ffffff',
        bgSurface: '#f8f9fa',
        bgElevated: '#e9ecef',
        bgOverlay: '#dee2e6',
        textPrimary: '#212529',
        textSecondary: '#495057',
        // 3.32:1 at best and 2.55:1 at worst at #868e96 — the light ramp
        // failed on all four of its own surfaces.
        textTertiary: '#5c636a',
        successText: '#1f6129',
        warningText: '#9c3808',
        errorText: '#b02525',
        infoText: '#364fc7',
        border: '#ced4da',
        borderSubtle: '#dee2e6',
        hoverOverlay: 'rgba(0, 0, 0, 0.05)',
        selectionBg: 'rgba(100, 160, 255, 0.15)',
    },
};

/** Parse a hex colour string to RGB components. */
function hexToRgb(hex: string): { r: number; g: number; b: number } {
    let cleaned = hex.replace('#', '');

    if (cleaned.length === 3) {
        cleaned =
            cleaned[0]! + cleaned[0]! +
            cleaned[1]! + cleaned[1]! +
            cleaned[2]! + cleaned[2]!;
    }

    return {
        r: parseInt(cleaned.slice(0, 2), 16),
        g: parseInt(cleaned.slice(2, 4), 16),
        b: parseInt(cleaned.slice(4, 6), 16),
    };
}

/** Mix a colour towards white by a fraction (0..1). */
function lighten(hex: string, amount: number): string {
    const { r, g, b } = hexToRgb(hex);
    const lr = Math.round(r + (255 - r) * amount);
    const lg = Math.round(g + (255 - g) * amount);
    const lb = Math.round(b + (255 - b) * amount);

    return `#${lr.toString(16).padStart(2, '0')}${lg.toString(16).padStart(2, '0')}${lb.toString(16).padStart(2, '0')}`;
}

/** Mix a colour towards black by a fraction (0..1). */
function darken(hex: string, amount: number): string {
    const { r, g, b } = hexToRgb(hex);
    const dr = Math.round(r * (1 - amount));
    const dg = Math.round(g * (1 - amount));
    const db = Math.round(b * (1 - amount));

    return `#${dr.toString(16).padStart(2, '0')}${dg.toString(16).padStart(2, '0')}${db.toString(16).padStart(2, '0')}`;
}

/**
 * Derive the full set of CSS custom properties from accent + shade.
 */
/**
 * Black or white, whichever is readable on `hex` — preferring white.
 *
 * White is the app's foreground on every solid button, so this only
 * moves when white does not clear 4.5:1. That keeps a red danger button
 * looking like one (white, 4.51:1) while a green or amber one, where
 * white measures 3.45:1 and 3.58:1, flips to black rather than staying
 * conventional and unreadable.
 */
/** WCAG relative luminance of a hex colour. */
function luminance(hex: string): number {
    const { r, g, b } = hexToRgb(hex);

    const channel = (v: number) => {
        const c = v / 255;

        return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
    };

    return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

function contrastRatio(a: string, b: string): number {
    const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [
        number,
        number,
    ];

    return (hi + 0.05) / (lo + 0.05);
}

/**
 * The accent, moved just far enough to be readable *as text* on a
 * surface — and no further.
 *
 * The accent is a colour picker, so this cannot be a table. The default
 * `#ffd43b` measures 10.82:1 on the dark ramp's surface and **1.35:1**
 * on the light one, which is what made every accent-coloured label on
 * the light theme unreadable. Mixing towards the surface's opposite in
 * small steps keeps the hue and stops at the first value that clears
 * 4.5:1, so a dark ramp gets the accent back unchanged.
 */
function accentTextOn(accent: string, surface: string): string {
    if (contrastRatio(accent, surface) >= 4.5) return accent;

    const towardsBlack = luminance(surface) > 0.18;

    for (let step = 1; step <= 20; step++) {
        const candidate = towardsBlack
            ? darken(accent, step * 0.05)
            : lighten(accent, step * 0.05);

        if (contrastRatio(candidate, surface) >= 4.5) return candidate;
    }

    // Nothing along the hue worked; fall back to something that reads.
    return towardsBlack ? '#000000' : '#ffffff';
}

function readableOn(hex: string): string {
    const { r, g, b } = hexToRgb(hex);

    const channel = (v: number) => {
        const c = v / 255;

        return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
    };

    const l =
        0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
    const onWhite = 1.05 / (l + 0.05);

    return onWhite >= 4.5 ? '#ffffff' : '#000000';
}

function deriveThemeVariables(
    accent: string,
    shade: BackgroundShade,
): Record<string, string> {
    const palette = SHADE_PALETTES[shade];
    const { r, g, b } = hexToRgb(accent);

    return {
        // Background ramp
        '--yj-bg-base': palette.bgBase,
        '--yj-bg-surface': palette.bgSurface,
        '--yj-bg-elevated': palette.bgElevated,
        '--yj-bg-overlay': palette.bgOverlay,

        // Accent ramp
        '--yj-accent': accent,
        '--yj-accent-hover': lighten(accent, 0.15),
        '--yj-accent-muted': darken(accent, 0.5),
        '--yj-accent-bg': `rgba(${r}, ${g}, ${b}, 0.1)`,
        '--yj-accent-bg-strong': `rgba(${r}, ${g}, ${b}, 0.15)`,

        // Text
        '--yj-text-primary': palette.textPrimary,
        '--yj-text-secondary': palette.textSecondary,
        '--yj-text-tertiary': palette.textTertiary,

        // Borders
        '--yj-border': palette.border,
        '--yj-border-subtle': palette.borderSubtle,

        // Interactive overlays
        '--yj-hover-overlay': palette.hoverOverlay,
        '--yj-selection-bg': palette.selectionBg,

        // Semantic *fills* — the background of a solid button or badge.
        // These stay fixed across ramps on purpose: a danger button is
        // red in every theme. What cannot be fixed is the text on top,
        // which is why each has an -fg below.
        '--yj-success': '#2f9e44',
        '--yj-success-hover': '#2b8a3e',
        '--yj-warning': '#e8590c',
        '--yj-warning-hover': '#d9480f',
        '--yj-error': '#e03131',
        '--yj-error-hover': '#c92a2a',
        '--yj-info': '#4263eb',
        '--yj-info-hover': '#3b5bdb',

        // Readable foregrounds for every fill in the app, including the
        // user's accent — which is why they are computed rather than
        // written down. White on the default #ffd43b is 1.43:1, and the
        // accent is a colour picker, so no fixed answer survives it.
        '--yj-accent-fg': readableOn(accent),
        '--yj-accent-text': accentTextOn(accent, palette.bgSurface),
        '--yj-success-fg': readableOn('#2f9e44'),
        '--yj-warning-fg': readableOn('#e8590c'),
        '--yj-error-fg': readableOn('#e03131'),
        '--yj-info-fg': readableOn('#4263eb'),

        // Semantic colours as text on this ramp's surfaces.
        '--yj-success-text': palette.successText,
        '--yj-warning-text': palette.warningText,
        '--yj-error-text': palette.errorText,
        '--yj-info-text': palette.infoText,
    };
}

class ThemeStore {
    private state: ThemeState = {
        accentColor: '#ffd43b',
        backgroundShade: 'dark',
    };

    private subscribers = new Set<Subscriber>();
    private initialized = false;

    constructor() {
        this.initializeEventListeners();
        this.loadFromBackend();
    }

    // ===================================================================
    // WAILS EVENT BRIDGE
    // ===================================================================

    private initializeEventListeners(): void {
        EventsOn(
            Events.ThemeConfigChanged,
            (data: {
                AccentColor: string;
                BackgroundShade: string;
            }) => {
                this.update({
                    accentColor: data.AccentColor,
                    backgroundShade:
                        data.BackgroundShade as BackgroundShade,
                });
            },
        );
    }

    private async loadFromBackend(): Promise<void> {
        try {
            const [accent, shade] = await Promise.all([
                GetThemeAccentColor(),
                GetThemeBackgroundShade(),
            ]);

            this.update({
                accentColor: accent,
                backgroundShade: shade as BackgroundShade,
            });
            this.initialized = true;
        } catch {
            // Use defaults on failure, apply them so the UI has variables.
            this.applyVariables();
            this.initialized = true;
        }
    }

    // ===================================================================
    // STATE ACCESS
    // ===================================================================

    getState(): Readonly<ThemeState> {
        return this.state;
    }

    isInitialized(): boolean {
        return this.initialized;
    }

    // ===================================================================
    // ACTIONS
    // ===================================================================

    async setAccentColor(color: string): Promise<void> {
        await SetThemeAccentColor(color);
    }

    async setBackgroundShade(
        shade: BackgroundShade,
    ): Promise<void> {
        await SetThemeBackgroundShade(shade);
    }

    // ===================================================================
    // SUBSCRIPTION SYSTEM
    // ===================================================================

    subscribe(callback: Subscriber): () => void {
        this.subscribers.add(callback);

        return () => this.subscribers.delete(callback);
    }

    private update(partial: Partial<ThemeState>): void {
        this.state = { ...this.state, ...partial };
        this.applyVariables();
        this.notify();
    }

    private notify(): void {
        this.subscribers.forEach((callback) => callback());
    }

    /**
     * Apply the full set of CSS custom properties to :root so every
     * component (including Shadow DOM) inherits them automatically.
     */
    private applyVariables(): void {
        const vars = deriveThemeVariables(
            this.state.accentColor,
            this.state.backgroundShade,
        );

        const root = document.documentElement;

        for (const [prop, value] of Object.entries(vars)) {
            root.style.setProperty(prop, value);
        }

        // Set the document color-scheme so native form controls
        // (select dropdowns, scrollbars, etc.) match the theme.
        const isDark =
            this.state.backgroundShade !== 'light';

        root.style.colorScheme = isDark
            ? 'dark'
            : 'light';

        // Bridge to WebAwesome's theme system so wa-dialog,
        // wa-drawer, and other WA components inherit the
        // correct surface colours instead of defaulting to
        // white (light mode).
        if (isDark) {
            root.classList.add('wa-dark');
        } else {
            root.classList.remove('wa-dark');
        }

        const palette =
            SHADE_PALETTES[this.state.backgroundShade];

        root.style.setProperty(
            '--wa-color-surface-raised',
            palette.bgSurface,
        );
        root.style.setProperty(
            '--wa-color-surface-default',
            palette.bgBase,
        );
        root.style.setProperty(
            '--wa-color-surface-lowered',
            palette.bgElevated,
        );
    }
}

// Singleton instance.
export const themeStore = new ThemeStore();
