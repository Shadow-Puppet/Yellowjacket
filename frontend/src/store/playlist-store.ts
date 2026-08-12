import { EventsOn } from '@runtime/runtime';
import {
    GetAllPlaylists,
    GetAllPlaylistsWithTracks,
    GetPlaylistTracks,
} from '@go/playlist/Service';
import type { playlist } from '@go/models';
import { Events } from '../events';

type Subscriber = () => void;

class PlaylistStore {
    private playlists: playlist.WithTracks[] | null = null;
    private playlistsLoading = false;
    private scrollPosition = 0;
    private subscribers = new Set<Subscriber>();
    private notifyScheduled = false;

    constructor() {
        EventsOn(Events.LibraryScanComplete, () => {
            this.invalidate();
        });

        EventsOn(Events.PlaylistCreated, () => {
            this.invalidate();
        });

        EventsOn(Events.PlaylistDeleted, () => {
            this.invalidate();
        });

        EventsOn(Events.PlaylistRenamed, () => {
            this.invalidate();
        });

        // `PlaylistTracksChanged` carries the playlist that changed,
        // and toggling one heart in the track list is by far its most
        // frequent source.  Answering it with a full invalidate meant
        // `GetAllPlaylistsWithTracks` — every row of every playlist,
        // with full track metadata — for a one-row edit: measured at
        // 2.61 MB and 172 ms across ten 500-track playlists.  Patch the
        // one playlist instead; the id is right there.
        EventsOn(
            Events.PlaylistTracksChanged,
            (playlistId?: number | null) => {
                void this.patchPlaylist(playlistId);
            },
        );

        EventsOn(Events.PlaylistsRestored, () => {
            this.invalidate();
        });

        // Deliberately no eager fetch here.  This store is a singleton
        // constructed at import time, so warming it put every track of
        // every playlist on the path to first paint — for a view the
        // user may never open.  `getPlaylists()` fetches on first
        // access, and `playlist-view` awaits it when it loads.
    }

    // ===================================================================
    // DATA ACCESS
    // Returns cached data or fetches from backend on first access.
    // ===================================================================

    async getPlaylists(): Promise<playlist.WithTracks[]> {
        if (this.playlists !== null) {
            return this.playlists;
        }

        if (this.playlistsLoading) {
            return this.waitForPlaylists();
        }

        this.playlistsLoading = true;
        this.notify();

        try {
            const result = await GetAllPlaylistsWithTracks();
            this.playlists = result ?? [];

            return this.playlists;
        } finally {
            this.playlistsLoading = false;
            this.notify();
        }
    }

    // ===================================================================
    // STATE ACCESSORS
    // Synchronous access for controllers that need current cached values.
    // ===================================================================

    getCachedPlaylists(): playlist.WithTracks[] | null {
        return this.playlists;
    }

    isLoading(): boolean {
        return this.playlistsLoading;
    }

    // ===================================================================
    // SCROLL POSITION
    // ===================================================================

    getScrollPosition(): number {
        return this.scrollPosition;
    }

    setScrollPosition(offset: number): void {
        this.scrollPosition = offset;
    }

    // ===================================================================
    // REFETCH
    // Fetches fresh data without clearing the cache first, so existing
    // consumers keep rendering stale data until the new data arrives.
    // ===================================================================

    async refetch(): Promise<playlist.WithTracks[]> {
        if (this.playlistsLoading) {
            return this.waitForPlaylists();
        }

        this.playlistsLoading = true;

        try {
            const result =
                await GetAllPlaylistsWithTracks();
            this.playlists = result ?? [];

            return this.playlists;
        } finally {
            this.playlistsLoading = false;
            this.notify();
        }
    }

    // ===================================================================
    // INVALIDATION
    // ===================================================================

    invalidate(): void {
        this.playlists = null;
        this.scrollPosition = 0;
        this.notify();

        // Only refetch eagerly if something is actually rendering
        // playlists.  `playlist-view` is the sole subscriber and is
        // created lazily on first navigation, so before it has ever
        // been opened this used to fetch every track of every playlist
        // in answer to an event nobody was listening for.  Once it
        // exists it is a cached view and stays subscribed, so the
        // refetch-then-rerender path it depends on is unchanged.
        if (this.subscribers.size > 0) {
            void this.getPlaylists();
        }
    }

    // ===================================================================
    // PATCHING
    // Refetch one playlist rather than all of them.
    // ===================================================================

    /**
     * Replace a single playlist's tracks in the cache.
     *
     * Falls back to a full invalidate whenever the patch cannot be
     * shown to be equivalent: an event with no id (the bulk restore and
     * reorder paths emit one), a cold cache, an id we have never seen,
     * or a fetch already in flight — which would otherwise land on top
     * of the patch and undo it.
     *
     * Summaries come along because `updated_at` moves with the edit and
     * `playlist-view` sorts on it; `GetAllPlaylists` is summaries only,
     * so its cost does not scale with how many tracks a playlist holds.
     */
    private async patchPlaylist(
        playlistId?: number | null,
    ): Promise<void> {
        const cached = this.playlists;

        if (
            typeof playlistId !== 'number' ||
            cached === null ||
            this.playlistsLoading ||
            !cached.some((p) => p.Summary?.ID === playlistId)
        ) {
            this.invalidate();

            return;
        }

        try {
            const [summaries, tracks] = await Promise.all([
                GetAllPlaylists(),
                GetPlaylistTracks(playlistId),
            ]);

            // The cache may have been replaced while those were in
            // flight, in which case this patch describes a state that
            // no longer exists and the newer one is already correct.
            if (this.playlists !== cached) return;

            const summaryById = new Map(
                (summaries ?? []).map((s) => [s.ID, s]),
            );

            // A new array identity, because `playlist-view` keys its
            // reload off it — while sharing the `Tracks` array of every
            // playlist that did not change.
            //
            // `WithTracks` is a generated *class* (it carries
            // `convertValues`), so an object spread would produce
            // something that no longer is one. Clone through the
            // prototype instead.
            const withChanges = (
                entry: playlist.WithTracks,
                changes: Partial<playlist.WithTracks>,
            ): playlist.WithTracks =>
                Object.assign(
                    Object.create(
                        Object.getPrototypeOf(entry) as object,
                    ) as playlist.WithTracks,
                    entry,
                    changes,
                );

            this.playlists = cached.map((entry) => {
                const summary =
                    summaryById.get(entry.Summary?.ID) ??
                    entry.Summary;

                if (entry.Summary?.ID !== playlistId) {
                    return summary === entry.Summary
                        ? entry
                        : withChanges(entry, {
                              Summary: summary,
                          });
                }

                return withChanges(entry, {
                    Summary: summary,
                    Tracks: tracks ?? [],
                });
            });

            this.notify();
        } catch (err) {
            console.error(
                'Failed to patch playlist after track change:',
                err,
            );
            this.invalidate();
        }
    }

    // ===================================================================
    // SUBSCRIPTION SYSTEM
    // ===================================================================

    subscribe(callback: Subscriber): () => void {
        this.subscribers.add(callback);

        return () => this.subscribers.delete(callback);
    }

    /**
     * Coalesced to a microtask, the way every other store in this app
     * does it (perf.p3). A patch writes the tracks and the summaries in two statements;
     * without coalescing that is two synchronous passes over every
     * subscriber.
     * Lit batches the resulting `requestUpdate()`s anyway, so the win is
     * small; the point is that five stores doing this and two not is
     * where a real double-notify hides.
     */
    private notify(): void {
        if (this.notifyScheduled) return;

        this.notifyScheduled = true;

        queueMicrotask(() => {
            this.notifyScheduled = false;
            this.subscribers.forEach((callback) => callback());
        });
    }

    // ===================================================================
    // HELPERS
    // Wait for an in-flight fetch to complete.
    // ===================================================================

    private waitForPlaylists(): Promise<playlist.WithTracks[]> {
        return new Promise((resolve) => {
            const unsub = this.subscribe(() => {
                if (
                    !this.playlistsLoading &&
                    this.playlists !== null
                ) {
                    unsub();
                    resolve(this.playlists);
                }
            });
        });
    }
}

// Singleton instance.
export const playlistStore = new PlaylistStore();
