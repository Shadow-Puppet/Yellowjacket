import { LitElement, html, css, nothing } from 'lit';
import { customElement, property } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';

/**
 * Library status for an entity (artist, album, or track).
 *
 *  - `in-library`: the entity is already in the user's local library.
 *  - `queued`: the entity has been handed off to a download client but
 *    hasn't arrived yet.  Reserved for future download-client plumbing.
 *  - `not-in-library` (default): the entity is not owned and has not
 *    been requested.  A click should eventually kick off a download,
 *    but for now the button is inert.
 */
export type LibraryStatus = 'in-library' | 'queued' | 'not-in-library';

/**
 * Tri-state library status indicator rendered as a small circular
 * button.  Intended to be embedded in track rows, album cards, and
 * artist cards.  The click handler is a no-op for now — the button
 * exists so the layout is stable when "add to library" integration
 * lands later.
 *
 * Colours and glyphs:
 *  - in-library    → green circle, check mark
 *  - queued        → amber circle, hourglass
 *  - not-in-library → grey circle, plus sign
 *
 * Usage:
 *
 *   <library-status-indicator
 *       status="in-library"
 *       entity-type="album"
 *       label="Abbey Road"
 *   ></library-status-indicator>
 */
@customElement('library-status-indicator')
export class LibraryStatusIndicator extends LitElement {
    /** Current status. */
    @property({ type: String })
    status: LibraryStatus = 'not-in-library';

    /**
     * Entity kind for tooltip/aria-label phrasing.  Purely cosmetic
     * right now but required so the label text makes sense regardless
     * of where the indicator is rendered.
     */
    @property({ type: String, attribute: 'entity-type' })
    entityType: 'artist' | 'album' | 'track' = 'track';

    /** Optional label of the entity — used for the tooltip text. */
    @property({ type: String })
    label = '';

    /** Render size in CSS pixels. Default is 20. */
    @property({ type: Number })
    size = 20;

    static override styles = css`
        :host {
            display: inline-flex;
            align-items: center;
            justify-content: center;
            --indicator-size: 20px;
            --indicator-bg: transparent;
            --indicator-fg: #fff;
            --indicator-border: transparent;
        }

        :host([status='in-library']) {
            --indicator-bg: #1db954;
            --indicator-fg: #000;
        }

        :host([status='queued']) {
            --indicator-bg: #f5a623;
            --indicator-fg: #000;
        }

        :host([status='not-in-library']) {
            --indicator-bg: rgba(255, 255, 255, 0.08);
            --indicator-fg: rgba(255, 255, 255, 0.65);
            --indicator-border: rgba(255, 255, 255, 0.2);
        }

        button {
            width: var(--indicator-size);
            height: var(--indicator-size);
            min-width: var(--indicator-size);
            min-height: var(--indicator-size);
            border-radius: 50%;
            background: var(--indicator-bg);
            color: var(--indicator-fg);
            border: 1px solid var(--indicator-border);
            padding: 0;
            cursor: pointer;
            display: inline-flex;
            align-items: center;
            justify-content: center;
            transition:
                background-color 150ms ease,
                color 150ms ease,
                transform 120ms ease,
                border-color 150ms ease;
            -webkit-tap-highlight-color: transparent;
        }

        button:hover {
            transform: scale(1.08);
        }

        :host([status='not-in-library']) button:hover {
            background: rgba(255, 255, 255, 0.14);
            color: #fff;
            border-color: rgba(255, 255, 255, 0.3);
        }

        button:focus-visible {
            outline: 2px solid var(--yj-accent, #1db954);
            outline-offset: 2px;
        }

        wa-icon {
            font-size: calc(var(--indicator-size) * 0.55);
            line-height: 1;
        }

        /* Prevent the button from intercepting drag gestures on album
         * cards — the parent typically owns the drag behaviour. */
        :host {
            user-select: none;
        }
    `;

    private iconName(): string {
        switch (this.status) {
            case 'in-library':
                return 'check';
            case 'queued':
                return 'hourglass-half';
            default:
                return 'plus';
        }
    }

    private tooltip(): string {
        const kind =
            this.entityType === 'album'
                ? 'album'
                : this.entityType === 'artist'
                  ? 'artist'
                  : 'track';
        const name = this.label ? ` "${this.label}"` : '';

        switch (this.status) {
            case 'in-library':
                return `${capitalize(kind)}${name} is in your library`;
            case 'queued':
                return `${capitalize(kind)}${name} is queued for download`;
            default:
                return `Add ${kind}${name} to library`;
        }
    }

    private handleClick(e: Event) {
        // Stop propagation so clicking the button doesn't bubble up
        // to the parent card and trigger navigation.  The click
        // itself is a no-op for now — wire up download-client
        // integration later.
        e.stopPropagation();
    }

    private handleKeydown(e: KeyboardEvent) {
        // Same reasoning: don't let Enter/Space bubble to a wrapping
        // card and trigger navigation.
        if (e.key === 'Enter' || e.key === ' ') {
            e.stopPropagation();
        }
    }

    override render() {
        // Sync the host CSS variable with the configured size.
        if (this.size && this.size !== 20) {
            this.style.setProperty('--indicator-size', `${this.size}px`);
        }

        const title = this.tooltip();

        return html`
            <button
                type="button"
                title=${title}
                aria-label=${title}
                @click=${this.handleClick}
                @keydown=${this.handleKeydown}
            >
                ${this.iconName()
                    ? html`<wa-icon name=${this.iconName()}></wa-icon>`
                    : nothing}
            </button>
        `;
    }
}

function capitalize(s: string): string {
    return s.length > 0 ? s[0].toUpperCase() + s.slice(1) : s;
}

declare global {
    interface HTMLElementTagNameMap {
        'library-status-indicator': LibraryStatusIndicator;
    }
}
