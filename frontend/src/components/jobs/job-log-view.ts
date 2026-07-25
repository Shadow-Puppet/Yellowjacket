import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { designTokens } from '../../styles/tokens.css';
import type { Job, JobLogEntry } from '@store/job-store';
import { formatLogTime, logToText } from './job-format';

type LevelFilter = 'all' | 'warn' | 'error';

/**
 * Scrolling tail of a job's output log, with a severity filter and a
 * copy-to-clipboard button.
 *
 * The buffer is bounded on the backend (500 entries), so this is a tail
 * rather than a complete transcript — the header says so when entries
 * have been dropped, rather than silently showing a partial log.
 */
@customElement('job-log-view')
export class JobLogView extends LitElement {
    @property({ type: Object })
    job!: Job;

    @property({ type: Array })
    entries: JobLogEntry[] = [];

    @state()
    private filter: LevelFilter = 'all';

    @state()
    private copied = false;

    /** Set while the user has scrolled up, which suspends auto-follow. */
    @state()
    private following = true;

    static override styles = [
        designTokens,
        css`
            :host {
                display: flex;
                flex-direction: column;
                min-height: 0;
            }

            .toolbar {
                display: flex;
                align-items: center;
                gap: 0.5em;
                padding-bottom: 0.5em;
            }

            .filters {
                display: flex;
                gap: 0.2em;
            }

            .filters button {
                border: none;
                background: transparent;
                color: var(--yj-text-secondary, #adb5bd);
                font-size: var(--yj-text-sm);
                padding: 0.25em 0.6em;
                border-radius: 6px;
                cursor: pointer;
            }

            .filters button.active {
                background: rgba(255, 255, 255, 0.1);
                color: var(--yj-text-primary, #e9ecef);
            }

            .filters button:focus-visible {
                outline: 2px solid var(--yj-accent, #ffd43b);
                outline-offset: 1px;
            }

            .spacer {
                flex: 1;
            }

            .copy {
                display: inline-flex;
                align-items: center;
                gap: 0.4em;
                border: none;
                background: transparent;
                color: var(--yj-text-secondary, #adb5bd);
                font-size: var(--yj-text-sm);
                padding: 0.25em 0.5em;
                border-radius: 6px;
                cursor: pointer;
            }

            .copy:hover {
                background: rgba(255, 255, 255, 0.1);
                color: var(--yj-text-primary, #e9ecef);
            }

            .log {
                flex: 1;
                min-height: 0;
                overflow-y: auto;
                overflow-x: auto;
                background: rgba(0, 0, 0, 0.25);
                border-radius: 8px;
                padding: 0.6em 0.75em;
                font-family:
                    ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
                font-size: var(--yj-text-sm);
                line-height: 1.55;
            }

            .entry {
                display: flex;
                gap: 0.7em;
                white-space: pre-wrap;
                overflow-wrap: anywhere;
            }

            .time {
                color: var(--yj-text-tertiary, #868e96);
                flex-shrink: 0;
                font-variant-numeric: tabular-nums;
            }

            .message {
                color: var(--yj-text-secondary, #adb5bd);
            }

            .entry.warn .message {
                color: #f5a623;
            }

            .entry.error .message {
                color: #ff6b6b;
            }

            .detail {
                color: var(--yj-text-tertiary, #868e96);
                padding-left: 1em;
                overflow-wrap: anywhere;
            }

            .empty {
                color: var(--yj-text-tertiary, #868e96);
                font-style: italic;
                padding: 0.5em 0;
            }

            .truncation-note {
                color: var(--yj-text-tertiary, #868e96);
                font-style: italic;
                padding-bottom: 0.4em;
                border-bottom: 1px solid rgba(255, 255, 255, 0.08);
                margin-bottom: 0.4em;
            }
        `,
    ];

    private get filtered(): JobLogEntry[] {
        switch (this.filter) {
            case 'warn':
                return this.entries.filter(
                    (e) => e.level === 'warn' || e.level === 'error',
                );
            case 'error':
                return this.entries.filter((e) => e.level === 'error');
            default:
                return this.entries;
        }
    }

    /** Entries the backend ring buffer dropped before we fetched it. */
    private get droppedCount(): number {
        return Math.max(0, (this.job?.logCount ?? 0) - this.entries.length);
    }

    override updated() {
        if (!this.following) return;

        const log = this.renderRoot.querySelector('.log');

        if (log) log.scrollTop = log.scrollHeight;
    }

    private onScroll = (e: Event) => {
        const el = e.target as HTMLElement;
        // Re-engage auto-follow when the user returns to the bottom.
        this.following =
            el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    };

    private setFilter(filter: LevelFilter) {
        this.filter = filter;
    }

    private async copyLog() {
        try {
            await navigator.clipboard.writeText(
                logToText(this.job, this.entries),
            );
            this.copied = true;
            setTimeout(() => {
                this.copied = false;
            }, 1500);
        } catch (err) {
            console.error('Failed to copy job log:', err);
        }
    }

    private renderFilterButton(value: LevelFilter, label: string) {
        return html`
            <button
                class=${this.filter === value ? 'active' : ''}
                @click=${() => this.setFilter(value)}
            >
                ${label}
            </button>
        `;
    }

    override render() {
        const entries = this.filtered;
        const dropped = this.droppedCount;

        return html`
            <div class="toolbar">
                <div class="filters">
                    ${this.renderFilterButton('all', 'All')}
                    ${this.renderFilterButton(
                        'warn',
                        `Warnings${this.job?.warnCount ? ` (${this.job.warnCount})` : ''}`,
                    )}
                    ${this.renderFilterButton(
                        'error',
                        `Errors${this.job?.errorCount ? ` (${this.job.errorCount})` : ''}`,
                    )}
                </div>
                <div class="spacer"></div>
                <button class="copy" @click=${this.copyLog}>
                    ${this.copied
                        ? html`<wa-icon name="check"></wa-icon>Copied`
                        : 'Copy'}
                </button>
            </div>

            <div class="log" @scroll=${this.onScroll}>
                ${dropped > 0
                    ? html`
                          <div class="truncation-note">
                              ${dropped.toLocaleString()} earlier
                              ${dropped === 1 ? 'entry' : 'entries'} dropped —
                              showing the most recent output
                          </div>
                      `
                    : nothing}
                ${entries.length === 0
                    ? html`<div class="empty">No output yet.</div>`
                    : entries.map(
                          (entry) => html`
                              <div class="entry ${entry.level}">
                                  <span class="time"
                                      >${formatLogTime(entry)}</span
                                  >
                                  <span class="message">${entry.message}</span>
                              </div>
                              ${entry.detail
                                  ? html`<div class="detail">
                                        ${entry.detail}
                                    </div>`
                                  : nothing}
                          `,
                      )}
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'job-log-view': JobLogView;
    }
}
