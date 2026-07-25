export namespace autotagservice {
	
	export class AlignmentView {
	    localIndex: number;
	    localTitle: string;
	    localLengthMillis: number;
	    candidatePosition: number;
	    candidateDiscNumber: number;
	    candidateTitle: string;
	    candidateMbid: string;
	    candidateLength: number;
	    titleScore: number;
	    lengthDeltaMs: number;
	    trackNumberOk: boolean;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new AlignmentView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.localIndex = source["localIndex"];
	        this.localTitle = source["localTitle"];
	        this.localLengthMillis = source["localLengthMillis"];
	        this.candidatePosition = source["candidatePosition"];
	        this.candidateDiscNumber = source["candidateDiscNumber"];
	        this.candidateTitle = source["candidateTitle"];
	        this.candidateMbid = source["candidateMbid"];
	        this.candidateLength = source["candidateLength"];
	        this.titleScore = source["titleScore"];
	        this.lengthDeltaMs = source["lengthDeltaMs"];
	        this.trackNumberOk = source["trackNumberOk"];
	        this.status = source["status"];
	    }
	}
	export class FailureView {
	    filePath: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new FailureView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filePath = source["filePath"];
	        this.error = source["error"];
	    }
	}
	export class ApplyResultView {
	    groupKey: string;
	    succeeded: number;
	    failed: number;
	    failures: FailureView[];
	
	    static createFrom(source: any = {}) {
	        return new ApplyResultView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.groupKey = source["groupKey"];
	        this.succeeded = source["succeeded"];
	        this.failed = source["failed"];
	        this.failures = this.convertValues(source["failures"], FailureView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ScoreBreakdownView {
	    titleAvg: number;
	    lengthAvg: number;
	    artistFit: number;
	    albumFit: number;
	    trackCountFit: number;
	    releaseMeta: number;
	    evidence: number;
	
	    static createFrom(source: any = {}) {
	        return new ScoreBreakdownView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.titleAvg = source["titleAvg"];
	        this.lengthAvg = source["lengthAvg"];
	        this.artistFit = source["artistFit"];
	        this.albumFit = source["albumFit"];
	        this.trackCountFit = source["trackCountFit"];
	        this.releaseMeta = source["releaseMeta"];
	        this.evidence = source["evidence"];
	    }
	}
	export class CandidateView {
	    releaseMbid: string;
	    releaseGroupMbid: string;
	    title: string;
	    artistCredit: string;
	    date: string;
	    originalDate: string;
	    country: string;
	    status: string;
	    primaryType: string;
	    trackCount: number;
	    score: number;
	    breakdown: ScoreBreakdownView;
	    source: string;
	    provenance: string;
	    coverArtUrl: string;
	    alignments: AlignmentView[];
	
	    static createFrom(source: any = {}) {
	        return new CandidateView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.releaseMbid = source["releaseMbid"];
	        this.releaseGroupMbid = source["releaseGroupMbid"];
	        this.title = source["title"];
	        this.artistCredit = source["artistCredit"];
	        this.date = source["date"];
	        this.originalDate = source["originalDate"];
	        this.country = source["country"];
	        this.status = source["status"];
	        this.primaryType = source["primaryType"];
	        this.trackCount = source["trackCount"];
	        this.score = source["score"];
	        this.breakdown = this.convertValues(source["breakdown"], ScoreBreakdownView);
	        this.source = source["source"];
	        this.provenance = source["provenance"];
	        this.coverArtUrl = source["coverArtUrl"];
	        this.alignments = this.convertValues(source["alignments"], AlignmentView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class LocalTrackView {
	    audioFileId: number;
	    filePath: string;
	    title: string;
	    artist: string;
	    trackNumber: number;
	    discNumber: number;
	    lengthMillis: number;
	    recordingMbid: string;
	
	    static createFrom(source: any = {}) {
	        return new LocalTrackView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.audioFileId = source["audioFileId"];
	        this.filePath = source["filePath"];
	        this.title = source["title"];
	        this.artist = source["artist"];
	        this.trackNumber = source["trackNumber"];
	        this.discNumber = source["discNumber"];
	        this.lengthMillis = source["lengthMillis"];
	        this.recordingMbid = source["recordingMbid"];
	    }
	}
	export class PendingItem {
	    groupKey: string;
	    libraryId: number;
	    libraryName: string;
	    folderSubPath: string;
	    trackCount: number;
	    albumName: string;
	    albumArtist: string;
	    discNumber: number;
	    bestMatchReleaseMbid: string;
	    score: number;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new PendingItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.groupKey = source["groupKey"];
	        this.libraryId = source["libraryId"];
	        this.libraryName = source["libraryName"];
	        this.folderSubPath = source["folderSubPath"];
	        this.trackCount = source["trackCount"];
	        this.albumName = source["albumName"];
	        this.albumArtist = source["albumArtist"];
	        this.discNumber = source["discNumber"];
	        this.bestMatchReleaseMbid = source["bestMatchReleaseMbid"];
	        this.score = source["score"];
	        this.status = source["status"];
	    }
	}
	
	export class ScoreView {
	    groupKey: string;
	    localTracks: LocalTrackView[];
	    candidates: CandidateView[];
	    recommendation: string;
	
	    static createFrom(source: any = {}) {
	        return new ScoreView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.groupKey = source["groupKey"];
	        this.localTracks = this.convertValues(source["localTracks"], LocalTrackView);
	        this.candidates = this.convertValues(source["candidates"], CandidateView);
	        this.recommendation = source["recommendation"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SearchHitView {
	    mbid: string;
	    kind: string;
	    title: string;
	    artist: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new SearchHitView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mbid = source["mbid"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.artist = source["artist"];
	        this.detail = source["detail"];
	    }
	}

}

export namespace explore {
	
	export class TierStatus {
	    name: string;
	    state: string;
	    total: number;
	    completed: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new TierStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.state = source["state"];
	        this.total = source["total"];
	        this.completed = source["completed"];
	        this.error = source["error"];
	    }
	}
	export class IndexStatus {
	    building: boolean;
	    ready: boolean;
	    lastBuilt?: string;
	    tiers: TierStatus[];
	    artists: number;
	    recordings: number;
	    releaseGroups: number;
	    totalRows: number;
	
	    static createFrom(source: any = {}) {
	        return new IndexStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.building = source["building"];
	        this.ready = source["ready"];
	        this.lastBuilt = source["lastBuilt"];
	        this.tiers = this.convertValues(source["tiers"], TierStatus);
	        this.artists = source["artists"];
	        this.recordings = source["recordings"];
	        this.releaseGroups = source["releaseGroups"];
	        this.totalRows = source["totalRows"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LBSimilarArtist {
	    artistMbid: string;
	    name: string;
	    score: number;
	
	    static createFrom(source: any = {}) {
	        return new LBSimilarArtist(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.artistMbid = source["artistMbid"];
	        this.name = source["name"];
	        this.score = source["score"];
	    }
	}
	export class LBTopRecording {
	    recordingMbid: string;
	    artistName: string;
	    trackName: string;
	    totalListenCount: number;
	    caaReleaseMbid: string;
	    releaseGroupMbid?: string;
	    releaseName: string;
	    length: number;
	    inLibrary: boolean;
	    localId?: number;
	
	    static createFrom(source: any = {}) {
	        return new LBTopRecording(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.recordingMbid = source["recordingMbid"];
	        this.artistName = source["artistName"];
	        this.trackName = source["trackName"];
	        this.totalListenCount = source["totalListenCount"];
	        this.caaReleaseMbid = source["caaReleaseMbid"];
	        this.releaseGroupMbid = source["releaseGroupMbid"];
	        this.releaseName = source["releaseName"];
	        this.length = source["length"];
	        this.inLibrary = source["inLibrary"];
	        this.localId = source["localId"];
	    }
	}
	export class LBTopReleaseGroup {
	    releaseGroupMbid: string;
	    title: string;
	    artistName: string;
	    type: string;
	    date: string;
	    totalListenCount: number;
	    caaReleaseMbid: string;
	    inLibrary: boolean;
	    localId?: number;
	
	    static createFrom(source: any = {}) {
	        return new LBTopReleaseGroup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.releaseGroupMbid = source["releaseGroupMbid"];
	        this.title = source["title"];
	        this.artistName = source["artistName"];
	        this.type = source["type"];
	        this.date = source["date"];
	        this.totalListenCount = source["totalListenCount"];
	        this.caaReleaseMbid = source["caaReleaseMbid"];
	        this.inLibrary = source["inLibrary"];
	        this.localId = source["localId"];
	    }
	}
	export class LyricsResult {
	    recordingId: number;
	    filePath: string;
	    lengthMs: number;
	    title: string;
	    artist: string;
	    album: string;
	
	    static createFrom(source: any = {}) {
	        return new LyricsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.recordingId = source["recordingId"];
	        this.filePath = source["filePath"];
	        this.lengthMs = source["lengthMs"];
	        this.title = source["title"];
	        this.artist = source["artist"];
	        this.album = source["album"];
	    }
	}
	export class MBArtist {
	    mbid: string;
	    name: string;
	    sortName: string;
	    englishName?: string;
	    type: string;
	    country: string;
	    disambiguation: string;
	    score: number;
	    popularity: number;
	    listenerCount: number;
	    inLibrary: boolean;
	    localId?: number;
	
	    static createFrom(source: any = {}) {
	        return new MBArtist(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mbid = source["mbid"];
	        this.name = source["name"];
	        this.sortName = source["sortName"];
	        this.englishName = source["englishName"];
	        this.type = source["type"];
	        this.country = source["country"];
	        this.disambiguation = source["disambiguation"];
	        this.score = source["score"];
	        this.popularity = source["popularity"];
	        this.listenerCount = source["listenerCount"];
	        this.inLibrary = source["inLibrary"];
	        this.localId = source["localId"];
	    }
	}
	export class MBRecording {
	    mbid: string;
	    title: string;
	    length: number;
	    artistCredit: string;
	    artistMbid?: string;
	    score: number;
	    popularity: number;
	    listenerCount: number;
	    caaReleaseMbid?: string;
	    releaseGroupMbid?: string;
	    releaseName?: string;
	    inLibrary: boolean;
	    localId?: number;
	
	    static createFrom(source: any = {}) {
	        return new MBRecording(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mbid = source["mbid"];
	        this.title = source["title"];
	        this.length = source["length"];
	        this.artistCredit = source["artistCredit"];
	        this.artistMbid = source["artistMbid"];
	        this.score = source["score"];
	        this.popularity = source["popularity"];
	        this.listenerCount = source["listenerCount"];
	        this.caaReleaseMbid = source["caaReleaseMbid"];
	        this.releaseGroupMbid = source["releaseGroupMbid"];
	        this.releaseName = source["releaseName"];
	        this.inLibrary = source["inLibrary"];
	        this.localId = source["localId"];
	    }
	}
	export class MBTrack {
	    position: number;
	    discNumber: number;
	    title: string;
	    length: number;
	    mbid: string;
	    inLibrary: boolean;
	    localId?: number;
	
	    static createFrom(source: any = {}) {
	        return new MBTrack(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.position = source["position"];
	        this.discNumber = source["discNumber"];
	        this.title = source["title"];
	        this.length = source["length"];
	        this.mbid = source["mbid"];
	        this.inLibrary = source["inLibrary"];
	        this.localId = source["localId"];
	    }
	}
	export class MBRelease {
	    mbid: string;
	    title: string;
	    date: string;
	    country: string;
	    status: string;
	    artistCredit?: string;
	    tracks?: MBTrack[];
	
	    static createFrom(source: any = {}) {
	        return new MBRelease(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mbid = source["mbid"];
	        this.title = source["title"];
	        this.date = source["date"];
	        this.country = source["country"];
	        this.status = source["status"];
	        this.artistCredit = source["artistCredit"];
	        this.tracks = this.convertValues(source["tracks"], MBTrack);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MBReleaseGroup {
	    mbid: string;
	    title: string;
	    primaryType: string;
	    secondaryTypes?: string[];
	    firstReleaseDate: string;
	    artistCredit: string;
	    artistMbid?: string;
	    popularity: number;
	    listenerCount: number;
	    inLibrary: boolean;
	    localId?: number;
	
	    static createFrom(source: any = {}) {
	        return new MBReleaseGroup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mbid = source["mbid"];
	        this.title = source["title"];
	        this.primaryType = source["primaryType"];
	        this.secondaryTypes = source["secondaryTypes"];
	        this.firstReleaseDate = source["firstReleaseDate"];
	        this.artistCredit = source["artistCredit"];
	        this.artistMbid = source["artistMbid"];
	        this.popularity = source["popularity"];
	        this.listenerCount = source["listenerCount"];
	        this.inLibrary = source["inLibrary"];
	        this.localId = source["localId"];
	    }
	}
	export class TopResult {
	    entityType: string;
	    mbid: string;
	    name: string;
	    artistCredit?: string;
	    artistMbid?: string;
	    intentScore: number;
	    artistType?: string;
	    country?: string;
	    primaryType?: string;
	    year?: string;
	    length?: number;
	    caaReleaseMbid?: string;
	    releaseGroupMbid?: string;
	    releaseName?: string;
	    inLibrary: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TopResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entityType = source["entityType"];
	        this.mbid = source["mbid"];
	        this.name = source["name"];
	        this.artistCredit = source["artistCredit"];
	        this.artistMbid = source["artistMbid"];
	        this.intentScore = source["intentScore"];
	        this.artistType = source["artistType"];
	        this.country = source["country"];
	        this.primaryType = source["primaryType"];
	        this.year = source["year"];
	        this.length = source["length"];
	        this.caaReleaseMbid = source["caaReleaseMbid"];
	        this.releaseGroupMbid = source["releaseGroupMbid"];
	        this.releaseName = source["releaseName"];
	        this.inLibrary = source["inLibrary"];
	    }
	}
	export class MBSearchResult {
	    artists?: MBArtist[];
	    releaseGroups?: MBReleaseGroup[];
	    recordings?: MBRecording[];
	    topResults?: TopResult[];
	
	    static createFrom(source: any = {}) {
	        return new MBSearchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.artists = this.convertValues(source["artists"], MBArtist);
	        this.releaseGroups = this.convertValues(source["releaseGroups"], MBReleaseGroup);
	        this.recordings = this.convertValues(source["recordings"], MBRecording);
	        this.topResults = this.convertValues(source["topResults"], TopResult);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class MusicBrainzClient {
	
	
	    static createFrom(source: any = {}) {
	        return new MusicBrainzClient(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class RateLimiter {
	
	
	    static createFrom(source: any = {}) {
	        return new RateLimiter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class ThumbnailRequest {
	    mbid: string;
	    albumName: string;
	    artistName: string;
	
	    static createFrom(source: any = {}) {
	        return new ThumbnailRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mbid = source["mbid"];
	        this.albumName = source["albumName"];
	        this.artistName = source["artistName"];
	    }
	}
	
	
	export class TrackLyrics {
	    plain: string;
	    synced: string;
	    instrumental: boolean;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new TrackLyrics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.plain = source["plain"];
	        this.synced = source["synced"];
	        this.instrumental = source["instrumental"];
	        this.source = source["source"];
	    }
	}
	export class TrackThumbnailRequest {
	    key: string;
	    releaseMbid: string;
	    releaseGroupMbid: string;
	    albumName: string;
	    artistName: string;
	
	    static createFrom(source: any = {}) {
	        return new TrackThumbnailRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.releaseMbid = source["releaseMbid"];
	        this.releaseGroupMbid = source["releaseGroupMbid"];
	        this.albumName = source["albumName"];
	        this.artistName = source["artistName"];
	    }
	}

}

export namespace jobs {
	
	export class Caps {
	    pausable: boolean;
	    cancellable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Caps(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pausable = source["pausable"];
	        this.cancellable = source["cancellable"];
	    }
	}
	export class Stat {
	    label: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new Stat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.value = source["value"];
	    }
	}
	export class Stage {
	    name: string;
	    state: string;
	    current: number;
	    total: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new Stage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.state = source["state"];
	        this.current = source["current"];
	        this.total = source["total"];
	        this.error = source["error"];
	    }
	}
	export class Job {
	    id: string;
	    kind: string;
	    title: string;
	    subtitle?: string;
	    state: string;
	    phase?: string;
	    current: number;
	    total: number;
	    caps: Caps;
	    stages: Stage[];
	    stats: Stat[];
	    error?: string;
	    startedAt: number;
	    updatedAt: number;
	    endedAt?: number;
	    logCount: number;
	    warnCount: number;
	    errorCount: number;
	
	    static createFrom(source: any = {}) {
	        return new Job(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.subtitle = source["subtitle"];
	        this.state = source["state"];
	        this.phase = source["phase"];
	        this.current = source["current"];
	        this.total = source["total"];
	        this.caps = this.convertValues(source["caps"], Caps);
	        this.stages = this.convertValues(source["stages"], Stage);
	        this.stats = this.convertValues(source["stats"], Stat);
	        this.error = source["error"];
	        this.startedAt = source["startedAt"];
	        this.updatedAt = source["updatedAt"];
	        this.endedAt = source["endedAt"];
	        this.logCount = source["logCount"];
	        this.warnCount = source["warnCount"];
	        this.errorCount = source["errorCount"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LogEntry {
	    time: number;
	    level: string;
	    message: string;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new LogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.level = source["level"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	    }
	}
	export class Registry {
	
	
	    static createFrom(source: any = {}) {
	        return new Registry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	

}

export namespace library {
	
	export class Album {
	    ID: number;
	    Name: string;
	    ArtistName: string;
	    ArtistMBID: string;
	    MBID: string;
	    CoverArtPath: string;
	    CoverArtSmall: string;
	    CoverArtMedium: string;
	    CoverArtLarge: string;
	    Year: number;
	    ReleaseYear: number;
	
	    static createFrom(source: any = {}) {
	        return new Album(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.ArtistName = source["ArtistName"];
	        this.ArtistMBID = source["ArtistMBID"];
	        this.MBID = source["MBID"];
	        this.CoverArtPath = source["CoverArtPath"];
	        this.CoverArtSmall = source["CoverArtSmall"];
	        this.CoverArtMedium = source["CoverArtMedium"];
	        this.CoverArtLarge = source["CoverArtLarge"];
	        this.Year = source["Year"];
	        this.ReleaseYear = source["ReleaseYear"];
	    }
	}
	export class Artist {
	    ID: number;
	    Name: string;
	    MBID: string;
	    ImageSmall: string;
	    ImageMedium: string;
	    ImageLarge: string;
	
	    static createFrom(source: any = {}) {
	        return new Artist(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.MBID = source["MBID"];
	        this.ImageSmall = source["ImageSmall"];
	        this.ImageMedium = source["ImageMedium"];
	        this.ImageLarge = source["ImageLarge"];
	    }
	}
	export class GenreWithCount {
	    Name: string;
	    TrackCount: number;
	
	    static createFrom(source: any = {}) {
	        return new GenreWithCount(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.TrackCount = source["TrackCount"];
	    }
	}
	export class Info {
	    id: number;
	    name: string;
	    path: string;
	    trackCount: number;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.trackCount = source["trackCount"];
	    }
	}
	export class RemovalHooks {
	
	
	    static createFrom(source: any = {}) {
	        return new RemovalHooks(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class RemovalImpact {
	    trackCount: number;
	    playlistsAffected: number;
	    queueItemCount: number;
	
	    static createFrom(source: any = {}) {
	        return new RemovalImpact(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trackCount = source["trackCount"];
	        this.playlistsAffected = source["playlistsAffected"];
	        this.queueItemCount = source["queueItemCount"];
	    }
	}
	export class RemovalSummary {
	    tracksDeleted: number;
	    artistsRemoved: number;
	    albumsRemoved: number;
	    genresRemoved: number;
	    playlistsAffected: number;
	    queueItemsRemoved: number;
	
	    static createFrom(source: any = {}) {
	        return new RemovalSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tracksDeleted = source["tracksDeleted"];
	        this.artistsRemoved = source["artistsRemoved"];
	        this.albumsRemoved = source["albumsRemoved"];
	        this.genresRemoved = source["genresRemoved"];
	        this.playlistsAffected = source["playlistsAffected"];
	        this.queueItemsRemoved = source["queueItemsRemoved"];
	    }
	}
	export class RescanHooks {
	
	
	    static createFrom(source: any = {}) {
	        return new RescanHooks(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class ScanHooks {
	
	
	    static createFrom(source: any = {}) {
	        return new ScanHooks(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class ScanWarning {
	    filePath: string;
	    phase: string;
	    err: string;
	
	    static createFrom(source: any = {}) {
	        return new ScanWarning(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filePath = source["filePath"];
	        this.phase = source["phase"];
	        this.err = source["err"];
	    }
	}
	export class ScanMetrics {
	    total: number;
	    loadExisting: number;
	    walkDuration: number;
	    extractionWallClock: number;
	    dbWritesWallClock: number;
	    orphanCleanup: number;
	    postScanVariants: number;
	    formatExtraction: Record<string, number>;
	    formatCount: Record<string, number>;
	    tagExtraction: number;
	    durationExtraction: number;
	    batchCommits: number;
	    coverArtSave: number;
	    thumbnailWallClock: number;
	    thumbnailGeneration: number;
	    thumbnailSmall: number;
	    thumbnailMedium: number;
	    thumbnailLarge: number;
	    clearQueue: number;
	    clearDatabase: number;
	    clearCoverFiles: number;
	    added: number;
	    updated: number;
	    skipped: number;
	    removed: number;
	    cancelled: boolean;
	    libraryId: number;
	    libraryName: string;
	    warnings: ScanWarning[];
	
	    static createFrom(source: any = {}) {
	        return new ScanMetrics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.loadExisting = source["loadExisting"];
	        this.walkDuration = source["walkDuration"];
	        this.extractionWallClock = source["extractionWallClock"];
	        this.dbWritesWallClock = source["dbWritesWallClock"];
	        this.orphanCleanup = source["orphanCleanup"];
	        this.postScanVariants = source["postScanVariants"];
	        this.formatExtraction = source["formatExtraction"];
	        this.formatCount = source["formatCount"];
	        this.tagExtraction = source["tagExtraction"];
	        this.durationExtraction = source["durationExtraction"];
	        this.batchCommits = source["batchCommits"];
	        this.coverArtSave = source["coverArtSave"];
	        this.thumbnailWallClock = source["thumbnailWallClock"];
	        this.thumbnailGeneration = source["thumbnailGeneration"];
	        this.thumbnailSmall = source["thumbnailSmall"];
	        this.thumbnailMedium = source["thumbnailMedium"];
	        this.thumbnailLarge = source["thumbnailLarge"];
	        this.clearQueue = source["clearQueue"];
	        this.clearDatabase = source["clearDatabase"];
	        this.clearCoverFiles = source["clearCoverFiles"];
	        this.added = source["added"];
	        this.updated = source["updated"];
	        this.skipped = source["skipped"];
	        this.removed = source["removed"];
	        this.cancelled = source["cancelled"];
	        this.libraryId = source["libraryId"];
	        this.libraryName = source["libraryName"];
	        this.warnings = this.convertValues(source["warnings"], ScanWarning);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class Track {
	    TrackName: string;
	    ArtistName: string;
	    TrackLength: string;
	    FilePath: string;
	    TrackNumber: number;
	    DiscNumber: number;
	    Album: string;
	    Genre: string[];
	    Year: number;
	    Composer: string;
	    FileType: string;
	    SampleRate: number;
	    BitDepth: number;
	    Channels: number;
	    Bitrate: number;
	    FileSize: number;
	    PlayCount: number;
	    LastPlayed: string;
	    RecordingMBID: string;
	    ArtistMBID: string;
	    ReleaseGroupMBID: string;
	    CoverArtPath: string;
	    CoverArtSmall: string;
	    CoverArtMedium: string;
	    CoverArtLarge: string;
	
	    static createFrom(source: any = {}) {
	        return new Track(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.TrackName = source["TrackName"];
	        this.ArtistName = source["ArtistName"];
	        this.TrackLength = source["TrackLength"];
	        this.FilePath = source["FilePath"];
	        this.TrackNumber = source["TrackNumber"];
	        this.DiscNumber = source["DiscNumber"];
	        this.Album = source["Album"];
	        this.Genre = source["Genre"];
	        this.Year = source["Year"];
	        this.Composer = source["Composer"];
	        this.FileType = source["FileType"];
	        this.SampleRate = source["SampleRate"];
	        this.BitDepth = source["BitDepth"];
	        this.Channels = source["Channels"];
	        this.Bitrate = source["Bitrate"];
	        this.FileSize = source["FileSize"];
	        this.PlayCount = source["PlayCount"];
	        this.LastPlayed = source["LastPlayed"];
	        this.RecordingMBID = source["RecordingMBID"];
	        this.ArtistMBID = source["ArtistMBID"];
	        this.ReleaseGroupMBID = source["ReleaseGroupMBID"];
	        this.CoverArtPath = source["CoverArtPath"];
	        this.CoverArtSmall = source["CoverArtSmall"];
	        this.CoverArtMedium = source["CoverArtMedium"];
	        this.CoverArtLarge = source["CoverArtLarge"];
	    }
	}
	export class TrackMBIDs {
	    recordingMbid: string;
	    releaseGroupMbid: string;
	    artistMbid: string;
	
	    static createFrom(source: any = {}) {
	        return new TrackMBIDs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.recordingMbid = source["recordingMbid"];
	        this.releaseGroupMbid = source["releaseGroupMbid"];
	        this.artistMbid = source["artistMbid"];
	    }
	}

}

export namespace player {
	
	export class TrackInfo {
	    fileName: string;
	    filePath: string;
	    state: string;
	    title: string;
	    artist: string;
	    album: string;
	    coverArt: string;
	    coverArtSmall: string;
	    coverArtMedium: string;
	    coverArtLarge: string;
	    trackLength: number;
	    seekPosition: number;
	    trackChangeId: number;
	    artistMbid: string;
	    releaseGroupMbid: string;
	    recordingMbid: string;
	
	    static createFrom(source: any = {}) {
	        return new TrackInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fileName = source["fileName"];
	        this.filePath = source["filePath"];
	        this.state = source["state"];
	        this.title = source["title"];
	        this.artist = source["artist"];
	        this.album = source["album"];
	        this.coverArt = source["coverArt"];
	        this.coverArtSmall = source["coverArtSmall"];
	        this.coverArtMedium = source["coverArtMedium"];
	        this.coverArtLarge = source["coverArtLarge"];
	        this.trackLength = source["trackLength"];
	        this.seekPosition = source["seekPosition"];
	        this.trackChangeId = source["trackChangeId"];
	        this.artistMbid = source["artistMbid"];
	        this.releaseGroupMbid = source["releaseGroupMbid"];
	        this.recordingMbid = source["recordingMbid"];
	    }
	}

}

export namespace playlist {
	
	export class CandidateTrack {
	    FilePath: string;
	    Title: string;
	    Artist: string;
	    Album: string;
	    Duration: string;
	    Score: number;
	
	    static createFrom(source: any = {}) {
	        return new CandidateTrack(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.FilePath = source["FilePath"];
	        this.Title = source["Title"];
	        this.Artist = source["Artist"];
	        this.Album = source["Album"];
	        this.Duration = source["Duration"];
	        this.Score = source["Score"];
	    }
	}
	export class DuplicateTrackInfo {
	    FilePath: string;
	    Title: string;
	    Artist: string;
	    Album: string;
	    Duration: string;
	
	    static createFrom(source: any = {}) {
	        return new DuplicateTrackInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.FilePath = source["FilePath"];
	        this.Title = source["Title"];
	        this.Artist = source["Artist"];
	        this.Album = source["Album"];
	        this.Duration = source["Duration"];
	    }
	}
	export class DuplicateCheckResult {
	    Duplicates: DuplicateTrackInfo[];
	    Unique: string[];
	
	    static createFrom(source: any = {}) {
	        return new DuplicateCheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Duplicates = this.convertValues(source["Duplicates"], DuplicateTrackInfo);
	        this.Unique = source["Unique"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class PhantomMatch {
	    PhantomPath: string;
	    PhantomTitle: string;
	    Candidate: CandidateTrack;
	
	    static createFrom(source: any = {}) {
	        return new PhantomMatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.PhantomPath = source["PhantomPath"];
	        this.PhantomTitle = source["PhantomTitle"];
	        this.Candidate = this.convertValues(source["Candidate"], CandidateTrack);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PhantomSearchResult {
	    AutoMatched: PhantomMatch[];
	    Unmatched: string[];
	
	    static createFrom(source: any = {}) {
	        return new PhantomSearchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.AutoMatched = this.convertValues(source["AutoMatched"], PhantomMatch);
	        this.Unmatched = source["Unmatched"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Summary {
	    ID: number;
	    Name: string;
	    CreatedAt: string;
	    UpdatedAt: string;
	    IsSmart: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Summary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.CreatedAt = source["CreatedAt"];
	        this.UpdatedAt = source["UpdatedAt"];
	        this.IsSmart = source["IsSmart"];
	    }
	}
	export class Track {
	    ID: number;
	    Position: number;
	    FilePath: string;
	    Title: string;
	    Artist: string;
	    Album: string;
	    CoverArtPath: string;
	    CoverArtSmall: string;
	    CoverArtMedium: string;
	    CoverArtLarge: string;
	    Duration: string;
	    Phantom: boolean;
	    ArtistMBID: string;
	    ReleaseGroupMBID: string;
	    RecordingMBID: string;
	
	    static createFrom(source: any = {}) {
	        return new Track(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Position = source["Position"];
	        this.FilePath = source["FilePath"];
	        this.Title = source["Title"];
	        this.Artist = source["Artist"];
	        this.Album = source["Album"];
	        this.CoverArtPath = source["CoverArtPath"];
	        this.CoverArtSmall = source["CoverArtSmall"];
	        this.CoverArtMedium = source["CoverArtMedium"];
	        this.CoverArtLarge = source["CoverArtLarge"];
	        this.Duration = source["Duration"];
	        this.Phantom = source["Phantom"];
	        this.ArtistMBID = source["ArtistMBID"];
	        this.ReleaseGroupMBID = source["ReleaseGroupMBID"];
	        this.RecordingMBID = source["RecordingMBID"];
	    }
	}
	export class WithTracks {
	    Summary: Summary;
	    Tracks: Track[];
	
	    static createFrom(source: any = {}) {
	        return new WithTracks(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Summary = this.convertValues(source["Summary"], Summary);
	        this.Tracks = this.convertValues(source["Tracks"], Track);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace queue {
	
	export class Track {
	    id: number;
	    audioFileId: number;
	    filePath: string;
	    position: number;
	    title: string;
	    artist: string;
	    album: string;
	    coverArtPath: string;
	    artistMbid: string;
	    releaseGroupMbid: string;
	    recordingMbid: string;
	
	    static createFrom(source: any = {}) {
	        return new Track(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.audioFileId = source["audioFileId"];
	        this.filePath = source["filePath"];
	        this.position = source["position"];
	        this.title = source["title"];
	        this.artist = source["artist"];
	        this.album = source["album"];
	        this.coverArtPath = source["coverArtPath"];
	        this.artistMbid = source["artistMbid"];
	        this.releaseGroupMbid = source["releaseGroupMbid"];
	        this.recordingMbid = source["recordingMbid"];
	    }
	}
	export class State {
	    tracks: Track[];
	    currentIndex: number;
	    shuffleMode: boolean;
	    repeatMode: string;
	    sourcePlaylistId: number;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tracks = this.convertValues(source["tracks"], Track);
	        this.currentIndex = source["currentIndex"];
	        this.shuffleMode = source["shuffleMode"];
	        this.repeatMode = source["repeatMode"];
	        this.sourcePlaylistId = source["sourcePlaylistId"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace sqlcgen {
	
	export class Library {
	    ID: number;
	    Name: string;
	    Path: string;
	    // Go type: time
	    CreatedAt: any;
	    AutotagWarningAcked: number;
	
	    static createFrom(source: any = {}) {
	        return new Library(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.Path = source["Path"];
	        this.CreatedAt = this.convertValues(source["CreatedAt"], null);
	        this.AutotagWarningAcked = source["AutotagWarningAcked"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace tagwriter {
	
	export class BatchFailure {
	    filePath: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new BatchFailure(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filePath = source["filePath"];
	        this.error = source["error"];
	    }
	}
	export class BatchResult {
	    total: number;
	    succeeded: number;
	    failed: number;
	    cancelled: boolean;
	    failures: BatchFailure[];
	
	    static createFrom(source: any = {}) {
	        return new BatchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.succeeded = source["succeeded"];
	        this.failed = source["failed"];
	        this.cancelled = source["cancelled"];
	        this.failures = this.convertValues(source["failures"], BatchFailure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace tracklist {
	
	export class Column {
	    id: string;
	
	    static createFrom(source: any = {}) {
	        return new Column(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	    }
	}

}

