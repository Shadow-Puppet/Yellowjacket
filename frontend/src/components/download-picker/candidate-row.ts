import { LitElement, html, css, nothing } from 'lit';
import { customElement, property } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/button/button.js';
import { designTokens } from '../../styles/tokens.css';
import type { DownloadCandidate } from '@store/download-store';
import { candidateSummary, scorePercent } from '@store/download-store';

/**
 * One candidate in the download picker.
 *
 * The row shows match and quality as two separate meters rather than
 * one blended score, because they fail differently: a flawless copy of
 * the wrong album is useless, a mediocre copy of the right one is
 * merely disappointing, and only the user knows which they will accept.
 * Collapsing them into a single number would make the ranking
 * impossible to argue with.
 */
@customElement('candidate-row')
export class CandidateRow extends LitElement {
    @property({ type: Object })
    candidate!: DownloadCandidate;

    /** Marks the row the ranking put first. */
    @property({ type: Boolean, attribute: 'is-best' })
    isBest = false;

    @property({ type: Boolean })
    busy = false;

    static override styles = [
        designTokens,
        css`
            :host {
                display: block;
            }

            .row {
                display: grid;
                grid-template-columns: 1fr auto;
                gap: 1em;
                align-items: center;
                padding: 0.75em 0.9em;
                border: 1px solid var(--wa-color-surface-border, #333);
                border-radius: 8px;
                background: var(--wa-color-surface-raised, #1c1c1c);
            }

            .row.best {
                border-color: var(--wa-color-brand-fill-loud, #d9a441);
            }

            .title {
                font-weight: 600;
                overflow: hidden;
                text-overflow: ellipsis;
                white-space: nowrap;
            }

            .summary {
                font-size: 0.85em;
                opacity: 0.75;
                margin-top: 0.15em;
                overflow: hidden;
                text-overflow: ellipsis;
                white-space: nowrap;
            }

            .badges {
                display: flex;
                gap: 0.4em;
                margin-top: 0.4em;
                flex-wrap: wrap;
            }

            .badge {
                font-size: 0.72em;
                padding: 0.1em 0.45em;
                border-radius: 4px;
                background: rgba(255, 255, 255, 0.08);
                white-space: nowrap;
            }

            .badge.best {
                background: var(--wa-color-brand-fill-loud, #d9a441);
                color: #111;
                font-weight: 600;
            }

            .badge.warn {
                background: rgba(217, 119, 65, 0.25);
            }

            .meters {
                display: grid;
                grid-template-columns: auto 1fr auto;
                gap: 0.3em 0.5em;
                align-items: center;
                margin-top: 0.5em;
                font-size: 0.75em;
                max-width: 340px;
            }

            .meter-label {
                opacity: 0.7;
            }

            .track {
                height: 5px;
                border-radius: 3px;
                background: rgba(255, 255, 255, 0.1);
                overflow: hidden;
            }

            .fill {
                height: 100%;
                border-radius: 3px;
                transition: width 150ms ease;
            }

            .fill.match {
                background: var(--wa-color-success-fill-loud, #4c9f70);
            }

            .fill.match.low {
                background: var(--wa-color-warning-fill-loud, #d97741);
            }

            .fill.quality {
                background: var(--wa-color-brand-fill-loud, #6a8cc7);
            }

            .value {
                font-variant-numeric: tabular-nums;
                opacity: 0.85;
            }
        `,
    ];

    /** Match below this reads as "probably not what you asked for". */
    private static readonly LOW_MATCH = 0.7;

    override render() {
        const candidate = this.candidate;
        if (!candidate) return nothing;

        const match = candidate.match?.overall ?? 0;
        const quality = candidate.quality?.overall ?? 0;

        return html`
            <div class="row ${this.isBest ? 'best' : ''}">
                <div class="info">
                    <div class="title" title=${candidate.title}>
                        ${candidate.title}
                    </div>
                    <div class="summary">${candidateSummary(candidate)}</div>
                    ${this.renderBadges()}
                    <div class="meters">
                        <span class="meter-label">Match</span>
                        <div class="track">
                            <div
                                class="fill match ${match < CandidateRow.LOW_MATCH
                                    ? 'low'
                                    : ''}"
                                style="width: ${Math.round(match * 100)}%"
                            ></div>
                        </div>
                        <span class="value">${scorePercent(match)}</span>

                        <span class="meter-label">Quality</span>
                        <div class="track">
                            <div
                                class="fill quality"
                                style="width: ${Math.round(quality * 100)}%"
                            ></div>
                        </div>
                        <span class="value">${scorePercent(quality)}</span>
                    </div>
                </div>

                <wa-button
                    variant=${this.isBest ? 'brand' : 'neutral'}
                    size="small"
                    ?disabled=${this.busy}
                    @click=${this.onPick}
                >
                    Download
                </wa-button>
            </div>
        `;
    }

    private renderBadges() {
        const candidate = this.candidate;
        const badges = [];

        if (this.isBest) {
            badges.push(html`<span class="badge best">Best match</span>`);
        }

        // An unanchored match is a guess: there was no MusicBrainz ID to
        // check the result against, so the score cannot mean much and
        // saying so is more honest than showing a confident number.
        if (candidate.match && !candidate.match.anchored) {
            badges.push(
                html`<span class="badge warn" title="No MusicBrainz match to verify against">
                    Unverified
                </span>`,
            );
        }

        if (candidate.quality?.mixed) {
            badges.push(
                html`<span class="badge warn" title="Files are not all the same format">
                    Mixed formats
                </span>`,
            );
        }

        const completeness = candidate.match?.completeness ?? 1;

        if (completeness < 1 && completeness > 0) {
            badges.push(
                html`<span class="badge warn">
                    ${scorePercent(completeness)} of tracks
                </span>`,
            );
        }

        if (candidate.protocol && candidate.protocol !== 'direct') {
            badges.push(html`<span class="badge">${candidate.protocol}</span>`);
        }

        return badges.length > 0
            ? html`<div class="badges">${badges}</div>`
            : nothing;
    }

    private onPick() {
        this.dispatchEvent(
            new CustomEvent('candidate-pick', {
                detail: { candidateId: this.candidate.id },
                bubbles: true,
                composed: true,
            }),
        );
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'candidate-row': CandidateRow;
    }
}
