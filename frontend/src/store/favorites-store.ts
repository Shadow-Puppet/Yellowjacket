import { EventsOn } from '@runtime/runtime';
import {
    GetDefaultPlaylistTrackPaths,
    GetDefaultPlaylistInfo,
    ToggleDefaultPlaylistTrack,
    AddToDefaultPlaylist,
    RemoveFromDefaultPlaylist,
} from '@go/playlist/Service';
import {
    GetFavoritesIconStyle,
    GetFavoritesPlaylistID,
    GetPinDefaultPlaylist,
    SetFavoritesIconStyle,
    SetFavoritesPlaylistID,
    SetPinDefaultPlaylist,
} from '@go/config/Config';
import { Events } from '../events';
import { describeError } from '@utils/describe-error';
import { notificationStore } from './notification-store';

export type IconStyle = 'heart' | 'star';

/**
 * A revert the user can see is not an explanation (errors.m2): the
 * heart fills, and half a second later it empties again. Transient by
 * the plan's rule — the state has already put itself back, so there is
 * nothing to do but say why.
 */
function reportRevert(what: string, err: unknown): void {
    console.error(`favorites: ${what} failed`, err);
    notificationStore.transient({
        key: 'favorites',
        text: `Could not ${what}. ${describeError(err)}`,
        detail: String(err),
        coalescedText: (count) =>
            `Could not ${what} — ${count} changes were undone.`,
    });
}

export interface FavoritesState {
    playlistId: number;
    playlistName: string;
    iconStyle: IconStyle;
    favoritedPaths: Set<string>;
}

type Subscriber = () => void;

class FavoritesStore {
    private playlistId = 0;
    private playlistName = 'Favorites';
    private iconStyle: IconStyle = 'heart';
    private pinDefault = true;
    private favoritedPaths = new Set<string>();
    private subscribers = new Set<Subscriber>();
    private notifyScheduled = false;
    private loading = false;

    constructor() {
        // Load initial state.
        void this.loadConfig();
        void this.loadPaths();

        // React to changes from the backend.
        EventsOn(
            Events.FavoritesConfigChanged,
            (data: {
                PlaylistID: number;
                IconStyle: string;
                PinDefault: boolean;
            }) => {
                this.playlistId = data.PlaylistID;
                this.iconStyle =
                    data.IconStyle as IconStyle;
                this.pinDefault = data.PinDefault;
                this.notify();
                void this.loadPlaylistName();
                void this.loadPaths();
            },
        );

        EventsOn(
            Events.DefaultPlaylistChanged,
            () => {
                void this.loadPaths();
            },
        );

        // When a playlist's tracks change, check if it's
        // our default playlist and reload if so.
        EventsOn(
            Events.PlaylistTracksChanged,
            (playlistId: number) => {
                if (playlistId === this.playlistId) {
                    void this.loadPaths();
                }
            },
        );

        // When a playlist is deleted and recreated,
        // reload everything.
        EventsOn(Events.PlaylistDeleted, () => {
            void this.loadConfig();
            void this.loadPaths();
        });

        EventsOn(Events.PlaylistRenamed, () => {
            void this.loadPlaylistName();
        });

        EventsOn(Events.PlaylistsRestored, () => {
            void this.loadPaths();
        });
    }

    // ===============================================================
    // DATA ACCESS
    // ===============================================================

    isFavorited(filePath: string): boolean {
        return this.favoritedPaths.has(filePath);
    }

    /**
     * Check if all given file paths are in the default
     * playlist.
     */
    allFavorited(filePaths: string[]): boolean {
        if (filePaths.length === 0) return false;

        return filePaths.every((fp) =>
            this.favoritedPaths.has(fp),
        );
    }

    getIconStyle(): IconStyle {
        return this.iconStyle;
    }

    getPlaylistName(): string {
        return this.playlistName;
    }

    getPlaylistId(): number {
        return this.playlistId;
    }

    getPinDefault(): boolean {
        return this.pinDefault;
    }

    isLoading(): boolean {
        return this.loading;
    }

    // ===============================================================
    // ACTIONS
    // ===============================================================

    async toggleFavorite(filePath: string): Promise<void> {
        // Optimistic update.
        const wasIn = this.favoritedPaths.has(filePath);

        if (wasIn) {
            this.favoritedPaths.delete(filePath);
        } else {
            this.favoritedPaths.add(filePath);
        }

        this.notify();

        try {
            await ToggleDefaultPlaylistTrack(filePath);
        } catch (err) {
            // Revert optimistic update.
            if (wasIn) {
                this.favoritedPaths.add(filePath);
            } else {
                this.favoritedPaths.delete(filePath);
            }

            this.notify();
            reportRevert(
                wasIn ? 'remove that favourite' : 'save that favourite',
                err,
            );
        }
    }

    async addToFavorites(
        filePaths: string[],
    ): Promise<void> {
        for (const fp of filePaths) {
            this.favoritedPaths.add(fp);
        }

        this.notify();

        try {
            await AddToDefaultPlaylist(filePaths);
        } catch (err) {
            void this.loadPaths();
            reportRevert('save those favourites', err);
        }
    }

    async removeFromFavorites(
        filePaths: string[],
    ): Promise<void> {
        for (const fp of filePaths) {
            this.favoritedPaths.delete(fp);
        }

        this.notify();

        try {
            await RemoveFromDefaultPlaylist(filePaths);
        } catch (err) {
            void this.loadPaths();
            reportRevert('remove those favourites', err);
        }
    }

    async setIconStyle(
        style: IconStyle,
    ): Promise<void> {
        this.iconStyle = style;
        this.notify();
        await SetFavoritesIconStyle(style);
    }

    async setDefaultPlaylist(
        id: number,
    ): Promise<void> {
        this.playlistId = id;
        this.notify();
        await SetFavoritesPlaylistID(id);
        await this.loadPlaylistName();
        await this.loadPaths();
    }

    async setPinDefault(
        pin: boolean,
    ): Promise<void> {
        this.pinDefault = pin;
        this.notify();
        await SetPinDefaultPlaylist(pin);
    }

    // ===============================================================
    // SUBSCRIPTION SYSTEM
    // ===============================================================

    subscribe(callback: Subscriber): () => void {
        this.subscribers.add(callback);

        return () => this.subscribers.delete(callback);
    }

    private notify(): void {
        if (this.notifyScheduled) return;
        this.notifyScheduled = true;
        queueMicrotask(() => {
            this.notifyScheduled = false;
            for (const cb of this.subscribers) {
                cb();
            }
        });
    }

    // ===============================================================
    // LOADING HELPERS
    // ===============================================================

    private async loadConfig(): Promise<void> {
        try {
            const [id, style, pin] =
                await Promise.all([
                    GetFavoritesPlaylistID(),
                    GetFavoritesIconStyle(),
                    GetPinDefaultPlaylist(),
                ]);

            this.playlistId = id;
            this.iconStyle = style as IconStyle;
            this.pinDefault = pin;
            await this.loadPlaylistName();
            this.notify();
        } catch {
            // Defaults are already set.
        }
    }

    private async loadPlaylistName(): Promise<void> {
        if (this.playlistId === 0) {
            this.playlistName = 'Favorites';
            this.notify();

            return;
        }

        try {
            const info =
                await GetDefaultPlaylistInfo();

            if (info?.Name) {
                this.playlistName = info.Name;
                this.notify();
            }
        } catch {
            // Keep current name.
        }
    }

    private async loadPaths(): Promise<void> {
        this.loading = true;

        try {
            const paths =
                await GetDefaultPlaylistTrackPaths();
            this.favoritedPaths = new Set(paths ?? []);
        } catch {
            // Keep current set.
        } finally {
            this.loading = false;
            this.notify();
        }
    }
}

// Singleton instance.
export const favoritesStore = new FavoritesStore();
