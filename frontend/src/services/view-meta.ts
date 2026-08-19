import {
    ICON_PLAYLIST,
    ICON_AUTOTAG,
    ICON_REQUESTED,
} from '@utils/icon-language';

/** A primary destination. Mirrors `backend/config.View`. */
export type View =
    | 'home'
    | 'playlists'
    | 'artists'
    | 'genres'
    | 'albums'
    | 'tracks'
    | 'explore'
    | 'downloads'
    | 'autotag'
    | 'jobs'
    | 'settings';

export interface ViewMeta {
    id: View;
    label: string;
    icon: string;
    /**
     * Views that are never offered as a toggle. Settings alone, because
     * a user who hides it cannot get back to unhide it.
     *
     * This is the *affordance*; the rule is `backend/config.ViewSpec`'s
     * `Hideable`, which refuses at the setter and drops the key on load.
     * `config.toml` is hand-editable, so the checkbox being absent is
     * not what makes this safe — it is only what stops the question
     * being asked.
     */
    alwaysShown?: boolean;
}

/**
 * The app's primary destinations, in the order the navigation draws
 * them and Settings lists them.
 *
 * It is here rather than inside `app-sidebar` because #25 gave it a
 * second reader: Settings renders a toggle per view and needs the same
 * labels in the same order. Same shape as `services/shortcut-meta.ts`,
 * which moved out of `config-page` for the same reason -- a private
 * static that two surfaces need is a private static that is about to be
 * copied.
 *
 * The labels and icons deliberately do not exist in Go. Which views
 * exist and what an unconfigured install shows is `backend/config.Views`
 * and is asked for over the binding; how they are *drawn* is the
 * frontend's, and lives beside the rest of the icon vocabulary.
 */
export const VIEW_META: ViewMeta[] = [
    { id: 'home', label: 'Home', icon: 'house' },
    { id: 'playlists', label: 'Playlists', icon: ICON_PLAYLIST },
    { id: 'artists', label: 'Artists', icon: 'user-group' },
    { id: 'genres', label: 'Genres', icon: 'masks-theater' },
    { id: 'albums', label: 'Albums', icon: 'compact-disc' },
    { id: 'tracks', label: 'Tracks', icon: 'music' },
    { id: 'explore', label: 'Explore', icon: 'globe' },
    { id: 'downloads', label: 'Downloads', icon: ICON_REQUESTED },
    { id: 'autotag', label: 'Autotag', icon: ICON_AUTOTAG },
    { id: 'jobs', label: 'Jobs', icon: 'list-check' },
    { id: 'settings', label: 'Settings', icon: 'gear', alwaysShown: true },
];
