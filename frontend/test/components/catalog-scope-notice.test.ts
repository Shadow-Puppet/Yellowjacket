/**
 * The scope notice exists because an album or artist page looked
 * identical whether it was showing the catalog, a library stand-in, or
 * nothing yet. So the assertions here are about the one thing it must
 * never do — stay silent when the page is not the whole story — and
 * about staying out of the way when it is.
 */
import { describe, expect, it } from 'vitest';

import '@components/catalog-scope-notice/catalog-scope-notice';
import { fixture, shadow, text } from '@test/support/render';

describe('catalog scope notice', () => {
  it('renders nothing at all for full catalog data', async () => {
    const el = await fixture('catalog-scope-notice', { scope: 'catalog' });

    expect(shadow(el, '.notice')).toBeNull();
  });

  it('names the entity it is talking about', async () => {
    const album = await fixture('catalog-scope-notice', {
      scope: 'library',
      entityType: 'album',
    });
    const artist = await fixture('catalog-scope-notice', {
      scope: 'library',
      entityType: 'artist',
    });

    expect(text(album, '.text')).toContain('album');
    expect(text(artist, '.text')).toContain('artist');
  });

  it('distinguishes "still loading" from "this is all there is"', async () => {
    const loading = await fixture('catalog-scope-notice', { scope: 'loading' });
    const library = await fixture('catalog-scope-notice', { scope: 'library' });

    expect(text(loading, '.text')).toContain('load');
    expect(text(library, '.text')).toContain('Library only');
  });

  it('offers a retry only where retrying could change anything', async () => {
    for (const scope of ['loading', 'library']) {
      const el = await fixture('catalog-scope-notice', { scope });

      expect(shadow(el, 'button')).toBeNull();
    }

    const el = await fixture('catalog-scope-notice', { scope: 'unavailable' });

    expect(shadow(el, 'button')).not.toBeNull();
  });

  it('asks its host to retry rather than fetching anything itself', async () => {
    const el = await fixture('catalog-scope-notice', { scope: 'unavailable' });

    let asked = 0;
    el.addEventListener('catalog-retry', () => {
      asked += 1;
    });

    shadow<HTMLElement>(el, 'button')!.click();

    expect(asked).toBe(1);
  });
});
