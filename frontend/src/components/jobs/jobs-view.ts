import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@components/page-header/page-header';
import { designTokens } from '../../styles/tokens.css';
import {
    GetAllLibrariesWithTrackCounts,
    ScanLibrary,
    ScanAllLibraries,
    FullRescan,
} from '@go/library/Library';
import type { library } from '@go/models';
import { EventsOn } from '@runtime/runtime';
import { Events } from '../../events';
import { jobStore } from '@store/job-store';
import type { Job } from '@store/job-store';
import { notificationStore } from '@store/notification-store';
import { describeError } from '@utils/describe-error';
import { confirmAction } from '../confirm-dialog/confirm-dialog';
import './job-row';
import './job-details-drawer';
import { applyJobControl } from './job-controls';
import { jobStateStyles } from './job-format';
import { ViewLifecycleMixin } from '../../utils/view-lifecycle';

type LibraryInfo = library.Info;

/** Job states meaning the job will not progress further. */
const TERMINAL_STATES: ReadonlySet<string> = new Set([
    'complete',
    'cancelled',
    'error',
]);

/**
 * Full-page view of background work: everything running right now, the
 * per-library scan controls that used to live in Settings, and a short
 * history of what recently finished.
 *
 * This is the same job rows as the top-bar popover at a larger density —
 * one implementation, two placements, so the two can never disagree.
 */
@customElement('jobs-view')
export class JobsView extends ViewLifecycleMixin(LitElement) {
    @state()
    private jobs: Job[] = [];

    @state()
    private libraries: LibraryInfo[] = [];

    @state()
    private drawerJobId = '';

    @state()
    private drawerOpen = false;

    /**
     * Set between pressing a scan button and the job snapshot that
     * proves it started. `anyScanning` is derived from `JobsChanged`,
     * which is coalesced at 250 ms — long enough for a second click to
     * start a second scan (errors.M5).
     */
    @state()
    private starting = false;

    private unsubscribe: (() => void) | null = null;

    private eventCleanups: Array<() => void> = [];

    static override styles = [
        designTokens,
        jobStateStyles,
        css`
            :host {
                display: block;
                overflow-y: auto;
                height: 100%;
                padding: 1.5em 1.75em 3em;
                box-sizing: border-box;
            }

            /* The header supplies its own padding and rule, so it runs
               to the edge of a host that pads its own content. */
            page-header {
                margin: -1.5em -1.75em 1em;
            }

            h1 {
                font-size: var(--yj-text-xl);
                color: var(--yj-text-primary, #e9ecef);
                margin: 0 0 0.2em;
            }

            .page-sub {
                font-size: var(--yj-text-md);
                color: var(--yj-text-tertiary, #868e96);
                margin: 0 0 1.75em;
            }

            section {
                margin-bottom: 2em;
            }

            .section-head {
                display: flex;
                align-items: center;
                justify-content: space-between;
                gap: 1em;
                margin-bottom: 0.75em;
            }

            h2 {
                font-size: var(--yj-text-sm);
                text-transform: uppercase;
                letter-spacing: 0.06em;
                color: var(--yj-text-tertiary, #868e96);
                margin: 0;
            }

            .card {
                background: rgba(255, 255, 255, 0.03);
                border: 1px solid rgba(255, 255, 255, 0.07);
                border-radius: 10px;
                overflow: hidden;
            }

            .card > * + * {
                border-top: 1px solid rgba(255, 255, 255, 0.06);
            }

            .job-entry {
                display: grid;
                grid-template-columns: 1fr auto;
                align-items: center;
                gap: 0.5em;
                padding-right: 0.75em;
            }

            .empty {
                padding: 1.1em;
                font-size: var(--yj-text-md);
                color: var(--yj-text-tertiary, #868e96);
                font-style: italic;
            }

            .library-row {
                display: grid;
                grid-template-columns: 1fr auto;
                align-items: center;
                gap: 1em;
                padding: 0.75em 0.9em;
            }

            .library-name {
                font-size: var(--yj-text-md);
                color: var(--yj-text-primary, #e9ecef);
            }

            .library-meta {
                font-size: var(--yj-text-sm);
                color: var(--yj-text-tertiary, #868e96);
                margin-top: 0.15em;
                overflow-wrap: anywhere;
            }

            .library-state {
                font-size: var(--yj-text-sm);
                color: var(--job-tone);
                margin-top: 0.15em;
            }

            button.action {
                display: inline-flex;
                align-items: center;
                gap: 0.45em;
                border: 1px solid rgba(255, 255, 255, 0.14);
                border-radius: 7px;
                background: rgba(255, 255, 255, 0.05);
                color: var(--yj-text-primary, #e9ecef);
                font-size: var(--yj-text-sm);
                padding: 0.42em 0.85em;
                cursor: pointer;
                white-space: nowrap;
                transition:
                    background-color 120ms ease,
                    border-color 120ms ease;
            }

            button.action:hover:not(:disabled) {
                background: rgba(255, 255, 255, 0.11);
            }

            button.action:disabled {
                opacity: 0.4;
                cursor: default;
            }

            button.action:focus-visible {
                outline: 2px solid var(--yj-accent, #ffd43b);
                outline-offset: 2px;
            }

            button.action.danger {
                color: #ff6b6b;
                border-color: rgba(255, 107, 107, 0.35);
            }

            button.action.danger:hover:not(:disabled) {
                background: rgba(255, 107, 107, 0.12);
            }

            button.link {
                border: none;
                background: transparent;
                color: var(--yj-accent, #ffd43b);
                font-size: var(--yj-text-sm);
                cursor: pointer;
                padding: 0.2em 0.4em;
                border-radius: 5px;
            }

            button.link:hover {
                text-decoration: underline;
            }

            .details-btn {
                border: none;
                background: transparent;
                color: var(--yj-text-secondary, #adb5bd);
                font-size: var(--yj-text-sm);
                cursor: pointer;
                padding: 0.3em 0.5em;
                border-radius: 6px;
                white-space: nowrap;
            }

            .details-btn:hover {
                background: rgba(255, 255, 255, 0.1);
                color: var(--yj-text-primary, #e9ecef);
            }
        `,
    ];

    protected override onViewActivate(): void {
        this.unsubscribe = jobStore.subscribe(() => {
            this.jobs = jobStore.jobs;
        });
        void jobStore.init();
        this.jobs = jobStore.jobs;
        void this.loadLibraries();

        // Library CRUD happens elsewhere; keep the picker in step.
        for (const event of [
            Events.LibraryAdded,
            Events.LibraryRemoved,
            Events.LibraryRenamed,
            Events.LibraryScanComplete,
        ]) {
            this.eventCleanups.push(
                EventsOn(event, () => void this.loadLibraries()),
            );
        }
    }

    protected override onViewDeactivate(): void {
        this.unsubscribe?.();
        this.unsubscribe = null;
        this.eventCleanups.forEach((off) => off());
        this.eventCleanups = [];
    }

    private async loadLibraries(): Promise<void> {
        try {
            this.libraries = (await GetAllLibrariesWithTrackCounts()) ?? [];
        } catch (err) {
            console.error('Failed to load libraries:', err);
        }
    }

    /** The scan job for a library, if one is registered. */
    private jobForLibrary(id: number): Job | undefined {
        return jobStore.getJob(`scan:${id}`);
    }

    private openDetails(id: string) {
        this.drawerJobId = id;
        this.drawerOpen = true;
    }

    private onDrawerClosed = () => {
        this.drawerOpen = false;
    };

    /**
     * Run something that starts a job, holding the buttons until the
     * snapshot lands and saying so when it does not start at all.
     *
     * Persistent, not a toast: the user asked for work to happen, it
     * did not, and retrying is exactly the useful response.
     */
    private async startJob(
        what: string,
        start: () => Promise<unknown>,
        retry: () => void,
    ): Promise<void> {
        if (this.starting) return;

        this.starting = true;

        try {
            await start();
        } catch (err) {
            console.error(`${what} failed:`, err);
            notificationStore.persistent({
                key: 'scan-start',
                title: 'Scan did not start',
                text: `${what} failed. ${describeError(err)}`,
                detail: String(err),
                action: { label: 'Try again', run: retry },
            });
        } finally {
            this.starting = false;
        }
    }

    private async startScan(id: number) {
        await this.startJob(
            'Scanning that library',
            () => ScanLibrary(id),
            () => void this.startScan(id),
        );
    }

    private async startAllScans() {
        await this.startJob(
            'Scanning your libraries',
            () => ScanAllLibraries(),
            () => void this.startAllScans(),
        );
    }

    private async clearFinished() {
        await jobStore.clearFinished();
    }

    private async fullRescan() {
        const ok = await confirmAction({
            title: 'Full rescan',
            message:
                'This deletes all library data — including downloaded ' +
                'cover art — and rebuilds it from your files.',
            impact:
                'It is not the same as “Scan now”, which only picks up ' +
                'what changed.',
            confirmLabel: 'Rebuild everything',
            danger: true,
        });

        if (!ok) return;

        await this.startJob(
            'The full rescan',
            () => FullRescan(),
            () => void this.fullRescan(),
        );
    }

    private renderJobList(list: Job[], emptyText: string) {
        if (list.length === 0) {
            return html`<div class="card">
                <div class="empty">${emptyText}</div>
            </div>`;
        }

        return html`
            <div class="card">
                ${list.map(
                    (job) => html`
                        <div class="job-entry">
                            <job-row
                                .job=${job}
                                variant="full"
                                @job-control=${applyJobControl}
                            ></job-row>
                            <button
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
        `;
    }

    /** The status line under a library name in the scan-control list. */
    private libraryStatus(job: Job | undefined): string | null {
        if (!job) return null;

        switch (job.state) {
            case 'running':
                return job.phase ? `Scanning · ${job.phase}` : 'Scanning';
            case 'queued':
                return 'Queued';
            case 'paused':
                return 'Paused';
            case 'pausing':
                return 'Pausing…';
            case 'cancelling':
                return 'Stopping…';
            default:
                return null;
        }
    }

    private renderLibraryRow(lib: LibraryInfo) {
        const job = this.jobForLibrary(lib.id);
        const status = this.libraryStatus(job);
        const busy = status !== null;

        return html`
            <div class="library-row">
                <div>
                    <div class="library-name">${lib.name}</div>
                    <div class="library-meta">
                        ${lib.trackCount.toLocaleString()} tracks · ${lib.path}
                    </div>
                    ${status
                        ? html`<div class="library-state">${status}</div>`
                        : nothing}
                </div>

                ${busy
                    ? html`
                          <button
                              class="link"
                              @click=${() => this.openDetails(`scan:${lib.id}`)}
                          >
                              View progress
                          </button>
                      `
                    : html`
                          <button
                              class="action"
                              ?disabled=${this.starting}
                              @click=${() => this.startScan(lib.id)}
                          >
                              <wa-icon name="arrows-rotate"></wa-icon>
                              Scan now
                          </button>
                      `}
            </div>
        `;
    }

    override render() {
        // Derived from `this.jobs` rather than the store getters so Lit
        // sees the reactive dependency and re-renders on every snapshot.
        const active = this.jobs.filter((j) => !TERMINAL_STATES.has(j.state));
        const finished = this.jobs.filter((j) => TERMINAL_STATES.has(j.state));
        const anyScanning = this.libraries.some((lib) =>
            Boolean(this.libraryStatus(this.jobForLibrary(lib.id))),
        );

        return html`
            <page-header heading="Background jobs"></page-header>
            <p class="page-sub">
                Library scans and search index builds, with their progress and
                output.
            </p>

            <section>
                <div class="section-head">
                    <h2>Running now</h2>
                </div>
                ${this.renderJobList(active, 'Nothing is running.')}
            </section>

            <section>
                <div class="section-head">
                    <h2>Libraries</h2>
                    <button
                        class="action"
                        ?disabled=${anyScanning ||
                        this.starting ||
                        this.libraries.length === 0}
                        @click=${this.startAllScans}
                    >
                        <wa-icon name="arrows-rotate"></wa-icon>
                        Scan all
                    </button>
                </div>

                <div class="card">
                    ${this.libraries.length === 0
                        ? html`<div class="empty">
                              No libraries yet — add one in Settings.
                          </div>`
                        : this.libraries.map((lib) =>
                              this.renderLibraryRow(lib),
                          )}
                </div>
            </section>

            <section>
                <div class="section-head">
                    <h2>Maintenance</h2>
                </div>
                <div class="card">
                    <div class="library-row">
                        <div>
                            <div class="library-name">Full rescan</div>
                            <div class="library-meta">
                                Wipes all library data and cover art, then
                                rebuilds from your files. Only needed when the
                                library is corrupt — a normal scan already
                                picks up changes.
                            </div>
                        </div>
                        <button
                            class="action danger"
                            ?disabled=${anyScanning ||
                            this.starting ||
                            this.libraries.length === 0}
                            @click=${this.fullRescan}
                        >
                            <wa-icon name="triangle-exclamation"></wa-icon>
                            Full rescan
                        </button>
                    </div>
                </div>
            </section>

            ${finished.length > 0
                ? html`
                      <section>
                          <div class="section-head">
                              <h2>Recently finished</h2>
                              <button
                                  class="link"
                                  @click=${this.clearFinished}
                              >
                                  Clear
                              </button>
                          </div>
                          ${this.renderJobList(finished, '')}
                      </section>
                  `
                : nothing}

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
        'jobs-view': JobsView;
    }
}
