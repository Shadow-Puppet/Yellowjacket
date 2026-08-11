/**
 * The transport bar: five buttons and a seek bar, all of them driven by
 * backend push events rather than by their own clicks. These are the
 * controls the e2e tier drives by accessible name, so the names are as
 * much of a contract as the behaviour.
 */
import { describe, expect, it, beforeEach, vi, afterEach } from 'vitest';

import '@components/audio-player/controls/player-controls';
import '@components/audio-player/seekbar/seek-bar';
import { Events } from '../../src/events';
import { emit, calls, lastArgs, flush } from '@test/support/harness';
import {
  fixture,
  shadow,
  shadowAll,
  text,
  click,
  visual,
} from '@test/support/render';
import type { TrackInfo } from '@store/player-store';

const TRACK: TrackInfo = {
  fileName: 'long.mp3',
  filePath: '/music/long.mp3',
  trackLength: 90,
  seekPosition: 0,
  state: 'playing',
  title: 'Long Player',
  artist: 'Test Artist',
  album: 'Fixtures',
  coverArt: '',
  coverArtSmall: '',
  coverArtMedium: '',
  coverArtLarge: '',
  trackChangeId: 1,
  artistMbid: '',
  releaseGroupMbid: '',
  recordingMbid: '',
};

/** Reset the backend-owned state both components read from. */
function idle(): void {
  emit(Events.TrackChanged, null);
  emit(Events.PlaybackStateChanged, { state: 'stopped' });
  emit(Events.QueueModeChanged, { shuffleMode: false, repeatMode: 'off' });
}

function labelOf(host: Element, index: number): string | null {
  return shadowAll(host, 'button')[index]?.getAttribute('aria-label') ?? null;
}

describe('<player-controls>', () => {
  beforeEach(() => {
    idle();
  });

  it('names every button, so both a screen reader and a selector can find it', async () => {
    const el = await fixture('player-controls');

    expect(shadowAll(el, 'button').map((b) => b.getAttribute('aria-label'))).toEqual(
      ['Shuffle', 'Previous track', 'Play', 'Next track', 'Repeat: off'],
    );
  });

  it('becomes a pause button while playing', async () => {
    const el = await fixture('player-controls');

    emit(Events.PlaybackStateChanged, { state: 'playing' });
    await flush();
    await el.updateComplete;

    expect([labelOf(el, 2), shadow(el, 'wa-icon[name="pause"]')]).not.toContain(
      null,
    );
  });

  it('asks the queue to play, not the player — the queue owns what plays next', async () => {
    const el = await fixture('player-controls');

    await click(el, 'button[aria-label="Play"]');

    expect(calls().map((c) => c.path)).toEqual(['queue.Queue.Play']);
  });

  it('pauses through the player once playing', async () => {
    const el = await fixture('player-controls');

    emit(Events.PlaybackStateChanged, { state: 'playing' });
    await flush();
    await el.updateComplete;
    await click(el, 'button[aria-label="Pause"]');

    expect(calls().map((c) => c.path)).toEqual(['player.Player.Pause']);
  });

  it('wires skip forward and back to the queue', async () => {
    const el = await fixture('player-controls');

    await click(el, 'button[aria-label="Next track"]');
    await click(el, 'button[aria-label="Previous track"]');

    expect(calls().map((c) => c.path)).toEqual([
      'queue.Queue.Next',
      'queue.Queue.Previous',
    ]);
  });

  it('reports shuffle state through aria-pressed, not just colour', async () => {
    const el = await fixture('player-controls');
    const before = shadow(el, 'button[aria-label="Shuffle"]')?.getAttribute(
      'aria-pressed',
    );

    emit(Events.QueueModeChanged, { shuffleMode: true, repeatMode: 'off' });
    await flush();
    await el.updateComplete;

    expect([
      before,
      shadow(el, 'button[aria-label="Shuffle"]')?.getAttribute('aria-pressed'),
    ]).toEqual(['false', 'true']);
  });

  it('spells the repeat mode into the label, since one icon covers three states', async () => {
    const el = await fixture('player-controls');

    emit(Events.QueueModeChanged, { shuffleMode: false, repeatMode: 'one' });
    await flush();
    await el.updateComplete;

    expect(labelOf(el, 4)).toBe('Repeat: one');
  });

  it('marks repeat-one so its badge renders', async () => {
    const el = await fixture('player-controls');

    emit(Events.QueueModeChanged, { shuffleMode: false, repeatMode: 'one' });
    await flush();
    await el.updateComplete;

    expect(shadow(el, 'button.repeat-one')).not.toBeNull();
  });

  it('does not toggle its own state — the backend confirms it', async () => {
    const el = await fixture('player-controls');

    await click(el, 'button[aria-label="Shuffle"]');

    expect([
      calls().map((c) => c.path),
      shadow(el, 'button[aria-label="Shuffle"]')?.getAttribute('aria-pressed'),
    ]).toEqual([['queue.Queue.ToggleShuffle'], 'false']);
  });

  it('stops listening to the queue once removed', async () => {
    const el = await fixture('player-controls');

    el.remove();
    emit(Events.QueueModeChanged, { shuffleMode: true, repeatMode: 'all' });
    await flush();

    // A leaked subscription would keep rendering a detached element.
    expect(el.isConnected).toBe(false);
  });

  it('looks the way it did last time', async () => {
    const el = await fixture('player-controls');

    emit(Events.QueueModeChanged, { shuffleMode: true, repeatMode: 'one' });
    await flush();
    await el.updateComplete;

    await visual(el, 'player-controls');
    expect(shadowAll(el, 'button')).toHaveLength(5);
  });
});

describe('<seek-bar>', () => {
  beforeEach(() => {
    idle();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('shows placeholder clocks with nothing loaded', async () => {
    const el = await fixture('seek-bar');

    expect([
      text(el, '[data-testid="elapsed-time"]'),
      text(el, '[data-testid="remaining-time"]'),
    ]).toEqual(['--:--', '--:--']);
  });

  it('shows elapsed and remaining once a track is loaded', async () => {
    const el = await fixture('seek-bar');

    emit(Events.TrackChanged, TRACK);
    await flush();
    await el.updateComplete;

    expect([
      text(el, '[data-testid="elapsed-time"]'),
      text(el, '[data-testid="remaining-time"]'),
    ]).toEqual(['00:00', '01:30']);
  });

  it('resumes mid-track from the position the backend reported', async () => {
    const el = await fixture('seek-bar');

    emit(Events.TrackChanged, { ...TRACK, seekPosition: 30, trackChangeId: 2 });
    await flush();
    await el.updateComplete;

    expect(text(el, '[data-testid="elapsed-time"]')).toBe('00:30');
  });

  it('rewinds when the same file plays again, which only the change id reveals', async () => {
    const el = await fixture('seek-bar');

    emit(Events.TrackChanged, { ...TRACK, seekPosition: 45, trackChangeId: 3 });
    await flush();
    await el.updateComplete;

    emit(Events.TrackChanged, { ...TRACK, seekPosition: 0, trackChangeId: 4 });
    await flush();
    await el.updateComplete;

    expect(text(el, '[data-testid="elapsed-time"]')).toBe('00:00');
  });

  it('ticks the clock forward while playing', async () => {
    vi.useFakeTimers();

    const el = await fixture('seek-bar');

    emit(Events.TrackChanged, TRACK);
    emit(Events.PlaybackStateChanged, { state: 'playing' });
    await vi.advanceTimersByTimeAsync(0);
    await el.updateComplete;

    await vi.advanceTimersByTimeAsync(3000);
    await el.updateComplete;

    expect(text(el, '[data-testid="elapsed-time"]')).toBe('00:03');
  });

  it('stops ticking when paused', async () => {
    vi.useFakeTimers();

    const el = await fixture('seek-bar');

    emit(Events.TrackChanged, TRACK);
    emit(Events.PlaybackStateChanged, { state: 'playing' });
    await vi.advanceTimersByTimeAsync(2000);
    await el.updateComplete;

    emit(Events.PlaybackStateChanged, { state: 'paused' });
    await vi.advanceTimersByTimeAsync(0);
    await el.updateComplete;
    await vi.advanceTimersByTimeAsync(5000);
    await el.updateComplete;

    expect(text(el, '[data-testid="elapsed-time"]')).toBe('00:02');
  });

  it('seeks to the position the slider was dropped at', async () => {
    const el = await fixture('seek-bar');

    emit(Events.TrackChanged, TRACK);
    await flush();
    await el.updateComplete;

    const slider = shadow<HTMLElement & { value: number }>(el, 'wa-slider');

    if (slider) slider.value = 42;

    slider?.dispatchEvent(new Event('change'));
    await el.updateComplete;

    expect(lastArgs('player.Player.Seek')).toEqual([42]);
  });

  it('bounds the slider by the track length', async () => {
    const el = await fixture('seek-bar');

    emit(Events.TrackChanged, TRACK);
    await flush();
    await el.updateComplete;

    expect(shadow(el, 'wa-slider')?.getAttribute('max')).toBe('90');
  });

  it('carries an accessible name, since it is otherwise an unlabelled slider', async () => {
    const el = await fixture('seek-bar');

    expect(shadow(el, 'wa-slider')?.getAttribute('aria-label')).toBe('Seek');
  });

  it('looks the way it did last time', async () => {
    const el = await fixture('seek-bar');

    emit(Events.TrackChanged, { ...TRACK, seekPosition: 30, trackChangeId: 9 });
    await flush();
    await el.updateComplete;

    await visual(el, 'seek-bar');
    expect(text(el, '[data-testid="elapsed-time"]')).toBe('00:30');
  });
});
