/**
 * A drag says how much it is carrying.
 *
 * Every drag in the app already did — `createDragImage` is a count and
 * nothing else — except the one with a picture to show instead. An
 * album dragged to the queue put its cover under the cursor and said
 * nothing about how many tracks that was, and an album is 1 track or 30
 * with the same thumbnail either way. The number is the thing the drop
 * is about.
 *
 * A count of 1 draws no badge: "1" over a single cover is noise, and
 * the absence reads unambiguously beside a badge that only ever appears
 * when there is more than one.
 */
import { describe, expect, it, afterEach } from 'vitest';

import {
  createAlbumArtDragImage,
  removeDragImage,
} from '@utils/drag-image';

const made: HTMLElement[] = [];

function dragImage(count?: number): HTMLElement {
  const el =
    count === undefined
      ? createAlbumArtDragImage('data:image/gif;base64,R0lGODlhAQABAAAAACw=')
      : createAlbumArtDragImage(
          'data:image/gif;base64,R0lGODlhAQABAAAAACw=',
          count,
        );

  made.push(el);

  return el;
}

const badge = (el: HTMLElement) =>
  el.querySelector<HTMLElement>('.drag-count-badge');

describe('the album drag image', () => {
  afterEach(() => {
    while (made.length > 0) removeDragImage(made.pop()!);
  });

  it('says how many tracks are being dragged', () => {
    expect(badge(dragImage(12))?.textContent).toBe('12');
  });

  it('says nothing when there is only one track', () => {
    expect(badge(dragImage(1))).toBeNull();
  });

  it('still draws a bare cover for a caller that gives no count', () => {
    // The count is optional so the helper stays usable from a call site
    // that has a cover and no list; it must not badge such a drag "1".
    expect(badge(dragImage())).toBeNull();
  });

  it('keeps the badge inside the cover', () => {
    // setDragImage snapshots the element, and anything outside its box
    // risks being clipped out of that snapshot — while padding the box
    // instead would move the cover away from the cursor.
    const el = dragImage(30);
    const outer = el.getBoundingClientRect();
    const mark = badge(el)!.getBoundingClientRect();

    expect(mark.right).toBeLessThanOrEqual(outer.right);
    expect(mark.top).toBeGreaterThanOrEqual(outer.top);
  });
});
