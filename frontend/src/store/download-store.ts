import { EventsOn } from '@runtime/runtime';
import {
    AddProvider,
    AddRequest,
    Cancel,
    Candidates,
    ClearFinished,
    ClearSatisfiedRequests,
    DeleteProvider,
    ImportExternalRequests,
    ListDownloads,
    ListProviders,
    ListRequests,
    PauseRequest,
    Pick,
    ProviderKinds,
    ReconcileRequests,
    RemoveRequest,
    StartDownload,
    TestProvider,
    UpdateProvider,
} from '@go/download/service.js';
import type * as download from '@go/download/models.js';
import { Events } from '../events';

export type DownloadCandidate = download.Candidate;
export type DownloadProvider = download.Config;
export type DownloadDescriptor = download.Descriptor;
export type DownloadView = download.DownloadView;
export type ProviderField = download.Field;
export type Request = download.Request;
export type RequestSummary = download.Summary;

/**
 * What a request's MBID names. Mirrors backend/download.Entity — the
 * request list makes no other type distinction, because an MBID plus
 * what it names is the whole of a request.
 */
export type RequestEntity = 'artist' | 'release-group' | 'release' | 'recording';

/**
 * Where a request sits. There is deliberately no "failed": an attempt can
 * fail, a request cannot — something unfindable today is still requested.
 */
export type RequestState = 'wanted' | 'satisfied' | 'paused';

/**
 * How much of an artist's output a subscription covers. 'future' is the
 * default so subscribing does not silently queue a back catalogue.
 */
export type RequestScope = 'future' | 'all';

/** Lifecycle states a download can be in. Mirrors backend/download.State. */
export type DownloadLifecycleState =
    | 'searching'
    | 'found'
    | 'queued'
    | 'grabbing'
    | 'verifying'
    | 'tagging'
    | 'importing'
    | 'complete'
    | 'cancelled'
    | 'failed';

type Subscriber = () => void;

const TERMINAL_STATES: ReadonlySet<string> = new Set([
    'complete',
    'cancelled',
    'failed',
]);

export function isDownloadTerminal(view: DownloadView): boolean {
    return TERMINAL_STATES.has(view.state);
}

/**
 * Human-readable label for a download state. Kept here rather than in the
 * components so the downloads list and the picker never disagree about
 * what a state is called.
 */
export function stateLabel(state: string): string {
    switch (state) {
        case 'searching':
            return 'Searching';
        case 'found':
            return 'Waiting for you to choose';
        case 'queued':
            return 'Queued';
        case 'grabbing':
            return 'Downloading';
        case 'verifying':
            return 'Verifying';
        case 'tagging':
            return 'Tagging';
        case 'importing':
            return 'Importing';
        case 'complete':
            return 'Complete';
        case 'cancelled':
            return 'Cancelled';
        case 'failed':
            return 'Failed';
        default:
            return state;
    }
}

/**
 * Formats a 0..1 score as a percentage for display.
 */
export function scorePercent(score: number): string {
    return `${Math.round(score * 100)}%`;
}

/**
 * Describes why a candidate ranks where it does, in the user's terms.
 *
 * Match and quality are reported separately on purpose: a perfect match
 * at low bitrate and a great-sounding copy of the wrong album are
 * different problems, and only the user knows which they will accept.
 */
export function candidateSummary(candidate: DownloadCandidate): string {
    const audio = (candidate.files ?? []).filter((f) => f.isAudio);
    const formats = new Set(audio.map((f) => f.format).filter(Boolean));

    const parts: string[] = [];

    const [onlyFormat] = [...formats];

    if (formats.size === 1 && onlyFormat) {
        parts.push(onlyFormat.toUpperCase());
    } else if (formats.size > 1) {
        parts.push('Mixed formats');
    }

    if (audio.length > 0) {
        parts.push(`${audio.length} track${audio.length === 1 ? '' : 's'}`);
    }

    if (candidate.totalSize > 0) {
        parts.push(formatBytes(candidate.totalSize));
    }

    if (candidate.origin) {
        parts.push(candidate.origin);
    }

    return parts.join(' · ');
}

export function formatBytes(bytes: number): string {
    if (!bytes || bytes <= 0) return '';

    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let value = bytes;
    let unit = 0;

    while (value >= 1024 && unit < units.length - 1) {
        value /= 1024;
        unit += 1;
    }

    return `${value < 10 && unit > 0 ? value.toFixed(1) : Math.round(value)} ${units[unit]}`;
}

/**
 * Reactive singleton for the download subsystem.
 *
 * Per-transfer progress deliberately does not flow through here — that
 * lives in the jobs registry, which already coalesces high-frequency
 * updates into one event. This store handles the coarse changes: which
 * providers exist, which downloads exist, and what the user is being
 * asked to choose between.
 */
class DownloadStore {
    private providersValue: DownloadProvider[] = [];

    private descriptorsValue: DownloadDescriptor[] = [];

    private downloadsValue: DownloadView[] = [];

    private requestsValue: Request[] = [];

    private subscribers = new Set<Subscriber>();

    private notifyScheduled = false;

    private initialized = false;

    constructor() {
        EventsOn(Events.DownloadProvidersChanged, () => {
            void this.refreshProviders();
        });

        EventsOn(Events.DownloadsChanged, () => {
            void this.refreshDownloads();
        });

        // The request list changes on its own — a background reconcile
        // pass expands an artist, retires something the library gained,
        // or starts a download nobody asked for just now. So it is
        // event-driven rather than fetched once on mount.
        EventsOn(Events.RequestsChanged, () => {
            void this.refreshRequests();
        });
    }

    /**
     * Loads providers, downloads and requests once. Safe to call from
     * every component's connectedCallback — subsequent calls are no-ops.
     */
    async init(): Promise<void> {
        if (this.initialized) return;

        this.initialized = true;

        await Promise.all([
            this.refreshDescriptors(),
            this.refreshProviders(),
            this.refreshDownloads(),
            this.refreshRequests(),
        ]);
    }

    get providers(): DownloadProvider[] {
        return this.providersValue;
    }

    /** Providers the user has switched on. */
    get enabledProviders(): DownloadProvider[] {
        return this.providersValue.filter((p) => p.enabled);
    }

    /** Provider types available to add. */
    get descriptors(): DownloadDescriptor[] {
        return this.descriptorsValue;
    }

    get downloads(): DownloadView[] {
        return this.downloadsValue;
    }

    get activeDownloads(): DownloadView[] {
        return this.downloadsValue.filter((d) => !isDownloadTerminal(d));
    }

    /**
     * True when at least one provider is configured and enabled. The UI
     * uses this to decide whether to offer downloading at all, rather
     * than letting the user start a search that cannot succeed.
     */
    get available(): boolean {
        return this.enabledProviders.length > 0;
    }

    subscribe(callback: Subscriber): () => void {
        this.subscribers.add(callback);

        return () => this.subscribers.delete(callback);
    }

    /**
     * Coalesces notifications into one microtask so a burst of refreshes
     * causes a single render pass.
     */
    private notify(): void {
        if (this.notifyScheduled) return;

        this.notifyScheduled = true;

        queueMicrotask(() => {
            this.notifyScheduled = false;
            this.subscribers.forEach((callback) => callback());
        });
    }

    async refreshDescriptors(): Promise<void> {
        try {
            this.descriptorsValue = (await ProviderKinds()) ?? [];
            this.notify();
        } catch (err) {
            console.error('Failed to load download client types:', err);
        }
    }

    async refreshProviders(): Promise<void> {
        try {
            this.providersValue = (await ListProviders()) ?? [];
            this.notify();
        } catch (err) {
            console.error('Failed to load download clients:', err);
        }
    }

    async refreshDownloads(): Promise<void> {
        try {
            this.downloadsValue = (await ListDownloads(50)) ?? [];
            this.notify();
        } catch (err) {
            console.error('Failed to load downloads:', err);
        }
    }

    // -----------------------------------------------------------------
    // Provider configuration
    // -----------------------------------------------------------------

    async addProvider(
        kind: string,
        name: string,
        settings: Record<string, string>,
    ): Promise<number> {
        const id = await AddProvider(kind, name, settings);

        await this.refreshProviders();

        return id;
    }

    async updateProvider(
        id: number,
        name: string,
        enabled: boolean,
        priority: number,
        settings: Record<string, string>,
    ): Promise<void> {
        await UpdateProvider(id, name, enabled, priority, settings);
        await this.refreshProviders();
    }

    async deleteProvider(id: number): Promise<void> {
        await DeleteProvider(id);
        await this.refreshProviders();
    }

    /**
     * Tests a provider's connection. Resolves on success and rejects
     * with the backend's message, which is what the settings page
     * shows — these errors are the user's main debugging tool for a
     * misconfigured client.
     */
    async testProvider(id: number): Promise<void> {
        await TestProvider(id);
    }

    // -----------------------------------------------------------------
    // Downloads (one search+grab attempt)
    // -----------------------------------------------------------------

    /**
     * Starts a download. Returns the ranked candidates plus whether the
     * pipeline already picked one, so the caller knows whether to open
     * the picker or just show progress.
     */
    async start(request: download.SearchRequest): Promise<download.StartResult> {
        const result = await StartDownload(request);

        await this.refreshDownloads();

        return result;
    }

    async pick(downloadId: string, candidateId: string): Promise<void> {
        await Pick(downloadId, candidateId);
        await this.refreshDownloads();
    }

    async cancel(downloadId: string): Promise<void> {
        await Cancel(downloadId);
        await this.refreshDownloads();
    }

    async candidates(downloadId: string): Promise<DownloadCandidate[]> {
        return (await Candidates(downloadId)) ?? [];
    }

    async clearFinished(): Promise<void> {
        await ClearFinished();
        await this.refreshDownloads();
    }

    // -----------------------------------------------------------------
    // Requests (durable "I asked for this")
    // -----------------------------------------------------------------

    get requests(): Request[] {
        return this.requestsValue;
    }

    /** Requests still being looked for. */
    get activeRequests(): Request[] {
        return this.requestsValue.filter((r) => r.state === 'wanted');
    }

    /** Artist subscriptions, which expand rather than download. */
    get subscriptions(): Request[] {
        return this.requestsValue.filter((r) => r.entity === 'artist');
    }

    async refreshRequests(): Promise<void> {
        try {
            this.requestsValue = (await ListRequests()) ?? [];
            this.notify();
        } catch (err) {
            console.error('Failed to load the requests list:', err);
        }
    }

    /**
     * True when this MBID is already requested.
     *
     * Checked against the locally cached request list rather than the
     * `IsRequested` RPC: the list is already kept current via
     * `RequestsChanged`, and a local lookup keeps this usable
     * synchronously from render — the same shape callers relied on
     * before the rename.
     */
    isRequested(mbid: string): boolean {
        const needle = mbid.trim().toLowerCase();

        return this.requestsValue.some((r) => r.mbid === needle);
    }

    /** The request for an MBID, if one exists. */
    requestFor(mbid: string): Request | undefined {
        const needle = mbid.trim().toLowerCase();

        return this.requestsValue.find((r) => r.mbid === needle);
    }

    async addRequest(request: download.RequestInput): Promise<number> {
        const id = await AddRequest(request);

        await this.refreshRequests();

        return id;
    }

    async removeRequest(id: number): Promise<void> {
        await RemoveRequest(id);
        await this.refreshRequests();
    }

    async pauseRequest(id: number, paused: boolean): Promise<void> {
        await PauseRequest(id, paused);
        await this.refreshRequests();
    }

    async clearSatisfiedRequests(): Promise<void> {
        await ClearSatisfiedRequests();
        await this.refreshRequests();
    }

    /**
     * Runs a reconcile pass now, for the "check now" button. Resolves
     * with what the pass did so the UI can say something concrete
     * rather than just stopping its spinner.
     */
    async reconcileRequests(): Promise<RequestSummary> {
        const summary = await ReconcileRequests();

        await Promise.all([this.refreshRequests(), this.refreshDownloads()]);

        return summary;
    }

    /** Adopts a provider's own list, e.g. Lidarr's monitored artists. */
    async importExternalRequests(
        providerId: number,
        libraryId: number,
    ): Promise<number> {
        const count = await ImportExternalRequests(providerId, libraryId);

        await this.refreshRequests();

        return count;
    }
}

export const downloadStore = new DownloadStore();
