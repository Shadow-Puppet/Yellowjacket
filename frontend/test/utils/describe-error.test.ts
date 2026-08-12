/**
 * Eight places in this app render a Go error verbatim, so a person is
 * shown `Get "https://musicbrainz.org/ws/2/…": context deadline
 * exceeded` and asked to make something of it (errors.M9).
 *
 * `describeError` is the one map from those strings to a sentence. It
 * is deliberately conservative: it recognises the handful of causes a
 * user can act on and says something generic about everything else,
 * because a wrong guess about a cause is worse than no guess.
 */
import { describe, expect, it } from 'vitest';

import { describeError, explainError } from '@utils/describe-error';

describe('describeError', () => {
  const cases: Array<[label: string, raw: string, expected: RegExp]> = [
    [
      'a timed-out MusicBrainz lookup',
      'Get "https://musicbrainz.org/ws/2/artist": context deadline exceeded',
      /took too long/i,
    ],
    [
      'an http client timeout',
      'Get "https://example.com": net/http: request canceled (Client.Timeout exceeded while awaiting headers)',
      /took too long/i,
    ],
    [
      'a name that does not resolve',
      'Get "https://musicbrainz.org": dial tcp: lookup musicbrainz.org: no such host',
      /connection|offline|reach/i,
    ],
    [
      'a refused connection',
      'Post "http://localhost:8080/api": dial tcp 127.0.0.1:8080: connect: connection refused',
      /connection|offline|reach/i,
    ],
    [
      'a locked database',
      "failed to remove 'Music': sql: database is locked",
      /busy|in use/i,
    ],
    [
      'a file that moved',
      'open /music/gone.mp3: no such file or directory',
      /not be found|moved/i,
    ],
    ['a 404 from a provider', 'unexpected status 404 Not Found', /not be found/i],
    [
      'a file the app may not read',
      'open /music/locked.flac: permission denied',
      /permission/i,
    ],
    [
      'a folder Windows will not open',
      'CreateFile C:\\Music: Access is denied.',
      /permission/i,
    ],
    ['a cancelled operation', 'context canceled', /cancelled|canceled|stopped/i],
    [
      'a disk with nothing left',
      'write /music/a.mp3: no space left on device',
      /space/i,
    ],
  ];

  for (const [label, raw, expected] of cases) {
    it(`describes ${label}`, () => {
      expect(describeError(new Error(raw))).toMatch(expected);
    });
  }

  it('never leaks the raw Go string into the sentence', () => {
    const raw =
      'Get "https://musicbrainz.org/ws/2/artist": context deadline exceeded';

    expect(describeError(new Error(raw))).not.toContain('context deadline');
  });

  it('falls back to something generic rather than guessing', () => {
    const described = describeError(new Error('build plan: 7 of 9 rejected'));

    expect([described.length > 0, described.includes('build plan')]).toEqual([
      true,
      false,
    ]);
  });

  it('accepts the shapes a rejected binding actually produces', () => {
    // Wails rejects with whatever the Go error marshalled to, which is
    // often a bare string and occasionally not a string at all.
    expect([
      describeError('sql: database is locked'),
      describeError(null),
      describeError({ message: 'permission denied' }),
    ]).toEqual([
      describeError(new Error('sql: database is locked')),
      describeError(new Error('')),
      describeError(new Error('permission denied')),
    ]);
  });

  it('takes a caller-supplied fallback for the unrecognised case', () => {
    expect(
      describeError(new Error('build plan: 7 of 9 rejected'), 'Nothing was written.'),
    ).toBe('Nothing was written.');
  });
});

/**
 * Some backend errors are already sentences — the sentinels this app
 * writes for conditions it defined. Those are the most useful thing to
 * show, and dropping them for a generic line would be a regression.
 */
describe('explainError', () => {
  it('repeats a sentinel the backend wrote for a person', () => {
    expect(
      explainError(new Error('a library with that name already exists: "Decoy"')),
    ).toContain('already exists');
  });

  it('does not repeat a wrapped runtime error', () => {
    const described = explainError(
      new Error('could not rename library: sql: database is locked'),
    );

    expect([described.includes('sql:'), /busy/i.test(described)]).toEqual([
      false,
      true,
    ]);
  });

  it('punctuates what it repeats', () => {
    expect(explainError(new Error('no candidate selected'))).toBe(
      'no candidate selected.',
    );
  });
});
