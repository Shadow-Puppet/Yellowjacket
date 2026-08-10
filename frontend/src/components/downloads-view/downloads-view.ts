import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/button/button.js';
import { designTokens } from '../../styles/tokens.css';
import { downloadStore, stateLabel } from '@store/download-store';
import type { Request, RequestSummary, DownloadView as DownloadRecord } from '@store/download-store';
import { libraryStore } from '@store/library-store';

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
export class DownloadsView extends LitElement {
    @state() private tab: Tab = 'requests';

    @state() private requests: Request[] = [];

    @state() private downloads: DownloadRecord[] = [];

    @state() private checking = false;

    @state() private lastSummary: RequestSummary | null = null;

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
                border-bottom: 2px solid transparent;
                user-select: none;
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
        `,
    ];

    override connectedCallback(): void {
        super.connectedCallback();

        this.unsubscribe = downloadStore.subscribe(() => {
            this.requests = downloadStore.requests;
            this.downloads = downloadStore.downloads;
        });

        void downloadStore.init().then(() => {
            this.requests = downloadStore.requests;
            this.downloads = downloadStore.downloads;
        });
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();

        this.unsubscribe?.();
        this.unsubscribe = null;
    }

    override render() {
        return html`
            <header>
                <h1>Downloads</h1>
                ${this.tab === 'requests'
                    ? html`
                          <wa-button
                              size="small"
                              appearance="outlined"
                              ?disabled=${this.checking}
                              @click=${() => void this.checkNow()}
                          >
                              <wa-icon slot="start" name="rotate"></wa-icon>
                              ${this.checking ? 'Checking…' : 'Check now'}
                          </wa-button>
                      `
                    : nothing}
            </header>

            <p class="subtitle">
                Music you have requested, and the download attempts that
                have run for it. A request that cannot be found today stays
                on the list and is looked for again later.
            </p>

            <div class="tabs">
                <div
                    class="tab ${this.tab === 'requests' ? 'active' : ''}"
                    @click=${() => (this.tab = 'requests')}
                >
                    Requests
                </div>
                <div
                    class="tab ${this.tab === 'downloads' ? 'active' : ''}"
                    @click=${() => (this.tab = 'downloads')}
                >
                    Downloads
                </div>
            </div>

            ${this.tab === 'requests' ? this.renderRequests() : this.renderDownloads()}
        `;
    }

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
            ${this.renderSummary()}
            ${satisfied.length > 0
                ? html`
                      <div class="actions">
                          <wa-button
                              size="small"
                              appearance="plain"
                              @click=${() =>
                                  void downloadStore.clearSatisfiedRequests()}
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
            ${this.renderRequestSection('Looking for', wanted, (r) => this.renderRequest(r))}
            ${this.renderRequestSection('Paused', paused, (r) => this.renderRequest(r))}
            ${this.renderRequestSection('Found', satisfied, (r) => this.renderRequest(r))}
        `;
    }

    private renderEmptyRequests() {
        return html`
            <div class="empty">
                Nothing requested yet. Use “Want this” on an album or artist
                to add it here.
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
            s.attempted > 0 ? `${s.attempted} searched for` : '',
        ].filter(Boolean);

        return html`
            <p class="summary">
                ${parts.length > 0 ? parts.join(' · ') : 'Nothing new this time.'}
            </p>
        `;
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
                    <div class="detail">${requestDetail(request)}</div>
                </div>
                <div class="actions">
                    ${request.state === 'satisfied'
                        ? nothing
                        : html`
                              <wa-button
                                  size="small"
                                  appearance="plain"
                                  @click=${() =>
                                      void downloadStore.pauseRequest(
                                          request.id,
                                          request.state !== 'paused',
                                      )}
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
                @click=${() => void downloadStore.removeRequest(request.id)}
            >
                <wa-icon name="xmark"></wa-icon>
            </wa-button>
        `;
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
            ...new Set(view.items.map((item) => item.candidate?.origin).filter(Boolean)),
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
function requestDetail(request: Request): string {
    if (request.state === 'satisfied') return 'In your library';
    if (request.state === 'paused') return 'Paused';

    if (request.attempts === 0) return 'Not looked for yet';

    const reason = request.lastError ? ` — ${request.lastError}` : '';

    return `Looked for ${request.attempts} time${request.attempts === 1 ? '' : 's'}${reason}`;
}

declare global {
    interface HTMLElementTagNameMap {
        'downloads-view': DownloadsView;
    }
}
