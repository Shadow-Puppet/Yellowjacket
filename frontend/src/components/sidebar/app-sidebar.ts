import { LitElement, html, css } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { designTokens } from '../../styles/tokens.css';

import type { DragActiveDetail } from '@utils/drag-controller';

type View = 'home' | 'playlists' | 'artists' | 'genres' | 'albums' | 'tracks' | 'explore' | 'autotag' | 'settings';

interface NavItem {
    id: View;
    label: string;
    icon: string;
}

const MIN_WIDTH = 56;
const MAX_WIDTH = 400;
const DEFAULT_WIDTH = 200;
const COLLAPSE_WIDTH = 142;

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

        li {
            display: flex;
            align-items: center;
            gap: 10px;
            border-radius: 5px;
            padding: 8px;
            cursor: pointer;
            transition: background-color 0.15s ease;
        }

        li wa-icon {
            font-size: var(--yj-icon-md);
            flex-shrink: 0;
            width: 20px;
            text-align: center;
        }

        li:hover {
            background-color: var(--yj-bg-elevated, #343a40);
        }

        li.active {
            background-color: var(--yj-bg-overlay, #495057);
        }

        li p {
            margin: 0;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        li.drag-hover {
            background-color: var(
                --yj-accent-bg-strong,
                rgba(255, 212, 59, 0.15)
            );
            outline: 1px dashed var(--yj-accent, #ffd43b);
            outline-offset: -1px;
        }

        li p {
            font-size: var(--yj-text-md);
        }

        /* Icon-only collapsed mode */
        :host(.collapsed) ul {
            padding: 8px;
        }

        :host(.collapsed) li {
            justify-content: center;
            padding: 10px;
        }

        :host(.collapsed) li p {
            display: none;
        }

        :host(.collapsed) li wa-icon {
            font-size: var(--yj-icon-md);
        }
    `];

    /** Delay in ms before a drag-hover triggers navigation. */
    private static readonly HOVER_NAV_DELAY = 600;

    @state()
    private activeView: View = 'tracks';

    @state()
    private isDragging = false;

    @state()
    private collapsed = false;

    /** Whether a track drag is in progress somewhere in the app. */
    @state()
    private trackDragActive = false;

    /** The nav item ID being hovered during a drag. */
    @state()
    private dragHoverView: View | null = null;

    private dragHoverTimer: ReturnType<
        typeof setTimeout
    > | null = null;

    private navItems: NavItem[] = [
        { id: 'home', label: 'Home', icon: 'house' },
        { id: 'playlists', label: 'Playlists', icon: 'list' },
        { id: 'artists', label: 'Artists', icon: 'user-group' },
        { id: 'genres', label: 'Genres', icon: 'masks-theater' },
        { id: 'albums', label: 'Albums', icon: 'compact-disc' },
        { id: 'tracks', label: 'Tracks', icon: 'music' },
        { id: 'explore', label: 'Explore', icon: 'globe' },
        { id: 'autotag', label: 'Autotag', icon: 'tag' },
        { id: 'settings', label: 'Settings', icon: 'gear' },
    ];

    override connectedCallback() {
        super.connectedCallback();
        this.style.width = `${DEFAULT_WIDTH}px`;
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
            <ul>
                ${this.navItems.map((item) => {
            const classes = [
                this.activeView === item.id
                    ? 'active'
                    : '',
                this.dragHoverView === item.id
                    ? 'drag-hover'
                    : '',
            ]
                .filter(Boolean)
                .join(' ');

            return html`
                        <li
                            class=${classes}
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
                        </li>
                    `;
        })}
            </ul>
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
    };

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
        this.activeView = view;
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
