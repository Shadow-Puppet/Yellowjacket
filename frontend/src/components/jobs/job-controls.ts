import { jobStore } from '@store/job-store';

export type JobControlAction = 'pause' | 'resume' | 'cancel' | 'dismiss';

export interface JobControlDetail {
    id: string;
    action: JobControlAction;
}

/**
 * Applies a `job-control` event emitted by a `<job-row>`.
 *
 * Shared by every host that renders job rows — the top-bar popover, the
 * jobs page, and the details drawer — so a control behaves identically
 * wherever it is pressed, and so no host can forget to wire one up.
 */
export async function applyJobControl(e: Event): Promise<void> {
    const { id, action } = (e as CustomEvent).detail as JobControlDetail;

    if (action === 'cancel' && !confirmCancel(id)) return;

    switch (action) {
        case 'pause':
            await jobStore.pause(id);
            break;
        case 'resume':
            await jobStore.resume(id);
            break;
        case 'cancel':
            await jobStore.cancel(id);
            break;
        case 'dismiss':
            await jobStore.dismiss(id);
            break;
    }
}

/**
 * Stopping an index build feels like discarding hours of downloading,
 * so it is worth a confirmation even though the checkpoint survives. A
 * library scan is cheap to re-run — don't nag for that one.
 */
function confirmCancel(id: string): boolean {
    const job = jobStore.getJob(id);

    if (job?.kind !== 'index-build') return true;

    return window.confirm(
        'Stop building the search index?\n\n' +
            'Progress is checkpointed, so you can resume later without ' +
            're-downloading. Until it finishes, search results stay ' +
            'limited to your own library.',
    );
}
