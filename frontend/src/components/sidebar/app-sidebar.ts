import { LitElement, html, css } from 'lit';
import { customElement, state, property } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { designTokens } from '../../styles/tokens.css';

import type { DragActiveDetail } from '@utils/drag-controller';
import { ActiveViewController } from '@store/controllers/active-view-controller';
import { ViewVisibilityController } from '@store/controllers/view-visibility-controller';
import { VIEW_META } from '../../services/view-meta';
import type { View } from '../../services/view-meta';

const MIN_WIDTH = 56;
const MAX_WIDTH = 400;
const DEFAULT_WIDTH = 200;
const COLLAPSE_WIDTH = 142;

/**
 * Below this viewport width the sidebar collapses itself to icons.
 * `.collapsed` existed and only a manual drag ever reached it (H-11),
 * so a small window kept a 200 px sidebar it could not afford and the
 * content pane wore the whole loss.
 */
const AUTO_COLLAPSE_VIEWPORT = 900;

@customElement('app-sidebar')
export class AppSidebar extends LitElement {
    static override styles = [designTokens, css`
        :host {
            display: block;
            position: relative;
            height: 100%;
            background-color: var(--yj-bg-surface, #212529);
            min-width: ${MIN_WIDTH}px;
            max-width: ${MAX_WIDTH}px;
            /* Eleven items need ~406 px and the pane is whatever the
               window leaves it — 352 px at 700x480, which clipped Jobs
               and Settings behind the player bar with no way to reach
               them (H-11).  Collapsing to icons does not help: it is a
               width mode, and this is the height. */
            overflow-y: auto;
            overflow-x: hidden;
            scrollbar-width: thin;
        }

        .resize-handle {
            position: absolute;
            top: 0;
            right: 0;
            width: 4px;
            height: 100%;
            cursor: col-resize;
            background-color: transparent;
            transition: background-color 0.15s ease;
            z-index: 10;
        }

        .resize-handle:hover,
        .resize-handle.dragging {
            background-color: var(--yj-text-tertiary, #6c757d);
        }

        ul {
            list-style-type: none;
            margin: 0;
            padding: 16px;
        }

        /* The nav item is a real <button>: it was a bare <li @click>,
           which is why tabbing through the whole app reached fourteen
           controls and not one of them was navigation (H-5). */
        li {
            display: block;
        }

        li button {
            display: flex;
            width: 100%;
            align-items: center;
            gap: 10px;
            border: none;
            border-radius: 5px;
            padding: 8px;
            cursor: pointer;
            background: none;
            color: inherit;
            font: inherit;
            text-align: left;
            transition: background-color 0.15s ease;
        }

        li button wa-icon {
            font-size: var(--yj-icon-md);
            flex-shrink: 0;
            width: 20px;
            text-align: center;
        }

        li button:hover {
            background-color: var(--yj-bg-elevated, #343a40);
        }

        li button:focus-visible {
            outline: 2px solid var(--yj-accent, #ffd43b);
            outline-offset: -2px;
        }

        li button.active {
            background-color: var(--yj-bg-overlay, #495057);
        }

        li button p {
            margin: 0;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        li button.drag-hover {
            background-color: var(
                --yj-accent-bg-strong,
                rgba(255, 212, 59, 0.15)
            );
            outline: 1px dashed var(--yj-accent, #ffd43b);
            outline-offset: -1px;
        }

        li button p {
            font-size: var(--yj-text-md);
        }

        /* Icon-only collapsed mode */
        :host(.collapsed) ul {
            padding: 8px;
        }

        :host(.collapsed) li button {
            justify-content: center;
            padding: 10px;
        }

        :host(.collapsed) li button p {
            display: none;
        }

        :host(.collapsed) li button wa-icon {
            font-size: var(--yj-icon-md);
        }
    `];

    /** Delay in ms before a drag-hover triggers navigation. */
    private static readonly HOVER_NAV_DELAY = 600;

    /**
     * Which item is lit, read from the shell rather than tracked here.
     *
     * This used to be a `@state()` field defaulting to `home` -- the
     * landing view -- because "the sidebar does not hear a `navigate`
     * it did not send". That default was the only honest moment it
     * ever had: a back-navigation dispatches no `navigate`, so the
     * highlight stayed on the view the user had just left (#72), and
     * the copy of this component that `bottom-nav` mounts inside its
     * drawer opened on `home` from whatever page you were standing on.
     * The shell publishes the active view now, so there is nothing to
     * default and nothing to keep in step.
     */
    private activeCtrl = new ActiveViewController(this);

    @state()
    private isDragging = false;

    @state()
    private collapsed = false;

    /**
     * Keep the labels regardless of the viewport, for a host that has
     * made room for them -- `bottom-nav`'s drawer, which is the whole
     * screen wide on the phone where this would otherwise auto-collapse
     * to icons. The auto-collapse is a *width* response to a narrow
     * shell, and inside a drawer the shell is not what the sidebar is
     * sharing space with.
     */
    @property({ type: Boolean, reflect: true })
    expanded = false;

    /** The width the user chose, restored when the window grows back. */
    private userWidth = DEFAULT_WIDTH;

    private narrowViewport: MediaQueryList | null =
        null;

    /** Whether a track drag is in progress somewhere in the app. */
    @state()
    private trackDragActive = false;

    /** The nav item ID being hovered during a drag. */
    @state()
    private dragHoverView: View | null = null;

    private dragHoverTimer: ReturnType<
        typeof setTimeout
    > | null = null;

    /**
     * Which destinations the user has kept (#25). The list below is
     * still the whole set and its order -- this only filters it, and
     * only for drawing: a hidden view is still reachable by `navigate`,
     * which is what detail views and the launch page depend on.
     */
    private visibilityCtrl = new ViewVisibilityController(this);

    private navItems = VIEW_META;

    override connectedCallback() {
        super.connectedCallback();
        this.style.width = `${DEFAULT_WIDTH}px`;
        this.narrowViewport = window.matchMedia(
            `(max-width: ${AUTO_COLLAPSE_VIEWPORT - 1}px)`,
        );
        this.narrowViewport.addEventListener(
            'change',
            this.onViewportChange,
        );
        this.applyViewportWidth();
        document.addEventListener(
            'mousemove',
            this.handleMouseMove,
        );
        document.addEventListener(
            'mouseup',
            this.handleMouseUp,
        );
        document.addEventListener(
            'yj-drag-active',
            this.onDragActive as EventListener,
        );
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.narrowViewport?.removeEventListener(
            'change',
            this.onViewportChange,
        );
        this.narrowViewport = null;
        document.removeEventListener(
            'mousemove',
            this.handleMouseMove,
        );
        document.removeEventListener(
            'mouseup',
            this.handleMouseUp,
        );
        document.removeEventListener(
            'yj-drag-active',
            this.onDragActive as EventListener,
        );
        this.clearDragHoverTimer();
    }

    override updated() {
        this.classList.toggle('collapsed', this.collapsed);
    }

    override render() {
        return html`
            <div
                class="resize-handle ${this.isDragging ? 'dragging' : ''}"
                @mousedown=${this.handleMouseDown}
            ></div>
            <nav aria-label="Main">
            <ul>
                ${this.navItems
            .filter((item) => this.visibilityCtrl.visible(item.id))
            .map((item) => {
            const active = this.activeCtrl.isActive(item.id);
            const classes = [
                active
                    ? 'active'
                    : '',
                this.dragHoverView === item.id
                    ? 'drag-hover'
                    : '',
            ]
                .filter(Boolean)
                .join(' ');

            return html`
                        <li>
                            <button
                                type="button"
                                class=${classes}
                                data-testid="nav-${item.id}"
                                aria-current=${active
                    ? 'page'
                    : 'false'}
                                @click=${() =>
                    this.navigate(item.id)}
                                @dragover=${(e: DragEvent) =>
                    this.onNavDragOver(
                        e,
                        item.id,
                    )}
                                @dragleave=${() =>
                    this.onNavDragLeave(
                        item.id,
                    )}
                                @drop=${(e: DragEvent) =>
                    this.onNavDrop(e)}
                            >
                                <wa-icon
                                    name=${item.icon}
                                ></wa-icon>
                                <p>${item.label}</p>
                            </button>
                        </li>
                    `;
        })}
            </ul>
            </nav>
        `;
    }

    private handleMouseDown = (e: MouseEvent) => {
        e.preventDefault();
        this.isDragging = true;
    };

    private handleMouseMove = (e: MouseEvent) => {
        if (!this.isDragging) return;

        const rect = this.getBoundingClientRect();
        const newWidth = e.clientX - rect.left;
        const clampedWidth = Math.min(
            Math.max(newWidth, MIN_WIDTH),
            MAX_WIDTH,
        );

        this.style.width = `${clampedWidth}px`;
        this.collapsed = clampedWidth < COLLAPSE_WIDTH;
        this.userWidth = clampedWidth;
    };

    private onViewportChange = () => {
        this.applyViewportWidth();
    };

    /**
     * Icons below the breakpoint, the user's own width above it.  The
     * width is inline (set here and by the drag handle), so this cannot
     * be a media query in the stylesheet.
     */
    private applyViewportWidth() {
        const narrow =
            !this.expanded &&
            (this.narrowViewport?.matches ?? false);
        const width = narrow
            ? MIN_WIDTH
            : this.userWidth;

        this.style.width = `${width}px`;
        this.collapsed = width < COLLAPSE_WIDTH;
    }

    private handleMouseUp = () => {
        this.isDragging = false;
    };

    // =================================================================
    // Drag-hover navigation
    // =================================================================

    /** Views that accept track drops. */
    private static readonly DROP_VIEWS: Set<View> =
        new Set(['playlists']);

    private onDragActive = (
        e: CustomEvent<DragActiveDetail>,
    ) => {
        this.trackDragActive = e.detail.active;

        if (!e.detail.active) {
            this.clearDragHoverTimer();
            this.dragHoverView = null;
        }
    };

    private onNavDragOver = (
        e: DragEvent,
        view: View,
    ) => {
        if (!this.trackDragActive) return;

        if (!AppSidebar.DROP_VIEWS.has(view)) return;

        // Prevent default so that `drop` can fire.
        e.preventDefault();

        if (e.dataTransfer) {
            e.dataTransfer.dropEffect = 'copy';
        }

        // Already hovering this item — no-op.
        if (this.dragHoverView === view) return;

        this.clearDragHoverTimer();
        this.dragHoverView = view;

        this.dragHoverTimer = setTimeout(() => {
            this.dragHoverTimer = null;

            if (this.dragHoverView === view) {
                this.navigate(view);
            }
        }, AppSidebar.HOVER_NAV_DELAY);
    };

    private onNavDragLeave = (view: View) => {
        if (this.dragHoverView !== view) return;

        this.clearDragHoverTimer();
        this.dragHoverView = null;
    };

    private onNavDrop = (e: DragEvent) => {
        // The drop target is the playlist-view, not
        // the sidebar itself — just prevent the
        // default browser action.
        e.preventDefault();
        this.clearDragHoverTimer();
        this.dragHoverView = null;
    };

    private clearDragHoverTimer() {
        if (this.dragHoverTimer !== null) {
            clearTimeout(this.dragHoverTimer);
            this.dragHoverTimer = null;
        }
    }

    private navigate(view: View) {
        // No optimistic highlight: the shell answers, and it answers
        // synchronously in `handleNavigate` before it awaits anything.
        // Setting it here as well is the second opinion this fix
        // removes -- it is what let a click's highlight survive a
        // navigation the shell then handled differently.
        this.dispatchEvent(new CustomEvent('navigate', {
            detail: { view },
            bubbles: true,
            composed: true,
        }));
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'app-sidebar': AppSidebar;
    }
}
