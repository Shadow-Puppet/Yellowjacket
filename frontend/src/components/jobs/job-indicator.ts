import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import { designTokens } from '../../styles/tokens.css';
import { srOnly } from '../../styles/sr-only.css';
import { jobStore } from '@store/job-store';
import type { Job } from '@store/job-store';
import { isIndeterminate, progressFraction } from '@store/job-store';
import './job-row';
import './job-details-drawer';
import { applyJobControl } from './job-controls';
import {
    jobIcon,
    stateTone,
    stateLabel,
    jobStateStyles,
} from './job-format';

/** Circumference of the progress ring at r=9. */
const RING_CIRCUMFERENCE = 2 * Math.PI * 9;

/**
 * Persistent background-job indicator for the top bar.
 *
 * Hidden entirely when nothing is running, so it costs no attention in
 * the common case. When work is in flight it shows a determinate ring
 * for a single job, or a count badge for several. Clicking opens a
 * popover with inline pause/stop controls; "Details" opens the drawer.
 */
@customElement('job-indicator')
export class JobIndicator extends LitElement {
    @state()
    private jobs: Job[] = [];

    @state()
    private popoverOpen = false;

    @state()
    private drawerJobId = '';

    @state()
    private drawerOpen = false;

    private unsubscribe: (() => void) | null = null;

    static override styles = [
        designTokens,
        srOnly,
        jobStateStyles,
        css`
            :host {
                display: inline-flex;
                align-items: center;
                position: relative;
            }

            :host([hidden]) {
                display: none;
            }

            .trigger {
                display: inline-flex;
                align-items: center;
                gap: 0.5em;
                padding: 0.3em 0.7em 0.3em 0.35em;
                border: 1px solid rgba(255, 255, 255, 0.12);
                border-radius: 999px;
                background: rgba(255, 255, 255, 0.05);
                color: var(--yj-text-secondary, #adb5bd);
                cursor: pointer;
                font-size: var(--yj-text-sm);
                transition:
                    background-color 140ms ease,
                    border-color 140ms ease,
                    color 140ms ease;
            }

            .trigger:hover {
                background: rgba(255, 255, 255, 0.1);
                color: var(--yj-text-primary, #e9ecef);
            }

            .trigger:focus-visible {
                outline: 2px solid var(--yj-accent, #ffd43b);
                outline-offset: 2px;
            }

            .ring-wrap {
                position: relative;
                width: 22px;
                height: 22px;
                flex-shrink: 0;
            }

            svg {
                width: 22px;
                height: 22px;
                transform: rotate(-90deg);
            }

            .ring-track {
                fill: none;
                stroke: rgba(255, 255, 255, 0.14);
                stroke-width: 2.5;
            }

            .ring-value {
                fill: none;
                stroke: var(--job-tone);
                stroke-width: 2.5;
                stroke-linecap: round;
                transition: stroke-dashoffset 240ms ease;
            }

            /* Indeterminate work spins the whole ring instead of
             * advancing it, so it never implies false precision. */
            .ring-wrap.spin svg {
                animation: spin 1.1s linear infinite;
            }

            @keyframes spin {
                to {
                    transform: rotate(270deg);
                }
            }

            @media (prefers-reduced-motion: reduce) {
                .ring-wrap.spin svg {
                    animation-duration: 3s;
                }
            }

            .ring-glyph {
                position: absolute;
                inset: 0;
                display: flex;
                align-items: center;
                justify-content: center;
                font-size: 9px;
                color: var(--job-tone);
                font-variant-numeric: tabular-nums;
            }

            .label {
                white-space: nowrap;
                max-width: 12rem;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .alert-dot {
                width: 6px;
                height: 6px;
                border-radius: 50%;
                background: #ff8787;
                flex-shrink: 0;
            }

            .panel {
                width: 24rem;
                max-width: 92vw;
                background: var(--yj-surface, #212529);
                border: 1px solid rgba(255, 255, 255, 0.12);
                border-radius: 12px;
                box-shadow: 0 12px 32px rgba(0, 0, 0, 0.45);
                padding: 0.4em;
                max-height: 70vh;
                overflow-y: auto;
            }

            .panel-header {
                display: flex;
                align-items: center;
                justify-content: space-between;
                padding: 0.4em 0.6em 0.5em;
                font-size: var(--yj-text-sm);
                color: var(--yj-text-tertiary, #868e96);
                text-transform: uppercase;
                letter-spacing: 0.06em;
            }

            .panel-header button {
                border: none;
                background: transparent;
                color: var(--yj-text-secondary, #adb5bd);
                font-size: var(--yj-text-sm);
                cursor: pointer;
                text-transform: none;
                letter-spacing: normal;
                padding: 0.15em 0.4em;
                border-radius: 5px;
            }

            .panel-header button:hover {
                background: rgba(255, 255, 255, 0.1);
                color: var(--yj-text-primary, #e9ecef);
            }

            .job-entry {
                border-radius: 8px;
            }

            .job-entry + .job-entry {
                border-top: 1px solid rgba(255, 255, 255, 0.06);
            }

            .details-link {
                display: block;
                width: 100%;
                text-align: left;
                border: none;
                background: transparent;
                color: var(--yj-accent, #ffd43b);
                font-size: var(--yj-text-sm);
                cursor: pointer;
                padding: 0 0.75em 0.6em 3.2em;
            }

            .details-link:hover {
                text-decoration: underline;
            }

            .empty {
                padding: 0.8em;
                font-size: var(--yj-text-sm);
                color: var(--yj-text-tertiary, #868e96);
                font-style: italic;
            }
        `,
    ];

    override connectedCallback(): void {
        super.connectedCallback();
        this.unsubscribe = jobStore.subscribe(() => this.syncJobs());
        void jobStore.init();
        this.syncJobs();
        document.addEventListener('click', this.onDocumentClick);
        document.addEventListener('keydown', this.onKeydown);
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        this.unsubscribe?.();
        this.unsubscribe = null;
        document.removeEventListener('click', this.onDocumentClick);
        document.removeEventListener('keydown', this.onKeydown);
    }

    private syncJobs() {
        this.jobs = jobStore.jobs;
        // The drawer stays mounted so it can animate closed; the pill
        // itself disappears once nothing is happening.
        this.hidden = !jobStore.shouldShowIndicator && !this.drawerOpen;

        if (this.hidden) this.popoverOpen = false;
    }

    private onDocumentClick = (e: MouseEvent) => {
        if (!this.popoverOpen) return;
        if (e.composedPath().includes(this)) return;

        this.popoverOpen = false;
    };

    private onKeydown = (e: KeyboardEvent) => {
        if (e.key === 'Escape' && this.popoverOpen) this.popoverOpen = false;
    };

    private onTriggerClick = (e: Event) => {
        e.stopPropagation();
        this.popoverOpen = !this.popoverOpen;
    };

    private openDetails(id: string) {
        this.drawerJobId = id;
        this.drawerOpen = true;
        this.popoverOpen = false;
    }

    private onDrawerClosed = () => {
        this.drawerOpen = false;
        this.syncJobs();
    };

    private async clearFinished(e: Event) {
        e.stopPropagation();
        await jobStore.clearFinished();
    }

    /** The job whose progress the ring represents. */
    private get primaryJob(): Job | null {
        const working = jobStore.workingJobs;

        if (working.length > 0) return working[0] ?? null;

        const active = jobStore.activeJobs;

        return active[0] ?? null;
    }

    private renderRing(job: Job | null) {
        const activeCount = jobStore.activeJobs.length;
        const fraction = job ? progressFraction(job) : null;
        const tone = job ? stateTone(job) : 'success';

        // Only spin for work that is actually moving. A paused or
        // queued job spinning would say "busy" when nothing is running.
        const spin = Boolean(
            job && job.state === 'running' && isIndeterminate(job),
        );

        const offset =
            fraction === null
                ? RING_CIRCUMFERENCE * 0.72
                : RING_CIRCUMFERENCE * (1 - fraction);

        return html`
            <div class="ring-wrap tone-${tone} ${spin ? 'spin' : ''}">
                <svg viewBox="0 0 22 22" aria-hidden="true">
                    <circle class="ring-track" cx="11" cy="11" r="9"></circle>
                    <circle
                        class="ring-value"
                        cx="11"
                        cy="11"
                        r="9"
                        stroke-dasharray=${RING_CIRCUMFERENCE}
                        stroke-dashoffset=${offset}
                    ></circle>
                </svg>
                <div class="ring-glyph">
                    ${activeCount > 1
                        ? activeCount
                        : html`<wa-icon
                              name=${job ? jobIcon(job) : 'check'}
                          ></wa-icon>`}
                </div>
            </div>
        `;
    }

    /**
     * What the trigger says. Extracted so the live region can announce
     * the same sentence — it swung between "Scanning Music", "3
     * background jobs" and "Finished" in silence (a11y.12).
     */
    private triggerLabel(): string {
        const job = this.primaryJob;
        const activeCount = jobStore.activeJobs.length;

        if (activeCount > 1) return `${activeCount} background jobs`;

        if (job && job.state === 'running') return job.title;

        // "Scanning Music" would be a lie for a job that is paused or
        // queued, so lead with the state instead.
        if (job) return `${stateLabel(job)} · ${job.title}`;

        return 'Finished';
    }

    private renderTrigger() {
        const job = this.primaryJob;
        const hasFailure = jobStore.failedJobs.length > 0;
        const label = this.triggerLabel();

        return html`
            <button
                class="trigger"
                aria-haspopup="true"
                aria-expanded=${this.popoverOpen}
                title="Background jobs"
                @click=${this.onTriggerClick}
            >
                ${this.renderRing(job)}
                <span class="label">${label}</span>
                ${hasFailure
                    ? html`<span
                          class="alert-dot"
                          role="img"
                          aria-label="A background job failed"
                      ></span>`
                    : nothing}
            </button>
        `;
    }

    private renderPanel() {
        const finished = jobStore.finishedJobs;

        return html`
            <!-- Not role="dialog": nothing moves focus into this, traps
                 Tab or handles Escape, so announcing a dialog that never
                 receives focus was a promise it does not keep (a11y.17). -->
            <div class="panel" role="group" aria-label="Background jobs">
                <div class="panel-header">
                    <span>Background jobs</span>
                    ${finished.length > 0
                        ? html`<button @click=${this.clearFinished}>
                              Clear finished
                          </button>`
                        : nothing}
                </div>

                ${this.jobs.length === 0
                    ? html`<div class="empty">Nothing running.</div>`
                    : this.jobs.map(
                          (job) => html`
                              <div class="job-entry">
                                  <job-row
                                      .job=${job}
                                      variant="compact"
                                      open-on-click
                                      @job-control=${applyJobControl}
                                      @job-open=${() =>
                                          this.openDetails(job.id)}
                                  ></job-row>
                                  <button
                                      class="details-link"
                                      @click=${() => this.openDetails(job.id)}
                                  >
                                      Details${job.warnCount
                                          ? ` · ${job.warnCount} warning${job.warnCount === 1 ? '' : 's'}`
                                          : ''}
                                  </button>
                              </div>
                          `,
                      )}
            </div>
        `;
    }

    override render() {
        return html`
            <div class="sr-only" role="status" aria-live="polite">
                ${this.triggerLabel()}
            </div>
            <wa-popup
                placement="bottom-end"
                distance="8"
                ?active=${this.popoverOpen}
            >
                <span slot="anchor">${this.renderTrigger()}</span>
                ${this.popoverOpen ? this.renderPanel() : nothing}
            </wa-popup>

            <job-details-drawer
                job-id=${this.drawerJobId}
                ?open=${this.drawerOpen}
                @drawer-closed=${this.onDrawerClosed}
            ></job-details-drawer>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'job-indicator': JobIndicator;
    }
}
