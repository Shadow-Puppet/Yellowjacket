/**
 * Who owns the volume, and what the control does when it is not us
 * (#64).
 *
 * **This is the tier that can exercise the Android branch**, and it is
 * the reason the predicate is a backend answer rather than a build tag
 * the frontend cannot see: `SystemOwnsVolume` is a stub here, so the
 * "no volume" rendering is checked on an ordinary Linux CI runner with
 * no device anywhere. What no tier here can check is the *constant*
 * behind it, which `TestPlatformVolumeOwnershipIsDeclaredOncePerPlatform`
 * sweeps the Go source for instead.
 *
 * It is a file of its own because `volumeStyleStore` asks once and
 * latches — the answer is a property of the binary and cannot change
 * while the app runs, so there is deliberately no event that refreshes
 * it. Vitest gives each file its own module registry, which is what
 * lets the stub be in place before the singleton is first touched.
 * The *available* case is the rest of `transport.test.ts`, which mounts
 * the same element under the default stub.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import '@components/audio-player/volume-control/volume-control';
import '@components/now-playing-view/now-playing-view';
import { Events } from '../../src/events';
import { emit, resetHarness, stub } from '@test/support/harness';
import { fixture, shadow, shadowAll } from '@test/support/render';
import type { TrackInfo } from '@store/player-store';

const TRACK: TrackInfo = {
  fileName: 'tideline.mp3',
  filePath: '/music/tideline.mp3',
  trackLength: 245,
  seekPosition: 0,
  state: 'playing',
  title: 'Tideline',
  artist: 'Sea Change',
  album: 'Ebb',
  coverArt: '',
  coverArtSmall: '',
  coverArtMedium: '',
  coverArtLarge: '',
  trackChangeId: 1,
  artistMbid: '',
  releaseGroupMbid: '',
  recordingMbid: '',
};

describe('a platform whose volume we do not own', () => {
  beforeEach(async () => {
    resetHarness();
    stub('player.Player.SystemOwnsVolume', true);
    stub('config.Config.GetPopupVolume', false);

    // The store latches on the first mount; do it here so every test
    // below sees a settled answer rather than the first frame.
    const warm = await fixture('volume-control');

    await warm.updateComplete;
  });

  it('renders no control at all, and no empty shadow root to find', async () => {
    const el = await fixture('volume-control');

    await el.updateComplete;

    // Both halves matter. An empty shadow root is what stops a
    // positional or by-role query finding a button that cannot act;
    // `hidden` is what stops the host taking a flex item's worth of
    // space in the transport it sits in.
    expect(shadowAll(el, 'button')).toHaveLength(0);
    expect(shadowAll(el, 'wa-slider')).toHaveLength(0);
    expect(el.hidden, 'the host is not hidden').toBe(true);
  });

  it('leaves the rest of the phone transport alone', async () => {
    emit(Events.TrackChanged, TRACK);

    const view = await fixture('now-playing-view');

    await view.updateComplete;

    // Seeking and the transport buttons are not volume, and #64 is
    // allowed to remove one control, not to thin the screen out.
    expect(shadow(view, 'seek-bar')).not.toBeNull();
    expect(shadow(view, 'player-controls')).not.toBeNull();

    const volume = shadow(view, 'volume-control') as HTMLElement | null;

    expect(volume, 'the element is still mounted').not.toBeNull();
    expect(volume!.hidden, 'a mounted volume-control is not hidden').toBe(true);
  });
});
