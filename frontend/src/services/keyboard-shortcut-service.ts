/**
 * Keyboard Shortcut Service
 *
 * Singleton that listens for keydown events on document and dispatches
 * shortcut actions based on the current scope. Handles:
 *
 * - Key string normalization (modifiers in canonical order)
 * - Shadow DOM active element resolution
 * - Text input suppression (only Escape passes through)
 * - Scope resolution: text-input > panel-specific > global
 * - Action dispatch to player/queue/nav stores
 */
import { shortcutsStore } from '@store/shortcuts-store';
import { playerStore } from '@store/player-store';
import { queueStore } from '@store/queue-store';
import * as Player from '@go/player/Player';
import type { SearchBar } from '@components/search-bar/search-bar';

// ===================================================================
// KEY STRING UTILITIES
// ===================================================================

/** Map of KeyboardEvent.key values to canonical key names. */
const KEY_ALIASES: Record<string, string> = {
    ArrowUp: 'Up',
    ArrowDown: 'Down',
    ArrowLeft: 'Left',
    ArrowRight: 'Right',
    ' ': 'Space',
};

/** Keys that are modifier-only presses and should be ignored. */
const MODIFIER_KEYS = new Set([
    'Control',
    'Alt',
    'Shift',
    'Meta',
]);

/**
 * Build a canonical key string from a KeyboardEvent.
 *
 * Format: `[Ctrl+][Alt+][Shift+]Key`
 * Examples: "Ctrl+F", "Space", "Shift+Delete", "N"
 *
 * Exported for reuse by the shortcut-capture widget (Plan 04).
 */
export function buildKeyString(e: KeyboardEvent): string {
    // Skip bare modifier presses.
    if (MODIFIER_KEYS.has(e.key)) return '';

    const parts: string[] = [];

    // Modifiers in fixed order. Treat Meta (Cmd on Mac) as Ctrl.
    if (e.ctrlKey || e.metaKey) parts.push('Ctrl');
    if (e.altKey) parts.push('Alt');
    if (e.shiftKey) parts.push('Shift');

    // Normalize the key name.
    let key = KEY_ALIASES[e.key] ?? e.key;

    // Single printable characters → uppercase.
    if (key.length === 1) {
        key = key.toUpperCase();
    }

    parts.push(key);

    return parts.join('+');
}

// ===================================================================
// SHADOW DOM HELPERS
// ===================================================================

/**
 * Walk the shadow DOM active element chain to find the deepest
 * focused element. Necessary because `document.activeElement`
 * stops at the shadow host boundary.
 */
function getDeepActiveElement(): Element | null {
    let el = document.activeElement;

    while (el?.shadowRoot?.activeElement) {
        el = el.shadowRoot.activeElement;
    }

    return el;
}

/** Text input types that should suppress shortcuts. */
const TEXT_INPUT_TYPES = new Set([
    'text',
    'search',
    'url',
    'email',
    'password',
    'number',
    'tel',
]);

/**
 * Check whether the deepest active element is a text input.
 */
function isTextInputFocused(el: Element | null): boolean {
    if (!el) return false;

    const tag = el.tagName.toUpperCase();

    if (tag === 'TEXTAREA') return true;

    if (tag === 'INPUT') {
        const inputType = (
            el as HTMLInputElement
        ).type.toLowerCase();

        return TEXT_INPUT_TYPES.has(inputType) || inputType === '';
    }

    if ((el as HTMLElement).isContentEditable) return true;

    return false;
}

// ===================================================================
// SCOPE RESOLUTION
// ===================================================================

type ShortcutScope =
    | 'text-input'
    | `panel:${string}`
    | 'global';

/**
 * Resolve the current shortcut scope based on the focused element.
 *
 * Priority: text-input > panel-specific > global
 */
function resolveScope(deepEl: Element | null): ShortcutScope {
    if (isTextInputFocused(deepEl)) return 'text-input';

    // Walk up from the deep active element looking for a
    // `data-shortcut-scope` attribute on any ancestor.
    let walker: Element | null = deepEl;

    while (walker) {
        const scope = walker.getAttribute?.('data-shortcut-scope');

        if (scope) return `panel:${scope}` as ShortcutScope;

        // Cross shadow boundaries: if we're at a shadow root host,
        // continue walking from the host element.
        const parent =
            walker.parentElement ??
            (walker.getRootNode() as ShadowRoot).host;

        if (parent === walker) break;

        walker = parent ?? null;
    }

    return 'global';
}

// ===================================================================
// VOLUME / SEEK STEP SIZES
// ===================================================================

const VOLUME_STEP = 5;
const SEEK_STEP = 5; // seconds

// ===================================================================
// ACTION DISPATCH
// ===================================================================

/**
 * Dispatch the action associated with an action ID.
 *
 * Each case maps an action string to the appropriate store method
 * or Wails binding call.
 */
async function dispatch(action: string): Promise<void> {
    switch (action) {
        // Player controls
        case 'player.playPause':
            if (playerStore.getState().isPlaying) {
                playerStore.pause();
            } else {
                queueStore.play();
            }

            break;

        case 'player.next':
            queueStore.next();
            break;

        case 'player.previous':
            queueStore.previous();
            break;

        case 'player.volumeUp':
            await Player.ChangeVolume(VOLUME_STEP);
            break;

        case 'player.volumeDown':
            await Player.ChangeVolume(-VOLUME_STEP);
            break;

        case 'player.seekForward': {
            const pos = await Player.CurrentPositionSeconds();
            const len = await Player.TrackLengthInSeconds();
            const target = Math.min(pos + SEEK_STEP, len);

            await Player.Seek(target);
            break;
        }

        case 'player.seekBack': {
            const pos = await Player.CurrentPositionSeconds();
            const target = Math.max(pos - SEEK_STEP, 0);

            await Player.Seek(target);
            break;
        }

        case 'player.shuffle':
            queueStore.toggleShuffle();
            break;

        case 'player.repeat':
            queueStore.cycleRepeat();
            break;

        case 'player.mute':
            await Player.MuteToggle();
            break;

        // Navigation
        case 'nav.search':
        case 'nav.searchAlt': {
            const bar = document.querySelector(
                'search-bar',
            ) as SearchBar | null;

            if (bar && !bar.hasAttribute('hidden')) {
                bar.focusInput();
            }

            break;
        }

        case 'nav.queue': {
            const queuePanel = document.getElementById(
                'queue-panel',
            ) as HTMLElement | null;

            if (queuePanel) {
                const isOpen = queuePanel.hasAttribute('open');

                if (isOpen) {
                    queuePanel.removeAttribute('open');
                } else {
                    queuePanel.setAttribute('open', '');
                }
            }

            break;
        }

        // App actions
        case 'app.selectAll':
            document.dispatchEvent(
                new CustomEvent('shortcut:select-all'),
            );
            break;

        // Panel-specific: track list
        case 'tracklist.play':
            document.dispatchEvent(
                new CustomEvent('shortcut:tracklist-play'),
            );
            break;

        case 'tracklist.delete':
            document.dispatchEvent(
                new CustomEvent('shortcut:tracklist-delete'),
            );
            break;

        default:
            // Unknown action — silently ignore.
            break;
    }
}

// ===================================================================
// SERVICE
// ===================================================================

/**
 * KeyboardShortcutService is a singleton that intercepts keydown
 * events on the document and dispatches matched shortcut actions.
 */
class KeyboardShortcutService {
    constructor() {
        document.addEventListener('keydown', this.handleKeydown);
    }

    private handleKeydown = (e: KeyboardEvent): void => {
        const deepEl = getDeepActiveElement();
        const scope = resolveScope(deepEl);

        // In text inputs, only allow Escape (to blur the input).
        if (scope === 'text-input') {
            if (e.key === 'Escape' && deepEl) {
                (deepEl as HTMLElement).blur();
                e.preventDefault();
            }

            // Suppress all other shortcuts in text inputs.
            return;
        }

        // Build the canonical key string.
        const keyStr = buildKeyString(e);

        if (!keyStr) return;

        // Look up the action: panel-specific first, then global.
        const action = shortcutsStore.getActionForKey(
            keyStr,
            scope,
        );

        if (!action) return;

        // Found a match — prevent default and dispatch.
        e.preventDefault();
        void dispatch(action);
    };

    /** Remove the event listener (for cleanup if ever needed). */
    destroy(): void {
        document.removeEventListener(
            'keydown',
            this.handleKeydown,
        );
    }
}

// Singleton instance — instantiation registers the keydown listener.
export const keyboardShortcutService =
    new KeyboardShortcutService();

// Re-export for the shortcut capture widget.
export type { KeyboardShortcutService };
