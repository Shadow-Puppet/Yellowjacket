// ---------------------------------------------------------------------------
// What is imported here is what is parsed and evaluated before first
// paint.  Everything else is a chunk fetched on the navigation that
// needs it (VIEW_LOADERS / DETAIL_LOADERS below) and warmed in the
// background once the app is idle, so only the *first* second of the
// session pays for the views the user has not asked for.
//
// Three things stay eager that look like candidates:
//
//   notification-host, inline-notice, confirm-dialog — the app's only
//   failure surface.  A message that has to fetch a chunk before it can
//   be shown is not a failure surface; the moment it is most needed is
//   exactly the moment loading one may not work.
//
//   first-run-wizard — it covers the app until a library exists, so on
//   the one launch it matters it is on the critical path anyway.
//
//   track-list — index.html renders one, so it is the first paint.
// ---------------------------------------------------------------------------
import '@components/audio-player/audio-player.ts';
// In the bar rather than inside `audio-player` since #42, so the shell
// is what has to register it.
import '@components/audio-player/volume-control/volume-control.ts';
import '@components/track-list/track-list.ts';
import '@components/now-playing/now-playing.ts';
import '@components/sidebar/app-sidebar.ts';
import '@components/bottom-nav/bottom-nav.ts';
import '@components/queue-panel/queue-panel.ts';
import '@components/nav-history/nav-history.ts';
import '@components/search-bar/search-bar.ts';
import '@components/library-filter/library-filter.ts';
import '@components/first-run-wizard/first-run-wizard.ts';
import '@components/notifications/notification-host.ts';
import '@components/notifications/inline-notice.ts';
import '@components/confirm-dialog/confirm-dialog.ts';
// The `?` overlay: help, so it is eager for the same reason the failure
// surface is — the moment it is asked for is the moment the user does
// not know what is going on. It costs a dialog and a table.
import '@components/shortcuts-overlay/shortcuts-overlay.ts';
import '@components/jobs/job-indicator.ts';
// The phone's half of the same thing (#62). Eager because it is part
// of the shell's first paint below 600px, and because a band that has
// to fetch a chunk before it can say the app is busy is late by
// exactly the interval it exists to explain.
import '@components/jobs/job-band.ts';
import '@awesome.me/webawesome/dist/styles/themes/default.css';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { setBasePath } from '@awesome.me/webawesome/dist/webawesome.js';
import { registerBundledIcons } from './src/icons';
import { queueStore } from '@store/queue-store';
import { searchStore } from '@store/search-store';
import { activeViewStore } from '@store/active-view-store';
import { historyStore } from '@store/history-store';
import * as Player from '@go/player/player.js';
import * as Queue from '@go/queue/queue.js';
import { GetDefaultPage } from '@go/config/config.js';
// Importing the theme store triggers initialization: it fetches the saved
// theme from the backend and applies CSS custom properties to :root.
import '@store/theme-store';
// Importing the keyboard shortcut service triggers initialization:
// registers the document keydown listener for global shortcuts.
import './src/services/keyboard-shortcut-service';
import { activateView, deactivateView } from '@utils/view-lifecycle';
import { installLongPressContextMenu } from '@utils/long-press';
import { installTopBarFit } from './src/services/top-bar-fit';
import {
    hasTrackPayload,
    getDragPayload,
} from '@utils/drag-controller';
import type { DragActiveDetail } from '@utils/drag-controller';

setBasePath('/dist/webawesome');

// Before any component renders: an icon resolved by the default
// (remote) library is a request to fontawesome.com, and the module
// caches by URL, so one early render would pin the remote answer for
// the session.
registerBundledIcons();

// The touch equivalent of a right-click, installed once for every menu
// in the app rather than per component. Harmless on a desktop: it acts
// on `pointerType === 'touch'` only.
installLongPressContextMenu();

// The top bar decides what it can afford to show (#143). Here rather
// than in a component because the bar is light DOM in index.html and
// its children are five separate elements; the shell is the only thing
// that can see all five at once.
const topBar = document.querySelector<HTMLElement>('header.top-bar');

if (topBar) installTopBarFit(topBar);

// ---------------------------------------------------------------------------
// View caching navigation system
// ---------------------------------------------------------------------------
// Primary views (tracks, albums, artists, genres, playlists, settings) are
// created once and kept alive in the DOM.  Navigation toggles visibility
// (display: none ↔ display: '') instead of destroying/recreating via
// innerHTML.  Detail views (artist-details, playlist-details, genre-details)
// are ephemeral — created fresh each navigation because they depend on
// specific entity IDs that change.
//
// Because a cached view is never disconnected, `disconnectedCallback` is
// not where it stops listening.  Navigation calls viewDeactivated() on
// the outgoing element and viewActivated() on the incoming one; views
// hang their document listeners, timers and subscriptions off that pair
// (see utils/view-lifecycle.ts).  Skipping either call leaves a view
// listening from a page it is not on, which is finding H-1.
// ---------------------------------------------------------------------------

const VIEW_TAGS: Record<string, string> = {
    home: 'home-view',
    tracks: 'track-list',
    albums: 'cover-grid',
    artists: 'artists-view',
    genres: 'genres-view',
    playlists: 'playlist-view',
    explore: 'explore-view',
    autotag: 'autotag-view',
    downloads: 'downloads-view',
    settings: 'config-page',
};

// The module that defines each view's custom element.  `createElement`
// on an undefined tag silently produces an inert HTMLElement rather
// than throwing, so a navigation has to await its loader before it
// builds anything — a missing entry here is a blank page, not an error.
const VIEW_LOADERS: Record<string, () => Promise<unknown>> = {
    home: () => import('@components/home-view/home-view.ts'),
    tracks: () => Promise.resolve(),
    albums: () => import('@components/cover-grid/cover-grid.ts'),
    artists: () => import('@components/artists-view/artists-view.ts'),
    genres: () => import('@components/genres-view/genres-view.ts'),
    playlists: () => import('@components/playlist-view/playlist-view.ts'),
    explore: () => import('@components/explore-view/explore-view.ts'),
    autotag: () => import('@components/autotag-view/autotag-view.ts'),
    downloads: () => import('@components/downloads-view/downloads-view.ts'),
    settings: () => import('@components/config-page/config-page.ts'),
};

const DETAIL_LOADERS: Record<string, () => Promise<unknown>> = {
    'artist-details': () =>
        import('@components/artist-details/artist-details.ts'),
    'playlist-details': () =>
        import('@components/playlist-details/playlist-details.ts'),
    'smart-playlist-details': () =>
        import('@components/smart-playlist-details/smart-playlist-details.ts'),
    'genre-details': () =>
        import('@components/genre-details/genre-details.ts'),
    'explore-artist-details': () =>
        import('@components/explore-artist-details/explore-artist-details.js'),
    'explore-album-details': () =>
        import('@components/explore-album-details/explore-album-details.js'),
    // A detail view rather than a primary one on purpose: it is
    // somewhere you go and come back from, so the nav stack carries
    // the way out (016 B2 phase 2).
    'now-playing': () =>
        import('@components/now-playing-view/now-playing-view.ts'),
};

// Opened from a menu rather than by navigating, so they have no entry
// above; warmed with everything else below.
//
// `track-details` is loaded at the point of use by
// `utils/lazy-track-details.ts`, from the five components that open it
// (`track-list`, `cover-grid`, `queue-panel` and both playlist detail
// views). It used to be imported statically by all five, so its 42 kB
// rode in the startup chunk whatever this file said. Warming it here
// means the first open still does not wait for it.
const EXTRA_LOADERS: Array<() => Promise<unknown>> = [
    () => import('@components/smart-playlist-editor/smart-playlist-editor.ts'),
    // Same specifier as `utils/lazy-track-details.ts` uses, so this is
    // the same chunk rather than a second copy of it.
    () => import('@components/track-details/track-details.js'),
];

const viewCache = new Map<string, HTMLElement>();
let currentViewEl: HTMLElement | null = null;
let currentDetailEl: HTMLElement | null = null;


const mainContent = document.getElementById('main-content');

// Seed the cache with the default track-list rendered in index.html —
// otherwise the very first navigation (to whatever GetDefaultPage
// resolves to) creates and shows a second view while this one, never
// tracked as currentViewEl, is never hidden: two visible primary views
// splitting the main panel between them regardless of which is
// selected.
if (mainContent) {
    const initialTrackList = mainContent.querySelector('track-list');

    if (initialTrackList) {
        viewCache.set('tracks', initialTrackList as HTMLElement);
        currentViewEl = initialTrackList as HTMLElement;
        activateView(currentViewEl);
    }
}

/**
 * Navigations are numbered, because loading a view's chunk is
 * asynchronous and a user can click twice. Anything after the `await`
 * checks that it is still the newest navigation before touching the
 * DOM; otherwise a slow chunk would land on top of a faster one and
 * show the page the user navigated *away* from.
 */
let navSeq = 0;

document.addEventListener('navigate', (e: Event) => {
    void handleNavigate((e as CustomEvent).detail);
});

// ---------------------------------------------------------------------------
// The platform's back gesture
// ---------------------------------------------------------------------------
// Android's back button is not a keystroke the page can bind: the
// scaffold's `MainActivity.onBackPressed` asks `webView.canGoBack()` and
// otherwise finishes the activity.  This app never touched `history`, so
// that was always false and back quit the app from any depth -- reported
// from a device as "back does not navigate back".
//
// So a navigation is a history entry, and back is `popstate`.  It hooks
// the platform's own mechanism rather than a JNI callback of our own,
// which is the same reason `events.ts` hooks the runtime's transport:
// the Java half needs no change, and the behaviour is testable in a
// browser (`page.goBack()`) instead of only on a phone.
//
// Two rules keep the two stacks from disagreeing.  A navigation that
// *came from* history pushes nothing (`_isBack`), or going back would
// deepen the stack it is unwinding.  And the in-app back buttons --
// `navigate-back`, which the detail views and `now-playing-view` fire --
// go through `history.back()` rather than popping `navStack`
// themselves, so one press cannot consume two entries.

/** The navigation an entry stands for, and where it sits in this
 *  session's list. `undefined` on the entry that predates the app's own
 *  routing, which is the one back exits from. */
type NavState = { yjNav?: { view: string; [key: string]: any }; yjIdx?: number };

/** Whether the app's first navigation has been recorded. It *replaces*
 *  the launch entry rather than pushing, or every launch would cost one
 *  back press before the app would exit. */
let historyStarted = false;

// Back and forward are the *same* `popstate` event -- it carries no
// direction, and the History API exposes neither the current position
// nor a reachable depth. So the shell numbers its own entries: the
// index of the one showing, and the highest index reachable from here.
//
// The counter this replaced (`pushedEntries`, one number decremented on
// every pop) could not express forward at all: going forward looked
// exactly like going back again, so two presses of a Forward button
// would have claimed the app was at its root.

/** Index of the entry now showing. 0 is the launch entry, which is
 *  replaced rather than pushed -- so this is also how deep back can go
 *  while staying inside the app. */
let currentIndex = 0;

/** The highest index reachable from here: how far forward is left.
 *  A new navigation truncates the forward list, exactly as a browser
 *  does, so this is reset to the entry being pushed. */
let maxIndex = 0;

function publishDepth(): void {
    historyStore.setDepth(currentIndex > 0, currentIndex < maxIndex);
}

function recordNavigation(detail: { view: string; [key: string]: any }): void {
    // `_isBack` and `_replace` are bookkeeping, not destination: keeping
    // either in the entry would make a replayed navigation claim to be
    // one.
    const { _isBack: _ignored, _replace: replace, ...nav } = detail;

    // Still launching: the configured landing page is not a navigation
    // *away* from the eager one, it is the same arrival arriving late
    // (#142). Pushing it left the app one entry deep before the user
    // had touched anything, so the first back press replayed home over
    // home -- invisible on desktop until #6 drew a Back button, and on
    // Android the press that should have exited the app instead did
    // nothing, because `canGoBack()` was true.
    //
    // Guarded on being at the root rather than on a flag, because
    // `GetDefaultPage()` is a backend call and the user can navigate
    // while it is in flight: past index 0 this is an ordinary
    // navigation, or a slow answer would overwrite an entry they made.
    if (historyStarted && replace && currentIndex === 0) {
        history.replaceState({ yjNav: nav, yjIdx: 0 }, '');
        maxIndex = 0;
        publishDepth();

        return;
    }

    // Same URL, deliberately: the app has no routes, and a path a
    // reload cannot resolve is worse than no path at all.
    if (historyStarted) {
        currentIndex += 1;
        // Navigating from the middle of the list drops what was ahead
        // of it -- there is no longer a forward to go to.
        maxIndex = currentIndex;
        history.pushState({ yjNav: nav, yjIdx: currentIndex }, '');
    } else {
        currentIndex = 0;
        maxIndex = 0;
        history.replaceState({ yjNav: nav, yjIdx: 0 }, '');
        historyStarted = true;
    }

    publishDepth();
}

window.addEventListener('popstate', (e: PopStateEvent) => {
    const state = e.state as NavState | null;
    const nav = state?.yjNav;

    // Before the app's first navigation, or an entry somebody else
    // pushed: nothing to restore, and the activity should be free to
    // finish.
    if (!nav) return;

    // The entry says where it is, so this works in both directions and
    // across a jump of more than one -- which a long-press on a
    // browser's back button, and `history.go(-n)`, both produce.
    // The fallback is for an entry pushed before this numbering
    // existed; it can only be wrong about a control's disabled state,
    // never about which view is restored.
    currentIndex = state?.yjIdx ?? Math.max(0, currentIndex - 1);
    publishDepth();

    void handleNavigate({ ...nav, _isBack: true });
});

async function handleNavigate(
    detail: { view: string; [key: string]: any },
): Promise<void> {
    const view: string = detail.view;

    if (!mainContent) return;

    const seq = ++navSeq;

    if (!detail._isBack) recordNavigation(detail);

    // Bookkeeping stays synchronous with the click: the search box's
    // scope and the active-view attribute describe the navigation that
    // was *asked for*, and are what the rest of the app and the e2e
    // selectors read.
    searchStore.setCurrentView(view);

    // Which view is showing is otherwise only inferable from which of
    // the cached children lacks .view-hidden.  Publishing it as an
    // attribute keeps e2e selectors semantic instead of structural.
    mainContent.dataset.activeView = view;

    // And publishing it as a *value* is what the nav components read.
    // They used to learn the active view from the `navigate` event,
    // which only the outbound path dispatches -- so a back-navigation
    // left both of them highlighting the view it had just left (#72).
    // Re-dispatching `navigate` here is not the fix: this file is a
    // document listener for it, so that is an infinite loop, and
    // "please go to X" is not the statement being made.
    //
    // `view in VIEW_TAGS` is the primary/detail split, and it is passed
    // rather than re-derived because this table is where it is written
    // down.  A detail view therefore leaves the tab it was opened from
    // lit, which is what the report asks for.
    activeViewStore.setView(view, view in VIEW_TAGS);

    // --- Primary (cacheable) views ----------------------------------------
    if (view in VIEW_TAGS) {
        // Remove any active detail view first
        if (currentDetailEl) {
            deactivateView(currentDetailEl);
            currentDetailEl.remove();
            currentDetailEl = null;
        }

        let target = viewCache.get(view);

        if (!target) {
            await (VIEW_LOADERS[view]?.() ?? Promise.resolve());
            if (seq !== navSeq) return;

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
            deactivateView(currentViewEl);
        }
        target.classList.remove('view-hidden');

        // A primary view is cached, so there is no construction to
        // hand a payload to the way a detail view gets one below.  The
        // one navigation that carries something is the album page's
        // "Review in Autotag", which has to land on *that* album: the
        // request goes on as an attribute and `autotag-view` consumes
        // it (removes it) once acted on, or every later visit would
        // reopen a folder the user finished with long ago.
        if (view === 'autotag' && typeof detail.groupKey === 'string') {
            target.setAttribute('group-key', detail.groupKey);
        }

        // A freshly created view was appended hidden, so it did not
        // self-activate on connection; a cached one was deactivated on
        // the way out.  Either way this is the call that starts it.
        activateView(target);
        currentViewEl = target;

        return;
    }

    await (DETAIL_LOADERS[view]?.() ?? Promise.resolve());
    if (seq !== navSeq) return;

    // --- Detail (ephemeral) views -----------------------------------------
    // Hide the current primary view
    if (currentViewEl) {
        currentViewEl.classList.add('view-hidden');
        deactivateView(currentViewEl);
    }
    // Remove any prior detail element
    if (currentDetailEl) {
        deactivateView(currentDetailEl);
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
        case 'now-playing': {
            const npEl = document.createElement('now-playing-view');

            mainContent.appendChild(npEl);
            currentDetailEl = npEl;
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
            const { artistMBID, artistName, localArtistId } = detail;
            const el = document.createElement('explore-artist-details');

            if (artistMBID) el.setAttribute('artist-mbid', artistMBID);
            el.setAttribute('artist-name', artistName);
            if (localArtistId) el.setAttribute('local-artist-id', String(localArtistId));
            mainContent.appendChild(el);
            currentDetailEl = el;
            break;
        }
        case 'explore-album-details': {
            const {
                releaseGroupMBID,
                albumName,
                artistName,
                highlightTrackMBID,
                highlightTrackTitle,
                localAlbumId,
            } = detail;
            const el = document.createElement('explore-album-details');

            if (releaseGroupMBID) el.setAttribute('release-group-mbid', releaseGroupMBID);
            el.setAttribute('album-name', albumName);
            if (artistName) el.setAttribute('artist-name', artistName);
            if (highlightTrackMBID) {
                el.setAttribute('highlight-track-mbid', highlightTrackMBID);
            }
            if (highlightTrackTitle) {
                el.setAttribute('highlight-track-title', highlightTrackTitle);
            }
            if (localAlbumId) el.setAttribute('local-album-id', String(localAlbumId));
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
}

// Every view the user has not opened yet, fetched once the app has
// settled. Splitting keeps them off the path to first paint; warming
// them means the navigation that needs one almost never waits, which is
// the cost a naive split would have traded the startup win for.
function warmViewChunks(): void {
    const loaders = [
        ...Object.values(VIEW_LOADERS),
        ...Object.values(DETAIL_LOADERS),
        ...EXTRA_LOADERS,
    ];

    const warmNext = (i: number): void => {
        if (i >= loaders.length) return;

        void loaders[i]!()
            .catch(() => {
                // A chunk that will not preload is not a failure: the
                // navigation that needs it will ask again and report
                // for itself if it still cannot be had.
            })
            .finally(() => {
                schedule(() => warmNext(i + 1));
            });
    };

    schedule(() => warmNext(0));
}

/** requestIdleCallback where it exists; WebKit2GTK does not have it. */
function schedule(fn: () => void): void {
    const ric = (
        window as unknown as {
            requestIdleCallback?: (cb: () => void) => number;
        }
    ).requestIdleCallback;

    if (ric) {
        ric(fn);

        return;
    }

    setTimeout(fn, 200);
}

// Navigate-back: the in-app back buttons, which are the same press as
// the phone's.  It goes through the history rather than a stack of its
// own, so one press is one entry however it arrived -- two stacks is
// how a detail view's own button and the back gesture come to disagree.
//
// At the root there is nothing of ours to go back to, and going back
// anyway would leave the app: the depth check is what stops a stray
// `navigate-back` closing it.
document.addEventListener('navigate-back', () => {
    if (currentIndex > 0) history.back();
});

// Forward: the other half of #6. The stack was always global -- every
// navigation is an entry and `popstate` restores any of them -- so what
// was missing is a way to ask for one, and a truthful answer to whether
// there is one to ask for. It is guarded for the same reason back is:
// `history.forward()` at the end of the list is silent, so a button
// that offers it when there is nothing there is a button that does
// nothing.
document.addEventListener('navigate-forward', () => {
    if (currentIndex < maxIndex) history.forward();
});

// Navigate to the user's configured launch page.  Falls back to 'home'
// if the backend call fails, matching the config's own default.
GetDefaultPage()
    .then((view) => {
        document.dispatchEvent(new CustomEvent('navigate', {
            bubbles: true,
            composed: true,
            // Part of launching, not a navigation away from the eager
            // 'home' above: it replaces that entry rather than
            // stacking on it (#142).
            detail: { view: view || 'home', _replace: true },
        }));
    })
    .catch(() => {
        document.dispatchEvent(new CustomEvent('navigate', {
            bubbles: true,
            composed: true,
            detail: { view: 'home', _replace: true },
        }));
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

    // The button says whether the panel is open, and it learns that
    // from the panel rather than from its own click handler.
    //
    // It is not the only thing that opens the queue -- `now-playing-view`
    // sets the same attribute, because it hides the bar this button
    // lives in -- so a state kept beside the click would be right until
    // something else opened the panel and then quietly wrong. The panel's
    // `open` attribute is the one fact; this reflects it.
    const reflectQueueState = () => {
        queueButton.setAttribute(
            'aria-expanded',
            String(queuePanel.hasAttribute('open')),
        );
    };

    new MutationObserver(reflectQueueState).observe(queuePanel, {
        attributes: true,
        attributeFilter: ['open'],
    });

    reflectQueueState();

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

// ---------------------------------------------------------------
// Land on Home
// ---------------------------------------------------------------
// The app opened on Tracks — an alphabetical list of everything, which
// is the one entry point that is identical every time and therefore
// gives the user nothing to start from. Home is listed first in the nav
// and is the page built to answer "what should I play", and it was
// never what anybody saw (H-8).
//
// index.html still renders the track list eagerly and it is still what
// paints first: it is the cached 'tracks' view, so this navigation is a
// class toggle plus one chunk, not a second render of the shell. Doing
// it here rather than by changing the markup keeps the first paint
// exactly as Phase 4 left it.
document.dispatchEvent(
    new CustomEvent('navigate', {
        detail: { view: 'home' },
    }),
);

warmViewChunks();
