import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/button/button.js';
import { designTokens } from '../../styles/tokens.css';
import { downloadStore } from '@store/download-store';
import type { Want, WantSummary } from '@store/download-store';
import { libraryStore } from '@store/library-store';

/**
 * The wanted list: music the user has said they want but does not have.
 *
 * The list is the durable thing here, not the downloads it produces.
 * Something unfindable today stays on the list and is retried on a
 * backoff, so this view is mostly about making the waiting legible —
 * what is being looked for, when it was last tried, and why it has not
 * turned up. A row is not a failure just because it is still here.
 */
@customElement('wanted-view')
export class WantedView extends LitElement {
    @state() private wants: Want[] = [];

    @state() private checking = false;

    @state() private lastSummary: WantSummary | null = null;

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
            this.wants = downloadStore.wants;
        });

        void downloadStore.init().then(() => {
            this.wants = downloadStore.wants;
        });
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();

        this.unsubscribe?.();
        this.unsubscribe = null;
    }

    override render() {
        const subscriptions = this.wants.filter((w) => w.entity === 'artist');
        const wanted = this.wants.filter(
            (w) => w.entity !== 'artist' && w.state === 'wanted',
        );
        const paused = this.wants.filter((w) => w.state === 'paused');
        const satisfied = this.wants.filter((w) => w.state === 'satisfied');

        return html`
            <header>
                <h1>Wanted</h1>
                <wa-button
                    size="small"
                    appearance="outlined"
                    ?disabled=${this.checking}
                    @click=${() => void this.checkNow()}
                >
                    <wa-icon slot="start" name="rotate"></wa-icon>
                    ${this.checking ? 'Checking…' : 'Check now'}
                </wa-button>
                ${satisfied.length > 0
                    ? html`
                          <wa-button
                              size="small"
                              appearance="plain"
                              @click=${() =>
                                  void downloadStore.clearSatisfiedWants()}
                          >
                              Clear found
                          </wa-button>
                      `
                    : nothing}
            </header>

            <p class="subtitle">
                Music you want but do not have. Anything that cannot be found
                stays here and is looked for again later.
            </p>

            ${this.renderSummary()}
            ${this.wants.length === 0 ? this.renderEmpty() : nothing}
            ${this.renderSection(
                'Following',
                subscriptions,
                (w) => this.renderSubscription(w),
            )}
            ${this.renderSection('Looking for', wanted, (w) => this.renderWant(w))}
            ${this.renderSection('Paused', paused, (w) => this.renderWant(w))}
            ${this.renderSection('Found', satisfied, (w) => this.renderWant(w))}
        `;
    }

    private renderEmpty() {
        return html`
            <div class="empty">
                Nothing wanted yet. Use “Want this” on an album or artist to
                add it here.
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

    private renderSection(
        title: string,
        items: Want[],
        renderer: (want: Want) => unknown,
    ) {
        if (items.length === 0) return nothing;

        return html`
            <h2>${title}</h2>
            ${items.map((want) => renderer(want))}
        `;
    }

    /**
     * An artist row is a subscription, not a queued download, so it
     * shows what it covers rather than a retry count — the albums it
     * produced appear in their own section.
     */
    private renderSubscription(want: Want) {
        return html`
            <div class="row">
                <wa-icon name="user-group"></wa-icon>
                <div class="row-main">
                    <div class="title">${want.artist || want.title || want.mbid}</div>
                    <div class="detail">
                        ${want.scope === 'all'
                            ? 'Whole discography, plus new releases'
                            : 'New releases only'}
                    </div>
                </div>
                <span class="badge">Following</span>
                <div class="actions">
                    <wa-button
                        size="small"
                        appearance="plain"
                        @click=${() => void this.toggleScope(want)}
                    >
                        ${want.scope === 'all' ? 'New only' : 'Everything'}
                    </wa-button>
                    ${this.renderRemove(want)}
                </div>
            </div>
        `;
    }

    private renderWant(want: Want) {
        return html`
            <div class="row">
                <wa-icon
                    name=${want.entity === 'recording' ? 'music' : 'compact-disc'}
                ></wa-icon>
                <div class="row-main">
                    <div class="title">
                        ${want.artist ? `${want.artist} — ` : ''}${want.title ||
                        want.mbid}
                    </div>
                    <div class="detail">${wantDetail(want)}</div>
                </div>
                <div class="actions">
                    ${want.state === 'satisfied'
                        ? nothing
                        : html`
                              <wa-button
                                  size="small"
                                  appearance="plain"
                                  @click=${() =>
                                      void downloadStore.pauseWant(
                                          want.id,
                                          want.state !== 'paused',
                                      )}
                              >
                                  ${want.state === 'paused' ? 'Resume' : 'Pause'}
                              </wa-button>
                          `}
                    ${this.renderRemove(want)}
                </div>
            </div>
        `;
    }

    private renderRemove(want: Want) {
        return html`
            <wa-button
                size="small"
                appearance="plain"
                @click=${() => void downloadStore.removeWant(want.id)}
            >
                <wa-icon name="xmark"></wa-icon>
            </wa-button>
        `;
    }

    /** Widens or narrows what an artist subscription covers. */
    private async toggleScope(want: Want): Promise<void> {
        try {
            const libraryId =
                want.libraryId || (await libraryStore.getDefaultLibraryId());
            if (!libraryId) {
                console.error(
                    'Could not change what this subscription covers: no library available',
                );
                return;
            }

            await downloadStore.addWant({
                mbid: want.mbid,
                entity: 'artist',
                libraryId,
                artist: want.artist,
                title: want.title,
                scope: want.scope === 'all' ? 'future' : 'all',
                secondary: want.secondary,
            } as never);
        } catch (err) {
            console.error('Could not change what this subscription covers:', err);
        }
    }

    private async checkNow(): Promise<void> {
        this.checking = true;

        try {
            this.lastSummary = await downloadStore.reconcileWanted();
        } catch (err) {
            console.error('Could not check the wanted list:', err);
        } finally {
            this.checking = false;
        }
    }
}

/**
 * The second line of a want row: what is happening, in the user's terms.
 *
 * A want that has been tried and not found is reported as still being
 * looked for rather than as an error, because that is what it is — the
 * retry is already scheduled and there is nothing for the user to do.
 */
function wantDetail(want: Want): string {
    if (want.state === 'satisfied') return 'In your library';
    if (want.state === 'paused') return 'Paused';

    if (want.attempts === 0) return 'Not looked for yet';

    const reason = want.lastError ? ` — ${want.lastError}` : '';

    return `Looked for ${want.attempts} time${want.attempts === 1 ? '' : 's'}${reason}`;
}

declare global {
    interface HTMLElementTagNameMap {
        'wanted-view': WantedView;
    }
}
