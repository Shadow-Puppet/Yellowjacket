import { css } from 'lit';
import type { Job, JobLogEntry } from '@store/job-store';
import { isIndeterminate, progressFraction } from '@store/job-store';

/** Icon for a job kind, used in the indicator and job rows. */
export function jobIcon(job: Job): string {
    switch (job.kind) {
        case 'library-scan':
            return 'folder';
        case 'index-build':
            return 'database';
        case 'autotag-apply':
            return 'tags';
        default:
            return 'gear';
    }
}

/** Human-readable label for a job state. */
export function stateLabel(job: Job): string {
    switch (job.state) {
        case 'queued':
            return 'Queued';
        case 'running':
            return 'Running';
        case 'pausing':
            return 'Pausing…';
        case 'paused':
            return 'Paused';
        case 'cancelling':
            return 'Cancelling…';
        case 'complete':
            return 'Complete';
        case 'cancelled':
            return 'Stopped';
        case 'error':
            return 'Failed';
        default:
            return job.state;
    }
}

/**
 * Semantic colour name for a state, mapped to CSS custom properties by
 * the `jobStateStyles` block below.
 */
export function stateTone(
    job: Job,
): 'active' | 'paused' | 'danger' | 'success' | 'muted' {
    switch (job.state) {
        case 'running':
            return 'active';
        case 'pausing':
        case 'paused':
            return 'paused';
        case 'error':
            return 'danger';
        case 'complete':
            return 'success';
        default:
            return 'muted';
    }
}

/** Compact "1,204 / 12,880" progress text, or null when indeterminate. */
export function progressText(job: Job): string | null {
    if (isIndeterminate(job)) return null;

    return `${formatCount(job.current)} / ${formatCount(job.total)}`;
}

/** Progress as a whole-number percentage, or null when indeterminate. */
export function progressPercent(job: Job): number | null {
    const fraction = progressFraction(job);

    return fraction === null ? null : Math.round(fraction * 100);
}

/**
 * The one-line status shown under a job title: phase plus counts, with
 * the state folded in when it is something other than plain running.
 */
export function statusLine(job: Job): string {
    const parts: string[] = [];
    const label = stateLabel(job);

    if (job.state !== 'running' && job.state !== 'queued') {
        parts.push(label);
    }

    // A paused job's phase is often just "Paused", which would render
    // as "Paused · Paused" alongside the state label.
    if (job.phase && job.phase !== label) parts.push(job.phase);

    const progress = progressText(job);

    if (progress && job.state !== 'complete') parts.push(progress);

    if (parts.length === 0) parts.push(stateLabel(job));

    return parts.join(' · ');
}

export function formatCount(n: number): string {
    return n.toLocaleString();
}

/** Wall-clock duration of a job, as "1m 24s". */
export function formatElapsed(job: Job): string {
    const end = job.endedAt && job.endedAt > 0 ? job.endedAt : Date.now();
    const seconds = Math.max(0, Math.round((end - job.startedAt) / 1000));

    if (seconds < 60) return `${seconds}s`;

    const minutes = Math.floor(seconds / 60);
    const remainder = seconds % 60;

    if (minutes < 60) return `${minutes}m ${remainder}s`;

    return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

/** Clock time for a log entry, e.g. "14:03:21". */
export function formatLogTime(entry: JobLogEntry): string {
    return new Date(entry.time).toLocaleTimeString(undefined, {
        hour12: false,
    });
}

/** Renders a job log as plain text for the clipboard. */
export function logToText(job: Job, entries: JobLogEntry[]): string {
    const header = `${job.title} — ${stateLabel(job)}`;
    const lines = entries.map((entry) => {
        const detail = entry.detail ? `  (${entry.detail})` : '';

        return `${formatLogTime(entry)} [${entry.level}] ${entry.message}${detail}`;
    });

    return [header, ...lines].join('\n');
}

/**
 * Shared state-tone colour variables. Include in a component's styles
 * array so `.tone-active`, `.tone-paused` etc. resolve consistently
 * wherever job state is rendered.
 */
export const jobStateStyles = css`
    .tone-active {
        --job-tone: var(--yj-accent, #ffd43b);
    }

    .tone-paused {
        --job-tone: #f5a623;
    }

    .tone-danger {
        --job-tone: var(--yj-error-text, #ff8787);
    }

    .tone-success {
        --job-tone: var(--yj-success-text, #51cf66);
    }

    .tone-muted {
        --job-tone: var(--yj-text-tertiary, #868e96);
    }
`;
