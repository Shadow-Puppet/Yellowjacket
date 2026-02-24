import { GetAlbumTracks } from '@go/library/Library';
import type { library } from '@go/models';
import type { CoverArtUrls } from '@components/track-details/track-details.js';

/**
 * Manages album and track selection, file-path resolution,
 * and the drag-cache for the cover grid.
 *
 * This is a plain helper class (not a ReactiveController)
 * because selection state is owned by the component's
 * `@state()` properties — the manager only computes
 * derived data (file paths, ranges, cache entries).
 */
export class AlbumSelectionManager {
    /**
     * Map from album ID to Album for O(1) lookups.
     * Rebuilt via `setAlbums()` when the album list changes.
     */
    private albumById = new Map<number, library.Album>();

    /**
     * Pre-resolved file paths for selected albums, keyed by album ID.
     * Populated asynchronously when albums are selected so that
     * dragstart can read them synchronously.
     */
    private albumFilePathCache = new Map<
        number,
        string[]
    >();

    /**
     * Update the album-by-ID index.  Call this whenever
     * the full album list changes (initial load, library
     * rescan, external album prop change).
     *
     * Also clears the file-path cache since album IDs may
     * have shifted after a rescan.
     */
    setAlbums(albums: library.Album[]): void {
        this.albumById = new Map(
            albums.map((a) => [a.ID, a]),
        );
        this.albumFilePathCache.clear();
    }

    // ================================================================
    // Album selection helpers
    // ================================================================

    /**
     * Return the set of album IDs in the range
     * [from, to] (inclusive, order-independent)
     * within the filtered album list.
     */
    selectAlbumRange(
        from: number,
        to: number,
        filteredAlbums: library.Album[],
    ): Set<number> {
        const start = Math.min(from, to);
        const end = Math.max(from, to);
        const ids = new Set<number>();

        for (let i = start; i <= end; i++) {
            const album = filteredAlbums[i];

            if (album) {
                ids.add(album.ID);
            }
        }

        return ids;
    }

    /**
     * Fetch file paths for all albums in the given
     * selection set.  Uses the albumById index for
     * O(1) lookups instead of filtering the full list.
     */
    async getSelectedAlbumFilePaths(
        selectedAlbums: Set<number>,
    ): Promise<string[]> {
        const allPaths: string[] = [];

        for (const id of selectedAlbums) {
            const album = this.albumById.get(id);

            if (!album) continue;

            const paths =
                await this.getAlbumFilePaths(album);
            allPaths.push(...paths);
        }

        return allPaths;
    }

    /**
     * Return file paths for the context menu target.
     * If the right-clicked album is part of the current
     * selection, return paths for all selected albums.
     * Otherwise return paths for the right-clicked
     * album only.
     */
    async getContextMenuAlbumFilePaths(
        contextMenuAlbumId: number | null,
        selectedAlbums: Set<number>,
    ): Promise<string[]> {
        if (
            contextMenuAlbumId !== null &&
            !selectedAlbums.has(contextMenuAlbumId)
        ) {
            const album = this.albumById.get(
                contextMenuAlbumId,
            );

            if (album) {
                return this.getAlbumFilePaths(album);
            }

            return [];
        }

        return this.getSelectedAlbumFilePaths(
            selectedAlbums,
        );
    }

    /**
     * Fetch file paths for a single album by loading
     * its tracks from the backend.
     */
    async getAlbumFilePaths(
        album: library.Album,
    ): Promise<string[]> {
        try {
            const tracks = await GetAlbumTracks(
                album.ID,
            );

            return tracks.map((t) => t.FilePath);
        } catch (error) {
            console.error(
                'Error loading album tracks:',
                error,
            );

            return [];
        }
    }

    // ================================================================
    // Drag file-path cache
    // ================================================================

    /**
     * Pre-resolve file paths for all selected albums so
     * that dragstart can read them synchronously.  Called
     * fire-and-forget whenever the album selection changes.
     *
     * After warming, prunes entries whose album ID is no
     * longer in the selection to prevent unbounded growth.
     */
    async warmCache(
        selectedAlbums: Set<number>,
    ): Promise<void> {
        for (const id of selectedAlbums) {
            if (this.albumFilePathCache.has(id)) {
                continue;
            }

            const album = this.albumById.get(id);

            if (!album) continue;

            try {
                const tracks = await GetAlbumTracks(
                    album.ID,
                );

                // Only store if still selected.
                if (selectedAlbums.has(album.ID)) {
                    this.albumFilePathCache.set(
                        album.ID,
                        tracks.map((t) => t.FilePath),
                    );
                }
            } catch {
                // Silently skip — drag will just not
                // include this album's paths.
            }
        }

        // Prune stale entries (6h).
        for (const id of this.albumFilePathCache.keys()) {
            if (!selectedAlbums.has(id)) {
                this.albumFilePathCache.delete(id);
            }
        }
    }

    /**
     * Read cached file paths for the current album
     * selection.  Returns concatenated paths (may be
     * incomplete if some albums haven't been cached yet).
     */
    getCachedSelectedPaths(
        selectedAlbums: Set<number>,
    ): string[] {
        const result: string[] = [];

        for (const id of selectedAlbums) {
            const paths =
                this.albumFilePathCache.get(id);

            if (paths) {
                result.push(...paths);
            }
        }

        return result;
    }

    /**
     * Check whether a single album's paths are in the
     * cache, and return them if so.
     */
    getCachedAlbumPaths(
        albumId: number,
    ): string[] | undefined {
        return this.albumFilePathCache.get(albumId);
    }

    /**
     * Warm a single album's cache entry (used by
     * pointerdown before a potential dragstart).
     */
    async warmSingleAlbum(
        album: library.Album,
    ): Promise<void> {
        if (this.albumFilePathCache.has(album.ID)) {
            return;
        }

        const paths = await this.getAlbumFilePaths(
            album,
        );

        if (paths.length > 0) {
            this.albumFilePathCache.set(
                album.ID,
                paths,
            );
        }
    }

    // ================================================================
    // Track selection helpers
    // ================================================================

    /**
     * Return the set of track file paths in the range
     * [from, to] (inclusive, order-independent).
     */
    selectTrackRange(
        from: number,
        to: number,
        expandedTracks: library.Track[],
    ): Set<string> {
        const start = Math.min(from, to);
        const end = Math.max(from, to);
        const paths = new Set<string>();

        for (let i = start; i <= end; i++) {
            const track = expandedTracks[i];

            if (track) {
                paths.add(track.FilePath);
            }
        }

        return paths;
    }

    /**
     * Return selected track file paths in their
     * original track order.
     */
    getSelectedTrackFilePaths(
        selectedTracks: Set<string>,
        expandedTracks: library.Track[],
    ): string[] {
        return expandedTracks
            .filter((t) =>
                selectedTracks.has(t.FilePath),
            )
            .map((t) => t.FilePath);
    }

    // ================================================================
    // Cover art resolution
    // ================================================================

    /**
     * Resolve cover art URLs for a track's album.
     * Uses the albumById index with the expanded album ID
     * for an O(1) lookup instead of a name-based O(n) scan.
     *
     * Falls back to name-based search if the expanded album
     * doesn't match (defensive).
     */
    resolveTrackCoverArt(
        albumName: string,
        expandedAlbumId: number | null,
    ): CoverArtUrls | null {
        if (!albumName) return null;

        // Prefer the expanded album (we know the track
        // belongs to it) for an O(1) lookup.
        if (expandedAlbumId !== null) {
            const album = this.albumById.get(
                expandedAlbumId,
            );

            if (album?.CoverArtPath) {
                return {
                    coverArtPath: album.CoverArtPath,
                    coverArtSmall: album.CoverArtSmall,
                    coverArtMedium:
                        album.CoverArtMedium,
                    coverArtLarge: album.CoverArtLarge,
                };
            }
        }

        // Fallback: name-based search across all albums.
        for (const album of this.albumById.values()) {
            if (
                album.Name === albumName &&
                album.CoverArtPath
            ) {
                return {
                    coverArtPath: album.CoverArtPath,
                    coverArtSmall: album.CoverArtSmall,
                    coverArtMedium:
                        album.CoverArtMedium,
                    coverArtLarge: album.CoverArtLarge,
                };
            }
        }

        return null;
    }
}
