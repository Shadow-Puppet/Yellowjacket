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
     * The icon name for the current icon style, unfilled.
     *
     * Prefer `iconFor(isFav)` — this getter is the name of the *empty*
     * glyph, which is what every caller that does not know the state
     * should draw.
     */
    get iconName(): string {
        return this.iconFor(false);
    }

    /**
     * The glyph for one track's favourite state.
     *
     * **A filled shape means favourited and an outline means not**, in
     * every list in the app. Nine components rendered `iconName`, which
     * was the *solid* glyph in both states — so "not a favourite" was a
     * filled heart in a duller colour, and the only thing separating
     * the two states was hue. That fails for anyone who cannot see the
     * difference between them, and reads as "everything is a favourite"
     * to everyone else. `track-list` and `album-dropdown` already drew
     * it correctly, from inline SVG paths of their own; this is the
     * same rule for the `<wa-icon>` call sites.
     *
     * The Font Awesome family is part of the name — `regular/heart` is
     * the outline, a bare `heart` is the solid one (`src/icons`).
     */
    iconFor(favorited: boolean): string {
        const shape = this.iconStyle === 'star' ? 'star' : 'heart';

        return favorited ? shape : `regular/${shape}`;
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
