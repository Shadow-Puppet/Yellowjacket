import '@components/audio-player/audio-player.ts';
import '@components/track-list/track-list.ts';
import '@components/cover-grid/cover-grid.ts';
import '@components/now-playing/now-playing.ts';
import '@components/sidebar/app-sidebar.ts';
import '@components/queue-panel/queue-panel.ts';
import '@components/playlist-view/playlist-view.ts';
import '@components/library-manager/library-manager.ts';
import '@awesome.me/webawesome/dist/styles/themes/default.css';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { setBasePath } from '@awesome.me/webawesome/dist/webawesome.js';
import { libraryStore } from '@store/library-store';
import { playlistStore } from '@store/playlist-store';
import { queueStore } from '@store/queue-store';
import {
    hasTrackPayload,
    getDragPayload,
} from '@utils/drag-controller';
import type { DragActiveDetail } from '@utils/drag-controller';

setBasePath('/dist/webawesome');

// Pre-fetch data for views not yet mounted so they're cached when navigated to.
// These are fire-and-forget — the singleton stores deduplicate concurrent fetches,
// so if a component mounts before this completes, it joins the in-flight request.
libraryStore.getAlbums();
playlistStore.getPlaylists();

// Navigation event listener for view switching
document.addEventListener('navigate', (e: Event) => {
    const { view } = (e as CustomEvent).detail;
    const mainContent = document.getElementById('main-content');

    if (!mainContent) return;

    switch (view) {
        case 'albums':
            mainContent.innerHTML = '<cover-grid></cover-grid>';
            break;
        case 'tracks':
            mainContent.innerHTML = '<track-list></track-list>';
            break;
        case 'playlists':
            mainContent.innerHTML = '<playlist-view></playlist-view>';
            break;
        case 'libraries':
            mainContent.innerHTML = '<library-manager></library-manager>';
            break;
        default:
            mainContent.innerHTML = `<div style="padding: 1em; color: #b3b3b3;">
                <p>Coming soon: ${view}</p>
            </div>`;
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
