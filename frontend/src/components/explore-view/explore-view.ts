import { LitElement, html, css } from 'lit';
import { customElement } from 'lit/decorators.js';
import { designTokens } from '../../styles/tokens.css';

@customElement('explore-view')
export class ExploreView extends LitElement {
    static override styles = [
        designTokens,
        css`
            :host {
                display: block;
                padding: 24px;
            }

            h1 {
                margin: 0 0 16px;
                font-size: 1.5rem;
                color: var(--yj-text-primary);
            }

            .placeholder {
                color: var(--yj-text-secondary);
            }
        `,
    ];

    override render() {
        return html`
            <h1>Explore</h1>
            <p class="placeholder">
                Search MusicBrainz to discover artists, albums, and
                tracks.
            </p>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'explore-view': ExploreView;
    }
}
