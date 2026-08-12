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
 *    been requested.
 */
export type LibraryStatus = 'in-library' | 'queued' | 'not-in-library';

/**
 * Tri-state library status indicator: a small circular badge embedded
 * in track rows, album cards, and artist cards.
 *
 * **It is a badge, not a control.**  It was a `<button>` whose click
 * handler was a `stopPropagation()` and a comment saying to wire up
 * the download client later — so an Explore results page offered 20
 * keyboard stops (of 66) that promised an action and performed none,
 * and every one of them announced itself as a button.  A control that
 * cannot act is worse than no control: it costs the keyboard user the
 * tab stop *and* the expectation.
 *
 * So it is `role="img"` with a label, until there is something to
 * click.  When the download-client integration lands, the right change
 * is to make it a `<button>` again *with a handler* — not to add the
 * handler to something already shaped like a button.
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

        .badge {
            /* A <button> gets box-sizing: border-box from the UA
             * stylesheet and a <span> does not, so dropping the button
             * grew the badge by its 1px border on each side — 36px to
             * 38px, caught by the stored screenshot. */
            box-sizing: border-box;
            width: var(--indicator-size);
            height: var(--indicator-size);
            min-width: var(--indicator-size);
            min-height: var(--indicator-size);
            border-radius: 50%;
            background: var(--indicator-bg);
            color: var(--indicator-fg);
            border: 1px solid var(--indicator-border);
            padding: 0;
            display: inline-flex;
            align-items: center;
            justify-content: center;
            -webkit-tap-highlight-color: transparent;
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
                // Not "Add … to library": nothing here adds anything.
                // The old copy was the button's promise written out.
                return `${capitalize(kind)}${name} is not in your library`;
        }
    }

    override render() {
        // Sync the host CSS variable with the configured size.
        if (this.size && this.size !== 20) {
            this.style.setProperty('--indicator-size', `${this.size}px`);
        }

        const title = this.tooltip();

        return html`
            <span class="badge" role="img" title=${title} aria-label=${title}>
                ${this.iconName()
                    ? html`<wa-icon name=${this.iconName()} aria-hidden="true"></wa-icon>`
                    : nothing}
            </span>
        `;
    }
}

function capitalize(s: string): string {
    return s.length > 0 ? (s[0] ?? '').toUpperCase() + s.slice(1) : s;
}

declare global {
    interface HTMLElementTagNameMap {
        'library-status-indicator': LibraryStatusIndicator;
    }
}
