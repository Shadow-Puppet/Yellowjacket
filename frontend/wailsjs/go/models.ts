export namespace explore {
	
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
	
	    static createFrom(source: any = {}) {
	        return new LBTopRecording(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.recordingMbid = source["recordingMbid"];
	        this.artistName = source["artistName"];
	        this.trackName = source["trackName"];
	        this.totalListenCount = source["totalListenCount"];
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
	    }
	}
	export class MBRecording {
	    mbid: string;
	    title: string;
	    length: number;
	    artistCredit: string;
	    score: number;
	
	    static createFrom(source: any = {}) {
	        return new MBRecording(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mbid = source["mbid"];
	        this.title = source["title"];
	        this.length = source["length"];
	        this.artistCredit = source["artistCredit"];
	        this.score = source["score"];
	    }
	}
	export class MBTrack {
	    position: number;
	    discNumber: number;
	    title: string;
	    length: number;
	    mbid: string;
	
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
	    }
	}
	export class MBRelease {
	    mbid: string;
	    title: string;
	    date: string;
	    country: string;
	    status: string;
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
	    }
	}
	export class MBSearchResult {
	    artists?: MBArtist[];
	    releaseGroups?: MBReleaseGroup[];
	    recordings?: MBRecording[];
	
	    static createFrom(source: any = {}) {
	        return new MBSearchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.artists = this.convertValues(source["artists"], MBArtist);
	        this.releaseGroups = this.convertValues(source["releaseGroups"], MBReleaseGroup);
	        this.recordings = this.convertValues(source["recordings"], MBRecording);
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

export namespace library {
	
	export class Album {
	    ID: number;
	    Name: string;
	    ArtistName: string;
	    CoverArtPath: string;
	    CoverArtSmall: string;
	    CoverArtMedium: string;
	    CoverArtLarge: string;
	    Year: number;
	
	    static createFrom(source: any = {}) {
	        return new Album(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.ArtistName = source["ArtistName"];
	        this.CoverArtPath = source["CoverArtPath"];
	        this.CoverArtSmall = source["CoverArtSmall"];
	        this.CoverArtMedium = source["CoverArtMedium"];
	        this.CoverArtLarge = source["CoverArtLarge"];
	        this.Year = source["Year"];
	    }
	}
	export class Artist {
	    ID: number;
	    Name: string;
	
	    static createFrom(source: any = {}) {
	        return new Artist(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
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
	
	    static createFrom(source: any = {}) {
	        return new Library(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.Path = source["Path"];
	        this.CreatedAt = this.convertValues(source["CreatedAt"], null);
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

