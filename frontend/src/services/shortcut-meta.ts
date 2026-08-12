/**
 * What each shortcut action is called, where it applies, and what it is
 * bound to out of the box.
 *
 * This lived as a private static in `config-page`, which meant Settings
 * was the only place in the app the key bindings were written down —
 * and Settings renders three of the four categories, so the autotag
 * keys were written down nowhere at all. The `?` overlay and the
 * Settings editor now read one table.
 */

export interface ShortcutMeta {
    label: string;
    category: string;
    scope: string;
    defaultKey: string;
}

/** The categories, in the order both surfaces show them. */
export const SHORTCUT_CATEGORIES = [
    'Player',
    'Navigation',
    'App',
    'Autotag',
] as const;

export const SHORTCUT_META: Record<string, ShortcutMeta> = {
        'player.playPause': {
            label: 'Play / Pause',
            category: 'Player',
            scope: 'global',
            defaultKey: 'Space',
        },
        'player.next': {
            label: 'Next Track',
            category: 'Player',
            scope: 'global',
            defaultKey: 'N',
        },
        'player.previous': {
            label: 'Previous Track',
            category: 'Player',
            scope: 'global',
            defaultKey: 'P',
        },
        'player.volumeUp': {
            label: 'Volume Up',
            category: 'Player',
            scope: 'global',
            defaultKey: 'Up',
        },
        'player.volumeDown': {
            label: 'Volume Down',
            category: 'Player',
            scope: 'global',
            defaultKey: 'Down',
        },
        'player.seekForward': {
            label: 'Seek Forward',
            category: 'Player',
            scope: 'global',
            defaultKey: 'Right',
        },
        'player.seekBack': {
            label: 'Seek Back',
            category: 'Player',
            scope: 'global',
            defaultKey: 'Left',
        },
        'player.shuffle': {
            label: 'Toggle Shuffle',
            category: 'Player',
            scope: 'global',
            defaultKey: 'S',
        },
        'player.repeat': {
            label: 'Cycle Repeat',
            category: 'Player',
            scope: 'global',
            defaultKey: 'R',
        },
        'player.mute': {
            label: 'Toggle Mute',
            category: 'Player',
            scope: 'global',
            defaultKey: 'M',
        },
        'nav.search': {
            label: 'Focus Search',
            category: 'Navigation',
            scope: 'global',
            defaultKey: '/',
        },
        'nav.searchAlt': {
            label: 'Focus Search (Alt)',
            category: 'Navigation',
            scope: 'global',
            defaultKey: 'Ctrl+F',
        },
        'nav.queue': {
            label: 'Toggle Queue',
            category: 'Navigation',
            scope: 'global',
            defaultKey: 'Q',
        },
        'app.shortcuts': {
            label: 'Keyboard Shortcuts',
            category: 'App',
            scope: 'global',
            defaultKey: '?',
        },
        'app.selectAll': {
            label: 'Select All',
            category: 'App',
            scope: 'global',
            defaultKey: 'Ctrl+A',
        },
        'tracklist.play': {
            label: 'Play Selected',
            category: 'Navigation',
            scope: 'panel:track-list',
            defaultKey: 'Enter',
        },
        'autotag.apply': {
            label: 'Apply Match',
            category: 'Autotag',
            scope: 'panel:autotag',
            defaultKey: 'A',
        },
        'autotag.skip': {
            label: 'Skip Folder',
            category: 'Autotag',
            scope: 'panel:autotag',
            defaultKey: 'S',
        },
        'autotag.leave': {
            label: 'Leave As Is',
            category: 'Autotag',
            scope: 'panel:autotag',
            defaultKey: 'L',
        },
        'autotag.paste': {
            label: 'Paste Release URL',
            category: 'Autotag',
            scope: 'panel:autotag',
            defaultKey: 'U',
        },
        'autotag.search': {
            label: 'Search Candidates',
            category: 'Autotag',
            scope: 'panel:autotag',
            defaultKey: 'F',
        },
        'autotag.next': {
            label: 'Next Folder',
            category: 'Autotag',
            scope: 'panel:autotag',
            defaultKey: 'Down',
        },
        'autotag.previous': {
            label: 'Previous Folder',
            category: 'Autotag',
            scope: 'panel:autotag',
            defaultKey: 'Up',
        },
    };
