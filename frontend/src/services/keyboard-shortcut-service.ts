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
import { ambientShortcutScope } from './shortcut-scope';
import { playerStore } from '@store/player-store';
import { queueStore } from '@store/queue-store';
import * as Player from '@go/player/player.js';
import type { SearchBar } from '@components/search-bar/search-bar';
import { OPEN_SEARCH_EVENT } from '@components/search-dialog/search-dialog';

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

/** Whether a resolved key is a printable character that a Shift press
 *  produced, rather than a key Shift was held alongside. Letters are
 *  excluded: `A` and `Shift+A` are the same character, so Shift stays
 *  meaningful there. */
function isShiftedCharacter(key: string): boolean {
    return key.length === 1 && !/[A-Z0-9]/.test(key);
}

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

    // Normalize the key name.
    let key = KEY_ALIASES[e.key] ?? e.key;

    // Single printable characters → uppercase.
    if (key.length === 1) {
        key = key.toUpperCase();
    }

    // Modifiers in fixed order. Treat Meta (Cmd on Mac) as Ctrl.
    if (e.ctrlKey || e.metaKey) parts.push('Ctrl');
    if (e.altKey) parts.push('Alt');

    // Shift is only a modifier when it did not *produce* the key.
    // `?` is Shift+/ on a US layout and something else elsewhere, and
    // "Shift+?" is a binding nobody would write down; the character
    // already carries the shift.
    if (e.shiftKey && !isShiftedCharacter(key)) parts.push('Shift');

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
 * Elements that own particular keys themselves.
 *
 * The global bindings are unmodified single keys (Space, arrows, letters)
 * — see Decision 1 in plan 007 — so without this a focused button cannot
 * be activated with Space and a `<select>` cannot be arrowed through,
 * because the service `preventDefault()`s every match.  Only text inputs
 * were exempt, which is finding H-6.
 */
const ACTIVATION_KEYS = new Set(['Space', 'Enter']);
const ARROW_KEYS = new Set(['Up', 'Down', 'Left', 'Right', 'Home', 'End']);
const VERTICAL_KEYS = new Set(['Up', 'Down', 'Home', 'End']);
const LIST_KEYS = new Set([...ARROW_KEYS, ...ACTIVATION_KEYS]);
const SLIDER_KEYS = new Set([...ARROW_KEYS, 'PageUp', 'PageDown']);

/** Roles/tags that consume a key, and which keys they consume. */
function keysOwnedBy(el: Element): ReadonlySet<string> | null {
    const tag = el.tagName.toUpperCase();
    const role = el.getAttribute('role')?.toLowerCase() ?? '';

    if (tag === 'SELECT' || role === 'listbox' || role === 'combobox') {
        return LIST_KEYS;
    }

    if (role === 'menu' || role === 'menuitem' || role === 'menubar') {
        return LIST_KEYS;
    }

    // A grid row or a listbox option moves with the arrow keys, which is
    // what makes a track list navigable without a mouse — but only the
    // *vertical* ones. Every list in this app (`track-list`'s own
    // handler, `utils/roving-rows.ts`, the card grids that use it) moves
    // on Up/Down/Home/End and does nothing with Left/Right, so granting
    // those took keyboard seeking away from a focused row and gave it to
    // nobody: two ArrowRights on a focused track row produced zero
    // `Player.Seek` calls, against one per press with focus on the body.
    // Grant them back here if a list ever moves horizontally.
    if (
        role === 'row' ||
        role === 'gridcell' ||
        role === 'grid' ||
        role === 'option' ||
        role === 'treeitem'
    ) {
        return VERTICAL_KEYS;
    }

    if (tag === 'INPUT') {
        const type = (el as HTMLInputElement).type.toLowerCase();

        if (type === 'range') return SLIDER_KEYS;
        if (type === 'radio') return LIST_KEYS;
        if (type === 'checkbox') return ACTIVATION_KEYS;
    }

    if (role === 'slider' || tag === 'WA-SLIDER') return SLIDER_KEYS;

    if (
        role === 'checkbox' ||
        role === 'switch' ||
        role === 'radio' ||
        tag === 'WA-CHECKBOX' ||
        tag === 'WA-SWITCH'
    ) {
        return ACTIVATION_KEYS;
    }

    if (
        tag === 'BUTTON' ||
        tag === 'SUMMARY' ||
        tag === 'WA-BUTTON' ||
        role === 'button' ||
        (tag === 'A' && el.hasAttribute('href'))
    ) {
        return ACTIVATION_KEYS;
    }

    if (tag === 'WA-SELECT') return LIST_KEYS;

    return null;
}

/** Whether an open dialog contains the focused element.  A dialog owns
 *  the whole keyboard while it is up. */
function insideOpenDialog(el: Element | null): boolean {
    let walker: Element | null = el;

    while (walker) {
        const role = walker.getAttribute?.('role')?.toLowerCase();
        const tag = walker.tagName.toUpperCase();

        if (
            role === 'dialog' ||
            role === 'alertdialog' ||
            ((tag === 'DIALOG' || tag === 'WA-DIALOG' || tag === 'WA-DRAWER') &&
                walker.hasAttribute('open'))
        ) {
            return true;
        }

        const parent =
            walker.parentElement ??
            ((walker.getRootNode() as ShadowRoot).host ?? null);

        if (parent === walker) break;

        walker = parent;
    }

    return false;
}

/**
 * Whether the focused control, rather than the app, should get this key.
 */
function focusedControlOwnsKey(
    el: Element | null,
    keyStr: string,
): boolean {
    if (!el) return false;

    // Modified combinations (Ctrl+F, Ctrl+A) are the app's; only the
    // unmodified keys are ever contested.
    if (keyStr.includes('+')) return false;

    if (insideOpenDialog(el)) return true;

    return keysOwnedBy(el)?.has(keyStr) ?? false;
}

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

    // Nothing focused inside a panel.  Fall back to the panel the active
    // view claimed, if any: this app is driven with the mouse, so focus
    // usually sits on <body> and a focus-only rule would make panel
    // bindings work only after a click landed inside the panel.
    const ambient = ambientShortcutScope();

    return ambient ? (`panel:${ambient}` as ShortcutScope) : 'global';
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
        // The key has one meaning -- *let me search this page* -- and
        // two surfaces since #57. The header box is gone below 600px,
        // so scoping the query to the bar is not tidiness: an unscoped
        // `search-bar` also matches the one inside `search-dialog`
        // while that is open, and would focus a box the user is
        // already typing in while leaving the phone with nothing at
        // all. The dialog declines to open on a view with nothing to
        // search, which is the same condition the trigger renders on.
        case 'nav.search':
        case 'nav.searchAlt': {
            const bar = document.querySelector(
                'header.top-bar search-bar',
            ) as SearchBar | null;

            if (bar && bar.checkVisibility()) {
                bar.focusInput();
            } else {
                document.dispatchEvent(new CustomEvent(OPEN_SEARCH_EVENT));
            }

            break;
        }

        // The keyboard half of #6. It dispatches the same events the
        // header's buttons and the detail views' own back buttons do,
        // rather than calling `history.back()` here: the shell owns the
        // guard that stops a press at the root leaving the app, and a
        // second caller reaching for `history` directly is how the old
        // `navStack` came to disagree with the platform.
        case 'nav.back':
            document.dispatchEvent(new CustomEvent('navigate-back'));
            break;

        case 'nav.forward':
            document.dispatchEvent(new CustomEvent('navigate-forward'));
            break;

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
        case 'app.shortcuts':
            document.dispatchEvent(
                new CustomEvent('shortcut:app-shortcuts'),
            );
            break;

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

        // `tracklist.delete` opens the confirmation and nothing else:
        // the key is a request, not an action.  See
        // backend/shortcuts/config.go.
        case 'tracklist.delete':
            document.dispatchEvent(
                new CustomEvent('shortcut:tracklist-delete'),
            );
            break;

        // Panel-specific: autotag review.  The view listens for these
        // while it is the view on screen, and for nothing while it is
        // not — which is the whole of finding H-1.
        case 'autotag.apply':
        case 'autotag.skip':
        case 'autotag.leave':
        case 'autotag.paste':
        case 'autotag.search':
        case 'autotag.next':
        case 'autotag.previous':
            document.dispatchEvent(
                new CustomEvent(
                    `shortcut:autotag-${action.slice('autotag.'.length)}`,
                ),
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

        // A focused control that owns this key keeps it: Space activates
        // the button you tabbed to, arrows move the select you opened.
        if (focusedControlOwnsKey(deepEl, keyStr)) return;

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
