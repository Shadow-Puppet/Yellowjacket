/**
 * The phantom resolver showed the same library track twice.
 *
 * Its right-hand panel renders two lists one under the other — the
 * scored candidates and the library search results — and a search for
 * the obvious title returns exactly what scoring already found. So the
 * track appeared once with a score and once without, and double-clicking
 * either did the same thing.
 *
 * The second half is what a match *means*: one library file cannot stand
 * in for two unmatched tracks, or applying adds it to the playlist
 * twice. `FindPhantomMatches` has always claimed candidates on the
 * auto-match path; the manual path had no such rule.
 */
import { beforeEach, describe, expect, it } from 'vitest';
import type { LitElement } from 'lit';

import '@components/phantom-resolver/phantom-resolver';
import { flush, stub } from '@test/support/harness';
import { fixture } from '@test/support/render';

interface Candidate {
  FilePath: string;
  Title: string;
  Artist: string;
  Album: string;
  Duration: string;
  Score: number;
}

function candidate(
  path: string,
  title: string,
  score = 0.5,
): Candidate {
  return {
    FilePath: path,
    Title: title,
    Artist: 'An Artist',
    Album: 'An Album',
    Duration: '200000',
    Score: score,
  };
}

const PHANTOM_A = '/music/gone/one.mp3';
const PHANTOM_B = '/music/gone/two.mp3';

/** Mount the dialog with two unmatched tracks and no auto-matches. */
async function open(
  candidates: Candidate[],
  searchResults: Candidate[] = [],
): Promise<HTMLElement & { updateComplete: Promise<unknown> }> {
  stub('playlist.Service.FindPhantomMatches', {
    AutoMatched: [],
    Unmatched: [PHANTOM_A, PHANTOM_B],
  });
  stub('playlist.Service.GetPhantomCandidates', candidates);
  stub('playlist.Service.SearchLibrary', searchResults);

  const el = await fixture<
    LitElement & { show(id: number, tracks: unknown[]): void }
  >('phantom-resolver');

  el.show(1, [
    { FilePath: PHANTOM_A, Title: 'One', Phantom: true },
    { FilePath: PHANTOM_B, Title: 'Two', Phantom: true },
  ]);

  await flush();
  await el.updateComplete;
  await flush();
  await el.updateComplete;

  return el;
}

function candidateRows(el: HTMLElement): HTMLElement[] {
  return [
    ...(el.shadowRoot?.querySelectorAll<HTMLElement>('.candidate-item') ?? []),
  ];
}

/** The dialog renders the file path as each row's `title`. */
function rowPaths(el: HTMLElement): string[] {
  return candidateRows(el).map((r) => r.getAttribute('title') ?? '');
}

describe('the phantom resolver', () => {
  beforeEach(() => {
    stub('playlist.Service.ResolvePhantomTracks', null);
    stub('playlist.Service.RemovePhantomTracks', null);
  });

  it('lists a search result the candidates already show only once', async () => {
    const shared = candidate('/music/have/one.mp3', 'One', 0.7);
    const el = await open([shared], [shared, candidate('/music/have/x.mp3', 'X', 0)]);

    // Type into the search box and let the debounce fire.
    const input = el.shadowRoot?.querySelector<HTMLInputElement>(
      '.search-input',
    );

    expect(input, 'the search box is rendered').toBeTruthy();

    input!.value = 'one';
    input!.dispatchEvent(new InputEvent('input', { bubbles: true }));

    await new Promise((r) => setTimeout(r, 500));
    await flush();
    await el.updateComplete;

    const paths = rowPaths(el);

    expect(paths.filter((p) => p === shared.FilePath)).toHaveLength(1);
    expect(paths).toContain('/music/have/x.mp3');
  });

  it('matches a candidate from the keyboard, not only a double-click', async () => {
    const el = await open([candidate('/music/have/one.mp3', 'One', 0.7)]);
    const row = candidateRows(el)[0];

    expect(row, 'a candidate row is rendered').toBeTruthy();
    expect(row!.getAttribute('role')).toBe('button');
    expect(row!.getAttribute('tabindex')).toBe('0');

    row!.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }),
    );
    await el.updateComplete;

    // Matching the first phantom advances to the second, whose row
    // shows the check mark for the one just confirmed.
    const matched = el.shadowRoot?.querySelectorAll('.phantom-item.matched');

    expect(matched).toHaveLength(1);
  });

  it('will not spend one library file on two unmatched tracks', async () => {
    const only = candidate('/music/have/one.mp3', 'One', 0.7);
    const el = await open([only]);

    candidateRows(el)[0]!.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }),
    );
    await el.updateComplete;
    await flush();
    await el.updateComplete;

    // The second phantom is selected now and offered the same file,
    // which is already standing in for the first.
    const row = candidateRows(el)[0];

    expect(row!.classList.contains('claimed')).toBe(true);
    expect(row!.getAttribute('aria-disabled')).toBe('true');

    row!.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }),
    );
    await el.updateComplete;

    // Still one confirmed match, not two.
    expect(
      el.shadowRoot?.querySelectorAll('.phantom-item.matched'),
    ).toHaveLength(1);
  });

  it('gives the auto-match disclosure a keyboard-reachable control', async () => {
    stub('playlist.Service.FindPhantomMatches', {
      AutoMatched: [
        {
          PhantomPath: PHANTOM_A,
          PhantomTitle: 'One',
          Candidate: candidate('/music/have/one.mp3', 'One', 0.95),
        },
      ],
      Unmatched: [PHANTOM_B],
    });
    stub('playlist.Service.GetPhantomCandidates', []);

    const el = await fixture<
      LitElement & { show(id: number, tracks: unknown[]): void }
    >('phantom-resolver');

    el.show(1, [{ FilePath: PHANTOM_A, Title: 'One', Phantom: true }]);
    await flush();
    await el.updateComplete;

    const header = el.shadowRoot?.querySelector('.auto-match-header');

    expect(header?.tagName).toBe('BUTTON');
    expect(header?.getAttribute('aria-expanded')).toBe('false');

    // aria-controls has to name an element that is in the DOM, so the
    // list renders collapsed rather than not at all.
    const controls = header?.getAttribute('aria-controls');

    expect(controls).toBeTruthy();
    expect(el.shadowRoot?.getElementById(controls!)).toBeTruthy();
  });
});
