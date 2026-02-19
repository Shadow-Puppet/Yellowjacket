import { LitElement, html, css } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { EventsOn, EventsOff } from '@runtime/runtime';
import { Scan, FullRescan } from '@go/library/Library';
import {
    GetLibraryDirectory,
    SetLibraryDirectory,
} from '@go/config/Config';
import { DirectoryPicker } from '@go/frontendutil/FrontendUtil';
import { Events } from '../../events';

@customElement('library-manager')
export class LibraryManager extends LitElement {
    @state() private libraryDirectory = '';
    @state() private selectedDirectory = '';
    @state() private scanning = false;
    @state() private statusMessage = '';

    static override styles = css`
        :host {
            display: block;
            padding: 1.5em;
            color: #e9ecef;
            font-family: system-ui, -apple-system, sans-serif;
            overflow-y: auto;
        }

        h2 {
            margin: 0 0 1em 0;
            font-size: 1.4em;
            font-weight: 600;
            color: #f8f9fa;
        }

        .section {
            margin-bottom: 2em;
            padding: 1.25em;
            background: #2b3035;
            border-radius: 8px;
        }

        .section-title {
            margin: 0 0 0.75em 0;
            font-size: 1em;
            font-weight: 600;
            color: #dee2e6;
        }

        .section-description {
            margin: 0 0 1em 0;
            font-size: 0.85em;
            color: #868e96;
            line-height: 1.4;
        }

        .directory-row {
            display: flex;
            align-items: center;
            gap: 0.75em;
            margin-bottom: 1em;
        }

        .directory-path {
            flex: 1;
            padding: 0.5em 0.75em;
            background: #1a1d20;
            border: 1px solid #495057;
            border-radius: 4px;
            color: #adb5bd;
            font-size: 0.85em;
            font-family: monospace;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            min-height: 1.2em;
        }

        .directory-path.has-value {
            color: #e9ecef;
        }

        button {
            padding: 0.5em 1.25em;
            border: none;
            border-radius: 4px;
            font-size: 0.85em;
            font-weight: 500;
            cursor: pointer;
            transition: background-color 0.15s ease;
            white-space: nowrap;
        }

        button:disabled {
            opacity: 0.5;
            cursor: not-allowed;
        }

        .btn-primary {
            background: #4263eb;
            color: white;
        }

        .btn-primary:hover:not(:disabled) {
            background: #3b5bdb;
        }

        .btn-success {
            background: #2f9e44;
            color: white;
        }

        .btn-success:hover:not(:disabled) {
            background: #2b8a3e;
        }

        .btn-warning {
            background: #e8590c;
            color: white;
        }

        .btn-warning:hover:not(:disabled) {
            background: #d9480f;
        }

        .btn-danger {
            background: #e03131;
            color: white;
        }

        .btn-danger:hover:not(:disabled) {
            background: #c92a2a;
        }

        .scan-actions {
            display: flex;
            gap: 0.75em;
            flex-wrap: wrap;
        }

        .status-bar {
            margin-top: 1.5em;
            padding: 0.75em 1em;
            background: #1a1d20;
            border-radius: 4px;
            font-size: 0.85em;
            color: #868e96;
            min-height: 1.2em;
        }

        .status-bar.active {
            color: #ffd43b;
        }
    `;

    override connectedCallback(): void {
        super.connectedCallback();
        this.loadCurrentDirectory();

        EventsOn(
            Events.LibraryScanStarted,
            this.handleScanStarted,
        );
        EventsOn(
            Events.LibraryScanComplete,
            this.handleScanComplete,
        );
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        EventsOff(Events.LibraryScanStarted);
        EventsOff(Events.LibraryScanComplete);
    }

    private async loadCurrentDirectory(): Promise<void> {
        try {
            const dir = await GetLibraryDirectory();
            this.libraryDirectory = dir;
            this.selectedDirectory = dir;
        } catch (err) {
            console.error(
                'Failed to load library directory:',
                err,
            );
        }
    }

    private handleScanStarted = (): void => {
        this.scanning = true;
        this.statusMessage = 'Scanning...';
    };

    private handleScanComplete = (): void => {
        this.scanning = false;
        this.statusMessage = 'Scan complete.';
    };

    private handleSelectDirectory = async (): Promise<void> => {
        try {
            const dir = await DirectoryPicker();

            if (dir) {
                this.selectedDirectory = dir;
            }
        } catch (err) {
            console.error('Directory picker failed:', err);
        }
    };

    private handleSaveDirectory = async (): Promise<void> => {
        if (!this.selectedDirectory) return;

        try {
            await SetLibraryDirectory(this.selectedDirectory);
            this.libraryDirectory = this.selectedDirectory;
            this.statusMessage =
                'Library directory saved. A scan will start automatically if the directory changed.';
        } catch (err) {
            this.statusMessage = `Failed to save directory: ${err}`;
            console.error('Failed to save directory:', err);
        }
    };

    private handleSoftScan = async (): Promise<void> => {
        try {
            await Scan();
        } catch (err) {
            this.statusMessage = `Scan failed: ${err}`;
            console.error('Soft scan failed:', err);
        }
    };

    private handleFullRescan = async (): Promise<void> => {
        const confirmed = confirm(
            'This will delete ALL library data including cover art and re-scan from scratch. Continue?',
        );

        if (!confirmed) return;

        try {
            await FullRescan();
        } catch (err) {
            this.statusMessage = `Full rescan failed: ${err}`;
            console.error('Full rescan failed:', err);
        }
    };

    private get directoryChanged(): boolean {
        return (
            this.selectedDirectory !== this.libraryDirectory
        );
    }

    override render() {
        return html`
            <h2>Library Manager</h2>

            <div class="section">
                <p class="section-title">
                    Library Directory
                </p>
                <p class="section-description">
                    Select the root directory containing your
                    music files. Changing this will
                    automatically trigger a scan.
                </p>
                <div class="directory-row">
                    <div
                        class="directory-path ${this.selectedDirectory ? 'has-value' : ''}"
                    >
                        ${this.selectedDirectory ||
                        'No directory selected'}
                    </div>
                    <button
                        class="btn-primary"
                        @click=${this.handleSelectDirectory}
                    >
                        Browse
                    </button>
                    <button
                        class="btn-success"
                        ?disabled=${!this.directoryChanged ||
                        this.scanning}
                        @click=${this.handleSaveDirectory}
                    >
                        Save
                    </button>
                </div>
            </div>

            <div class="section">
                <p class="section-title">Scan Actions</p>
                <p class="section-description">
                    Soft scan finds new files, skips existing
                    ones, and removes orphaned entries. Full
                    rescan clears the entire database and
                    cover art cache, then re-imports
                    everything.
                </p>
                <div class="scan-actions">
                    <button
                        class="btn-warning"
                        ?disabled=${this.scanning}
                        @click=${this.handleSoftScan}
                    >
                        ${this.scanning
                            ? 'Scanning...'
                            : 'Soft Scan'}
                    </button>
                    <button
                        class="btn-danger"
                        ?disabled=${this.scanning}
                        @click=${this.handleFullRescan}
                    >
                        ${this.scanning
                            ? 'Scanning...'
                            : 'Full Rescan'}
                    </button>
                </div>
            </div>

            <div
                class="status-bar ${this.scanning ? 'active' : ''}"
            >
                ${this.statusMessage || 'Ready.'}
            </div>
        `;
    }
}
