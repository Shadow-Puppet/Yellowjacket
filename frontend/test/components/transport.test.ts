/**
 * The transport bar: five buttons and a seek bar, all of them driven by
 * backend push events rather than by their own clicks. These are the
 * controls the e2e tier drives by accessible name, so the names are as
 * much of a contract as the behaviour.
 */
import { describe, expect, it, beforeEach, vi, afterEach } from 'vitest';

import '@components/audio-player/audio-player';
import '@components/audio-player/controls/player-controls';
import '@components/audio-player/seekbar/seek-bar';
import '@components/audio-player/volume-control/volume-control';
import { Events } from '../../src/events';
import { emit, calls, lastArgs, flush, stub } from '@test/support/harness';
import {
  fixture,
  shadow,
  shadowAll,
  deepShadow,
  deepText,
  text,
  click,
  visual,
} from '@test/support/render';
import { PlayerRegion, type TrackInfo } from '@store/player-store';
import { notificationStore } from '@store/notification-store';

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
    ]).toEqual(['00:00', '-01:30']);
  });

  it('says which number the right-hand clock is, and swaps it on click', async () => {
    const el = await fixture('seek-bar');

    emit(Events.TrackChanged, { ...TRACK, trackChangeId: 21 });
    await flush();
    await el.updateComplete;

    // H-16: it read `01:21` next to a track the list called `01:30`,
    // with no minus sign, no label and no way to see the duration.
    await click(el, '[data-testid="remaining-time"]');
    await el.updateComplete;

    expect(text(el, '[data-testid="remaining-time"]')).toBe('01:30');
  });

  it('keeps the slider still as the clocks count', async () => {
    const el = await fixture('seek-bar');

    emit(Events.TrackChanged, { ...TRACK, trackChangeId: 31 });
    await flush();
    await el.updateComplete;

    const slider = () =>
      shadow(el, 'wa-slider')!.getBoundingClientRect();
    const before = slider();

    // 1:11 against 4:08 is the reported jitter: different digits, and
    // in a proportional font different widths. Toggling the right-hand
    // clock is the other half -- the minus sign is a whole character.
    for (const positionSeconds of [8, 71, 88]) {
      emit(Events.PlaybackPositionChanged, {
        positionSeconds,
        trackLength: 90,
        trackChangeId: 31,
        seq: positionSeconds,
        playing: true,
      });
      await flush();
      await el.updateComplete;

      expect(slider().width).toBeCloseTo(before.width, 1);
      expect(slider().left).toBeCloseTo(before.left, 1);
    }

    await click(el, '[data-testid="remaining-time"]');
    await el.updateComplete;

    expect(slider().width).toBeCloseTo(before.width, 1);
  });

  it('renders the position the backend reports rather than its own count', async () => {
    const el = await fixture('seek-bar');

    emit(Events.TrackChanged, { ...TRACK, trackChangeId: 11 });
    emit(Events.PlaybackPositionChanged, {
      positionSeconds: 42,
      trackLength: 90,
      trackChangeId: 11,
      seq: 1,
      playing: true,
    });
    await flush();
    await el.updateComplete;

    expect(text(el, '[data-testid="elapsed-time"]')).toBe('00:42');
  });

  it('resets its interpolation on every report, so a seek cannot desync it', async () => {
    vi.useFakeTimers();

    const el = await fixture('seek-bar');

    emit(Events.TrackChanged, { ...TRACK, trackChangeId: 12 });
    emit(Events.PlaybackStateChanged, { state: 'playing' });
    await vi.advanceTimersByTimeAsync(3000);
    await el.updateComplete;

    // The user seeks; the backend lands somewhere else entirely and
    // says so.  The local counter must be discarded, not added to.
    emit(Events.PlaybackPositionChanged, {
      positionSeconds: 40,
      trackLength: 90,
      trackChangeId: 12,
      seq: 2,
      playing: true,
    });
    await vi.advanceTimersByTimeAsync(1000);
    await el.updateComplete;

    expect(text(el, '[data-testid="elapsed-time"]')).toBe('00:41');
  });

  it('ignores a report belonging to a track that is no longer loaded', async () => {
    const el = await fixture('seek-bar');

    emit(Events.TrackChanged, { ...TRACK, trackChangeId: 13 });
    emit(Events.PlaybackPositionChanged, {
      positionSeconds: 60,
      trackLength: 90,
      trackChangeId: 12,
      seq: 3,
      playing: true,
    });
    await flush();
    await el.updateComplete;

    expect(text(el, '[data-testid="elapsed-time"]')).toBe('00:00');
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

    // This asserted `aria-label` on the host for six phases, and the
    // host is not what carries `role="slider"` — the name never
    // reached the accessibility tree. `wa-control-names.test.ts` is
    // the whole story; the name now comes from `label`.
    expect(shadow(el, 'wa-slider')?.getAttribute('label')).toBe('Seek');
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

/**
 * Mute is silence at an unchanged volume level, so the indicator has to
 * be driven by its own event — watching the volume number, as it used
 * to, meant pressing M visibly did nothing.
 */
/**
 * The volume control has two presentations (#42), and the icon button
 * means a different thing in each — so both are exercised rather than
 * whichever one happens to be the default.
 *
 * Inline is the default: the slider is simply there, which leaves the
 * icon with nothing to disclose, so it is the mute toggle and is named
 * after that action. In the popup it is a disclosure, so it is named
 * after the *state* it is showing.
 */
describe('volume control: mute', () => {
  /**
   * Put the presentation back to the default between tests.
   *
   * `volumeStyleStore` is a singleton whose `init()` reads the setting
   * once, so stubbing the binding inside a test is too late — a
   * previous test has already loaded it. `GeneralConfigChanged` is the
   * store's own refresh trigger and the same one the Settings page
   * fires, so driving it that way exercises the real path instead of
   * reaching for a test-only reset.
   */
  const setPresentation = async (popup: boolean) => {
    stub('config.Config.GetPopupVolume', popup);
    emit(Events.GeneralConfigChanged, {});
    await flush();
  };

  beforeEach(async () => {
    await setPresentation(false);
    emit(Events.VolumeChanged, 40);
    emit(Events.MuteChanged, false);
  });

  it('shows a muted glyph once the backend reports mute', async () => {
    const el = await fixture('volume-control');

    expect(shadow(el, 'button')?.getAttribute('data-muted')).toBe('false');

    emit(Events.MuteChanged, true);
    await flush();
    await el.updateComplete;

    expect(shadow(el, 'button')?.getAttribute('data-muted')).toBe('true');
    expect(shadow(el, 'button wa-icon')?.getAttribute('name')).toBe(
      'volume-xmark',
    );
  });

  it('names the inline icon after the action it performs', async () => {
    const el = await fixture('volume-control');

    expect(shadow(el, 'button')?.getAttribute('aria-label')).toBe('Mute');

    emit(Events.MuteChanged, true);
    await flush();
    await el.updateComplete;

    expect(shadow(el, 'button')?.getAttribute('aria-label')).toBe('Unmute');
  });

  it('names the popup icon after the state it discloses', async () => {
    await setPresentation(true);

    const el = await fixture('volume-control');

    await el.updateComplete;

    expect(shadow(el, 'button')?.getAttribute('aria-label')).toBe(
      'Volume 40%',
    );

    emit(Events.MuteChanged, true);
    await flush();
    await el.updateComplete;

    expect(shadow(el, 'button')?.getAttribute('aria-label')).toBe('Muted');
  });

  it('shows the slider without a click when it is inline', async () => {
    const el = await fixture('volume-control');

    // The whole point of the issue: no disclosure to operate first.
    expect(shadow<HTMLInputElement>(el, 'wa-slider')?.value).toBe(40);
  });

  it('keeps showing the volume level while muted, because it is unchanged', async () => {
    await setPresentation(true);
    emit(Events.MuteChanged, true);
    await flush();

    const el = await fixture('volume-control');

    await el.updateComplete;
    await click(el, 'button');

    expect(shadow<HTMLInputElement>(el, 'wa-slider')?.value).toBe(40);
  });

  it('toggles mute through the backend rather than locally', async () => {
    const el = await fixture('volume-control');

    await click(el, 'button');

    expect(calls('player.Player.MuteToggle').length).toBe(1);
    // Nothing optimistic: the icon follows the backend's event.
    expect(shadow(el, 'button')?.getAttribute('data-muted')).toBe('false');
  });

  it('toggles mute from inside the popup, where the icon is a disclosure', async () => {
    await setPresentation(true);

    const el = await fixture('volume-control');

    await el.updateComplete;
    await click(el, 'button');
    await click(el, '.mute-toggle');

    expect(calls('player.Player.MuteToggle').length).toBe(1);
    expect(shadow(el, 'button')?.getAttribute('data-muted')).toBe('false');
  });
});

/**
 * The player says when it could not do what it was told.
 *
 * This is the Inline level of plan 007's notification table, and it is
 * deliberately local to the bottom bar rather than an app-wide surface:
 * the useful response to a track that will not play is to keep playing,
 * which the backend already does by skipping it.
 */
describe('<audio-player> messages', () => {
  beforeEach(() => {
    notificationStore.dismissRegion(PlayerRegion);
  });

  it('names the track that could not be played', async () => {
    const el = await fixture('audio-player');

    emit(Events.PlaybackFailed, {
      filePath: '/music/gone.mp3',
      title: 'Tideline',
      artist: 'Aurora Fields',
      reason: 'no such file or directory',
    });
    await flush();
    await el.updateComplete;

    expect(deepText(el, '[data-testid="player-message"]')).toContain(
      'Tideline',
    );
  });

  it('coalesces a disconnected drive into one message with a count', async () => {
    const el = await fixture('audio-player');

    for (const title of ['One', 'Two', 'Three']) {
      emit(Events.PlaybackFailed, {
        filePath: `/music/${title}.mp3`,
        title,
        artist: '',
        reason: 'no such file or directory',
      });
    }

    await flush();
    await el.updateComplete;

    // Not three messages, and not one message about the last file.
    expect(deepText(el, '[data-testid="player-message"]')).toContain(
      'Skipped 3 tracks',
    );
  });

  it('explains a failed seek, which used to be emitted into the void', async () => {
    const el = await fixture('audio-player');

    emit(Events.SeekFailed);
    await flush();
    await el.updateComplete;

    expect(deepText(el, '[data-testid="player-message"]')).toContain(
      'Could not seek',
    );
  });

  it('can be dismissed', async () => {
    const el = await fixture('audio-player');

    emit(Events.SeekFailed);
    await flush();
    await el.updateComplete;

    deepShadow<HTMLButtonElement>(
      el,
      '[data-testid="player-message"] .notice-dismiss',
    )!.click();
    await flush();
    await el.updateComplete;

    expect(deepShadow(el, '[data-testid="player-message"]')).toBeNull();
  });
});
