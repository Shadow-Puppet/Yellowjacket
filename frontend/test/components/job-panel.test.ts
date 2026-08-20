/**
 * Background work, shown where the work is started (#27).
 *
 * The Jobs tab is gone; each kind's rows now live beside the thing that
 * starts it — scans in Settings → Libraries, index work in Search
 * Index, downloads under the download clients, the autotag apply in the
 * Autotag view. What those four surfaces never had, and what this
 * carries to them, is the *generic* affordances: pause, cancel,
 * "Details", and a finished job you can dismiss.
 *
 * The assertions are about which rows a panel owns and what its buttons
 * do, not about the store — `job-store` already has the snapshot
 * covered, and a panel that renders the right rows for the wrong reason
 * would pass either way.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import '@components/jobs/job-panel';
import { emit, flush, lastArgs, calls, resetHarness, stub } from '@test/support/harness';
import { Events } from '../../src/events';
import { fixture, shadow, shadowAll } from '@test/support/render';
import type { LitElement } from 'lit';

/** A job snapshot entry, with the fields `job-row` actually reads. */
const job = (over: Record<string, unknown> = {}) => ({
  id: 'scan:1',
  kind: 'library-scan',
  title: 'Scanning Music',
  state: 'running',
  current: 3,
  total: 10,
  caps: { pausable: true, cancellable: true },
  stages: null,
  stats: null,
  startedAt: Date.now(),
  updatedAt: Date.now(),
  logCount: 0,
  warnCount: 0,
  errorCount: 0,
  ...over,
});

/** Push a full snapshot, which is what the backend emits. */
async function snapshot(jobs: unknown[]): Promise<void> {
  emit(Events.JobsChanged, jobs);
  await flush();
}

const rows = (el: HTMLElement) => shadowAll(el, 'job-row');

const titles = (el: HTMLElement) =>
  rows(el).map((row) => (row as HTMLElement & { job: { title: string } }).job.title);

describe('<job-panel>', () => {
  beforeEach(async () => {
    resetHarness();
    stub('jobs.Service.GetJobs', []);
    await snapshot([]);
  });

  it('renders only the kinds it was asked for', async () => {
    const el = await fixture<LitElement>('job-panel', {
      kinds: 'index-build,catalog-enrich',
    });

    await snapshot([
      job({ id: 'scan:1', kind: 'library-scan', title: 'Scanning Music' }),
      job({ id: 'idx', kind: 'index-build', title: 'Building the index' }),
      job({ id: 'enrich', kind: 'catalog-enrich', title: 'Filling in artists' }),
    ]);
    await el.updateComplete;

    // The title is inside `job-row`'s own shadow root, so this asks
    // the rows what they are drawing rather than reading the panel's
    // text -- which would pass whether or not a row rendered.
    expect(titles(el)).toEqual(['Building the index', 'Filling in artists']);
  });

  /**
   * An idle panel in four places is four pieces of furniture describing
   * an absence — and `hidden` rather than an empty render, because the
   * host's own margin would otherwise still be spent.
   */
  it('takes up no room when it has nothing to say', async () => {
    const el = await fixture<LitElement>('job-panel', { kinds: 'download' });

    await snapshot([job({ id: 'scan:1', kind: 'library-scan' })]);
    await el.updateComplete;

    expect(el.hidden).toBe(true);
    expect(rows(el)).toHaveLength(0);

    await snapshot([
      job({ id: 'dl:1', kind: 'download', title: 'Downloading Glass Harbour' }),
    ]);
    await el.updateComplete;

    expect(el.hidden).toBe(false);
    expect(rows(el)).toHaveLength(1);
  });

  /**
   * The controls go through `applyJobControl`, which is what carries
   * the index build's "you will discard hours of downloading"
   * confirmation across this move. A host drawing its own buttons would
   * have dropped it silently.
   */
  it('pauses through the shared handler', async () => {
    const el = await fixture<LitElement>('job-panel', { kinds: 'library-scan' });

    await snapshot([job()]);
    await el.updateComplete;

    const row = rows(el)[0]!;

    shadow<HTMLButtonElement>(row, 'button[aria-label^="Pause"]')?.click();
    await flush();

    expect(lastArgs('jobs.Service.PauseJob')).toEqual(['scan:1']);
  });

  /**
   * Cancelling an index build asks first; cancelling a scan does not,
   * because a scan is cheap to re-run. Both answers live in
   * `applyJobControl` and both had to survive the move.
   */
  it('does not ask before cancelling a scan', async () => {
    const el = await fixture<LitElement>('job-panel', { kinds: 'library-scan' });

    await snapshot([job()]);
    await el.updateComplete;

    const row = rows(el)[0]!;

    shadow<HTMLButtonElement>(row, 'button[aria-label^="Stop"]')?.click();
    await flush();

    expect(lastArgs('jobs.Service.CancelJob')).toEqual(['scan:1']);
  });

  /**
   * A finished job is dismissed one at a time. There is deliberately no
   * "Clear finished" here: `ClearFinishedJobs` is global, so a Clear in
   * the Libraries panel would discard the index build's history too.
   */
  it('keeps finished jobs, dismissible one by one', async () => {
    const el = await fixture<LitElement>('job-panel', { kinds: 'library-scan' });

    await snapshot([job({ state: 'complete' })]);
    await el.updateComplete;

    const row = rows(el)[0]!;

    shadow<HTMLButtonElement>(row, 'button[aria-label^="Dismiss"]')?.click();
    await flush();

    expect(lastArgs('jobs.Service.DismissJob')).toEqual(['scan:1']);
    expect(calls('jobs.Service.ClearFinishedJobs')).toHaveLength(0);
  });
});
