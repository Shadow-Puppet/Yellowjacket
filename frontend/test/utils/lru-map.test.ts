/**
 * `perf.M7`/`M8`: the two Explore art caches were unbounded on a view
 * that never unmounts. Measured at twelve searches, a session retained
 * **8.48 MB** and was still climbing 0.7 MB per search, because a cover
 * thumbnail is a ~27 kB base64 data URL and an artist photo is ~128 kB.
 *
 * `LRUMap` is the bound. The behaviours below are the ones the call
 * sites actually depend on — in particular that a *read* is what keeps
 * an entry alive, since the entry being rendered must never be the one
 * evicted, and that `has()` is not a read, because both caches use
 * `has()` to test a negative "already tried, no art" marker.
 */
import { describe, expect, it } from 'vitest';

import { LRUMap } from '@utils/lru-map';

describe('LRUMap', () => {
  it('behaves like a Map below its cap', () => {
    const m = new LRUMap<string, number>(4);

    m.set('a', 1).set('b', 2);

    expect([m.get('a'), m.get('b'), m.get('c'), m.size]).toEqual([
      1, 2, undefined, 2,
    ]);
  });

  it('never exceeds its cap', () => {
    const m = new LRUMap<number, number>(10);

    for (let i = 0; i < 1000; i++) m.set(i, i);

    expect(m.size).toBe(10);
  });

  it('evicts the oldest entry first', () => {
    const m = new LRUMap<string, number>(2);

    m.set('a', 1).set('b', 2).set('c', 3);

    expect([m.get('a'), m.get('b'), m.get('c')]).toEqual([undefined, 2, 3]);
  });

  it('a read renews an entry, so the rendered one survives', () => {
    const m = new LRUMap<string, number>(2);

    m.set('a', 1).set('b', 2);
    m.get('a');
    m.set('c', 3);

    // 'b' was the least recently *used*, even though 'a' was older.
    expect([m.get('a'), m.get('b'), m.get('c')]).toEqual([1, undefined, 3]);
  });

  it('has() does not renew, so a negative marker cannot outrank real art', () => {
    const m = new LRUMap<string, string>(2);

    // '' is the "already attempted, no art" marker both caches store.
    m.set('miss', '').set('art', 'data:…');
    m.has('miss');
    m.set('new', 'data:…');

    expect(m.has('miss')).toBe(false);
    expect(m.get('art')).toBe('data:…');
  });

  it('overwriting an existing key does not grow the map or evict', () => {
    const m = new LRUMap<string, number>(2);

    m.set('a', 1).set('b', 2).set('a', 99);

    expect([m.size, m.get('a'), m.get('b')]).toEqual([2, 99, 2]);
  });

  it('rejects a cap that cannot hold anything', () => {
    expect(() => new LRUMap<string, number>(0)).toThrow(/at least 1/);
  });
});
