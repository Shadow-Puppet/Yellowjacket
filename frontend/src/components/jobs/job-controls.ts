import { jobStore } from '@store/job-store';
import { notificationStore } from '@store/notification-store';
import { describeError } from '@utils/describe-error';

import { confirmAction } from '../confirm-dialog/confirm-dialog';

export type JobControlAction = 'pause' | 'resume' | 'cancel' | 'dismiss';

export interface JobControlDetail {
    id: string;
    action: JobControlAction;
}

const VERB: Record<JobControlAction, string> = {
    pause: 'pause',
    resume: 'resume',
    cancel: 'cancel',
    dismiss: 'dismiss',
};

/**
 * Applies a `job-control` event emitted by a `<job-row>`.
 *
 * Shared by every host that renders job rows — the top-bar popover, the
 * jobs page, and the details drawer — so a control behaves identically
 * wherever it is pressed, and so no host can forget to wire one up.
 *
 * This is used directly as a DOM listener, so its promise is discarded:
 * a rejection has to be caught *here* or it is an unhandled rejection
 * and a button that silently does nothing (errors.M4).
 */
export async function applyJobControl(e: Event): Promise<void> {
    const { id, action } = (e as CustomEvent).detail as JobControlDetail;

    if (action === 'cancel' && !(await confirmCancel(id))) return;

    try {
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
    } catch (err) {
        console.error(`job ${action} failed`, err);

        const job = jobStore.getJob(id);
        const name = job?.title ?? 'that job';

        // Transient: the button visibly did not take, and the next
        // JobsChanged snapshot says what is actually true.
        notificationStore.transient({
            key: `job-${action}`,
            text: `Could not ${VERB[action]} ${name}. ${describeError(err)}`,
            detail: String(err),
        });
    }
}

/**
 * Stopping an index build feels like discarding hours of downloading,
 * so it is worth a confirmation even though the checkpoint survives. A
 * library scan is cheap to re-run — don't nag for that one.
 */
function confirmCancel(id: string): Promise<boolean> {
    const job = jobStore.getJob(id);

    if (job?.kind !== 'index-build') return Promise.resolve(true);

    return confirmAction({
        title: 'Stop building the search index?',
        message:
            'Progress is checkpointed, so you can resume later without ' +
            're-downloading.',
        impact:
            'Until it finishes, search results stay limited to your own ' +
            'library.',
        confirmLabel: 'Stop building',
        danger: true,
    });
}
