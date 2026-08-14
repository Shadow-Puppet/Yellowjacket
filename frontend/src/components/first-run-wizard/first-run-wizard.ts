import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/dialog/dialog.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import {
    AddLibrary,
    GetAllLibrariesWithTrackCounts,
} from '@go/library/library.js';
import { DirectoryPicker } from '@go/frontendutil/frontendutil.js';
import { describeError, explainError } from '@utils/describe-error';
import { nameDialogsIn } from '@utils/name-dialog';

/**
 * First-run setup wizard.
 *
 * On startup it checks whether any library has already been registered.
 * If one exists the wizard stays hidden and the app proceeds as normal.
 * If there are none (fresh install), it presents a non-dismissable modal
 * prompting the user to pick their music folder, registers it through the
 * library CRUD API, and dismisses itself.  AddLibrary emits LibraryAdded
 * and kicks off the initial scan automatically.
 */
@customElement('first-run-wizard')
export class FirstRunWizard extends LitElement {
    @query('wa-dialog')
    private dialog!: HTMLElement & { open: boolean };

    /** Whether the wizard should be shown at all (no library configured). */
    @state() private active = false;

    /** Directory chosen in the picker, not yet saved. */
    @state() private selectedDirectory = '';

    /** True while SetLibraryDirectory is in flight. */
    @state() private saving = false;

    /** Error message from a failed pick/save, if any. */
    @state() private errorMessage = '';

    override async connectedCallback(): Promise<void> {
        super.connectedCallback();

        try {
            const existing = await GetAllLibrariesWithTrackCounts();

            // An existing library means setup is already complete.
            if (existing && existing.length > 0) return;
        } catch (err) {
            console.error(
                'First-run wizard: failed to read libraries:',
                err,
            );

            return;
        }

        this.active = true;

        await this.updateComplete;

        if (this.dialog) this.dialog.open = true;
    }

    static override styles = css`
        wa-dialog {
            --width: 480px;
        }

        wa-dialog::part(dialog) {
            background: var(--yj-bg-surface, #212529);
            color: var(--yj-text-primary, #fff);
            border: 1px solid var(--yj-border, #444);
            border-radius: 8px;
        }

        wa-dialog::part(body) {
            padding: 24px;
        }

        .welcome {
            text-align: center;
            margin-bottom: 20px;
        }

        .welcome wa-icon {
            font-size: 40px;
            color: var(--yj-accent-text, #f5c518);
            margin-bottom: 8px;
        }

        .welcome h2 {
            margin: 0 0 4px;
            font-size: 20px;
            font-weight: 600;
        }

        .welcome p {
            margin: 0;
            font-size: 13px;
            color: var(--yj-text-secondary, #b3b3b3);
        }

        .chosen {
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 10px 12px;
            margin-bottom: 16px;
            font-size: 13px;
            background: var(--yj-bg-elevated, #2a2f34);
            border: 1px solid var(--yj-border, #444);
            border-radius: 6px;
            word-break: break-all;
        }

        .chosen wa-icon {
            color: var(--yj-accent-text, #f5c518);
            flex-shrink: 0;
        }

        .placeholder {
            color: var(--yj-text-tertiary, #888);
        }

        .error {
            font-size: 13px;
            color: var(--yj-danger, #e5484d);
            margin-bottom: 16px;
        }

        .actions {
            display: flex;
            justify-content: flex-end;
            gap: 8px;
        }

        .btn {
            padding: 8px 16px;
            font-size: 13px;
            font-weight: 500;
            border: 1px solid var(--yj-border, #444);
            border-radius: 6px;
            background: transparent;
            color: var(--yj-text-primary, #fff);
            cursor: pointer;
        }

        .btn:hover:not(:disabled) {
            background: var(--yj-bg-elevated, #2a2f34);
        }

        .btn-primary {
            background: var(--yj-accent, #f5c518);
            border-color: var(--yj-accent, #f5c518);
            color: var(--yj-accent-fg, #000);
        }

        .btn-primary:hover:not(:disabled) {
            filter: brightness(1.08);
        }

        .btn:disabled {
            opacity: 0.5;
            cursor: not-allowed;
        }
    `;

    /**
     * Web Awesome renders `label` into a heading it never points the
     * `<dialog>` at, so the dialog has no accessible name until
     * something sets one. See `utils/name-dialog.ts`.
     */
    override updated() {
        nameDialogsIn(this.shadowRoot);
    }

    override render() {
        if (!this.active) return nothing;

        return html`
            <wa-dialog
                label="Welcome to YellowJacket"
                without-header
                @wa-hide=${this.preventClose}
            >
                <div class="welcome">
                    <wa-icon name="music"></wa-icon>
                    <h2>Welcome to YellowJacket</h2>
                    <p>
                        Choose the folder where your music lives to
                        get started.
                    </p>
                </div>

                <div class="chosen">
                    <wa-icon name="folder"></wa-icon>
                    ${this.selectedDirectory
                        ? html`<span>${this.selectedDirectory}</span>`
                        : html`<span class="placeholder"
                              >No folder selected yet</span
                          >`}
                </div>

                ${this.errorMessage
                    ? html`<div class="error">${this.errorMessage}</div>`
                    : nothing}

                <div class="actions">
                    <button
                        class="btn"
                        ?disabled=${this.saving}
                        @click=${this.handleChoose}
                    >
                        ${this.selectedDirectory
                            ? 'Change Folder'
                            : 'Choose Folder'}
                    </button>
                    <button
                        class="btn btn-primary"
                        ?disabled=${!this.selectedDirectory || this.saving}
                        @click=${this.handleFinish}
                    >
                        ${this.saving ? 'Saving…' : 'Get Started'}
                    </button>
                </div>
            </wa-dialog>
        `;
    }

    /** Set once setup is finished so we allow the dialog to close. */
    private finished = false;

    /**
     * Block every close attempt (Escape, programmatic, backdrop) until
     * setup is finished, so the user can't skip picking a folder.
     * wa-hide is cancelable via preventDefault().
     */
    private preventClose = (e: Event): void => {
        if (!this.finished) e.preventDefault();
    };

    private handleChoose = async (): Promise<void> => {
        this.errorMessage = '';

        try {
            const dir = await DirectoryPicker();

            if (dir) this.selectedDirectory = dir;
        } catch (err) {
            this.errorMessage = describeError(
                err,
                'The folder picker could not be opened.',
            );
            console.error('First-run wizard: directory picker failed:', err);
        }
    };

    private handleFinish = async (): Promise<void> => {
        if (!this.selectedDirectory) return;

        this.saving = true;
        this.errorMessage = '';

        try {
            await AddLibrary(this.selectedDirectory);

            this.finished = true;

            if (this.dialog) this.dialog.open = false;

            this.active = false;
        } catch (err) {
            this.errorMessage = explainError(
                err,
                'That folder could not be added.',
            );
            console.error('First-run wizard: add library failed:', err);
        } finally {
            this.saving = false;
        }
    };
}

declare global {
    interface HTMLElementTagNameMap {
        'first-run-wizard': FirstRunWizard;
    }
}
