import { LitElement, html, css, nothing } from 'lit';
import { customElement, property } from 'lit/decorators.js';

import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { designTokens } from '../../styles/tokens.css';

import { formatMilliseconds } from '@utils/time';

/**
 * Reusable track info display component.
 *
 * All fields are optional — the parent decides which to provide.
 * Handles text truncation, fallback display for missing title
 * (uses filename from filePath), and a cover art placeholder.
 *
 * @example
 * ```html
 * <track-info
 *   trackTitle="Song Name"
 *   artist="Artist Name"
 *   duration="240000"
 * ></track-info>
 *
 * <track-info
 *   trackTitle="Song Name"
 *   artist="Artist Name"
 *   coverArt="/covers/abc.jpg"
 *   coverArtSmall="/covers/abc_sm.jpg"
 * ></track-info>
 * ```
 */
@customElement('track-info')
export class TrackInfo extends LitElement {
    @property() trackTitle?: string;
    @property() artist?: string;
    @property() album?: string;
    @property() coverArt?: string;
    @property() coverArtSmall?: string;
    @property() duration?: string;
    @property() filePath?: string;

    static override styles = [designTokens, css`
        :host {
            display: flex;
            align-items: center;
            gap: 10px;
            min-width: 0;
        }

        .cover-art {
            width: 36px;
            height: 36px;
            flex-shrink: 0;
            border-radius: 3px;
            overflow: hidden;
        }

        .cover-art img {
            width: 100%;
            height: 100%;
            object-fit: cover;
        }

        .cover-placeholder {
            width: 100%;
            height: 100%;
            background-color: var(--yj-bg-elevated, #2a2d30);
            display: flex;
            align-items: center;
            justify-content: center;
        }

        .cover-placeholder wa-icon {
            color: var(--yj-text-tertiary, #666);
            font-size: var(--yj-icon-md);
        }

        .text {
            display: flex;
            flex-direction: column;
            gap: 1px;
            min-width: 0;
            flex: 1;
        }

        .title {
            font-size: var(--yj-text-md);
            font-weight: 500;
            color: var(--yj-text-primary, #fff);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        .secondary {
            font-size: var(--yj-text-xs);
            color: var(--yj-text-tertiary, #888);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        .duration {
            font-size: var(--yj-text-sm);
            color: var(--yj-text-tertiary, #888);
            flex-shrink: 0;
            font-variant-numeric: tabular-nums;
        }
    `];

    override render() {
        const showCover =
            this.coverArt !== undefined || this.coverArtSmall !== undefined;
        const displayTitle = this.getDisplayTitle();
        const secondaryParts = this.getSecondaryText();

        return html`
            ${showCover ? this.renderCoverArt() : nothing}
            <div class="text">
                ${displayTitle
                    ? html`<span class="title">${displayTitle}</span>`
                    : nothing}
                ${secondaryParts
                    ? html`<span class="secondary"
                          >${secondaryParts}</span
                      >`
                    : nothing}
            </div>
            ${this.duration
                ? html`<span class="duration"
                      >${formatMilliseconds(this.duration)}</span
                  >`
                : nothing}
        `;
    }

    private renderCoverArt() {
        const src = this.coverArtSmall ?? this.coverArt;

        if (!src) {
            return html`
                <div class="cover-art">
                    <div class="cover-placeholder">
                        <wa-icon name="music"></wa-icon>
                    </div>
                </div>
            `;
        }

        return html`
            <div class="cover-art">
                <img
                    src="${src}"
                    alt="Cover art"
                    @error=${this.handleImageError}
                />
            </div>
        `;
    }

    private handleImageError = (e: Event) => {
        const img = e.target as HTMLImageElement;

        // Try full-size image if thumbnail failed.
        if (this.coverArt && img.src !== this.coverArt) {
            img.src = this.coverArt;

            return;
        }

        // Replace with placeholder on final failure.
        const container = img.parentElement;

        if (container) {
            container.innerHTML =
                '<div class="cover-placeholder">' +
                '<wa-icon name="music"></wa-icon>' +
                '</div>';
        }
    };

    private getDisplayTitle(): string {
        if (this.trackTitle) return this.trackTitle;

        if (this.filePath) {
            const parts = this.filePath.split(/[\\/]/);
            const filename = parts[parts.length - 1] ?? this.filePath;

            return filename.replace(/\.[^.]+$/, '');
        }

        return '';
    }

    private getSecondaryText(): string {
        const parts: string[] = [];

        if (this.artist) {
            parts.push(this.artist);
        }

        if (this.album) {
            parts.push(this.album);
        }

        return parts.join(' \u2014 ');
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'track-info': TrackInfo;
    }
}
