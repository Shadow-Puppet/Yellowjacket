import { EventsOn } from '@runtime/runtime';
import {
    GetThemeAccentColor,
    GetThemeBackgroundShade,
    SetThemeAccentColor,
    SetThemeBackgroundShade,
} from '@go/config/Config';
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
interface ShadePalette {
    bgBase: string;
    bgSurface: string;
    bgElevated: string;
    bgOverlay: string;
    textPrimary: string;
    textSecondary: string;
    textTertiary: string;
    border: string;
    borderSubtle: string;
    hoverOverlay: string;
    selectionBg: string;
}

const SHADE_PALETTES: Record<BackgroundShade, ShadePalette> = {
    darker: {
        bgBase: '#000000',
        bgSurface: '#121212',
        bgElevated: '#1e1e1e',
        bgOverlay: '#2a2a2a',
        textPrimary: '#ffffff',
        textSecondary: '#b3b3b3',
        textTertiary: '#888888',
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
        textTertiary: '#888888',
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
        textTertiary: '#868e96',
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

        // Semantic colours (fixed across themes)
        '--yj-success': '#2f9e44',
        '--yj-success-hover': '#2b8a3e',
        '--yj-warning': '#e8590c',
        '--yj-warning-hover': '#d9480f',
        '--yj-error': '#e03131',
        '--yj-error-hover': '#c92a2a',
        '--yj-info': '#4263eb',
        '--yj-info-hover': '#3b5bdb',
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
        root.style.colorScheme =
            this.state.backgroundShade === 'light'
                ? 'light'
                : 'dark';
    }
}

// Singleton instance.
export const themeStore = new ThemeStore();
