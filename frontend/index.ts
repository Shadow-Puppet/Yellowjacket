import '@components/audio-player/audio-player.ts';
import '@components/track-list/track-list.ts';
import '@components/cover-grid/cover-grid.ts';
import '@components/now-playing/now-playing.ts';
import '@components/sidebar/app-sidebar.ts';
import '@components/queue-panel/queue-panel.ts';
import '@awesome.me/webawesome/dist/styles/themes/default.css';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { setBasePath } from '@awesome.me/webawesome/dist/webawesome.js';

setBasePath('/dist/webawesome');

// Scroll state management per view
const scrollPositions = new Map<string, number>();
let currentView = 'tracks';

// Navigation event listener for view switching
document.addEventListener('navigate', (e: Event) => {
    const { view } = (e as CustomEvent).detail;
    const mainContent = document.getElementById('main-content');

    if (!mainContent) return;

    // Save scroll position for current view before switching
    scrollPositions.set(currentView, mainContent.scrollTop);

    switch (view) {
        case 'albums':
            mainContent.innerHTML = '<cover-grid></cover-grid>';
            break;
        case 'tracks':
            mainContent.innerHTML = '<track-list></track-list>';
            break;
        default:
            mainContent.innerHTML = `<div style="padding: 1em; color: #b3b3b3;">
                <p>Coming soon: ${view}</p>
            </div>`;
    }

    // Restore scroll position for new view
    mainContent.scrollTop = scrollPositions.get(view) ?? 0;

    // Update current view tracker
    currentView = view;
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

    // Close panel when the component dispatches a close event
    queuePanel.addEventListener('queue-panel-close', () => {
        queuePanel.removeAttribute('open');
    });
}
