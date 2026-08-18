import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/button/button.js';
import '@components/page-header/page-header';
import { designTokens } from '../../styles/tokens.css';
import { downloadStore, stateLabel } from '@store/download-store';
import type { Request, RequestSummary, DownloadView as DownloadRecord } from '@store/download-store';
import { libraryStore } from '@store/library-store';
import { notificationStore } from '@store/notification-store';
import { describeError } from '@utils/describe-error';
import { confirmAction } from '../confirm-dialog/confirm-dialog';
import { ViewLifecycleMixin } from '../../utils/view-lifecycle';

type Tab = 'requests' | 'downloads';

/**
 * The downloads page: what music the user has asked for, and what has
 * actually been attempted.
 *
 * These are two different lists on purpose. A Request is durable — "get
 * this whenever available" — and stays around, retried on a backoff,
 * until it is satisfied or removed. A Download is one search-and-grab
 * attempt; it can fail or complete and that is the end of its story. The
 * Requests tab is the list the durable, not-a-failure-just-because-it's-
 * still-here content the wanted list used to be; the Downloads tab is the
 * attempt history nothing rendered before this page existed.
 */
@customElement('downloads-view')
export class DownloadsView extends ViewLifecycleMixin(LitElement) {
    @state() private tab: Tab = 'requests';

    @state() private requests: Request[] = [];

    @state() private downloads: DownloadRecord[] = [];

    @state() private checking = false;

    @state() private lastSummary: RequestSummary | null = null;

    /** True when at least one download client is enabled. */
    @state() private canDownload = false;

    /** Ticks so "next check in …" ages while the page is open. */
    @state() private nowMs = Date.now();


    private unsubscribe: (() => void) | null = null;

    static override styles = [
        designTokens,
        css`
            :host {
                display: block;
                height: 100%;
                overflow-y: auto;
                padding: 20px;
                box-sizing: border-box;
            }

            /* The header supplies its own padding and rule, so it runs
               to the edge of a host that pads its own content. */
            page-header {
                margin: -20px -20px 1em;
            }

            header {
                display: flex;
                align-items: center;
                gap: 12px;
                margin-bottom: 4px;
            }

            h1 {
                margin: 0;
                font-size: 22px;
                font-weight: 700;
                color: var(--yj-text-primary, #fff);
                flex: 1;
            }

            .subtitle {
                margin: 0 0 20px;
                font-size: 13px;
                color: var(--yj-text-secondary, #b3b3b3);
            }

            .tabs {
                display: flex;
                gap: 4px;
                margin-bottom: 16px;
                border-bottom: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.08));
            }

            .tab {
                padding: 8px 14px;
                font-size: 13px;
                font-weight: 600;
                color: var(--yj-text-secondary, #b3b3b3);
                cursor: pointer;
                border: none;
                border-bottom: 2px solid transparent;
                background: none;
                font-family: inherit;
                user-select: none;
            }

            .tab:focus-visible {
                outline: 2px solid var(--yj-accent, #ffd43b);
                outline-offset: -2px;
            }

            .tab:hover {
                color: var(--yj-text-primary, #fff);
            }

            .tab.active {
                color: var(--yj-text-primary, #fff);
                border-bottom-color: var(--yj-accent, #ffd43b);
            }

            h2 {
                margin: 24px 0 8px;
                font-size: 13px;
                font-weight: 600;
                text-transform: uppercase;
                letter-spacing: 0.06em;
                color: var(--yj-text-secondary, #b3b3b3);
            }

            .row {
                display: flex;
                align-items: center;
                gap: 12px;
                padding: 10px 12px;
                border-radius: 6px;
                background: var(--yj-bg-surface, #181818);
            }

            .row + .row {
                margin-top: 6px;
            }

            .row-main {
                flex: 1;
                min-width: 0;
            }

            .title {
                font-size: 14px;
                color: var(--yj-text-primary, #fff);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .detail {
                font-size: 12px;
                color: var(--yj-text-tertiary, #888);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .detail.error {
                color: var(--wa-color-danger-fill-loud, #c65f5f);
            }

            .badge {
                font-size: 11px;
                padding: 2px 8px;
                border-radius: 10px;
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.08));
                color: var(--yj-text-secondary, #b3b3b3);
                flex-shrink: 0;
            }

            .empty {
                padding: 40px 20px;
                text-align: center;
                color: var(--yj-text-tertiary, #888);
                font-size: 14px;
            }

            .actions {
                display: flex;
                gap: 8px;
                flex-shrink: 0;
            }

            .summary {
                font-size: 12px;
                color: var(--yj-text-secondary, #b3b3b3);
                margin: 8px 0 0;
            }

            .notice {
                display: flex;
                align-items: center;
                gap: 8px;
                padding: 10px 12px;
                margin-bottom: 12px;
                border-radius: 6px;
                font-size: 12px;
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
                /* The only place in the app that puts text on bgOverlay,
                   which is too light on the dark ramp to carry anything
                   but primary: secondary measured 3.90:1 here. A notice
                   is the last thing that should be hard to read. */
                color: var(--yj-text-primary, #fff);
            }

            .section-hint {
                margin: 0 0 8px;
                font-size: 12px;
                color: var(--yj-text-tertiary, #888);
            }
        `,
    ];

    protected override onViewActivate(): void {
        this.unsubscribe = downloadStore.subscribe(() => {
            this.requests = downloadStore.requests;
            this.downloads = downloadStore.downloads;
            this.canDownload = downloadStore.available;
        });

        void downloadStore.init().then(() => {
            this.requests = downloadStore.requests;
            this.downloads = downloadStore.downloads;
            this.canDownload = downloadStore.available;
        });

        // A "next check" that never moves reads as a stuck page, so the
        // relative times re-render on their own — while the page is on
        // screen, where a re-render can be seen.
        this.intervalWhileActive(() => {
            this.nowMs = Date.now();
        }, 30_000);
    }

    protected override onViewDeactivate(): void {
        this.unsubscribe?.();
        this.unsubscribe = null;
    }

    override render() {
        return html`
            <page-header heading="Downloads">
                ${this.tab === 'requests'
                    ? html`
                          <wa-button
                              slot="actions"
                              size="small"
                              appearance="outlined"
                              ?disabled=${this.checking}
                              title="Search every download client for everything on this list right now, instead of waiting for the next scheduled check"
                              @click=${() => void this.checkNow()}
                          >
                              <wa-icon slot="start" name="rotate"></wa-icon>
                              ${this.checking ? 'Searching…' : 'Check now'}
                          </wa-button>
                      `
                    : nothing}
            </page-header>

            <p class="subtitle">
                Music you have requested, and the download attempts that
                have run for it. A request that cannot be found today stays
                on the list and is looked for again later — roughly every
                six hours at first, then less often the longer it goes
                unfound. “Check now” skips that wait and searches
                everything on the list immediately.
            </p>

            <div
                class="tabs"
                role="tablist"
                aria-label="Downloads sections"
                @keydown=${this.onTabKeydown}
            >
                ${DownloadsView.TABS.map(
                    ([id, label]) => html`
                        <button
                            type="button"
                            role="tab"
                            id=${`tab-${id}`}
                            class="tab ${this.tab === id ? 'active' : ''}"
                            aria-selected=${this.tab === id ? 'true' : 'false'}
                            aria-controls=${`panel-${id}`}
                            tabindex=${this.tab === id ? 0 : -1}
                            @click=${() => (this.tab = id)}
                        >
                            ${label}
                        </button>
                    `,
                )}
            </div>

            <div
                role="tabpanel"
                id=${`panel-${this.tab}`}
                aria-labelledby=${`tab-${this.tab}`}
            >
                ${this.tab === 'requests' ? this.renderRequests() : this.renderDownloads()}
            </div>
        `;
    }

    /**
     * The tabs, in order, so the markup and the keyboard model read
     * the same list rather than each spelling it out (a11y.2: these
     * were two `<div @click>`s with no roles, no tabindex and no
     * keyboard path at all, which made the Downloads half of the
     * Downloads view mouse-only).
     */
    private static readonly TABS: ReadonlyArray<readonly [Tab, string]> = [
        ['requests', 'Requests'],
        ['downloads', 'Downloads'],
    ];

    /**
     * A tablist moves with Left/Right/Home/End and activates as it
     * moves — the panel is already rendered, so there is nothing to
     * defer. Focus follows, which is what makes the roving tabindex
     * mean anything.
     */
    private onTabKeydown = (e: KeyboardEvent): void => {
        const ids = DownloadsView.TABS.map(([id]) => id);
        const at = ids.indexOf(this.tab);

        let next: number | null = null;
        if (e.key === 'ArrowRight') next = (at + 1) % ids.length;
        else if (e.key === 'ArrowLeft') next = (at - 1 + ids.length) % ids.length;
        else if (e.key === 'Home') next = 0;
        else if (e.key === 'End') next = ids.length - 1;

        if (next === null) return;

        e.preventDefault();
        this.tab = ids[next]!;
        void this.updateComplete.then(() => {
            this.shadowRoot
                ?.querySelector<HTMLButtonElement>(`#tab-${this.tab}`)
                ?.focus();
        });
    };

    // -----------------------------------------------------------------
    // Requests tab
    // -----------------------------------------------------------------

    private renderRequests() {
        const subscriptions = this.requests.filter((r) => r.entity === 'artist');
        const wanted = this.requests.filter(
            (r) => r.entity !== 'artist' && r.state === 'wanted',
        );
        const paused = this.requests.filter((r) => r.state === 'paused');
        const satisfied = this.requests.filter((r) => r.state === 'satisfied');

        return html`
            ${this.renderProviderNotice()}
            ${this.renderSummary()}
            ${satisfied.length > 0
                ? html`
                      <div class="actions">
                          <wa-button
                              size="small"
                              appearance="plain"
                              @click=${() => void this.clearSatisfied()}
                          >
                              Clear found
                          </wa-button>
                      </div>
                  `
                : nothing}
            ${this.requests.length === 0 ? this.renderEmptyRequests() : nothing}
            ${this.renderRequestSection(
                'Following',
                subscriptions,
                (r) => this.renderSubscription(r),
            )}
            ${wanted.length > 0
                ? html`
                      <h2>Looking for</h2>
                      <p class="section-hint">
                          Requested, not found yet. Nothing is wrong — each
                          of these is searched again on the schedule below,
                          and moves to “Found” the moment it lands in your
                          library, however it got there.
                      </p>
                      ${wanted.map((r) => this.renderRequest(r))}
                  `
                : nothing}
            ${this.renderRequestSection('Paused', paused, (r) => this.renderRequest(r))}
            ${this.renderRequestSection('Found', satisfied, (r) => this.renderRequest(r))}
        `;
    }

    private renderEmptyRequests() {
        return html`
            <div class="empty">
                Nothing requested yet. Use “Request this” on an album or artist
                to add it here.
            </div>
        `;
    }

    /**
     * A request list with no download client behind it is a list that
     * can never move, and that is the single most likely reason “check
     * now” appears to do nothing.  Say so where the button is.
     */
    private renderProviderNotice() {
        if (this.canDownload) return nothing;

        return html`
            <div class="notice">
                <wa-icon name="triangle-exclamation"></wa-icon>
                <span>
                    No download client is enabled, so nothing on this list
                    can be searched for. Requests are still kept — add a
                    client under Settings → Downloads and they start moving.
                </span>
            </div>
        `;
    }

    private renderSummary() {
        if (!this.lastSummary) return nothing;

        const s = this.lastSummary;

        const parts = [
            s.expanded > 0 ? `${s.expanded} new album${s.expanded === 1 ? '' : 's'} found` : '',
            s.satisfied > 0 ? `${s.satisfied} already owned` : '',
            s.started > 0 ? `${s.started} downloading` : '',
            s.attempted > 0
                ? `${s.attempted} searched, no clear match yet`
                : '',
        ].filter(Boolean);

        if (parts.length > 0) {
            return html`<p class="summary">${parts.join(' · ')}</p>`;
        }

        // "Nothing happened" needs a reason, or the button looks broken.
        const idle = s.noProviders
            ? 'Nothing was searched: no download client is enabled.'
            : s.waiting > 0
              ? `Searched all ${s.waiting} request${s.waiting === 1 ? '' : 's'} — no source has anything new yet.`
              : 'Nothing on the list to search for.';

        return html`<p class="summary">${idle}</p>`;
    }

    private renderRequestSection(
        title: string,
        items: Request[],
        renderer: (request: Request) => unknown,
    ) {
        if (items.length === 0) return nothing;

        return html`
            <h2>${title}</h2>
            ${items.map((request) => renderer(request))}
        `;
    }

    /**
     * An artist row is a subscription, not a queued download, so it
     * shows what it covers rather than a retry count — the albums it
     * produced appear in their own section.
     */
    private renderSubscription(request: Request) {
        return html`
            <div class="row">
                <wa-icon name="user-group"></wa-icon>
                <div class="row-main">
                    <div class="title">
                        ${request.artist || request.title || request.mbid}
                    </div>
                    <div class="detail">
                        ${request.scope === 'all'
                            ? 'Whole discography, plus new releases'
                            : 'New releases only'}
                    </div>
                </div>
                <span class="badge">Following</span>
                <div class="actions">
                    <wa-button
                        size="small"
                        appearance="plain"
                        @click=${() => void this.toggleScope(request)}
                    >
                        ${request.scope === 'all' ? 'New only' : 'Everything'}
                    </wa-button>
                    ${this.renderRemove(request)}
                </div>
            </div>
        `;
    }

    private renderRequest(request: Request) {
        return html`
            <div class="row">
                <wa-icon
                    name=${request.entity === 'recording' ? 'music' : 'compact-disc'}
                ></wa-icon>
                <div class="row-main">
                    <div class="title">
                        ${request.artist ? `${request.artist} — ` : ''}${request.title ||
                        request.mbid}
                    </div>
                    <div class="detail">
                        ${requestDetail(request, this.nowMs, this.canDownload)}
                    </div>
                </div>
                <div class="actions">
                    ${request.state === 'satisfied'
                        ? nothing
                        : html`
                              <wa-button
                                  size="small"
                                  appearance="plain"
                                  @click=${() => void this.pause(request)}
                              >
                                  ${request.state === 'paused' ? 'Resume' : 'Pause'}
                              </wa-button>
                          `}
                    ${this.renderRemove(request)}
                </div>
            </div>
        `;
    }

    private renderRemove(request: Request) {
        return html`
            <wa-button
                size="small"
                appearance="plain"
                aria-label="Stop following"
                @click=${() => void this.removeRequest(request)}
            >
                <wa-icon name="xmark"></wa-icon>
            </wa-button>
        `;
    }

    /** What to call a request in a sentence. */
    private static describeRequest(request: Request): string {
        return request.artist || request.title || request.mbid || 'that request';
    }

    /**
     * Removing a durable request used to be one unconfirmed click with
     * the promise thrown away (errors.M7) — on a subscription the user
     * may have been building for months.
     */
    private async removeRequest(request: Request): Promise<void> {
        const name = DownloadsView.describeRequest(request);
        const ok = await confirmAction({
            title: `Stop following “${name}”?`,
            message:
                request.scope === 'all'
                    ? 'YellowJacket will stop looking for this artist’s releases.'
                    : 'YellowJacket will stop looking for this release.',
            impact: 'Anything already downloaded stays in your library.',
            confirmLabel: 'Stop following',
            danger: true,
        });

        if (!ok) return;

        try {
            await downloadStore.removeRequest(request.id);
        } catch (err) {
            this.report(`remove “${name}”`, err);
        }
    }

    private async pause(request: Request): Promise<void> {
        const paused = request.state !== 'paused';

        try {
            await downloadStore.pauseRequest(request.id, paused);
        } catch (err) {
            this.report(
                `${paused ? 'pause' : 'resume'} “${DownloadsView.describeRequest(request)}”`,
                err,
            );
        }
    }

    private async clearSatisfied(): Promise<void> {
        try {
            await downloadStore.clearSatisfiedRequests();
        } catch (err) {
            this.report('clear the found requests', err);
        }
    }

    /** Persistent: the row is still there, and retrying is the point. */
    private report(what: string, err: unknown): void {
        console.error(`downloads: could not ${what}`, err);
        notificationStore.persistent({
            key: 'download-request',
            text: `Could not ${what}. ${describeError(err)}`,
            detail: String(err),
        });
    }

    /** Widens or narrows what an artist subscription covers. */
    private async toggleScope(request: Request): Promise<void> {
        try {
            const libraryId =
                request.libraryId || (await libraryStore.getDefaultLibraryId());
            if (!libraryId) {
                console.error(
                    'Could not change what this subscription covers: no library available',
                );
                return;
            }

            await downloadStore.addRequest({
                mbid: request.mbid,
                entity: 'artist',
                libraryId,
                artist: request.artist,
                title: request.title,
                scope: request.scope === 'all' ? 'future' : 'all',
                secondary: request.secondary,
            } as never);
        } catch (err) {
            console.error('Could not change what this subscription covers:', err);
        }
    }

    private async checkNow(): Promise<void> {
        this.checking = true;

        try {
            this.lastSummary = await downloadStore.reconcileRequests();
        } catch (err) {
            console.error('Could not check the requests list:', err);
        } finally {
            this.checking = false;
        }
    }

    // -----------------------------------------------------------------
    // Downloads tab
    // -----------------------------------------------------------------

    private renderDownloads() {
        if (this.downloads.length === 0) {
            return html`
                <div class="empty">
                    No downloads yet. Attempts made by "Download now" or the
                    background reconciler show up here.
                </div>
            `;
        }

        return this.downloads.map((view) => this.renderDownload(view));
    }

    private renderDownload(view: DownloadRecord) {
        const title = view.artist
            ? `${view.artist}${view.album ? ` — ${view.album}` : ''}`
            : view.query || view.album || 'Untitled download';

        return html`
            <div class="row">
                <wa-icon name="compact-disc"></wa-icon>
                <div class="row-main">
                    <div class="title">${title}</div>
                    <div class="detail">${this.downloadDetail(view)}</div>
                    ${view.error
                        ? html`<div class="detail error">${view.error}</div>`
                        : nothing}
                </div>
                <span class="badge">${stateLabel(view.state)}</span>
            </div>
        `;
    }

    /** Provider/progress summary for a download's second line. */
    private downloadDetail(view: DownloadRecord): string {
        const providers = [
            ...new Set((view.items ?? []).map((item) => item.candidate?.origin).filter(Boolean)),
        ];

        const parts: string[] = [];

        if (providers.length > 0) parts.push(providers.join(', '));
        if (view.source) parts.push(view.source);

        return parts.length > 0 ? parts.join(' · ') : 'No provider info';
    }
}

/**
 * The second line of a request row: what is happening, in the user's
 * terms.
 *
 * A request that has been tried and not found is reported as still being
 * looked for rather than as an error, because that is what it is — the
 * retry is already scheduled and there is nothing for the user to do.
 */
function requestDetail(
    request: Request,
    nowMs: number,
    canDownload: boolean,
): string {
    if (request.state === 'satisfied') return 'In your library';
    if (request.state === 'paused') return 'Paused — not being looked for';

    // With no client there is no search and no retry clock — the
    // backend stopped scheduling one — so a row must not imply either.
    // "Queued" and "next check in 6 hours" are both promises nothing is
    // in a position to keep.
    if (!canDownload) return 'On your list — no download client to search with';

    if (request.attempts === 0) return 'Queued — not searched for yet';

    const tries = `Searched ${request.attempts} time${request.attempts === 1 ? '' : 's'}`;
    const reason = request.lastError ? `, ${request.lastError}` : '';
    // Wails types a Go time.Time as an opaque class; over the wire it
    // is the RFC 3339 string JSON marshalled it as.
    const next = nextCheckPhrase(
        request.nextTryAt as unknown as string | undefined,
        nowMs,
    );

    return `${tries}${reason}${next}`;
}

/**
 * "Next check" as a phrase, because the retry schedule is the part of
 * this feature nothing in the UI used to admit existed — a row that
 * says only "searched 3 times" gives the user no way to tell a waiting
 * request from an abandoned one.
 */
function nextCheckPhrase(nextTryAt: string | undefined, nowMs: number): string {
    if (!nextTryAt) return '';

    const due = new Date(nextTryAt).getTime();
    if (Number.isNaN(due)) return '';

    const deltaMs = due - nowMs;
    if (deltaMs <= 0) return ' · due for another search';

    return ` · next check ${relativeFuture(deltaMs)}`;
}

/** Coarse "in 3 hours" phrasing; minutes are noise on a 6-hour cycle. */
function relativeFuture(ms: number): string {
    const minutes = Math.round(ms / 60_000);

    if (minutes < 60) return `in ${Math.max(1, minutes)} min`;

    const hours = Math.round(minutes / 60);
    if (hours < 48) return `in ${hours} hour${hours === 1 ? '' : 's'}`;

    const days = Math.round(hours / 24);

    return `in ${days} day${days === 1 ? '' : 's'}`;
}

declare global {
    interface HTMLElementTagNameMap {
        'downloads-view': DownloadsView;
    }
}
