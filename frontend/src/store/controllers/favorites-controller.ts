import type {
    ReactiveController,
    ReactiveControllerHost,
} from 'lit';
import {
    favoritesStore,
} from '../favorites-store';
import type { IconStyle } from '../favorites-store';

/**
 * FavoritesController connects a Lit component to the
 * FavoritesStore.
 *
 * Usage in a component:
 *
 *   private favCtrl = new FavoritesController(this);
 *
 *   render() {
 *     const isFav = this.favCtrl.isFavorited(filePath);
 *   }
 */
export class FavoritesController
    implements ReactiveController
{
    private host: ReactiveControllerHost;
    private unsubscribe?: () => void;

    constructor(host: ReactiveControllerHost) {
        this.host = host;
        host.addController(this);
    }

    // ===============================================================
    // LIFECYCLE HOOKS
    // ===============================================================

    hostConnected(): void {
        this.unsubscribe =
            favoritesStore.subscribe(() => {
                this.host.requestUpdate();
            });
    }

    hostDisconnected(): void {
        this.unsubscribe?.();
    }

    // ===============================================================
    // DATA ACCESS
    // ===============================================================

    isFavorited(filePath: string): boolean {
        return favoritesStore.isFavorited(filePath);
    }

    allFavorited(filePaths: string[]): boolean {
        return favoritesStore.allFavorited(filePaths);
    }

    get iconStyle(): IconStyle {
        return favoritesStore.getIconStyle();
    }

    get playlistName(): string {
        return favoritesStore.getPlaylistName();
    }

    get playlistId(): number {
        return favoritesStore.getPlaylistId();
    }

    get pinDefault(): boolean {
        return favoritesStore.getPinDefault();
    }

    /**
     * Returns the icon name for the current icon style.
     */
    get iconName(): string {
        return this.iconStyle === 'star'
            ? 'star'
            : 'heart';
    }

    // ===============================================================
    // ACTIONS
    // ===============================================================

    async toggleFavorite(
        filePath: string,
    ): Promise<void> {
        await favoritesStore.toggleFavorite(filePath);
    }

    async addToFavorites(
        filePaths: string[],
    ): Promise<void> {
        await favoritesStore.addToFavorites(filePaths);
    }

    async removeFromFavorites(
        filePaths: string[],
    ): Promise<void> {
        await favoritesStore.removeFromFavorites(
            filePaths,
        );
    }

    async setIconStyle(
        style: IconStyle,
    ): Promise<void> {
        await favoritesStore.setIconStyle(style);
    }

    async setDefaultPlaylist(
        id: number,
    ): Promise<void> {
        await favoritesStore.setDefaultPlaylist(id);
    }

    async setPinDefault(
        pin: boolean,
    ): Promise<void> {
        await favoritesStore.setPinDefault(pin);
    }
}
