import { EventsOn } from '@runtime/runtime';
import {
    GetJobs,
    GetJobLog,
    PauseJob,
    ResumeJob,
    CancelJob,
    DismissJob,
    ClearFinishedJobs,
} from '@go/jobs/Service';
import type { jobs } from '@go/models';
import { Events } from '../events';

export type Job = jobs.Job;
export type JobLogEntry = jobs.LogEntry;
export type JobStage = jobs.Stage;

/** Lifecycle states a job can be in. Mirrors backend/jobs.State. */
export type JobState =
    | 'queued'
    | 'running'
    | 'pausing'
    | 'paused'
    | 'cancelling'
    | 'complete'
    | 'cancelled'
    | 'error';

/** Job kinds. Mirrors backend/jobs.Kind. */
export type JobKind = 'library-scan' | 'index-build' | 'autotag-apply';

type Subscriber = () => void;

/** States meaning the job will not progress further. */
const TERMINAL_STATES: ReadonlySet<string> = new Set([
    'complete',
    'cancelled',
    'error',
]);

/**
 * How long a finished job keeps the indicator visible before it fades
 * out. Without this a fast scan would flash on and off, which reads as
 * a glitch rather than as progress.
 */
const FINISHED_LINGER_MS = 4000;

export function isTerminal(job: Job): boolean {
    return TERMINAL_STATES.has(job.state);
}

export function isActive(job: Job): boolean {
    return !isTerminal(job);
}

/** True when a job's progress bar has no meaningful denominator. */
export function isIndeterminate(job: Job): boolean {
    return !job.total || job.total <= 0;
}

/** Fractional progress in [0, 1], or null when indeterminate. */
export function progressFraction(job: Job): number | null {
    if (isIndeterminate(job)) return null;

    return Math.min(1, Math.max(0, job.current / job.total));
}

/**
 * Reactive singleton mirroring the backend job registry.
 *
 * The backend pushes a full snapshot on every JobsChanged event rather
 * than a delta, so a component that mounts mid-scan is correct from the
 * first event it receives. The initial GetJobs() call only covers the
 * window before the first event arrives.
 */
class JobStore {
    private jobsValue: Job[] = [];

    private logs = new Map<string, JobLogEntry[]>();

    private subscribers = new Set<Subscriber>();

    private notifyScheduled = false;

    /** Timer that clears the lingering "just finished" indicator. */
    private lingerTimer: ReturnType<typeof setTimeout> | null = null;

    /** Set while a finished job should still keep the indicator up. */
    private lingering = false;

    private initialized = false;

    constructor() {
        EventsOn(Events.JobsChanged, (snapshot: Job[]) => {
            this.applySnapshot(snapshot ?? []);
        });
    }

    /**
     * Fetches the current snapshot once. Safe to call from every
     * component's connectedCallback — subsequent calls are no-ops.
     */
    async init(): Promise<void> {
        if (this.initialized) return;

        this.initialized = true;

        try {
            this.applySnapshot((await GetJobs()) ?? []);
        } catch (err) {
            console.error('Failed to load background jobs:', err);
        }
    }

    get jobs(): Job[] {
        return this.jobsValue;
    }

    get activeJobs(): Job[] {
        return this.jobsValue.filter(isActive);
    }

    get finishedJobs(): Job[] {
        return this.jobsValue.filter(isTerminal);
    }

    /** Jobs that are running or queued — excludes paused ones. */
    get workingJobs(): Job[] {
        return this.jobsValue.filter(
            (j) => j.state === 'running' || j.state === 'queued',
        );
    }

    get pausedJobs(): Job[] {
        return this.jobsValue.filter(
            (j) => j.state === 'paused' || j.state === 'pausing',
        );
    }

    get failedJobs(): Job[] {
        return this.jobsValue.filter((j) => j.state === 'error');
    }

    /** Whether anything is in flight, including paused work. */
    get hasActive(): boolean {
        return this.jobsValue.some(isActive);
    }

    /**
     * Whether the persistent indicator should be shown at all: any
     * active job, or a recently finished one still lingering.
     */
    get shouldShowIndicator(): boolean {
        return this.hasActive || this.lingering;
    }

    getJob(id: string): Job | undefined {
        return this.jobsValue.find((j) => j.id === id);
    }

    /**
     * Returns the cached log for a job, fetching it if not yet loaded.
     * Logs are pulled on demand rather than pushed with every snapshot —
     * a scan can emit hundreds of warnings, and only the detail pane
     * ever renders them.
     */
    async loadLog(id: string): Promise<JobLogEntry[]> {
        try {
            const entries = (await GetJobLog(id)) ?? [];
            this.logs.set(id, entries);
            this.notify();

            return entries;
        } catch (err) {
            console.error('Failed to load job log:', err);

            return [];
        }
    }

    /** Cached log entries for a job, or an empty array if unfetched. */
    cachedLog(id: string): JobLogEntry[] {
        return this.logs.get(id) ?? [];
    }

    async pause(id: string): Promise<void> {
        await PauseJob(id);
    }

    async resume(id: string): Promise<void> {
        await ResumeJob(id);
    }

    async cancel(id: string): Promise<void> {
        await CancelJob(id);
    }

    async dismiss(id: string): Promise<void> {
        this.logs.delete(id);
        await DismissJob(id);
    }

    async clearFinished(): Promise<void> {
        for (const job of this.finishedJobs) {
            this.logs.delete(job.id);
        }

        await ClearFinishedJobs();
    }

    subscribe(fn: Subscriber): () => void {
        this.subscribers.add(fn);

        return () => this.subscribers.delete(fn);
    }

    private applySnapshot(snapshot: Job[]): void {
        const hadActive = this.jobsValue.some(isActive);
        this.jobsValue = snapshot;
        const hasActiveNow = this.hasActive;

        // The last active job just finished — keep the indicator up
        // briefly so the completion is actually seen.
        if (hadActive && !hasActiveNow) {
            this.startLinger();
        } else if (hasActiveNow) {
            this.clearLinger();
        }

        // Drop cached logs for jobs the backend has forgotten.
        const known = new Set(snapshot.map((j) => j.id));

        for (const id of this.logs.keys()) {
            if (!known.has(id)) this.logs.delete(id);
        }

        this.notify();
    }

    private startLinger(): void {
        this.lingering = true;

        if (this.lingerTimer) clearTimeout(this.lingerTimer);

        this.lingerTimer = setTimeout(() => {
            this.lingering = false;
            this.lingerTimer = null;
            this.notify();
        }, FINISHED_LINGER_MS);
    }

    private clearLinger(): void {
        this.lingering = false;

        if (this.lingerTimer) {
            clearTimeout(this.lingerTimer);
            this.lingerTimer = null;
        }
    }

    /**
     * Coalesces notifications to one per microtask. The backend already
     * throttles JobsChanged to 4Hz, but a burst of loadLog resolutions
     * can still stack up.
     */
    private notify(): void {
        if (this.notifyScheduled) return;

        this.notifyScheduled = true;

        queueMicrotask(() => {
            this.notifyScheduled = false;
            this.subscribers.forEach((fn) => fn());
        });
    }
}

export const jobStore = new JobStore();
