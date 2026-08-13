import { LitElement, html, css, nothing } from 'lit';
import { customElement, property } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/progress-bar/progress-bar.js';
import '@awesome.me/webawesome/dist/components/spinner/spinner.js';
import { designTokens } from '../../styles/tokens.css';
import type { Job } from '@store/job-store';
import { isTerminal, isIndeterminate } from '@store/job-store';
import {
    jobIcon,
    stateTone,
    statusLine,
    progressPercent,
    formatElapsed,
    jobStateStyles,
} from './job-format';

/**
 * A single background job: icon, title, status line, progress bar, and
 * whichever controls the job declares support for.
 *
 * Controls are rendered from `job.caps` rather than from the job kind,
 * so a job that gains pause support on the backend needs no change
 * here. The row emits `job-control` and `job-open`; the host decides
 * what to do with them, which is what lets the same row serve both the
 * top-bar popover and the full jobs page.
 */
@customElement('job-row')
export class JobRow extends LitElement {
    @property({ type: Object })
    job!: Job;

    /**
     * `compact` is the popover density — one line of status, small
     * controls. `full` adds elapsed time and per-job statistics.
     */
    @property({ type: String })
    variant: 'compact' | 'full' = 'compact';

    /** Whether clicking the row should emit `job-open`. */
    @property({ type: Boolean, attribute: 'open-on-click' })
    openOnClick = false;

    static override styles = [
        designTokens,
        jobStateStyles,
        css`
            :host {
                display: block;
            }

            .row {
                display: grid;
                grid-template-columns: auto 1fr auto;
                gap: 0.75em;
                align-items: start;
                padding: 0.6em 0.75em;
                border-radius: 8px;
                transition: background-color 120ms ease;
            }

            .row.clickable {
                cursor: pointer;
            }

            .row.clickable:hover {
                background: rgba(255, 255, 255, 0.05);
            }

            .icon {
                display: flex;
                align-items: center;
                justify-content: center;
                width: 28px;
                height: 28px;
                border-radius: 50%;
                background: rgba(255, 255, 255, 0.06);
                color: var(--job-tone);
                font-size: var(--yj-icon-sm);
                flex-shrink: 0;
            }

            .body {
                min-width: 0;
            }

            .title {
                font-size: var(--yj-text-md);
                color: var(--yj-text-primary, #e9ecef);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .status {
                font-size: var(--yj-text-sm);
                color: var(--yj-text-secondary, #adb5bd);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
                margin-top: 0.15em;
            }

            .status .tone {
                color: var(--job-tone);
            }

            wa-progress-bar {
                margin-top: 0.45em;
                --height: 4px;
                --indicator-color: var(--job-tone);
                --track-color: rgba(255, 255, 255, 0.09);
            }

            .indeterminate {
                margin-top: 0.5em;
                height: 4px;
                border-radius: 2px;
                background: rgba(255, 255, 255, 0.09);
                overflow: hidden;
            }

            /* A slider that sweeps left to right, for work with no
             * known denominator (the pre-walk file count, for one).
             * Static unless the job is actually moving. */
            /* Stopped: a dim full-width bar, which reads as "no progress
             * information" rather than a partial fill implying a
             * percentage the job never reported. */
            .indeterminate::after {
                content: '';
                display: block;
                width: 100%;
                height: 100%;
                border-radius: 2px;
                background: var(--job-tone);
                opacity: 0.3;
            }

            .indeterminate.moving::after {
                width: 35%;
                opacity: 1;
                animation: sweep 1.4s ease-in-out infinite;
            }

            @keyframes sweep {
                0% {
                    transform: translateX(-100%);
                }
                100% {
                    transform: translateX(320%);
                }
            }

            @media (prefers-reduced-motion: reduce) {
                .indeterminate.moving::after {
                    animation: none;
                    width: 100%;
                    opacity: 0.5;
                }
            }

            .stats {
                display: flex;
                flex-wrap: wrap;
                gap: 0.25em 1.1em;
                margin-top: 0.5em;
                font-size: var(--yj-text-sm);
            }

            .stat-label {
                color: var(--yj-text-tertiary, #868e96);
            }

            .stat-value {
                color: var(--yj-text-primary, #e9ecef);
                font-variant-numeric: tabular-nums;
                margin-left: 0.35em;
            }

            .error {
                margin-top: 0.45em;
                font-size: var(--yj-text-sm);
                color: var(--yj-error-text, #ff8787);
                overflow-wrap: anywhere;
            }

            .controls {
                display: flex;
                align-items: center;
                gap: 0.15em;
                flex-shrink: 0;
            }

            button {
                display: inline-flex;
                align-items: center;
                justify-content: center;
                width: 26px;
                height: 26px;
                border: none;
                border-radius: 6px;
                background: transparent;
                color: var(--yj-text-secondary, #adb5bd);
                cursor: pointer;
                font-size: var(--yj-icon-sm);
                transition:
                    background-color 120ms ease,
                    color 120ms ease;
            }

            button:hover:not(:disabled) {
                background: rgba(255, 255, 255, 0.1);
                color: var(--yj-text-primary, #e9ecef);
            }

            button.danger:hover:not(:disabled) {
                color: var(--yj-error-text, #ff8787);
            }

            button:disabled {
                opacity: 0.35;
                cursor: default;
            }

            button:focus-visible {
                outline: 2px solid var(--yj-accent, #ffd43b);
                outline-offset: 1px;
            }
        `,
    ];

    private emitControl(action: 'pause' | 'resume' | 'cancel' | 'dismiss') {
        this.dispatchEvent(
            new CustomEvent('job-control', {
                detail: { id: this.job.id, action },
                bubbles: true,
                composed: true,
            }),
        );
    }

    private emitOpen() {
        this.dispatchEvent(
            new CustomEvent('job-open', {
                detail: { id: this.job.id },
                bubbles: true,
                composed: true,
            }),
        );
    }

    private renderProgress() {
        const job = this.job;

        if (isTerminal(job)) return nothing;

        if (isIndeterminate(job)) {
            // Only sweep while work is actually moving — an animated bar
            // on a paused job reads as progress that isn't happening.
            return html`
                <div
                    class="indeterminate ${job.state === 'running'
                        ? 'moving'
                        : ''}"
                ></div>
            `;
        }

        return html`
            <wa-progress-bar
                label=${job.title}
                value=${progressPercent(job) ?? 0}
            ></wa-progress-bar>
        `;
    }

    private renderStats() {
        if (this.variant !== 'full') return nothing;
        if (!this.job.stats?.length) return nothing;

        return html`
            <div class="stats">
                ${this.job.stats.map(
                    (stat) => html`
                        <div>
                            <span class="stat-label">${stat.label}</span>
                            <span class="stat-value">${stat.value}</span>
                        </div>
                    `,
                )}
            </div>
        `;
    }

    private renderControls() {
        const job = this.job;

        // A finished job offers only dismissal.
        if (isTerminal(job)) {
            return html`
                <button
                    class="danger"
                    title="Dismiss"
                    aria-label="Dismiss ${job.title}"
                    @click=${this.onDismiss}
                >
                    <wa-icon name="xmark"></wa-icon>
                </button>
            `;
        }

        const paused = job.state === 'paused';
        const settling = job.state === 'pausing' || job.state === 'cancelling';

        return html`
            ${job.caps.pausable
                ? html`
                      <button
                          title=${paused ? 'Resume' : 'Pause'}
                          aria-label=${paused
                              ? `Resume ${job.title}`
                              : `Pause ${job.title}`}
                          ?disabled=${settling}
                          @click=${paused ? this.onResume : this.onPause}
                      >
                          <wa-icon name=${paused ? 'play' : 'pause'}></wa-icon>
                      </button>
                  `
                : nothing}
            ${job.caps.cancellable
                ? html`
                      <button
                          class="danger"
                          title="Stop"
                          aria-label="Stop ${job.title}"
                          ?disabled=${job.state === 'cancelling'}
                          @click=${this.onCancel}
                      >
                          <wa-icon name="stop"></wa-icon>
                      </button>
                  `
                : nothing}
        `;
    }

    private onPause = (e: Event) => {
        e.stopPropagation();
        this.emitControl('pause');
    };

    private onResume = (e: Event) => {
        e.stopPropagation();
        this.emitControl('resume');
    };

    private onCancel = (e: Event) => {
        e.stopPropagation();
        this.emitControl('cancel');
    };

    private onDismiss = (e: Event) => {
        e.stopPropagation();
        this.emitControl('dismiss');
    };

    private onRowClick = () => {
        if (this.openOnClick) this.emitOpen();
    };

    override render() {
        const job = this.job;

        if (!job) return nothing;

        const tone = stateTone(job);
        const elapsed =
            this.variant === 'full' ? ` · ${formatElapsed(job)}` : '';

        return html`
            <div
                class="row tone-${tone} ${this.openOnClick ? 'clickable' : ''}"
                @click=${this.onRowClick}
            >
                <div class="icon">
                    <wa-icon name=${jobIcon(job)}></wa-icon>
                </div>

                <div class="body">
                    <div class="title">${job.title}</div>
                    <div class="status">
                        <span class="tone">${statusLine(job)}</span>${elapsed}
                    </div>
                    ${this.renderProgress()} ${this.renderStats()}
                    ${job.error
                        ? html`<div class="error">${job.error}</div>`
                        : nothing}
                </div>

                <div class="controls">${this.renderControls()}</div>
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'job-row': JobRow;
    }
}
