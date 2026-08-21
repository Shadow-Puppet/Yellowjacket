/**
 * The phone's progress line (#58).
 *
 * **What this tier can and cannot see.** It can see the whole of what
 * the issue asks for that is not a pixel: that the line exists only on
 * a phone and only with a track, that it renders the position the
 * backend reported rather than a count of its own, and that it is
 * neither announced nor touchable. It cannot see where it sits — that
 * is the shell's grid, and it is asserted in
 * `e2e/specs/phone-transport.spec.ts` where there is a real bar with a
 * real tab bar under it.
 */
import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest';

import '@components/audio-player/progress-line/progress-line';
import { Events } from '../../src/events';
import { emit, flush } from '@test/support/harness';
import { fixture, shadow } from '@test/support/render';

const TRACK = {
    fileName: 'song.mp3',
    filePath: '/music/song.mp3',
    trackLength: 90,
    seekPosition: 0,
    state: 'playing',
    title: 'Song',
    artist: 'Artist',
    album: 'Album',
    coverArt: '',
    coverArtSmall: '',
    coverArtMedium: '',
    coverArtLarge: '',
    trackChangeId: 1,
    artistMbid: '',
    releaseGroupMbid: '',
    recordingMbid: '',
};

/**
 * Answer `matchMedia` for the phone query, since the runner's own
 * window is whatever size the browser provider gives it. Stubbed rather
 * than resized for `transport-context.test.ts`'s reason: what is under
 * test is the component's reaction to the answer.
 */
const realMatchMedia = window.matchMedia;

function pretendPhone(phone: boolean): void {
    window.matchMedia = ((query: string) => ({
        matches: phone && query.includes('599'),
        media: query,
        addEventListener: () => {},
        removeEventListener: () => {},
    })) as unknown as typeof window.matchMedia;
}

/** The horizontal scale of the fill, or null if there is no line. */
function scale(el: Element): number | null {
    const fill = shadow<HTMLElement>(el, '.fill');

    if (!fill) return null;

    const match = /scaleX\(([^)]+)\)/.exec(fill.style.transform);

    return match ? Number(match[1]) : null;
}

describe('<player-progress-line>', () => {
    beforeEach(() => {
        emit(Events.TrackChanged, null);
        emit(Events.PlaybackStateChanged, { state: 'stopped' });
    });

    afterEach(() => {
        window.matchMedia = realMatchMedia;
        vi.useRealTimers();
    });

    it('draws nothing above the phone breakpoint', async () => {
        pretendPhone(false);

        const el = await fixture('player-progress-line');

        emit(Events.TrackChanged, { ...TRACK, trackChangeId: 2 });
        emit(Events.PlaybackPositionChanged, {
            positionSeconds: 45,
            trackLength: 90,
            trackChangeId: 2,
            seq: 1,
            playing: true,
        });
        await flush();
        await el.updateComplete;

        // The desktop bar carries a real seek bar, and there is no tab
        // bar for this to sit on the border of.
        expect(el.shadowRoot!.querySelector('.track')).toBeNull();
    });

    it('draws nothing until there is a track', async () => {
        pretendPhone(true);

        const el = await fixture('player-progress-line');

        expect(el.shadowRoot!.querySelector('.track')).toBeNull();
    });

    it('renders the fraction the backend reported', async () => {
        pretendPhone(true);

        const el = await fixture('player-progress-line');

        emit(Events.TrackChanged, { ...TRACK, trackChangeId: 3 });
        emit(Events.PlaybackPositionChanged, {
            positionSeconds: 45,
            trackLength: 90,
            trackChangeId: 3,
            seq: 1,
            playing: true,
        });
        await flush();
        await el.updateComplete;

        expect(scale(el)).toBeCloseTo(0.5, 3);
    });

    it('resumes mid-track at the position the track arrived with', async () => {
        pretendPhone(true);

        const el = await fixture('player-progress-line');

        emit(Events.TrackChanged, {
            ...TRACK,
            seekPosition: 30,
            trackChangeId: 4,
        });
        await flush();
        await el.updateComplete;

        expect(scale(el)).toBeCloseTo(1 / 3, 3);
    });

    it('interpolates between reports, and every report resets it', async () => {
        pretendPhone(true);
        vi.useFakeTimers();

        const el = await fixture('player-progress-line');

        emit(Events.TrackChanged, { ...TRACK, trackChangeId: 5 });
        emit(Events.PlaybackStateChanged, { state: 'playing' });
        await vi.advanceTimersByTimeAsync(3000);
        await el.updateComplete;

        expect(scale(el)).toBeCloseTo(3 / 90, 3);

        // The user seeks; the backend lands somewhere else and says so.
        // The local count is discarded, never added to -- the seek
        // bar's rule, and the reason it has it.
        emit(Events.PlaybackPositionChanged, {
            positionSeconds: 40,
            trackLength: 90,
            trackChangeId: 5,
            seq: 2,
            playing: true,
        });
        await vi.advanceTimersByTimeAsync(1000);
        await el.updateComplete;

        expect(scale(el)).toBeCloseTo(41 / 90, 3);
    });

    it('ignores a report about a track that is no longer loaded', async () => {
        pretendPhone(true);

        const el = await fixture('player-progress-line');

        emit(Events.TrackChanged, { ...TRACK, trackChangeId: 6 });
        emit(Events.PlaybackPositionChanged, {
            positionSeconds: 60,
            trackLength: 90,
            trackChangeId: 5,
            seq: 3,
            playing: true,
        });
        await flush();
        await el.updateComplete;

        // The store is a singleton, so a line mounting late must not
        // adopt a report about the previous track.
        expect(scale(el)).toBe(0);
    });

    it('counts nothing while the player is paused', async () => {
        pretendPhone(true);
        vi.useFakeTimers();

        const el = await fixture('player-progress-line');

        emit(Events.TrackChanged, { ...TRACK, trackChangeId: 7 });
        emit(Events.PlaybackPositionChanged, {
            positionSeconds: 10,
            trackLength: 90,
            trackChangeId: 7,
            seq: 1,
            playing: false,
        });
        emit(Events.PlaybackStateChanged, { state: 'paused' });
        await vi.advanceTimersByTimeAsync(5000);
        await el.updateComplete;

        expect(scale(el)).toBeCloseTo(10 / 90, 3);
    });

    it('is decorative and cannot be touched', async () => {
        pretendPhone(true);

        const el = await fixture('player-progress-line');

        emit(Events.TrackChanged, { ...TRACK, trackChangeId: 8 });
        await flush();
        await el.updateComplete;

        // The seek bar on Now Playing is what announces the position;
        // this says the same thing with no name and no way to act on
        // it, and it sits exactly where a thumb aiming at a tab lands.
        expect(el.getAttribute('aria-hidden')).toBe('true');
        expect(getComputedStyle(el).pointerEvents).toBe('none');
    });
});
