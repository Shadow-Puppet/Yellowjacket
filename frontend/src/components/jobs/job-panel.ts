import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { designTokens } from '../../styles/tokens.css';
import { jobStore } from '@store/job-store';
import type { Job, JobKind } from '@store/job-store';
import { isTerminal } from '@store/job-store';
import './job-row';
import './job-details-drawer';
import { applyJobControl } from './job-controls';
import { jobStateStyles } from './job-format';

/**
 * The background work of one kind, rendered wherever that work is
 * started or configured.
 *
 * #27 folded the Jobs tab away, and the shape it folded into is this
 * rather than one "Background jobs" panel in Settings — which would
 * have been the tab again under another name. Reading the app first
 * turned up that **four of the five job kinds already had a home**
 * showing their work: Settings → Search Index draws per-tier index
 * progress, `downloads-view` draws every download's lifecycle state,
 * `autotag-view` draws its own apply ring, and only `library-scan` had
 * nowhere but the tab. What none of those four had is the *generic*
 * affordances — pause, cancel, "Details", the log, and a finished job
 * you can dismiss — which is what this carries to each of them.
 *
 * Three things about it are load-bearing.
 *
 * **The controls are `applyJobControl`, not a reimplementation.** That
 * is what keeps the "you will discard hours of downloading"
 * confirmation on an index build alive across the move: it is keyed on
 * `KindIndexBuild` inside the shared handler, and a host that rendered
 * its own buttons would silently drop it.
 *
 * **A panel with nothing to say renders nothing at all**, host padding
 * included — an idle panel in four places is four pieces of furniture
 * describing an absence. That is the rule `startBackfillJob` follows
 * for the indicator, one layer up.
 *
 * **There is no "Clear finished" here**, because `ClearFinishedJobs` is
 * global: a Clear in the Libraries panel would silently discard the
 * index build's history too. A finished row dismisses itself, which is
 * per-job and is what `job-row` already offers.
 */
@customElement('job-panel')
export class JobPanel extends LitElement {
    /**
     * Comma-separated job kinds, e.g. `index-build,catalog-enrich`.
     *
     * An attribute rather than a property because every call site is a
     * literal in a template, and one of them is inside an HTMX-adjacent
     * settings page where a property binding would be one more thing to
     * remember.
     */
    @property({ type: String })
    kinds = '';

    /** Heading above the rows. Omitted renders no heading. */
    @property({ type: String })
    heading = '';

    @state()
    private jobs: Job[] = [];

    @state()
    private drawerJobId = '';

    @state()
    private drawerOpen = false;

    private unsubscribe: (() => void) | null = null;

    static override styles = [
        designTokens,
        jobStateStyles,
        css`
            :host {
                display: block;
                margin-top: 1em;
            }

            /* An empty panel takes no room at all, margin included. */
            :host([hidden]) {
                display: none;
            }

            h3 {
                font-size: var(--yj-text-sm);
                text-transform: uppercase;
                letter-spacing: 0.06em;
                color: var(--yj-text-tertiary, #868e96);
                margin: 0 0 0.5em;
            }

            .card {
                background: var(--yj-bg-surface, #2b3035);
                border: 1px solid var(--yj-border, #495057);
                border-radius: 6px;
                overflow: hidden;
            }

            .job-entry {
                display: flex;
                align-items: center;
                gap: 0.75em;
                padding: 0.6em 0.8em;
                border-bottom: 1px solid var(--yj-border-subtle, #3a4046);
            }

            .job-entry:last-child {
                border-bottom: none;
            }

            job-row {
                flex: 1;
                /* A grid child's implicit minimum is its content, and a
                   job title is long. */
                min-width: 0;
            }

            .details-btn {
                background: none;
                border: 1px solid var(--yj-border, #495057);
                border-radius: 4px;
                color: var(--yj-text-secondary, #adb5bd);
                cursor: pointer;
                font-family: inherit;
                font-size: var(--yj-text-sm);
                padding: 0.3em 0.6em;
                white-space: nowrap;
            }

            .details-btn:hover {
                color: var(--yj-text-primary, #e9ecef);
            }
        `,
    ];

    override connectedCallback() {
        super.connectedCallback();
        this.unsubscribe = jobStore.subscribe(() => {
            this.jobs = jobStore.jobs;
        });
        this.jobs = jobStore.jobs;
        void jobStore.init();
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.unsubscribe?.();
        this.unsubscribe = null;
    }

    /** The kinds this panel answers for. */
    private get wanted(): ReadonlySet<string> {
        return new Set(
            this.kinds
                .split(',')
                .map((k) => k.trim())
                .filter(Boolean),
        );
    }

    private get mine(): Job[] {
        const wanted = this.wanted;

        return this.jobs.filter((job) => wanted.has(job.kind as JobKind));
    }

    private openDetails(id: string) {
        this.drawerJobId = id;
        this.drawerOpen = true;
    }

    private onDrawerClosed = () => {
        this.drawerOpen = false;
    };

    override render() {
        const mine = this.mine;

        // Hidden rather than empty: see the class comment. The drawer
        // goes with it, since it can only have been opened from a row.
        this.hidden = mine.length === 0;

        if (mine.length === 0) return nothing;

        const active = mine.filter((job) => !isTerminal(job));
        const finished = mine.filter(isTerminal);

        return html`
            ${this.heading ? html`<h3>${this.heading}</h3>` : nothing}
            <div class="card">
                ${[...active, ...finished].map(
                    (job) => html`
                        <div class="job-entry">
                            <job-row
                                .job=${job}
                                variant="full"
                                @job-control=${applyJobControl}
                            ></job-row>
                            <button
                                type="button"
                                class="details-btn"
                                @click=${() => this.openDetails(job.id)}
                            >
                                Details${job.warnCount
                                    ? ` · ${job.warnCount}⚠`
                                    : ''}
                            </button>
                        </div>
                    `,
                )}
            </div>

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
        'job-panel': JobPanel;
    }
}
