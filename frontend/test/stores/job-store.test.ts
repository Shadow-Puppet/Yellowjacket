/**
 * The job store mirrors the backend registry from full snapshots, so
 * the derivations on top of it — which jobs count as active, whether
 * the indicator should be up, the linger after the last job finishes —
 * are where the behaviour lives.
 */
import { describe, expect, it, beforeEach, vi, afterEach } from 'vitest';

import {
  jobStore,
  isTerminal,
  isActive,
  isIndeterminate,
  progressFraction,
  type Job,
} from '@store/job-store';
import { Events } from '../../src/events';
import { emit, calls, stub, flush } from '@test/support/harness';

// v3's bindings type `state` and `kind` as real enums where v2 typed
// them as strings, so the fixture widens both back to their value
// unions — which is still literal-checked, just not enum-checked.
type JobOverrides = Partial<Omit<Job, 'state' | 'kind'>> & {
  id: string;
  state?: `${Job['state']}`;
  kind?: `${Job['kind']}`;
};

function job(overrides: JobOverrides): Job {
  return {
    kind: 'library-scan',
    state: 'running',
    title: 'Scanning',
    current: 0,
    total: 0,
    ...overrides,
  } as Job;
}

/** Push a full snapshot, which is all the backend ever sends. */
function snapshot(jobs: Job[]): void {
  emit(Events.JobsChanged, jobs);
}

describe('job predicates', () => {
  it('treats complete, cancelled and error as terminal', () => {
    const states = ['complete', 'cancelled', 'error'] as const;

    expect(
      states.map((state) => isTerminal(job({ id: state, state }))),
    ).toEqual([true, true, true]);
  });

  it('treats every in-flight state, including paused, as active', () => {
    const states = ['queued', 'running', 'pausing', 'paused', 'cancelling'] as const;

    expect(states.map((state) => isActive(job({ id: state, state })))).toEqual([
      true,
      true,
      true,
      true,
      true,
    ]);
  });

  it('calls a job with no denominator indeterminate', () => {
    expect([
      isIndeterminate(job({ id: 'a', total: 0 })),
      isIndeterminate(job({ id: 'b', total: -1 })),
      isIndeterminate(job({ id: 'c', total: 10 })),
    ]).toEqual([true, true, false]);
  });

  it('has no progress fraction when indeterminate', () => {
    expect(progressFraction(job({ id: 'a', total: 0, current: 5 }))).toBeNull();
  });

  it('clamps a progress fraction that overshoots its total', () => {
    expect([
      progressFraction(job({ id: 'a', current: 5, total: 10 })),
      progressFraction(job({ id: 'b', current: 30, total: 10 })),
      progressFraction(job({ id: 'c', current: -5, total: 10 })),
    ]).toEqual([0.5, 1, 0]);
  });
});

describe('job store: snapshots', () => {
  beforeEach(() => {
    snapshot([]);
  });

  it('partitions a snapshot by state', () => {
    snapshot([
      job({ id: 'r', state: 'running' }),
      job({ id: 'q', state: 'queued' }),
      job({ id: 'p', state: 'paused' }),
      job({ id: 'e', state: 'error' }),
      job({ id: 'c', state: 'complete' }),
    ]);

    expect({
      working: jobStore.workingJobs.map((j) => j.id),
      paused: jobStore.pausedJobs.map((j) => j.id),
      failed: jobStore.failedJobs.map((j) => j.id),
      finished: jobStore.finishedJobs.map((j) => j.id),
      active: jobStore.activeJobs.map((j) => j.id),
    }).toEqual({
      working: ['r', 'q'],
      paused: ['p'],
      failed: ['e'],
      finished: ['e', 'c'],
      active: ['r', 'q', 'p'],
    });
  });

  it('tolerates a null snapshot, which Go sends for an empty registry', () => {
    emit(Events.JobsChanged, null);

    expect(jobStore.jobs).toEqual([]);
  });

  it('replaces rather than merges, so a removed job disappears', () => {
    snapshot([job({ id: 'a' }), job({ id: 'b' })]);
    snapshot([job({ id: 'b' })]);

    expect(jobStore.jobs.map((j) => j.id)).toEqual(['b']);
  });

  it('finds a job by id', () => {
    snapshot([job({ id: 'a', title: 'Indexing' })]);

    expect(jobStore.getJob('a')?.title).toBe('Indexing');
  });

  it('fetches the initial snapshot at most once', async () => {
    stub('jobs.Service.GetJobs', [job({ id: 'a' })]);

    await jobStore.init();
    await jobStore.init();

    expect(calls('jobs.Service.GetJobs').length).toBeLessThanOrEqual(1);
  });
});

describe('job store: indicator linger', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    // Emptying the registry is itself "the last job finished", so it
    // starts a linger; run it out before the test begins.
    snapshot([]);
    vi.advanceTimersByTime(4000);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('keeps the indicator up briefly after the last job finishes', () => {
    snapshot([job({ id: 'a', state: 'running' })]);
    snapshot([job({ id: 'a', state: 'complete' })]);

    const immediatelyAfter = jobStore.shouldShowIndicator;

    vi.advanceTimersByTime(4000);

    expect([immediatelyAfter, jobStore.shouldShowIndicator]).toEqual([
      true,
      false,
    ]);
  });

  it('cancels the linger when new work starts', () => {
    snapshot([job({ id: 'a', state: 'running' })]);
    snapshot([job({ id: 'a', state: 'complete' })]);
    snapshot([
      job({ id: 'a', state: 'complete' }),
      job({ id: 'b', state: 'running' }),
    ]);

    vi.advanceTimersByTime(4000);

    // Still up, because b is still working — the linger timer must not
    // hide the indicator out from under it.
    expect(jobStore.shouldShowIndicator).toBe(true);
  });

  it('does not linger for a snapshot that was never active', () => {
    snapshot([job({ id: 'a', state: 'complete' })]);

    expect(jobStore.shouldShowIndicator).toBe(false);
  });
});

describe('job store: logs', () => {
  beforeEach(() => {
    snapshot([]);
  });

  it('is empty until a log is fetched', () => {
    expect(jobStore.cachedLog('a')).toEqual([]);
  });

  it('caches a fetched log', async () => {
    stub('jobs.Service.GetJobLog', [{ level: 'warn', message: 'skipped' }]);
    snapshot([job({ id: 'a' })]);

    await jobStore.loadLog('a');

    expect(jobStore.cachedLog('a')).toHaveLength(1);
  });

  it('returns an empty log rather than throwing when the fetch fails', async () => {
    stub('jobs.Service.GetJobLog', () => {
      throw new Error('gone');
    });

    await expect(jobStore.loadLog('a')).resolves.toEqual([]);
  });

  it('drops cached logs for jobs the backend has forgotten', async () => {
    stub('jobs.Service.GetJobLog', [{ level: 'info', message: 'x' }]);
    snapshot([job({ id: 'a' })]);
    await jobStore.loadLog('a');

    snapshot([job({ id: 'b' })]);

    expect(jobStore.cachedLog('a')).toEqual([]);
  });

  it('forgets a dismissed job log without waiting for the next snapshot', async () => {
    stub('jobs.Service.GetJobLog', [{ level: 'info', message: 'x' }]);
    snapshot([job({ id: 'a' })]);
    await jobStore.loadLog('a');

    await jobStore.dismiss('a');

    expect(jobStore.cachedLog('a')).toEqual([]);
  });
});

describe('job store: controls', () => {
  beforeEach(() => {
    snapshot([]);
  });

  it('forwards each control to its bound method with the job id', async () => {
    await jobStore.pause('a');
    await jobStore.resume('a');
    await jobStore.cancel('a');

    expect(calls().map((c) => [c.path, c.args])).toEqual([
      ['jobs.Service.PauseJob', ['a']],
      ['jobs.Service.ResumeJob', ['a']],
      ['jobs.Service.CancelJob', ['a']],
    ]);
  });

  it('clears logs for every finished job when they are cleared', async () => {
    stub('jobs.Service.GetJobLog', [{ level: 'info', message: 'x' }]);
    snapshot([job({ id: 'a', state: 'complete' })]);
    await jobStore.loadLog('a');

    await jobStore.clearFinished();

    expect(jobStore.cachedLog('a')).toEqual([]);
  });

  it('coalesces a burst of snapshots into one notification', async () => {
    let notifications = 0;
    const off = jobStore.subscribe(() => {
      notifications += 1;
    });

    snapshot([job({ id: 'a', current: 1, total: 10 })]);
    snapshot([job({ id: 'a', current: 2, total: 10 })]);
    snapshot([job({ id: 'a', current: 3, total: 10 })]);
    await flush();
    off();

    expect(notifications).toBe(1);
  });
});
