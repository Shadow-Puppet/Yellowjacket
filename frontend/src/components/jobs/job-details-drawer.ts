import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/drawer/drawer.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { designTokens } from '../../styles/tokens.css';
import { jobStore } from '@store/job-store';
import type { Job } from '@store/job-store';
import './job-row';
import './job-log-view';
import { applyJobControl } from './job-controls';
import {
    stateLabel,
    stateTone,
    formatElapsed,
    jobStateStyles,
} from './job-format';

/** How often the open drawer re-fetches the job log. */
const LOG_POLL_MS = 1500;

/**
 * Right-hand drawer showing everything known about one job: its
 * progress row, stage breakdown, statistics, and log tail.
 *
 * A drawer rather than a route, because these jobs run *while* the user
 * is doing something else — navigating away from their music to read a
 * scan log would defeat the purpose.
 */
@customElement('job-details-drawer')
export class JobDetailsDrawer extends LitElement {
    /** ID of the job to show. Empty string closes the drawer. */
    @property({ type: String, attribute: 'job-id' })
    jobId = '';

    @property({ type: Boolean, reflect: true })
    open = false;

    @state()
    private job: Job | null = null;

    @state()
    private logVersion = 0;

    private unsubscribe: (() => void) | null = null;

    private pollTimer: ReturnType<typeof setInterval> | null = null;

    static override styles = [
        designTokens,
        jobStateStyles,
        css`
            wa-drawer {
                --size: 34rem;
            }

            .content {
                display: flex;
                flex-direction: column;
                gap: 1.1em;
                height: 100%;
                min-height: 0;
            }

            .section-title {
                font-size: var(--yj-text-sm);
                text-transform: uppercase;
                letter-spacing: 0.06em;
                color: var(--yj-text-tertiary, #868e96);
                margin-bottom: 0.5em;
            }

            .summary {
                display: grid;
                grid-template-columns: repeat(
                    auto-fit,
                    minmax(7.5rem, 1fr)
                );
                gap: 0.6em 1em;
            }

            .summary-item {
                display: flex;
                flex-direction: column;
                gap: 0.15em;
            }

            .summary-label {
                font-size: var(--yj-text-xs);
                color: var(--yj-text-tertiary, #868e96);
            }

            .summary-value {
                font-size: var(--yj-text-md);
                color: var(--yj-text-primary, #e9ecef);
                font-variant-numeric: tabular-nums;
                overflow-wrap: anywhere;
            }

            .summary-value.tone {
                color: var(--job-tone);
            }

            .stages {
                display: flex;
                flex-direction: column;
                gap: 0.4em;
            }

            .stage {
                display: grid;
                grid-template-columns: 1.2em 1fr auto;
                align-items: center;
                gap: 0.6em;
                font-size: var(--yj-text-md);
                color: var(--yj-text-secondary, #adb5bd);
            }

            .stage-icon {
                font-size: var(--yj-icon-sm);
            }

            .stage.running {
                color: var(--yj-text-primary, #e9ecef);
            }

            .stage.running .stage-icon {
                color: var(--yj-accent-text, #ffd43b);
            }

            .stage.complete .stage-icon {
                color: var(--yj-success-text, #51cf66);
            }

            .stage.error {
                color: var(--yj-error-text, #ff8787);
            }

            .stage-count {
                font-size: var(--yj-text-sm);
                color: var(--yj-text-tertiary, #868e96);
                font-variant-numeric: tabular-nums;
            }

            .stage-error {
                grid-column: 2 / -1;
                font-size: var(--yj-text-sm);
                color: var(--yj-error-text, #ff8787);
                overflow-wrap: anywhere;
            }

            .log-section {
                flex: 1;
                display: flex;
                flex-direction: column;
                min-height: 12rem;
            }

            job-log-view {
                flex: 1;
                min-height: 0;
            }

            .subtitle {
                font-size: var(--yj-text-sm);
                color: var(--yj-text-tertiary, #868e96);
                overflow-wrap: anywhere;
            }
        `,
    ];

    override connectedCallback(): void {
        super.connectedCallback();
        this.unsubscribe = jobStore.subscribe(() => this.syncJob());
        this.syncJob();
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        this.unsubscribe?.();
        this.unsubscribe = null;
        this.stopPolling();
    }

    override updated(changed: Map<string, unknown>) {
        if (changed.has('jobId') || changed.has('open')) {
            this.syncJob();

            if (this.open && this.jobId) {
                void this.refreshLog();
                this.startPolling();
            } else {
                this.stopPolling();
            }
        }
    }

    private syncJob() {
        this.job = this.jobId ? (jobStore.getJob(this.jobId) ?? null) : null;
    }

    /**
     * Logs are polled while the drawer is open rather than pushed with
     * every snapshot — a scan can emit hundreds of warnings and only
     * this panel ever renders them.
     */
    private startPolling() {
        this.stopPolling();
        this.pollTimer = setInterval(() => void this.refreshLog(), LOG_POLL_MS);
    }

    private stopPolling() {
        if (this.pollTimer) {
            clearInterval(this.pollTimer);
            this.pollTimer = null;
        }
    }

    private async refreshLog() {
        if (!this.jobId) return;

        await jobStore.loadLog(this.jobId);
        this.logVersion += 1;
    }

    private onHide = () => {
        this.open = false;
        this.dispatchEvent(
            new CustomEvent('drawer-closed', { bubbles: true, composed: true }),
        );
    };

    private stageIcon(state: string): string {
        switch (state) {
            case 'complete':
                return 'circle-check';
            case 'running':
                return 'arrows-rotate';
            case 'error':
                return 'triangle-exclamation';
            case 'skipped':
                return 'circle-minus';
            default:
                return 'circle-info';
        }
    }

    private renderStages(job: Job) {
        if (!job.stages?.length) return nothing;

        return html`
            <div>
                <div class="section-title">Stages</div>
                <div class="stages">
                    ${job.stages.map(
                        (stage) => html`
                            <div class="stage ${stage.state}">
                                <wa-icon
                                    class="stage-icon"
                                    name=${this.stageIcon(stage.state)}
                                ></wa-icon>
                                <span>${stage.name}</span>
                                <span class="stage-count">
                                    ${stage.total > 0
                                        ? `${stage.current.toLocaleString()} / ${stage.total.toLocaleString()}`
                                        : stage.state}
                                </span>
                                ${stage.error
                                    ? html`<div class="stage-error">
                                          ${stage.error}
                                      </div>`
                                    : nothing}
                            </div>
                        `,
                    )}
                </div>
            </div>
        `;
    }

    private renderSummary(job: Job) {
        const items = [
            { label: 'Status', value: stateLabel(job), tone: true },
            { label: 'Elapsed', value: formatElapsed(job), tone: false },
            ...(job.stats ?? []).map((s) => ({
                label: s.label,
                value: s.value,
                tone: false,
            })),
        ];

        return html`
            <div>
                <div class="section-title">Summary</div>
                <div class="summary">
                    ${items.map(
                        (item) => html`
                            <div class="summary-item">
                                <span class="summary-label">${item.label}</span>
                                <span
                                    class="summary-value ${item.tone
                                        ? 'tone'
                                        : ''}"
                                    >${item.value}</span
                                >
                            </div>
                        `,
                    )}
                </div>
            </div>
        `;
    }

    override render() {
        const job = this.job;

        return html`
            <wa-drawer
                ?open=${this.open}
                label=${job?.title ?? 'Job details'}
                @wa-hide=${this.onHide}
                class="tone-${job ? stateTone(job) : 'muted'}"
            >
                ${job
                    ? html`
                          <div class="content">
                              ${job.subtitle
                                  ? html`<div class="subtitle">
                                        ${job.subtitle}
                                    </div>`
                                  : nothing}

                              <job-row
                                  .job=${job}
                                  variant="full"
                                  @job-control=${applyJobControl}
                              ></job-row>

                              ${this.renderSummary(job)}
                              ${this.renderStages(job)}

                              <div class="log-section">
                                  <div class="section-title">Output</div>
                                  <job-log-view
                                      .job=${job}
                                      .entries=${jobStore.cachedLog(job.id)}
                                      data-version=${this.logVersion}
                                  ></job-log-view>
                              </div>
                          </div>
                      `
                    : html`<p>This job is no longer available.</p>`}
            </wa-drawer>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'job-details-drawer': JobDetailsDrawer;
    }
}
