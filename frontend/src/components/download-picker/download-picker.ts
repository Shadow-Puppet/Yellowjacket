import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/dialog/dialog.js';
import '@awesome.me/webawesome/dist/components/button/button.js';
import '@awesome.me/webawesome/dist/components/spinner/spinner.js';
import '@awesome.me/webawesome/dist/components/callout/callout.js';
import { designTokens } from '../../styles/tokens.css';
import type { DownloadCandidate } from '@store/download-store';
import { downloadStore } from '@store/download-store';
import type { download } from '@go/models';
import './candidate-row';
import { explainError } from '@utils/describe-error';

/**
 * The "find this album" dialog: searches every enabled download client,
 * ranks what comes back, and asks the user to choose.
 *
 * When the pipeline finds a clear winner it starts on its own and this
 * dialog reports that rather than asking a question with one obvious
 * answer. When it does not — two equally good candidates, or a free-text
 * request with nothing to verify against — the choice is the user's,
 * because guessing wrong puts the wrong files in their library.
 */
@customElement('download-picker')
export class DownloadPicker extends LitElement {
    @property({ type: Boolean, reflect: true })
    open = false;

    /** Library the imported files belong to. */
    @property({ type: Number, attribute: 'library-id' })
    libraryId = 0;

    @property({ type: String })
    artist = '';

    @property({ type: String })
    album = '';

    /** MusicBrainz release-group ID, when the caller has one. */
    @property({ type: String, attribute: 'release-group-mbid' })
    releaseGroupMbid = '';

    @property({ type: String, attribute: 'release-mbid' })
    releaseMbid = '';

    /**
     * Expected tracklist. Supplying it is what makes the result
     * trustworthy: without it there is nothing to check a candidate
     * against, and the pipeline will never auto-pick.
     */
    @property({ type: Array })
    expected: download.ExpectedTrack[] = [];

    @state()
    private searching = false;

    @state()
    private candidates: DownloadCandidate[] = [];

    @state()
    private downloadId = '';

    @state()
    private autoPicked = false;

    @state()
    private picking = false;

    @state()
    private errorMessage = '';

    static override styles = [
        designTokens,
        css`
            :host {
                display: contents;
            }

            .heading {
                display: flex;
                flex-direction: column;
                gap: 0.15em;
                margin-bottom: 1em;
            }

            .album {
                font-size: 1.05em;
                font-weight: 600;
            }

            .artist {
                opacity: 0.75;
                font-size: 0.9em;
            }

            .status {
                display: flex;
                align-items: center;
                gap: 0.6em;
                padding: 1.5em 0;
                justify-content: center;
                opacity: 0.85;
            }

            .list {
                display: flex;
                flex-direction: column;
                gap: 0.6em;
                max-height: 55vh;
                overflow-y: auto;
            }

            .footnote {
                margin-top: 1em;
                font-size: 0.8em;
                opacity: 0.65;
            }
        `,
    ];

    override updated(changed: Map<string, unknown>) {
        if (changed.has('open') && this.open) {
            void this.search();
        }
    }

    /**
     * Monotonic search version. Closing and reopening the dialog for a
     * different album used to let the first search's result overwrite
     * the second's (errors.m8); the guard is `explore-view`'s.
     */
    private searchVersion = 0;

    /** Runs the search that populates the dialog. */
    private async search(): Promise<void> {
        const version = ++this.searchVersion;

        this.searching = true;
        this.errorMessage = '';
        this.candidates = [];
        this.autoPicked = false;

        try {
            const result = await downloadStore.start({
                libraryId: this.libraryId,
                releaseMbid: this.releaseMbid,
                releaseGroupMbid: this.releaseGroupMbid,
                artist: this.artist,
                album: this.album,
                query: '',
                expected: this.expected ?? [],
            } as download.SearchRequest);

            if (version !== this.searchVersion) return;

            this.downloadId = result.downloadId;
            this.candidates = result.candidates ?? [];
            this.autoPicked = result.autoPicked;
        } catch (err) {
            if (version !== this.searchVersion) return;

            console.error('download search failed', err);
            this.errorMessage = explainError(
                err,
                'The search did not finish.',
            );
        } finally {
            if (version === this.searchVersion) {
                this.searching = false;
            }
        }
    }

    private async onPick(event: CustomEvent<{ candidateId: string }>) {
        if (this.picking) return;

        this.picking = true;
        this.errorMessage = '';

        try {
            await downloadStore.pick(this.downloadId, event.detail.candidateId);
            this.close();
        } catch (err) {
            console.error('download pick failed', err);
            this.errorMessage = explainError(
                err,
                'That download could not be started.',
            );
        } finally {
            this.picking = false;
        }
    }

    private close() {
        this.open = false;

        this.dispatchEvent(
            new CustomEvent('picker-close', { bubbles: true, composed: true }),
        );
    }

    override render() {
        return html`
            <wa-dialog
                label="Find this album"
                ?open=${this.open}
                @wa-hide=${() => this.close()}
            >
                <div class="heading">
                    <span class="album">${this.album || 'Unknown album'}</span>
                    <span class="artist">${this.artist}</span>
                </div>

                ${this.renderBody()}

                <wa-button slot="footer" variant="neutral" @click=${() => this.close()}>
                    Close
                </wa-button>
            </wa-dialog>
        `;
    }

    private renderBody() {
        if (this.errorMessage) {
            return html`
                <wa-callout variant="danger">${this.errorMessage}</wa-callout>
            `;
        }

        if (this.searching) {
            return html`
                <div class="status">
                    <wa-spinner></wa-spinner>
                    <span>Searching your download clients…</span>
                </div>
            `;
        }

        if (this.autoPicked) {
            return html`
                <wa-callout variant="success">
                    Found a clear match and started downloading it. Progress is
                    in the background jobs panel.
                </wa-callout>
            `;
        }

        if (this.candidates.length === 0) {
            return html`
                <wa-callout variant="neutral">
                    Nothing found. Try a different spelling, or connect more
                    download clients in Settings.
                </wa-callout>
            `;
        }

        return html`
            <div class="list">
                ${this.candidates.map(
                    (candidate, index) => html`
                        <candidate-row
                            .candidate=${candidate}
                            ?is-best=${index === 0}
                            ?busy=${this.picking}
                            @candidate-pick=${this.onPick}
                        ></candidate-row>
                    `,
                )}
            </div>
            ${this.renderFootnote()}
        `;
    }

    private renderFootnote() {
        const best = this.candidates[0];
        if (!best?.match) return nothing;

        // Say plainly why nothing was auto-picked, so the dialog does
        // not look like it is asking a question it could have answered.
        if (!best.match.anchored) {
            return html`
                <div class="footnote">
                    This search had no MusicBrainz match to verify against, so
                    these results could not be checked automatically.
                </div>
            `;
        }

        return html`
            <div class="footnote">
                Downloads are checked and tagged before they are added to your
                library.
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'download-picker': DownloadPicker;
    }
}
