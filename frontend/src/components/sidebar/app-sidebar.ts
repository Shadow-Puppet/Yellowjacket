import { LitElement, html, css } from 'lit';
import { customElement, state } from 'lit/decorators.js';

import type { DragActiveDetail } from '@utils/drag-controller';

type View = 'home' | 'libraries' | 'playlists' | 'artists' | 'albums' | 'tracks';

interface NavItem {
    id: View;
    label: string;
}

const MIN_WIDTH = 120;
const MAX_WIDTH = 400;
const DEFAULT_WIDTH = 200;

@customElement('app-sidebar')
export class AppSidebar extends LitElement {
    static override styles = css`
        :host {
            display: block;
            position: relative;
            height: 100%;
            background-color: #212529;
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
            background-color: #6c757d;
        }

        ul {
            list-style-type: none;
            margin: 0;
            padding: 1em;
        }

        li {
            text-align: left;
            border-radius: 5px;
            padding: 0.5em;
            cursor: pointer;
            transition: background-color 0.15s ease;
        }

        li:hover {
            background-color: #343a40;
        }

        li.active {
            background-color: #495057;
        }

        li p {
            margin: 0;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        li.drag-hover {
            background-color: rgba(255, 212, 59, 0.15);
            outline: 1px dashed #ffd43b;
            outline-offset: -1px;
        }
    `;

    /** Delay in ms before a drag-hover triggers navigation. */
    private static readonly HOVER_NAV_DELAY = 600;

    @state()
    private activeView: View = 'tracks';

    @state()
    private isDragging = false;

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
        { id: 'home', label: 'Home' },
        { id: 'libraries', label: 'Libraries' },
        { id: 'playlists', label: 'Playlists' },
        { id: 'artists', label: 'Artists' },
        { id: 'albums', label: 'Albums' },
        { id: 'tracks', label: 'Tracks' },
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
        const clampedWidth = Math.min(Math.max(newWidth, MIN_WIDTH), MAX_WIDTH);

        this.style.width = `${clampedWidth}px`;
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
