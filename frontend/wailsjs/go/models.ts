export namespace library {
	
	export class Album {
	    ID: number;
	    Name: string;
	    ArtistName: string;
	    CoverArtPath: string;
	    CoverArtThumbnailPath: string;
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
	        this.CoverArtThumbnailPath = source["CoverArtThumbnailPath"];
	        this.Year = source["Year"];
	    }
	}
	export class Track {
	    TrackName: string;
	    ArtistName: string;
	    TrackLength: string;
	    FilePath: string;
	
	    static createFrom(source: any = {}) {
	        return new Track(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.TrackName = source["TrackName"];
	        this.ArtistName = source["ArtistName"];
	        this.TrackLength = source["TrackLength"];
	        this.FilePath = source["FilePath"];
	    }
	}

}

