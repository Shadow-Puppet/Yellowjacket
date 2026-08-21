/**
 * The transport in its two contexts (#59, #56).
 *
 * `player-controls` is one component in two places, and what each place
 * wants differs *at the same viewport*: on a phone the bottom bar wants
 * three controls sized for a thumb, and `now-playing-view` wants five,
 * larger still. So the host states the context and the viewport states
 * the size band, and this file pins the half a media query cannot
 * express.
 *
 * **What this tier can and cannot see.** It can see which buttons
 * exist, because that is `matchMedia` and a render — and existence is
 * the whole of #59. It cannot see the *sizes*: those come from the
 * context's custom properties, and a component-tier render has no shell
 * around it, so the measurements live in `e2e/specs/phone-transport.spec.ts`
 * where there is a real bar in a real viewport. Asserting a pixel here
 * would be asserting the fallbacks, which is `ui-visual`'s documented
 * blind spot one tier over.
 */
import { describe, expect, it, beforeEach, afterEach } from 'vitest';

import '@components/audio-player/controls/player-controls';
import { Events } from '../../src/events';
import { emit, flush, calls } from '@test/support/harness';
import { fixture, shadowAll, click } from '@test/support/render';

/** Reset the backend-owned state the component reads from. */
function idle(): void {
  emit(Events.TrackChanged, null);
  emit(Events.PlaybackStateChanged, { state: 'stopped' });
  emit(Events.QueueModeChanged, { shuffleMode: false, repeatMode: 'off' });
}

const names = (el: Element): Array<string | null> =>
  shadowAll(el, 'button').map((b) => b.getAttribute('aria-label'));

/**
 * Answer `matchMedia` for the phone query, since the test runner's own
 * window is whatever size the browser provider gives it.
 *
 * It is stubbed rather than resized because what is under test is the
 * component's *reaction* to the answer, and a resize would additionally
 * be asserting that this runner's viewport can get below 600px.
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

afterEach(() => {
  window.matchMedia = realMatchMedia;
});

describe('<player-controls> in the bar', () => {
  beforeEach(() => {
    idle();
  });

  it('keeps all five on a desktop, in the order it always had', async () => {
    pretendPhone(false);

    const el = await fixture('player-controls');

    // Unchanged from before #59, deliberately: this is the desktop bar
    // and nothing about it was reported.
    expect(names(el)).toEqual([
      'Shuffle',
      'Previous track',
      'Play',
      'Next track',
      'Repeat: off',
    ]);
  });

  it('draws three on a phone, and does not merely hide the other two', async () => {
    pretendPhone(true);

    const el = await fixture('player-controls');

    expect(names(el)).toEqual(['Previous track', 'Play', 'Next track']);

    // The distinction this asserts is the point. A `display: none`
    // control is still in the shadow root, still something a positional
    // query finds, and still a thing the component claims to have --
    // so "the phone has three controls" would have been true of the
    // pixels and false of the element.
    expect(shadowAll(el, 'button')).toHaveLength(3);
  });

  it('follows the viewport when it changes, not just at construction', async () => {
    pretendPhone(false);

    const el = await fixture('player-controls');

    expect(names(el)).toHaveLength(5);

    // A desktop window dragged narrow is the phone layout, per plan
    // 018's decision 4 -- so this is a real transition and not a
    // hypothetical.
    (el as unknown as { phone: boolean }).phone = true;
    await flush();
    await el.updateComplete;

    expect(names(el)).toEqual(['Previous track', 'Play', 'Next track']);
  });
});

describe('<player-controls> full-screen', () => {
  beforeEach(() => {
    idle();
  });

  it('keeps all five on a phone, where the bar keeps three', async () => {
    pretendPhone(true);

    const el = await fixture('player-controls');

    el.setAttribute('context', 'full');
    await el.updateComplete;

    // The same viewport, the other answer: this is why the context is a
    // property and cannot be a media query.
    expect(names(el)).toHaveLength(5);
  });

  it('puts the secondary pair after the primary three, in the DOM', async () => {
    pretendPhone(true);

    const el = await fixture('player-controls');

    el.setAttribute('context', 'full');
    await el.updateComplete;

    // Order, not just membership: the secondary controls are drawn on a
    // second row, and this is asserted in the DOM because visual order
    // and focus order have to agree. A CSS `order` property would move
    // them on screen and leave Tab walking the old sequence.
    expect(names(el)).toEqual([
      'Previous track',
      'Play',
      'Next track',
      'Shuffle',
      'Repeat: off',
    ]);
  });

  it('still routes every button to the backend', async () => {
    pretendPhone(true);

    const el = await fixture('player-controls');

    el.setAttribute('context', 'full');
    await el.updateComplete;

    // Two arrangements, one set of handlers. The regression this
    // guards is the reason a second *component* was refused: a second
    // template renders buttons wired to nothing, which looks perfect
    // in a screenshot and does nothing at all.
    for (const name of [
      'Previous track',
      'Next track',
      'Shuffle',
      'Repeat: off',
    ]) {
      await click(el, `button[aria-label="${name}"]`);
    }

    expect(calls().map((c) => c.path)).toEqual([
      'queue.Queue.Previous',
      'queue.Queue.Next',
      'queue.Queue.ToggleShuffle',
      'queue.Queue.CycleRepeat',
    ]);
  });
});
