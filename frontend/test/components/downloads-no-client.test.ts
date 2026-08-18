/**
 * What a wanted list says when there is nothing to search with.
 *
 * Wanting something without a download client is a supported thing to
 * do — the list is kept, and it starts moving when a client is added.
 * What was not supported was the app *claiming to be looking*: every
 * pass attempted each request, failed it with "no download clients are
 * enabled", recorded that as an attempt and scheduled a retry, so a row
 * read "Searched 3 times, no download clients are enabled · next check
 * in 6 hours" about a check that could not happen.
 *
 * The backend half is `TestNoProvidersMeansNoAttempt`. This is the row.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/downloads-view/downloads-view';
import { stub, emit, flush, resetHarness } from '@test/support/harness';
import { Events } from '../../src/events';
import { fixture, shadowAll } from '@test/support/render';

/** A request that has been tried and is waiting on a retry — the shape
 *  a list with a client in it produces. */
const WAITING = {
  id: 1,
  mbid: 'rg-1',
  entity: 'release-group',
  libraryId: 1,
  artist: 'Aurora Fields',
  title: 'Glass Harbour',
  state: 'wanted',
  attempts: 3,
  lastError: 'no source has it yet',
  nextTryAt: new Date(Date.now() + 6 * 3600_000).toISOString(),
};

const PROVIDER = {
  id: 1,
  kind: 'slskd',
  name: 'Sound',
  enabled: true,
  priority: 50,
};

/**
 * The download store is a singleton whose `init()` runs once per
 * session, so a second mount does not re-read the provider list. The
 * event is how the app itself learns a client was added, and is what
 * makes this test independent of which case ran first.
 */
async function view(providers: unknown[]): Promise<LitElement> {
  stub('download.Service.ListProviders', providers);

  const el = await fixture<LitElement>('downloads-view');

  emit(Events.DownloadProvidersChanged);
  await flush();
  await el.updateComplete;

  return el;
}

const details = (el: LitElement) =>
  shadowAll(el, '.detail').map((d) => d.textContent!.trim());

describe('a request row with no download client', () => {
  beforeEach(() => {
    resetHarness();
    stub('download.Service.ProviderKinds', []);
    stub('download.Service.ListProviders', []);
    stub('download.Service.ListDownloads', []);
    stub('download.Service.ListRequests', [WAITING]);
  });

  it('does not promise a check that cannot happen', async () => {
    const el = await view([]);

    expect(details(el)).toHaveLength(1);
    expect(details(el)[0]).toBe(
      'On your list — no download client to search with',
    );
    expect(details(el)[0]).not.toMatch(/next check/);
  });

  it('reports the retry schedule again once a client exists', async () => {
    const el = await view([PROVIDER]);

    expect(details(el)[0]).toMatch(/next check/);
  });
});
