import { library } from '@go/models';
import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query } from 'lit/decorators.js';
import { formatMilliseconds } from '@utils/time';
import { PlayerController } from '@store/controllers/player-controller';
import { QueueController } from '@store/controllers/queue-controller';
import { LibraryController } from '@store/controllers/library-controller';
import '@lit-labs/virtualizer';
import type {
    LitVirtualizer,
    VisibilityChangedEvent,
} from '@lit-labs/virtualizer';
import { flow } from '@lit-labs/virtualizer/layouts/flow.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@components/playlist-picker/playlist-picker.js';
import type { PlaylistPicker } from '@components/playlist-picker/playlist-picker.js';

const COLUMN_STORAGE_KEY = 'track-list-column-widths';
const MIN_COLUMN_WIDTH = 50;
const DEFAULT_DURATION_WIDTH = 80;
const COLUMN_COUNT = 3;

@customElement('track-list')
export class TrackList extends LitElement {
    private player = new PlayerController(this);
    private queue = new QueueController(this);
    private libraryCtrl = new LibraryController(this);

    @state()
    private tracks: library.Track[] = [];

    @state()
    private selectedTracks: Set<string> = new Set();

    @state()
    private contextMenuOpen = false;

    @state()
    private playlistSubmenuOpen = false;

    @query('#context-menu')
    private contextMenuPopup!: HTMLElement;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: HTMLElement;

    @query('lit-virtualizer')
    private virtualizer!: LitVirtualizer;

    private lastSelectedIndex: number | null = null;
    private lastActiveTrackPath: string | null = null;

    private closeHandler = () => this.closeContextMenu();

    @state()
    private columnWidths: number[] = [];

    private resizingColumn: number | null = null;
    private resizeStartX = 0;
    private resizeStartWidths: number[] = [];
    private resizeObserver: ResizeObserver | null = null;
    private flowLayout = flow();
    private hasRestoredScroll = false;

    private get gridTemplateColumns(): string {
        if (this.columnWidths.length === 0) {
            return '1fr 1fr 80px';
        }

        return this.columnWidths
            .map((w) => `${w}px`)
            .join(' ');
    }

    private get colBoundaryPositions(): number[] {
        if (this.columnWidths.length === 0) return [];

        const padding = 8;
        const positions: number[] = [];
        let cumulative = padding;

        for (let i = 0; i < this.columnWidths.length - 1; i++) {
            cumulative += this.columnWidths[i] ?? 0;
            positions.push(cumulative);
        }

        return positions;
    }

    private initColumnWidths() {
        const saved = this.loadColumnWidths();

        if (saved) {
            this.columnWidths = saved;

            return;
        }

        this.computeDefaultWidths();
    }

    private computeDefaultWidths() {
        const totalWidth = this.clientWidth;

        if (totalWidth <= 0) return;

        const remaining = totalWidth - DEFAULT_DURATION_WIDTH;
        const half = Math.floor(remaining / 2);

        this.columnWidths = [
            half,
            remaining - half,
            DEFAULT_DURATION_WIDTH,
        ];
    }

    private loadColumnWidths(): number[] | null {
        try {
            const raw = localStorage.getItem(COLUMN_STORAGE_KEY);

            if (!raw) return null;

            const parsed: unknown = JSON.parse(raw);

            if (
                !Array.isArray(parsed) ||
                parsed.length !== COLUMN_COUNT ||
                !parsed.every(
                    (v: unknown) =>
                        typeof v === 'number' && v >= MIN_COLUMN_WIDTH,
                )
            ) {
                return null;
            }

            return parsed as number[];
        } catch {
            return null;
        }
    }

    private saveColumnWidths() {
        try {
            localStorage.setItem(
                COLUMN_STORAGE_KEY,
                JSON.stringify(this.columnWidths),
            );
        } catch {
            // Ignore storage errors.
        }
    }

    private onColResizeStart = (e: MouseEvent, columnIndex: number) => {
        e.preventDefault();
        this.resizingColumn = columnIndex;
        this.resizeStartX = e.clientX;
        this.resizeStartWidths = [...this.columnWidths];
        this.requestUpdate();
    };

    private onColResizeMove = (e: MouseEvent) => {
        if (this.resizingColumn === null) return;

        const delta = e.clientX - this.resizeStartX;
        const col = this.resizingColumn;
        const nextCol = col + 1;
        const startLeft = this.resizeStartWidths[col] ?? 0;
        const startRight = this.resizeStartWidths[nextCol] ?? 0;
        const total = startLeft + startRight;

        let newLeft = startLeft + delta;
        let newRight = startRight - delta;

        if (newLeft < MIN_COLUMN_WIDTH) {
            newLeft = MIN_COLUMN_WIDTH;
            newRight = total - MIN_COLUMN_WIDTH;
        }

        if (newRight < MIN_COLUMN_WIDTH) {
            newRight = MIN_COLUMN_WIDTH;
            newLeft = total - MIN_COLUMN_WIDTH;
        }

        const updated = [...this.resizeStartWidths];

        updated[col] = newLeft;
        updated[nextCol] = newRight;
        this.columnWidths = updated;
    };

    private onColResizeEnd = () => {
        if (this.resizingColumn === null) return;

        this.resizingColumn = null;
        this.saveColumnWidths();
        this.requestUpdate();
    };

    static override styles = css`
    :host {
      position: relative;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }

    .header-row {
      display: grid;
      padding: 8px;
      font-weight: bold;
      color: #fff;
      border-bottom: 1px solid #666;
      flex-shrink: 0;
    }

    .header-cell {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .resize-overlay {
      position: absolute;
      inset: 0;
      pointer-events: none;
      z-index: 2;
    }

    .col-resize-handle {
      position: absolute;
      top: 0;
      height: 100%;
      width: 1px;
      cursor: col-resize;
      pointer-events: auto;
      background-color: #444;
      transition: background-color 0.15s ease;
    }

    .col-resize-handle::before {
      content: '';
      position: absolute;
      top: 0;
      left: -3px;
      width: 7px;
      height: 100%;
    }

    .col-resize-handle:hover,
    .col-resize-handle.active {
      background-color: #6c757d;
    }

    lit-virtualizer {
      flex: 1;
      overflow-y: auto;
    }

    .track-row {
      display: grid;
      font-size: 12px;
      padding: 8px;
      border-bottom: 1px solid #333;
      align-items: center;
      width: 100%;
      cursor: default;
      user-select: none;
    }

    .track-row > * {
      min-width: 0;
    }

    .header-cell + .header-cell,
    .track-row > :not(:first-child) {
      padding-left: 6px;
    }

    .track-row:hover {
      background-color: rgba(255, 255, 255, 0.05);
    }

    .track-row.selected {
      background-color: rgba(100, 160, 255, 0.15);
    }

    .track-row.active {
      background-color: rgba(255, 212, 59, 0.1);
    }

    .track-row.active {
      color: #ffd43b;
    }

    .track-row.selected.active {
      background-color: rgba(100, 160, 255, 0.15);
    }

    .track-name {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      cursor: default;
      user-select: none;
    }

    .artist-name,
    .track-length {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    #context-menu {
      z-index: 200;
    }

    .context-menu-panel {
      background-color: #343a40;
      border: 1px solid #444;
      border-radius: 6px;
      padding: 4px 0;
      box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
      min-width: 160px;
    }

    .context-menu-panel wa-dropdown-item {
      cursor: pointer;
    }

    .context-menu-panel wa-dropdown-item {
      --wa-color-text-normal: #fff;
      font-size: 13px;
    }

    .context-menu-panel wa-dropdown-item:hover {
      background-color: rgba(255, 255, 255, 0.1);
    }

    .submenu-item {
      position: relative;
    }

    .submenu-arrow {
      font-size: 10px;
      margin-left: auto;
      padding-left: 12px;
    }

    #playlist-submenu {
      z-index: 210;
    }
  `;

    override connectedCallback() {
        super.connectedCallback();
        this.loadTracks();
        document.addEventListener('click', this.closeHandler);
        document.addEventListener('contextmenu', this.closeHandler);
        document.addEventListener('mousemove', this.onColResizeMove);
        document.addEventListener('mouseup', this.onColResizeEnd);

        this.resizeObserver = new ResizeObserver(() => {
            this.onHostResize();
        });

        this.resizeObserver.observe(this);
    }

    override disconnectedCallback() {
        this.virtualizer?.removeEventListener(
            'visibilityChanged',
            this.onVisibilityChanged,
        );
        this.hasRestoredScroll = false;
        super.disconnectedCallback();
        document.removeEventListener('click', this.closeHandler);
        document.removeEventListener('contextmenu', this.closeHandler);
        document.removeEventListener('mousemove', this.onColResizeMove);
        document.removeEventListener('mouseup', this.onColResizeEnd);

        this.resizeObserver?.disconnect();
        this.resizeObserver = null;
    }

    override firstUpdated() {
        this.initColumnWidths();
    }

    override updated(changed: Map<string, unknown>) {
        if (
            changed.has('columnWidths') ||
            changed.has('selectedTracks')
        ) {
            this.virtualizer?.requestUpdate();
        }

        const currentPath =
            this.player.currentTrack?.filePath ?? null;

        if (currentPath !== this.lastActiveTrackPath) {
            this.lastActiveTrackPath = currentPath;
            this.virtualizer?.requestUpdate();
        }
    }

    private previousHostWidth = 0;

    private onHostResize() {
        const newWidth = this.clientWidth;

        if (
            newWidth <= 0 ||
            this.columnWidths.length === 0 ||
            this.resizingColumn !== null
        ) {
            return;
        }

        if (this.previousHostWidth === 0) {
            this.previousHostWidth = newWidth;

            return;
        }

        const oldTotal = this.columnWidths.reduce(
            (sum, w) => sum + w,
            0,
        );

        if (oldTotal <= 0) return;

        const scale = newWidth / oldTotal;

        this.columnWidths = this.columnWidths.map((w) =>
            Math.max(
                MIN_COLUMN_WIDTH,
                Math.round(w * scale),
            ),
        );

        this.previousHostWidth = newWidth;
        this.saveColumnWidths();
    }

    async loadTracks() {
        try {
            const tracks = await this.libraryCtrl.getTracks();
            this.tracks = tracks;
            this.selectedTracks = new Set();
            this.lastSelectedIndex = null;
            await this.updateComplete;

            if (this.isConnected && this.virtualizer) {
                this.virtualizer.addEventListener(
                    'visibilityChanged',
                    this.onVisibilityChanged,
                );
            }
        } catch (error) {
            console.error('Error loading tracks:', error);
        }
    }

    private onVisibilityChanged = (e: Event) => {
        const { first } = e as VisibilityChangedEvent;

        if (!this.hasRestoredScroll) {
            this.hasRestoredScroll = true;

            const savedIndex =
                this.libraryCtrl.getScrollPosition('tracks');

            if (savedIndex > 0) {
                requestAnimationFrame(() => {
                    this.virtualizer?.scrollToIndex(
                        savedIndex,
                        'start',
                    );
                });

                return;
            }
        }

        this.libraryCtrl.setScrollPosition('tracks', first);
    };

    private getSelectedFilePaths(): string[] {
        return this.tracks
            .filter((t) => this.selectedTracks.has(t.FilePath))
            .map((t) => t.FilePath);
    }

    private selectRange(from: number, to: number): Set<string> {
        const start = Math.min(from, to);
        const end = Math.max(from, to);
        const paths = new Set<string>();

        for (let i = start; i <= end; i++) {
            const track = this.tracks[i];

            if (track) {
                paths.add(track.FilePath);
            }
        }

        return paths;
    }

    private onTrackRowClick(
        e: MouseEvent,
        track: library.Track,
        index: number,
    ) {
        const isCtrl = e.ctrlKey || e.metaKey;
        const isShift = e.shiftKey;

        if (isShift && this.lastSelectedIndex !== null) {
            const range = this.selectRange(
                this.lastSelectedIndex,
                index,
            );

            if (isCtrl) {
                // Ctrl+Shift: add range to existing selection.
                const next = new Set(this.selectedTracks);

                for (const path of range) {
                    next.add(path);
                }

                this.selectedTracks = next;
            } else {
                // Shift only: add range to existing selection.
                const next = new Set(this.selectedTracks);

                for (const path of range) {
                    next.add(path);
                }

                this.selectedTracks = next;
            }

            // Don't update anchor on shift-click so user can
            // adjust the range endpoint with another shift-click.
        } else if (isCtrl) {
            const next = new Set(this.selectedTracks);

            if (next.has(track.FilePath)) {
                next.delete(track.FilePath);
            } else {
                next.add(track.FilePath);
            }

            this.selectedTracks = next;
            this.lastSelectedIndex = index;
        } else {
            this.selectedTracks = new Set([track.FilePath]);
            this.lastSelectedIndex = index;
        }
    }

    private onTrackRowDblClick(track: library.Track) {
        this.selectedTracks = new Set();
        this.queue.setQueue([track.FilePath], 0);
    }

    private onTrackContextMenu(e: MouseEvent, track: library.Track) {
        e.preventDefault();
        e.stopPropagation();

        if (!this.selectedTracks.has(track.FilePath)) {
            this.selectedTracks = new Set([track.FilePath]);
        }

        this.contextMenuOpen = true;

        // Position the popup at the mouse cursor using a virtual anchor.
        this.updateComplete.then(() => {
            const popup = this.contextMenuPopup;

            if (popup) {
                (popup as any).anchor = {
                    getBoundingClientRect() {
                        return {
                            width: 0,
                            height: 0,
                            x: e.clientX,
                            y: e.clientY,
                            top: e.clientY,
                            left: e.clientX,
                            right: e.clientX,
                            bottom: e.clientY,
                        };
                    },
                };
                (popup as any).active = true;
            }
        });
    }

    private onContextMenuAction(action: string) {
        const filePaths = this.getSelectedFilePaths();

        if (filePaths.length === 0) return;

        switch (action) {
            case 'play':
                this.queue.setQueue(filePaths, 0);
                break;
            case 'add-to-queue':
                this.queue.addTracksToQueue(filePaths);
                break;
            case 'play-next':
                this.queue.playTracksNext(filePaths);
                break;
        }

        this.closeContextMenu(true);
    }

    private closeContextMenu(clearSelection = false) {
        if (!this.contextMenuOpen) return;

        this.closePlaylistSubmenu();
        this.contextMenuOpen = false;

        if (clearSelection) {
            this.selectedTracks = new Set();
        }

        const popup = this.contextMenuPopup;

        if (popup) {
            (popup as any).active = false;
        }
    }

    private async showPlaylistSubmenu() {
        if (this.playlistSubmenuOpen) return;

        this.playlistSubmenuOpen = true;

        await this.updateComplete;

        const submenu = this.playlistSubmenuPopup;
        const trigger = this.shadowRoot?.querySelector('.submenu-item');

        if (submenu && trigger) {
            (submenu as any).anchor = trigger;
            (submenu as any).active = true;
        }

        const picker = this.shadowRoot?.querySelector(
            'playlist-picker',
        ) as PlaylistPicker | null;

        picker?.reset();
    }

    private closePlaylistSubmenu() {
        if (!this.playlistSubmenuOpen) return;

        this.playlistSubmenuOpen = false;

        const submenu = this.playlistSubmenuPopup;

        if (submenu) {
            (submenu as any).active = false;
        }
    }

    private onPlaylistActionComplete = () => {
        this.closeContextMenu(true);
    };

    private isActiveTrack(track: library.Track): boolean {
        const currentTrack = this.player.currentTrack;

        if (!currentTrack) return false;

        return currentTrack.filePath === track.FilePath;
    }

    private renderTrackRow = (
        track: library.Track,
        index: number,
    ): unknown => {
        const active = this.isActiveTrack(track);
        const selected = this.selectedTracks.has(track.FilePath);

        const classes = [
            'track-row',
            active ? 'active' : '',
            selected ? 'selected' : '',
        ]
            .filter(Boolean)
            .join(' ');

        const colStyle =
            `grid-template-columns: ${this.gridTemplateColumns}`;

        return html`
      <div
        class=${classes}
        style=${colStyle}
        @click=${(e: MouseEvent) =>
                this.onTrackRowClick(e, track, index)}
        @dblclick=${() => this.onTrackRowDblClick(track)}
        @contextmenu=${(e: MouseEvent) =>
                this.onTrackContextMenu(e, track)}
      >
        <div class="track-name">${track.TrackName}</div>
        <div class="artist-name">${track.ArtistName}</div>
        <div class="track-length">
          ${formatMilliseconds(track.TrackLength)}
        </div>
      </div>
    `;
    };

    override render() {
        return html`
      ${this.tracks.length === 0
                ? html`<p>Loading tracks...</p>`
                : html`
            <div
              class="header-row"
              style="grid-template-columns: ${this.gridTemplateColumns}"
            >
              <div class="header-cell">Track Name</div>
              <div class="header-cell">Artist</div>
              <div class="header-cell">Track Length</div>
            </div>
            <lit-virtualizer
              scroller
              .items=${this.tracks}
              .renderItem=${this.renderTrackRow}
              .layout=${this.flowLayout}
            ></lit-virtualizer>
          `}

      <div class="resize-overlay">
        ${this.colBoundaryPositions.map(
                (pos, i) => html`
            <div
              class="col-resize-handle ${this.resizingColumn === i ? 'active' : ''}"
              style="left: ${pos}px"
              @mousedown=${(e: MouseEvent) =>
                        this.onColResizeStart(e, i)}
            ></div>
          `,
            )}
      </div>

      <wa-popup
        id="context-menu"
        placement="bottom-start"
        .active=${this.contextMenuOpen}
      >
        ${this.contextMenuOpen
                ? html`
              <div class="context-menu-panel">
                <wa-dropdown-item
                  @click=${() => this.onContextMenuAction('play')}
                >
                  <wa-icon slot="icon" name="play"></wa-icon>
                  Play
                </wa-dropdown-item>
                <wa-dropdown-item
                  @click=${() => this.onContextMenuAction('add-to-queue')}
                >
                  <wa-icon slot="icon" name="plus"></wa-icon>
                  Add to Queue
                </wa-dropdown-item>
                <wa-dropdown-item
                  @click=${() => this.onContextMenuAction('play-next')}
                >
                  <wa-icon slot="icon" name="forward-step"></wa-icon>
                  Play Next
                </wa-dropdown-item>
                <wa-dropdown-item
                  class="submenu-item"
                  @mouseenter=${() => this.showPlaylistSubmenu()}
                  @click=${(e: Event) => {
                        e.stopPropagation();
                        void this.showPlaylistSubmenu();
                    }}
                >
                  <wa-icon slot="icon" name="plus"></wa-icon>
                  Add to Playlist
                  <span class="submenu-arrow">&#9654;</span>
                </wa-dropdown-item>
              </div>
            `
                : nothing}
      </wa-popup>

      <wa-popup
        id="playlist-submenu"
        placement="right-start"
        .active=${this.playlistSubmenuOpen}
      >
        ${this.playlistSubmenuOpen && this.selectedTracks.size > 0
                ? html`
              <playlist-picker
                .filePaths=${this.getSelectedFilePaths()}
                @playlist-action-complete=${this.onPlaylistActionComplete}
                @click=${(e: Event) => e.stopPropagation()}
              ></playlist-picker>
            `
                : nothing}
      </wa-popup>
    `;
    }
}
