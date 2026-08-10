import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/button/button.js';
import '@awesome.me/webawesome/dist/components/input/input.js';
import '@awesome.me/webawesome/dist/components/select/select.js';
import '@awesome.me/webawesome/dist/components/option/option.js';
import '@awesome.me/webawesome/dist/components/switch/switch.js';
import '@awesome.me/webawesome/dist/components/spinner/spinner.js';
import '@awesome.me/webawesome/dist/components/callout/callout.js';
import { designTokens } from '../../styles/tokens.css';
import type {
    DownloadDescriptor,
    DownloadProvider,
    ProviderField,
} from '@store/download-store';
import { downloadStore } from '@store/download-store';
import { DirectoryPicker } from '@go/frontendutil/FrontendUtil';
import { GetDownloadPreferences, SetDownloadPreferences } from '@go/config/Config';
import { SetPreferences } from '@go/download/Service';
import type { download } from '@go/models';
import './config-section';

/**
 * Allowed audio formats for auto-download, mirrored from
 * backend/download/types.go's `Format` constants. `FormatUnknown` is
 * deliberately excluded — it names "no format detected", not a format a
 * user could opt into.
 */
const AUTO_DOWNLOAD_FORMATS: { value: string; label: string }[] = [
    { value: 'flac', label: 'FLAC' },
    { value: 'alac', label: 'ALAC' },
    { value: 'wav', label: 'WAV' },
    { value: 'mp3', label: 'MP3' },
    { value: 'aac', label: 'AAC' },
    { value: 'ogg', label: 'OGG' },
    { value: 'opus', label: 'Opus' },
    { value: 'wma', label: 'WMA' },
];

/**
 * Download client configuration.
 *
 * The forms are rendered from the descriptors the backend publishes, not
 * from anything hard-coded here, so adding a provider on the backend
 * gives it a settings UI with no frontend change. That is also why
 * secret fields render as password inputs purely on the descriptor's
 * say-so — the frontend never needs to know which services have keys.
 */
@customElement('download-clients')
export class DownloadClients extends LitElement {
    @state()
    private providers: DownloadProvider[] = [];

    @state()
    private descriptors: DownloadDescriptor[] = [];

    /** Provider being edited, or 'new' while adding one. */
    @state()
    private editing: number | 'new' | null = null;

    /** Kind selected in the add form. */
    @state()
    private newKind = '';

    /** Working copy of the form's field values. */
    @state()
    private draft: Record<string, string> = {};

    @state()
    private draftName = '';

    /** Per-provider connection test results, keyed by provider ID. */
    @state()
    private testResults: Record<number, { ok: boolean; message: string }> = {};

    @state()
    private testing: number | null = null;

    @state()
    private errorMessage = '';

    /** Working copy of the auto-download guardrails. */
    @state()
    private prefs: download.AutoDownloadPrefs = {
        minSizeMb: 0,
        maxSizeMb: 0,
        preferredSizeMb: 0,
        allowedFormats: [],
    } as download.AutoDownloadPrefs;

    @state()
    private prefsSaving = false;

    @state()
    private prefsError = '';

    @state()
    private prefsSaved = false;

    private unsubscribe: (() => void) | null = null;

    override connectedCallback(): void {
        super.connectedCallback();

        this.unsubscribe = downloadStore.subscribe(() => this.syncFromStore());

        void downloadStore.init().then(() => this.syncFromStore());
        void this.loadPreferences();
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();

        this.unsubscribe?.();
        this.unsubscribe = null;
    }

    private syncFromStore(): void {
        this.providers = downloadStore.providers;
        this.descriptors = downloadStore.descriptors;
    }

    private async loadPreferences(): Promise<void> {
        try {
            this.prefs = await GetDownloadPreferences();
        } catch (err) {
            console.error('Failed to load auto-download preferences:', err);
        }
    }

    static override styles = [
        designTokens,
        css`
            :host {
                display: block;
            }

            .clients {
                display: flex;
                flex-direction: column;
                gap: 0.6em;
            }

            .client {
                display: grid;
                grid-template-columns: 1fr auto;
                gap: 0.75em;
                align-items: center;
                padding: 0.7em 0.85em;
                border: 1px solid var(--wa-color-surface-border, #333);
                border-radius: 8px;
            }

            .client-name {
                font-weight: 600;
            }

            .client-meta {
                font-size: 0.82em;
                opacity: 0.7;
                margin-top: 0.15em;
            }

            .client-actions {
                display: flex;
                gap: 0.4em;
                align-items: center;
            }

            .test-result {
                font-size: 0.8em;
                margin-top: 0.35em;
            }

            .test-result.ok {
                color: var(--wa-color-success-fill-loud, #4c9f70);
            }

            .test-result.fail {
                color: var(--wa-color-danger-fill-loud, #c65f5f);
            }

            .form {
                display: flex;
                flex-direction: column;
                gap: 0.7em;
                padding: 0.9em;
                border: 1px solid var(--wa-color-surface-border, #333);
                border-radius: 8px;
                margin-top: 0.6em;
            }

            .form-actions {
                display: flex;
                gap: 0.5em;
                justify-content: flex-end;
                margin-top: 0.3em;
            }

            .requires {
                font-size: 0.82em;
                opacity: 0.75;
            }

            .empty {
                opacity: 0.7;
                font-size: 0.9em;
                padding: 0.5em 0;
            }

            .add-row {
                margin-top: 0.8em;
            }

            .field-row {
                display: flex;
                gap: 0.5em;
                align-items: flex-end;
            }

            .field-row wa-input {
                flex: 1;
            }

            .field-row .browse-button {
                flex-shrink: 0;
            }

            .format-options {
                display: flex;
                flex-wrap: wrap;
                gap: 0.4em 1em;
                margin-top: 0.4em;
            }

            .format-option {
                display: flex;
                align-items: center;
                gap: 0.4em;
                font-size: 0.9em;
                cursor: pointer;
            }
        `,
    ];

    override render() {
        return html`
            <config-section
                heading="Download Clients"
                description="Connect services you already run to search for and download music. Nothing is enabled until you add a client."
            >
                ${this.errorMessage
                    ? html`<wa-callout variant="danger">${this.errorMessage}</wa-callout>`
                    : nothing}

                <div class="clients">
                    ${this.providers.length === 0 && this.editing !== 'new'
                        ? html`<div class="empty">No download clients connected.</div>`
                        : nothing}
                    ${this.providers.map((provider) => this.renderProvider(provider))}
                </div>

                ${this.editing === 'new'
                    ? this.renderAddForm()
                    : html`
                        <div class="add-row">
                            <wa-button size="small" @click=${this.startAdd}>
                                Add download client
                            </wa-button>
                        </div>
                    `}
            </config-section>

            <config-section
                heading="Auto-download preferences"
                description="Guardrails on what the pipeline may grab without asking — a manual pick is never restricted by these, only automatic ones."
            >
                ${this.prefsError
                    ? html`<wa-callout variant="danger">${this.prefsError}</wa-callout>`
                    : nothing}

                <div class="form">
                    <div class="field-row">
                        <wa-input
                            label="Minimum size (MB)"
                            type="number"
                            min="0"
                            placeholder="No minimum"
                            .value=${this.prefs.minSizeMb ? String(this.prefs.minSizeMb) : ''}
                            @input=${(e: Event) => {
                                this.prefs = {
                                    ...this.prefs,
                                    minSizeMb: Number((e.target as HTMLInputElement).value) || 0,
                                };
                            }}
                        ></wa-input>
                        <wa-input
                            label="Maximum size (MB)"
                            type="number"
                            min="0"
                            placeholder="No maximum"
                            .value=${this.prefs.maxSizeMb ? String(this.prefs.maxSizeMb) : ''}
                            @input=${(e: Event) => {
                                this.prefs = {
                                    ...this.prefs,
                                    maxSizeMb: Number((e.target as HTMLInputElement).value) || 0,
                                };
                            }}
                        ></wa-input>
                        <wa-input
                            label="Preferred size (MB)"
                            type="number"
                            min="0"
                            placeholder="No preference"
                            .value=${this.prefs.preferredSizeMb
                                ? String(this.prefs.preferredSizeMb)
                                : ''}
                            @input=${(e: Event) => {
                                this.prefs = {
                                    ...this.prefs,
                                    preferredSizeMb:
                                        Number((e.target as HTMLInputElement).value) || 0,
                                };
                            }}
                        ></wa-input>
                    </div>

                    <div>
                        <div class="requires">
                            Allowed formats — leave all unchecked to allow any format.
                        </div>
                        <div class="format-options">
                            ${AUTO_DOWNLOAD_FORMATS.map(
                                (format) => html`
                                    <label class="format-option">
                                        <input
                                            type="checkbox"
                                            .checked=${(this.prefs.allowedFormats ?? []).includes(
                                                format.value,
                                            )}
                                            @change=${(e: Event) =>
                                                this.toggleFormat(
                                                    format.value,
                                                    (e.target as HTMLInputElement).checked,
                                                )}
                                        />
                                        ${format.label}
                                    </label>
                                `,
                            )}
                        </div>
                    </div>

                    <div class="form-actions">
                        ${this.prefsSaved
                            ? html`<span class="test-result ok">Saved.</span>`
                            : nothing}
                        <wa-button
                            size="small"
                            variant="brand"
                            ?disabled=${this.prefsSaving}
                            @click=${this.savePreferences}
                        >
                            ${this.prefsSaving
                                ? html`<wa-spinner></wa-spinner>`
                                : 'Save preferences'}
                        </wa-button>
                    </div>
                </div>
            </config-section>
        `;
    }

    private renderProvider(provider: DownloadProvider) {
        const descriptor = this.descriptorFor(provider.kind);
        const test = this.testResults[provider.id];

        if (this.editing === provider.id) {
            return this.renderEditForm(provider);
        }

        return html`
            <div class="client">
                <div>
                    <div class="client-name">${provider.name}</div>
                    <div class="client-meta">
                        ${descriptor?.name ?? provider.kind} ·
                        ${provider.enabled ? 'Enabled' : 'Disabled'} ·
                        priority ${provider.priority}
                    </div>
                    ${test
                        ? html`<div class="test-result ${test.ok ? 'ok' : 'fail'}">
                              ${test.message}
                          </div>`
                        : nothing}
                </div>
                <div class="client-actions">
                    <wa-button
                        size="small"
                        appearance="plain"
                        ?disabled=${this.testing === provider.id}
                        @click=${() => this.testProvider(provider)}
                    >
                        ${this.testing === provider.id
                            ? html`<wa-spinner></wa-spinner>`
                            : 'Test'}
                    </wa-button>
                    <wa-button
                        size="small"
                        appearance="plain"
                        @click=${() => this.startEdit(provider)}
                    >
                        Edit
                    </wa-button>
                    <wa-button
                        size="small"
                        appearance="plain"
                        variant="danger"
                        @click=${() => this.deleteProvider(provider)}
                    >
                        Remove
                    </wa-button>
                </div>
            </div>
        `;
    }

    private renderAddForm() {
        const descriptor = this.descriptorFor(this.newKind);

        return html`
            <div class="form">
                <wa-select
                    label="Client type"
                    .value=${this.newKind}
                    @change=${this.onKindChange}
                >
                    ${this.descriptors.map(
                        (d) => html`<wa-option value=${d.kind}>${d.name}</wa-option>`,
                    )}
                </wa-select>

                ${descriptor
                    ? html`
                        <div class="requires">
                            ${descriptor.summary}
                            ${descriptor.requiresExternal
                                ? html`<br />Requires a running
                                      ${descriptor.requiresExternal} instance.`
                                : nothing}
                        </div>

                        <wa-input
                            label="Name"
                            .value=${this.draftName}
                            @input=${(e: Event) => {
                                this.draftName = (e.target as HTMLInputElement).value;
                            }}
                        ></wa-input>

                        ${this.renderFields(descriptor)}
                    `
                    : nothing}

                <div class="form-actions">
                    <wa-button size="small" appearance="plain" @click=${this.cancelEdit}>
                        Cancel
                    </wa-button>
                    <wa-button
                        size="small"
                        variant="brand"
                        ?disabled=${!descriptor}
                        @click=${this.saveNew}
                    >
                        Add
                    </wa-button>
                </div>
            </div>
        `;
    }

    private renderEditForm(provider: DownloadProvider) {
        const descriptor = this.descriptorFor(provider.kind);

        return html`
            <div class="form">
                <wa-input
                    label="Name"
                    .value=${this.draftName}
                    @input=${(e: Event) => {
                        this.draftName = (e.target as HTMLInputElement).value;
                    }}
                ></wa-input>

                ${descriptor ? this.renderFields(descriptor, provider) : nothing}

                <wa-input
                    label="Priority"
                    type="number"
                    .value=${String(provider.priority)}
                    @input=${(e: Event) => {
                        this.draft['__priority'] = (e.target as HTMLInputElement).value;
                    }}
                ></wa-input>

                <wa-switch
                    ?checked=${provider.enabled}
                    @change=${(e: Event) => {
                        this.draft['__enabled'] = (e.target as HTMLInputElement)
                            .checked
                            ? '1'
                            : '';
                    }}
                >
                    Enabled
                </wa-switch>

                <div class="form-actions">
                    <wa-button size="small" appearance="plain" @click=${this.cancelEdit}>
                        Cancel
                    </wa-button>
                    <wa-button
                        size="small"
                        variant="brand"
                        @click=${() => this.saveEdit(provider)}
                    >
                        Save
                    </wa-button>
                </div>
            </div>
        `;
    }

    /** Renders one input per descriptor field, plus a folder browse
     * button for path fields and an "already set" placeholder for
     * secrets the provider already has a stored value for. */
    private renderFields(descriptor: DownloadDescriptor, provider?: DownloadProvider) {
        return (descriptor.fields ?? []).map((field) => {
            const isSet = field.secret && provider?.setSecrets?.[field.key];
            const placeholder = isSet
                ? '•••••••• (unchanged — enter a new value to replace it)'
                : (field.placeholder ?? '');

            return html`
                <div class="field-row">
                    <wa-input
                        label=${field.label}
                        placeholder=${placeholder}
                        type=${field.secret ? 'password' : 'text'}
                        .value=${this.draft[field.key] ?? ''}
                        @input=${(e: Event) => {
                            this.draft = {
                                ...this.draft,
                                [field.key]: (e.target as HTMLInputElement).value,
                            };
                        }}
                    >
                        ${field.help ? html`<span slot="hint">${field.help}</span>` : nothing}
                    </wa-input>
                    ${field.path
                        ? html`
                            <wa-button
                                size="small"
                                appearance="outlined"
                                class="browse-button"
                                @click=${() => this.browseForFolder(field)}
                            >
                                Browse
                            </wa-button>
                        `
                        : nothing}
                </div>
            `;
        });
    }

    private browseForFolder = async (field: ProviderField) => {
        try {
            const dir = await DirectoryPicker();

            if (dir) {
                this.draft = { ...this.draft, [field.key]: dir };
            }
        } catch (err) {
            console.error('Failed to open directory picker:', err);
        }
    };

    private descriptorFor(kind: string): DownloadDescriptor | undefined {
        return this.descriptors.find((d) => d.kind === kind);
    }

    private startAdd = () => {
        this.editing = 'new';
        this.errorMessage = '';
        this.newKind = this.descriptors[0]?.kind ?? '';
        this.draftName = this.descriptorFor(this.newKind)?.name ?? '';
        this.draft = this.defaultsFor(this.newKind);
    };

    private startEdit(provider: DownloadProvider) {
        this.editing = provider.id;
        this.errorMessage = '';
        this.draftName = provider.name;
        // Secrets are never sent back to the frontend, so their fields
        // start blank; a blank secret on save means "leave it alone"
        // rather than "clear it".
        this.draft = { ...(provider.settings ?? {}) };
    }

    private cancelEdit = () => {
        this.editing = null;
        this.draft = {};
        this.errorMessage = '';
    };

    private onKindChange = (event: Event) => {
        this.newKind = (event.target as HTMLInputElement).value;
        this.draftName = this.descriptorFor(this.newKind)?.name ?? '';
        this.draft = this.defaultsFor(this.newKind);
    };

    private defaultsFor(kind: string): Record<string, string> {
        const descriptor = this.descriptorFor(kind);
        const out: Record<string, string> = {};

        for (const field of descriptor?.fields ?? []) {
            if (field.default) out[field.key] = field.default;
        }

        return out;
    }

    private saveNew = async () => {
        this.errorMessage = '';

        try {
            await downloadStore.addProvider(
                this.newKind,
                this.draftName || this.newKind,
                this.cleanDraft(),
            );

            this.cancelEdit();
        } catch (err) {
            this.errorMessage = String(err);
        }
    };

    private async saveEdit(provider: DownloadProvider) {
        this.errorMessage = '';

        const priority = this.draft['__priority']
            ? Number(this.draft['__priority'])
            : provider.priority;

        const enabled =
            '__enabled' in this.draft
                ? this.draft['__enabled'] === '1'
                : provider.enabled;

        try {
            await downloadStore.updateProvider(
                provider.id,
                this.draftName || provider.name,
                enabled,
                priority,
                this.cleanDraft(),
            );

            this.cancelEdit();
        } catch (err) {
            this.errorMessage = String(err);
        }
    }

    /** Strips the form's internal bookkeeping keys before saving. */
    private cleanDraft(): Record<string, string> {
        const out: Record<string, string> = {};

        for (const [key, value] of Object.entries(this.draft)) {
            if (!key.startsWith('__')) out[key] = value;
        }

        return out;
    }

    private async deleteProvider(provider: DownloadProvider) {
        this.errorMessage = '';

        try {
            await downloadStore.deleteProvider(provider.id);
        } catch (err) {
            this.errorMessage = String(err);
        }
    }

    private async testProvider(provider: DownloadProvider) {
        this.testing = provider.id;

        try {
            await downloadStore.testProvider(provider.id);

            this.testResults = {
                ...this.testResults,
                [provider.id]: { ok: true, message: 'Connected.' },
            };
        } catch (err) {
            this.testResults = {
                ...this.testResults,
                [provider.id]: { ok: false, message: String(err) },
            };
        } finally {
            this.testing = null;
        }
    }

    private toggleFormat(format: string, checked: boolean): void {
        const current = this.prefs.allowedFormats ?? [];
        const allowedFormats = checked
            ? [...current, format]
            : current.filter((f) => f !== format);

        this.prefs = { ...this.prefs, allowedFormats };
    }

    /**
     * Saves the guardrails both to disk and to the running download
     * manager in one action — persistence alone would leave the setting
     * inert until restart, which is exactly the bug this mirrors away
     * from (see `config.Library`'s prior persist-without-apply gap).
     */
    private savePreferences = async () => {
        this.prefsSaving = true;
        this.prefsError = '';
        this.prefsSaved = false;

        try {
            await SetDownloadPreferences(this.prefs);
            await SetPreferences(this.prefs);
            this.prefsSaved = true;
        } catch (err) {
            this.prefsError = String(err);
        } finally {
            this.prefsSaving = false;
        }
    };
}

declare global {
    interface HTMLElementTagNameMap {
        'download-clients': DownloadClients;
    }
}
