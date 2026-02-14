import { LitElement, html, css } from 'lit';
import { customElement, state } from 'lit/decorators.js';

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
    `;

    @state()
    private activeView: View = 'tracks';

    @state()
    private isDragging = false;

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
        document.addEventListener('mousemove', this.handleMouseMove);
        document.addEventListener('mouseup', this.handleMouseUp);
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        document.removeEventListener('mousemove', this.handleMouseMove);
        document.removeEventListener('mouseup', this.handleMouseUp);
    }

    override render() {
        return html`
            <div
                class="resize-handle ${this.isDragging ? 'dragging' : ''}"
                @mousedown=${this.handleMouseDown}
            ></div>
            <ul>
                ${this.navItems.map(item => html`
                    <li
                        class="${this.activeView === item.id ? 'active' : ''}"
                        @click=${() => this.navigate(item.id)}
                    >
                        <p>${item.label}</p>
                    </li>
                `)}
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
