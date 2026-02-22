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
	    }
	}

}

export namespace playlist {
	
	export class Summary {
	    ID: number;
	    Name: string;
	
	    static createFrom(source: any = {}) {
	        return new Summary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
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

