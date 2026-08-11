/**
 * `<track-info>` is the shared row renderer: every list that shows a
 * track goes through it, so its fallbacks (missing title, missing
 * cover, missing duration) are visible in half the app.
 */
import { describe, expect, it } from 'vitest';

import '@components/track-info/track-info';
import { fixture, shadow, text, update, visual } from '@test/support/render';

describe('<track-info>', () => {
  it('renders title, artist and album', async () => {
    const el = await fixture('track-info', {
      trackTitle: 'Ashes to Ashes',
      artist: 'David Bowie',
      album: 'Scary Monsters',
    });

    expect([text(el, '.title'), text(el, '.secondary')]).toEqual([
      'Ashes to Ashes',
      'David Bowie — Scary Monsters',
    ]);
  });

  it('joins artist and album with an em dash, and omits the separator when one is missing', async () => {
    const el = await fixture('track-info', {
      trackTitle: 'X',
      artist: 'Only Artist',
    });

    expect(text(el, '.secondary')).toBe('Only Artist');
  });

  it('omits the secondary line entirely when there is nothing to put in it', async () => {
    const el = await fixture('track-info', { trackTitle: 'X' });

    expect(shadow(el, '.secondary')).toBeNull();
  });

  it('falls back to the filename, without its extension, when untitled', async () => {
    // WAV tracks currently scan in untitled, so this path is live.
    const el = await fixture('track-info', {
      filePath: '/music/field/01 - Dawn Chorus.wav',
    });

    expect(text(el, '.title')).toBe('01 - Dawn Chorus');
  });

  it('handles a Windows path in the same fallback', async () => {
    const el = await fixture('track-info', {
      filePath: 'C:\\Music\\Album\\Track.mp3',
    });

    expect(text(el, '.title')).toBe('Track');
  });

  it('prefers a real title over the filename', async () => {
    const el = await fixture('track-info', {
      trackTitle: 'Real Title',
      filePath: '/music/whatever.mp3',
    });

    expect(text(el, '.title')).toBe('Real Title');
  });

  it('renders no title element at all when it has neither title nor path', async () => {
    const el = await fixture('track-info', { artist: 'Someone' });

    expect(shadow(el, '.title')).toBeNull();
  });

  it('formats a duration given in milliseconds', async () => {
    const el = await fixture('track-info', {
      trackTitle: 'X',
      duration: '215000',
    });

    expect(text(el, '.duration')).toBe('03:35');
  });

  it('shows placeholder dashes for an unparseable duration', async () => {
    const el = await fixture('track-info', {
      trackTitle: 'X',
      duration: 'unknown',
    });

    expect(text(el, '.duration')).toBe('--:--');
  });

  it('omits the duration column when there is no duration', async () => {
    const el = await fixture('track-info', { trackTitle: 'X' });

    expect(shadow(el, '.duration')).toBeNull();
  });

  it('shows no cover slot at all unless a cover was supplied', async () => {
    const el = await fixture('track-info', { trackTitle: 'X' });

    expect(shadow(el, '.cover-art')).toBeNull();
  });

  it('prefers the small cover variant, which is what a row needs', async () => {
    const el = await fixture('track-info', {
      trackTitle: 'X',
      coverArt: '/covers/big.jpg',
      coverArtSmall: '/covers/small.jpg',
    });

    expect(shadow<HTMLImageElement>(el, '.cover-art img')?.getAttribute('src')).toBe(
      '/covers/small.jpg',
    );
  });

  it('falls back to the full-size cover when the thumbnail fails to load', async () => {
    const el = await fixture('track-info', {
      trackTitle: 'X',
      coverArt: '/covers/big.jpg',
      coverArtSmall: '/covers/missing.jpg',
    });

    const img = shadow<HTMLImageElement>(el, '.cover-art img');

    img?.dispatchEvent(new Event('error'));

    expect(img?.src).toContain('/covers/big.jpg');
  });

  it('degrades to the music-note placeholder when both covers fail', async () => {
    const el = await fixture('track-info', {
      trackTitle: 'X',
      coverArtSmall: '/covers/missing.jpg',
    });

    const img = shadow<HTMLImageElement>(el, '.cover-art img');

    img?.dispatchEvent(new Event('error'));

    expect(shadow(el, '.cover-placeholder wa-icon')).not.toBeNull();
  });

  it('re-renders when a property changes', async () => {
    const el = await fixture('track-info', { trackTitle: 'Before' });

    await update(el, { trackTitle: 'After' });

    expect(text(el, '.title')).toBe('After');
  });

  it('looks the way it did last time', async () => {
    const el = await fixture('track-info', {
      trackTitle: 'Ashes to Ashes',
      artist: 'David Bowie',
      album: 'Scary Monsters',
      duration: '215000',
    });

    await visual(el, 'track-info');
    expect(el.shadowRoot).not.toBeNull();
  });
});
