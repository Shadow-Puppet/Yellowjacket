import { css } from 'lit';
import type {
    ReactiveController,
    ReactiveControllerHost,
} from 'lit';

/**
 * Host interface for components using the ContextMenuController.
 * The host must provide access to the popup elements (typically
 * via @query decorators) and optionally a callback for cleanup
 * when the context menu closes.
 */
export interface ContextMenuHost
    extends ReactiveControllerHost {
    updateComplete: Promise<boolean>;
    shadowRoot: ShadowRoot | null;
    /** Return the main context-menu popup element. */
    getContextMenuPopup(): HTMLElement | undefined;
    /** Return the playlist submenu popup element. */
    getPlaylistSubmenuPopup(): HTMLElement | undefined;
    /**
     * Called when the context menu is closed by an
     * outside click/contextmenu/mousedown.  Components
     * use this to clear domain-specific state (e.g.
     * contextMenuAlbumId, contextMenuGenreName).
     */
    onContextMenuClose?(): void;
}

/** Submenu close delay in milliseconds. */
const SUBMENU_CLOSE_DELAY = 150;

/**
 * Reusable context menu controller that manages the open/close
 * state of a wa-popup context menu with an optional playlist
 * submenu.
 *
 * Handles:
 * - Opening the context menu at a given screen position
 * - Closing on outside click / contextmenu / mousedown
 * - Playlist submenu open/close with hover delay
 * - Document-level event listener lifecycle
 *
 * Does NOT handle:
 * - Rendering the context menu template (component-specific)
 * - Dispatching menu actions (component-specific)
 * - File path resolution for the playlist picker
 */
export class ContextMenuController
    implements ReactiveController
{
    private host: ContextMenuHost;

    /** Whether the main context menu popup is open. */
    contextMenuOpen = false;

    /** Whether the playlist submenu popup is open. */
    playlistSubmenuOpen = false;

    /** File paths to pass to the playlist picker. */
    playlistFilePaths: string[] = [];

    private submenuCloseTimer: ReturnType<
        typeof setTimeout
    > | null = null;

    /** Bound close handler for document events. */
    private closeHandler = () => this.close();

    /** Bound mousedown handler for outside-click detection. */
    private mousedownCloseHandler = (
        e: MouseEvent,
    ) => {
        const path = e.composedPath();
        const popup =
            this.host.getContextMenuPopup();
        const submenu =
            this.host.getPlaylistSubmenuPopup();

        if (popup && path.includes(popup)) return;

        if (submenu && path.includes(submenu)) {
            return;
        }

        this.close();
    };

    constructor(host: ContextMenuHost) {
        this.host = host;
        host.addController(this);
    }

    // =================================================================
    // LIFECYCLE
    // =================================================================

    hostConnected(): void {
        document.addEventListener(
            'click',
            this.closeHandler,
        );
        document.addEventListener(
            'contextmenu',
            this.closeHandler,
        );
        document.addEventListener(
            'mousedown',
            this.mousedownCloseHandler,
        );
    }

    hostDisconnected(): void {
        document.removeEventListener(
            'click',
            this.closeHandler,
        );
        document.removeEventListener(
            'contextmenu',
            this.closeHandler,
        );
        document.removeEventListener(
            'mousedown',
            this.mousedownCloseHandler,
        );
        this.clearSubmenuCloseTimer();
    }

    // =================================================================
    // MAIN CONTEXT MENU
    // =================================================================

    /**
     * Open the context menu at the given screen
     * coordinates using a virtual anchor.
     */
    openAt(clientX: number, clientY: number): void {
        this.contextMenuOpen = true;
        this.host.requestUpdate();

        void this.host.updateComplete.then(() => {
            const popup =
                this.host.getContextMenuPopup();

            if (!popup) return;

            (popup as any).anchor = {
                getBoundingClientRect() {
                    return {
                        width: 0,
                        height: 0,
                        x: clientX,
                        y: clientY,
                        top: clientY,
                        left: clientX,
                        right: clientX,
                        bottom: clientY,
                    };
                },
            };
            (popup as any).active = true;
        });
    }

    /**
     * Close the context menu and playlist submenu.
     * Notifies the host via `onContextMenuClose()` so
     * it can clear domain-specific state.
     */
    close(): void {
        if (!this.contextMenuOpen) return;

        this.closePlaylistSubmenu();
        this.contextMenuOpen = false;
        this.playlistFilePaths = [];

        const popup =
            this.host.getContextMenuPopup();

        if (popup) {
            (popup as any).active = false;
        }

        this.host.onContextMenuClose?.();
        this.host.requestUpdate();
    }

    // =================================================================
    // PLAYLIST SUBMENU
    // =================================================================

    /**
     * Open the playlist submenu, positioning it
     * relative to the `.submenu-item` trigger element.
     *
     * @param filePaths - File paths to pass to the
     *   playlist picker.  The caller resolves these
     *   before calling (sync or async).
     */
    async showPlaylistSubmenu(
        filePaths: string[],
    ): Promise<void> {
        this.clearSubmenuCloseTimer();

        if (this.playlistSubmenuOpen) return;

        if (filePaths.length === 0) return;

        this.playlistFilePaths = filePaths;
        this.playlistSubmenuOpen = true;
        this.host.requestUpdate();

        await this.host.updateComplete;

        const submenu =
            this.host.getPlaylistSubmenuPopup();
        const trigger =
            this.host.shadowRoot?.querySelector(
                '.submenu-item',
            );

        if (submenu && trigger) {
            (submenu as any).anchor = trigger;
            (submenu as any).active = true;
        }

        const picker =
            this.host.shadowRoot?.querySelector(
                'playlist-picker',
            ) as
                | (HTMLElement & { reset(): void })
                | null;

        picker?.reset();
    }

    /** Close the playlist submenu. */
    closePlaylistSubmenu(): void {
        this.clearSubmenuCloseTimer();

        if (!this.playlistSubmenuOpen) return;

        this.playlistSubmenuOpen = false;

        const submenu =
            this.host.getPlaylistSubmenuPopup();

        if (submenu) {
            (submenu as any).active = false;
        }

        this.host.requestUpdate();
    }

    /** Clear any pending submenu close timer. */
    clearSubmenuCloseTimer(): void {
        if (this.submenuCloseTimer !== null) {
            clearTimeout(this.submenuCloseTimer);
            this.submenuCloseTimer = null;
        }
    }

    /**
     * Schedule the submenu to close after a short
     * delay.  Used on mouseleave to allow the user to
     * move between the trigger and the submenu popup.
     */
    scheduleSubmenuClose = (): void => {
        this.clearSubmenuCloseTimer();
        this.submenuCloseTimer = setTimeout(() => {
            this.submenuCloseTimer = null;
            this.closePlaylistSubmenu();
        }, SUBMENU_CLOSE_DELAY);
    };

    /**
     * Convenience callback for the playlist-picker's
     * `playlist-action-complete` event.  Closes the
     * entire context menu.
     */
    onPlaylistActionComplete = (): void => {
        this.close();
    };
}

/**
 * Shared CSS styles for context menu and playlist submenu
 * popups.  Components include these via the static styles
 * array: `static override styles = [myStyles, contextMenuStyles]`.
 */
export const contextMenuStyles = css`
    #context-menu {
        z-index: 200;
    }

    .context-menu-panel {
        background-color: var(
            --yj-bg-elevated,
            #343a40
        );
        border: 1px solid var(--yj-border, #444);
        border-radius: 6px;
        padding: 4px 0;
        box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
        min-width: 160px;
    }

    .context-menu-panel wa-dropdown-item {
        cursor: pointer;
        --wa-color-text-normal: var(
            --yj-text-primary,
            #fff
        );
        font-size: 13px;
    }

    .context-menu-panel wa-dropdown-item:hover {
        background-color: var(
            --yj-hover-overlay,
            rgba(255, 255, 255, 0.1)
        );
    }

    .submenu-item {
        position: relative;
    }

    .submenu-arrow {
        font-size: 10px;
        margin-left: auto;
        padding-left: 12px;
    }

    #playlist-submenu {
        z-index: 210;
    }
`;
