import '@components/audio-player/audio-player.ts';
import '@components/track-list/track-list.ts';
import '@components/cover-grid/cover-grid.ts';
import '@components/now-playing/now-playing.ts';
import '@components/sidebar/app-sidebar.ts';
import '@components/queue-panel/queue-panel.ts';
import '@components/playlist-view/playlist-view.ts';
import '@components/config-page/config-page.ts';
import '@components/artists-view/artists-view.ts';
import '@components/artist-details/artist-details.ts';
import '@components/genres-view/genres-view.ts';
import '@components/genre-details/genre-details.ts';
import '@components/playlist-details/playlist-details.ts';
import '@components/smart-playlist-details/smart-playlist-details.ts';
import '@components/smart-playlist-editor/smart-playlist-editor.ts';
import '@components/search-bar/search-bar.ts';
import '@components/library-filter/library-filter.ts';
import '@components/track-details/track-details.ts';
import '@components/explore-view/explore-view.ts';
import '@components/explore-artist-details/explore-artist-details.js';
import '@components/explore-album-details/explore-album-details.js';
import '@awesome.me/webawesome/dist/styles/themes/default.css';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { setBasePath } from '@awesome.me/webawesome/dist/webawesome.js';
import { queueStore } from '@store/queue-store';
import { searchStore } from '@store/search-store';
import * as Player from '@go/player/Player';
import * as Queue from '@go/queue/Queue';
// Importing the theme store triggers initialization: it fetches the saved
// theme from the backend and applies CSS custom properties to :root.
import '@store/theme-store';
// Importing the keyboard shortcut service triggers initialization:
// registers the document keydown listener for global shortcuts.
import './src/services/keyboard-shortcut-service';
import {
    hasTrackPayload,
    getDragPayload,
} from '@utils/drag-controller';
import type { DragActiveDetail } from '@utils/drag-controller';

setBasePath('/dist/webawesome');

// ---------------------------------------------------------------------------
// View caching navigation system
// ---------------------------------------------------------------------------
// Primary views (tracks, albums, artists, genres, playlists, settings) are
// created once and kept alive in the DOM.  Navigation toggles visibility
// (display: none ↔ display: '') instead of destroying/recreating via
// innerHTML.  Detail views (artist-details, playlist-details, genre-details)
// are ephemeral — created fresh each navigation because they depend on
// specific entity IDs that change.
// ---------------------------------------------------------------------------

const VIEW_TAGS: Record<string, string> = {
    tracks: 'track-list',
    albums: 'cover-grid',
    artists: 'artists-view',
    genres: 'genres-view',
    playlists: 'playlist-view',
    explore: 'explore-view',
    settings: 'config-page',
};

const viewCache = new Map<string, HTMLElement>();
let currentViewEl: HTMLElement | null = null;
let currentDetailEl: HTMLElement | null = null;

// Seed the cache with the default track-list rendered in index.html.
const mainContent = document.getElementById('main-content');

if (mainContent) {
    const initialTrackList = mainContent.querySelector('track-list');

    if (initialTrackList) {
        viewCache.set('tracks', initialTrackList as HTMLElement);
        currentViewEl = initialTrackList as HTMLElement;
    }
}

document.addEventListener('navigate', (e: Event) => {
    const detail = (e as CustomEvent).detail;
    const view: string = detail.view;

    if (!mainContent) return;

    searchStore.setCurrentView(view);

    // --- Primary (cacheable) views ----------------------------------------
    if (view in VIEW_TAGS) {
        // Remove any active detail view first
        if (currentDetailEl) {
            currentDetailEl.remove();
            currentDetailEl = null;
        }

        let target = viewCache.get(view);

        if (!target) {
            target = document.createElement(VIEW_TAGS[view]);
            viewCache.set(view, target);
            // Start hidden — we'll un-hide below
            target.classList.add('view-hidden');
            mainContent.appendChild(target);
        }

        // Hide current, show target.  Uses CSS class instead of
        // display:none so scroll containers preserve scrollTop.
        if (currentViewEl && currentViewEl !== target) {
            currentViewEl.classList.add('view-hidden');
        }
        target.classList.remove('view-hidden');
        currentViewEl = target;
        return;
    }

    // --- Detail (ephemeral) views -----------------------------------------
    // Hide the current primary view
    if (currentViewEl) {
        currentViewEl.classList.add('view-hidden');
    }
    // Remove any prior detail element
    if (currentDetailEl) {
        currentDetailEl.remove();
        currentDetailEl = null;
    }

    switch (view) {
        case 'artist-details': {
            const { artistId, artistName } = detail;
            const el = document.createElement('artist-details');

            el.setAttribute('artist-id', String(artistId));
            el.setAttribute('artist-name', artistName);
            mainContent.appendChild(el);
            currentDetailEl = el;
            break;
        }
        case 'playlist-details': {
            const { playlistId, playlistName } = detail;
            const plEl = document.createElement('playlist-details');

            plEl.setAttribute('playlist-id', String(playlistId));
            plEl.setAttribute('playlist-name', playlistName);
            mainContent.appendChild(plEl);
            currentDetailEl = plEl;
            break;
        }
        case 'smart-playlist-details': {
            const { playlistId, playlistName } = detail;
            const spEl = document.createElement('smart-playlist-details');

            spEl.setAttribute('playlist-id', String(playlistId));
            spEl.setAttribute('playlist-name', playlistName);
            if (detail.autoEdit) {
                spEl.setAttribute('auto-edit', '');
            }
            mainContent.appendChild(spEl);
            currentDetailEl = spEl;
            break;
        }
        case 'genre-details': {
            const { genreName } = detail;
            const genreEl = document.createElement('genre-details');

            genreEl.setAttribute('genre-name', genreName);
            mainContent.appendChild(genreEl);
            currentDetailEl = genreEl;
            break;
        }
        case 'explore-artist-details': {
            const { artistMBID, artistName } = detail;
            const el = document.createElement('explore-artist-details');

            el.setAttribute('artist-mbid', artistMBID);
            el.setAttribute('artist-name', artistName);
            mainContent.appendChild(el);
            currentDetailEl = el;
            break;
        }
        case 'explore-album-details': {
            const { releaseGroupMBID, albumName } = detail;
            const el = document.createElement('explore-album-details');

            el.setAttribute('release-group-mbid', releaseGroupMBID);
            el.setAttribute('album-name', albumName);
            mainContent.appendChild(el);
            currentDetailEl = el;
            break;
        }
        default: {
            const fallback = document.createElement('div');

            fallback.style.padding = '1em';
            fallback.style.color = 'var(--yj-text-secondary, #b3b3b3)';
            fallback.innerHTML = `<p>Coming soon: ${view}</p>`;
            mainContent.appendChild(fallback);
            currentDetailEl = fallback;
        }
    }
});

// Queue panel toggle
const queueButton = document.getElementById('queue-button');
const queuePanel = document.getElementById('queue-panel') as HTMLElement | null;

if (queueButton && queuePanel) {
    queueButton.addEventListener('click', () => {
        const isOpen = queuePanel.hasAttribute('open');

        if (isOpen) {
            queuePanel.removeAttribute('open');
        } else {
            queuePanel.setAttribute('open', '');
        }
    });

    // ---------------------------------------------------------------
    // Queue button as drop target (when queue panel is closed)
    // ---------------------------------------------------------------

    queueButton.addEventListener('dragover', (e: DragEvent) => {
        if (!hasTrackPayload(e)) return;

        e.preventDefault();

        if (e.dataTransfer) {
            e.dataTransfer.dropEffect = 'copy';
        }

        queueButton.classList.add('drag-over');
    });

    queueButton.addEventListener('dragleave', () => {
        queueButton.classList.remove('drag-over');
    });

    queueButton.addEventListener('drop', (e: DragEvent) => {
        e.preventDefault();
        queueButton.classList.remove('drag-over');

        const payload = getDragPayload(e);

        if (!payload || payload.filePaths.length === 0) return;

        if (payload.source === 'queue') return;

        queueStore.addTracksToQueue(payload.filePaths);
    });

    // Show/hide drag-over styling globally.
    document.addEventListener(
        'yj-drag-active',
        ((e: CustomEvent<DragActiveDetail>) => {
            if (!e.detail.active) {
                queueButton.classList.remove('drag-over');
            }
        }) as EventListener,
    );
}

// ---------------------------------------------------------------
// Request current state from the backend
// ---------------------------------------------------------------
// All stores have registered their EventsOn listeners by now
// (module-level singletons are instantiated during import
// evaluation), so the state-push events emitted by these
// binding calls will be received deterministically — no sleep
// or timing assumptions needed.
void Player.EmitCurrentState();
void Queue.EmitCurrentState();
