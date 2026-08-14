/**
 * The download store is mostly event-driven refreshes plus a set of
 * pure formatters the downloads list and the picker share — they exist
 * so those two views cannot disagree about what a state is called, and
 * that only holds if both are tested against the same expectations.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import {
  downloadStore,
  isDownloadTerminal,
  stateLabel,
  scorePercent,
  candidateSummary,
  formatBytes,
  type DownloadView,
  type DownloadCandidate,
  type DownloadProvider,
  type Request,
} from '@store/download-store';
import { Events } from '../../src/events';
import {
  emit,
  calls,
  stub,
  flush,
  lastArgs,
  resetHarness,
} from '@test/support/harness';

function view(id: string, state: string): DownloadView {
  return { id, state } as DownloadView;
}

function provider(id: number, enabled: boolean): DownloadProvider {
  return {
    id,
    name: `p${id}`,
    enabled,
    kind: 'test',
  } as unknown as DownloadProvider;
}

// v3's bindings type `state` and `entity` as real enums where v2 typed
// them as strings; the fixture widens both back to their value unions.
type RequestOverrides = Partial<Omit<Request, 'state' | 'entity'>> & {
  state?: `${Request['state']}`;
  entity?: `${Request['entity']}`;
};

function request(overrides: RequestOverrides): Request {
  return {
    id: 1,
    mbid: 'abc',
    entity: 'release-group',
    state: 'wanted',
    ...overrides,
  } as Request;
}

function candidate(overrides: Partial<DownloadCandidate>): DownloadCandidate {
  return { id: 'c', totalSize: 0, origin: '', ...overrides } as DownloadCandidate;
}

describe('download formatters', () => {
  it('treats complete, cancelled and failed as terminal', () => {
    expect(
      ['complete', 'cancelled', 'failed', 'grabbing'].map((s) =>
        isDownloadTerminal(view('d', s)),
      ),
    ).toEqual([true, true, true, false]);
  });

  it('labels every lifecycle state in the user’s terms', () => {
    expect([
      stateLabel('found'),
      stateLabel('grabbing'),
      stateLabel('importing'),
    ]).toEqual(['Waiting for you to choose', 'Downloading', 'Importing']);
  });

  it('passes an unknown state through rather than showing a blank', () => {
    expect(stateLabel('teleporting')).toBe('teleporting');
  });

  it('rounds a score to whole percent', () => {
    expect([scorePercent(0), scorePercent(0.876), scorePercent(1)]).toEqual([
      '0%',
      '88%',
      '100%',
    ]);
  });

  it('scales bytes to the largest unit that fits', () => {
    expect([
      formatBytes(512),
      formatBytes(1024),
      formatBytes(5.5 * 1024 * 1024),
      formatBytes(20 * 1024 * 1024 * 1024),
    ]).toEqual(['512 B', '1.0 KB', '5.5 MB', '20 GB']);
  });

  it('renders no size at all rather than "0 B"', () => {
    expect([formatBytes(0), formatBytes(-1)]).toEqual(['', '']);
  });

  it('summarises a single-format candidate', () => {
    expect(
      candidateSummary(
        candidate({
          files: [
            { isAudio: true, format: 'flac' },
            { isAudio: true, format: 'flac' },
            { isAudio: false, format: 'jpg' },
          ],
          totalSize: 300 * 1024 * 1024,
          origin: 'Example',
        } as Partial<DownloadCandidate>),
      ),
    ).toBe('FLAC · 2 tracks · 300 MB · Example');
  });

  it('flags a mixed-format candidate instead of naming one format', () => {
    expect(
      candidateSummary(
        candidate({
          files: [
            { isAudio: true, format: 'flac' },
            { isAudio: true, format: 'mp3' },
          ],
        } as Partial<DownloadCandidate>),
      ),
    ).toBe('Mixed formats · 2 tracks');
  });

  it('singularises a one-track candidate', () => {
    expect(
      candidateSummary(
        candidate({
          files: [{ isAudio: true, format: 'mp3' }],
        } as Partial<DownloadCandidate>),
      ),
    ).toBe('MP3 · 1 track');
  });

  it('survives a candidate with no file list at all', () => {
    expect(candidateSummary(candidate({}))).toBe('');
  });
});

describe('download store: event-driven refresh', () => {
  beforeEach(async () => {
    stub('download.Service.ListProviders', []);
    stub('download.Service.ListDownloads', []);
    stub('download.Service.ListRequests', []);
    stub('download.Service.ProviderKinds', []);
    await downloadStore.init();
    await flush();
    resetHarness();
    stub('download.Service.ListProviders', [
      provider(1, true),
      provider(2, false),
    ]);
    stub('download.Service.ListDownloads', [
      view('a', 'grabbing'),
      view('b', 'complete'),
    ]);
    stub('download.Service.ListRequests', [
      request({ id: 1, mbid: 'abc', state: 'wanted' }),
      request({ id: 2, mbid: 'def', state: 'satisfied' }),
      request({ id: 3, mbid: 'ghi', entity: 'artist' }),
    ]);
  });

  it('reloads providers when the backend says they changed', async () => {
    emit(Events.DownloadProvidersChanged);
    await flush();

    expect(downloadStore.providers).toHaveLength(2);
  });

  it('reloads downloads on DownloadsChanged', async () => {
    emit(Events.DownloadsChanged);
    await flush();

    expect(downloadStore.downloads.map((d) => d.id)).toEqual(['a', 'b']);
  });

  it('reloads requests, which change without the user doing anything', async () => {
    // A background reconcile pass expands an artist or retires a want,
    // so the list is push-driven rather than fetched on mount.
    emit(Events.RequestsChanged);
    await flush();

    expect(downloadStore.requests).toHaveLength(3);
  });

  it('offers downloading only when a provider is enabled', async () => {
    emit(Events.DownloadProvidersChanged);
    await flush();
    const withEnabled = downloadStore.available;

    stub('download.Service.ListProviders', [provider(2, false)]);
    emit(Events.DownloadProvidersChanged);
    await flush();

    expect([withEnabled, downloadStore.available]).toEqual([true, false]);
  });

  it('separates active downloads from finished ones', async () => {
    emit(Events.DownloadsChanged);
    await flush();

    expect(downloadStore.activeDownloads.map((d) => d.id)).toEqual(['a']);
  });

  it('separates outstanding requests and artist subscriptions', async () => {
    emit(Events.RequestsChanged);
    await flush();

    expect({
      active: downloadStore.activeRequests.map((r) => r.id),
      subscriptions: downloadStore.subscriptions.map((r) => r.id),
    }).toEqual({ active: [1, 3], subscriptions: [3] });
  });

  it('survives a null list, which Go sends when nothing exists', async () => {
    stub('download.Service.ListDownloads', null);
    emit(Events.DownloadsChanged);
    await flush();

    expect(downloadStore.downloads).toEqual([]);
  });

  it('keeps the last good list when a refresh fails', async () => {
    emit(Events.DownloadsChanged);
    await flush();

    stub('download.Service.ListDownloads', () => {
      throw new Error('backend down');
    });
    emit(Events.DownloadsChanged);
    await flush();

    expect(downloadStore.downloads).toHaveLength(2);
  });

  it('coalesces three refreshes into one notification', async () => {
    let notifications = 0;
    const off = downloadStore.subscribe(() => {
      notifications += 1;
    });

    emit(Events.DownloadProvidersChanged);
    emit(Events.DownloadsChanged);
    emit(Events.RequestsChanged);
    await flush();
    off();

    expect(notifications).toBe(1);
  });
});

describe('download store: request lookup', () => {
  beforeEach(async () => {
    stub('download.Service.ListRequests', [
      request({ id: 1, mbid: 'abc-123', state: 'wanted' }),
    ]);
    emit(Events.RequestsChanged);
    await flush();
    resetHarness();
    stub('download.Service.ListRequests', [
      request({ id: 1, mbid: 'abc-123', state: 'wanted' }),
    ]);
  });

  it('answers from the cached list, synchronously enough to render with', () => {
    expect([
      downloadStore.isRequested('abc-123'),
      downloadStore.isRequested('nope'),
    ]).toEqual([true, false]);
  });

  it('normalises case and whitespace in the MBID it is given', () => {
    expect(downloadStore.isRequested('  ABC-123 ')).toBe(true);
  });

  it('returns the request itself for the caller that needs its state', () => {
    expect(downloadStore.requestFor('abc-123')?.id).toBe(1);
  });
});

describe('download store: writes refresh what they changed', () => {
  beforeEach(async () => {
    stub('download.Service.ListProviders', []);
    stub('download.Service.ListDownloads', []);
    stub('download.Service.ListRequests', []);
    await flush();
    resetHarness();
    stub('download.Service.ListProviders', []);
    stub('download.Service.ListDownloads', []);
    stub('download.Service.ListRequests', []);
  });

  it('refreshes providers after adding one', async () => {
    stub('download.Service.AddProvider', 5);

    await expect(
      downloadStore.addProvider('sab', 'Local', { url: 'http://x' }),
    ).resolves.toBe(5);
    expect(calls().map((c) => c.path)).toEqual([
      'download.Service.AddProvider',
      'download.Service.ListProviders',
    ]);
  });

  it('refreshes downloads after picking a candidate', async () => {
    await downloadStore.pick('d1', 'c1');

    expect([
      lastArgs('download.Service.Pick'),
      calls('download.Service.ListDownloads'),
    ]).toEqual([['d1', 'c1'], [{ path: 'download.Service.ListDownloads', args: [50] }]]);
  });

  it('refreshes requests after removing one', async () => {
    await downloadStore.removeRequest(3);

    expect(calls().map((c) => c.path)).toEqual([
      'download.Service.RemoveRequest',
      'download.Service.ListRequests',
    ]);
  });

  it('refreshes both lists after a manual reconcile, since it can start downloads', async () => {
    stub('download.Service.ReconcileRequests', { added: 1 });

    await downloadStore.reconcileRequests();

    expect(calls().map((c) => c.path).sort()).toEqual([
      'download.Service.ListDownloads',
      'download.Service.ListRequests',
      'download.Service.ReconcileRequests',
    ]);
  });

  it('propagates a provider test failure, which is the user’s only clue', async () => {
    stub('download.Service.TestProvider', () => {
      throw new Error('connection refused');
    });

    await expect(downloadStore.testProvider(1)).rejects.toThrow(
      'connection refused',
    );
  });
});
