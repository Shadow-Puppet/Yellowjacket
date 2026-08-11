package main

import (
	"errors"
	"time"

	"yellowjacket/backend/tagwriter"
)

// Case names group fixtures by the application behaviour they exist to
// exercise.  Tests select fixtures by case rather than by path, so a
// path can be renamed without breaking them.
const (
	caseCoverDedup    = "cover-dedup"
	caseMultiDisc     = "multi-disc"
	caseVariousArtist = "various-artists"
	caseFLACAlbum     = "flac-album"
	caseOGGAlbum      = "ogg-album"
	caseWAVTracks     = "wav-tracks"
	casePartialTags   = "partial-tags"
	caseUnicode       = "unicode"
	caseDuplicates    = "duplicates"
	caseEdgeLengths   = "edge-lengths"
	caseBroken        = "broken"
)

var errUnknownFormat = errors.New("gentestdata: unknown audio format")

// tags mirrors the subset of tagwriter fields a fixture can set.  A
// struct rather than a bare map so the spec table stays readable and
// the manifest can record exactly what was written.
type tags struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Genre       string
	Composer    string
	Year        int
	TrackNumber int
	DiscNumber  int
}

// changes converts a fixture's tags into a tagwriter diff map,
// omitting zero values so "no tag at all" is expressible.
func (t tags) changes() tagwriter.TagChanges {
	c := tagwriter.TagChanges{}

	set := func(field, value string) {
		if value != "" {
			c[field] = value
		}
	}

	set(tagwriter.FieldTitle, t.Title)
	set(tagwriter.FieldArtist, t.Artist)
	set(tagwriter.FieldAlbum, t.Album)
	set(tagwriter.FieldAlbumArtist, t.AlbumArtist)
	set(tagwriter.FieldGenre, t.Genre)
	set(tagwriter.FieldComposer, t.Composer)

	if t.Year != 0 {
		c[tagwriter.FieldYear] = t.Year
	}

	if t.TrackNumber != 0 {
		c[tagwriter.FieldTrackNumber] = t.TrackNumber
	}

	if t.DiscNumber != 0 {
		c[tagwriter.FieldDiscNumber] = t.DiscNumber
	}

	return c
}

// fixture is one generated audio file.
type fixture struct {
	// Rel is the path relative to the library root, with '/'
	// separators regardless of platform.
	Rel string
	// Case is the behaviour group this fixture belongs to.
	Case string
	// Format decides both the container and the tag writer used.
	Format tagwriter.AudioFormat
	// Duration is the nominal length of the synthesized tone.
	Duration time.Duration
	// FreqHz identifies the track audibly and in a decoded sample.
	FreqHz float64
	// Cover is a cover-art key; fixtures sharing a key get
	// byte-identical images, which is what dedup must collapse.
	Cover string
	// Tags is what gets written after encoding.
	Tags tags
}

// Durations kept short deliberately; one long track exists so the
// progress bar and seeking have something to work against.
const (
	durShort  = 2 * time.Second
	durNormal = 4 * time.Second
	durMedium = 6 * time.Second
	durLong   = 90 * time.Second
)

// longTitle is long enough to force truncation in every list view.
const longTitle = "An Exhaustively Overlong Track Title That Exists " +
	"Solely To Find Out Whether The Track List Truncates Or Overflows"

const longArtist = "The Orchestra Of Very Considerable And " +
	"Deliberately Unreasonable Length"

// duplicateTags is shared by the deliberate duplicate pair so the
// duplicate-tracks dialog has an unambiguous match to find.
var duplicateTags = tags{
	Title:       "Tideline",
	Artist:      "Aurora Fields",
	Album:       "Glass Harbour",
	AlbumArtist: "Aurora Fields",
	Genre:       "Dream Pop",
	Year:        2019,
	TrackNumber: 2,
}

// libraryFixtures is the full contents of the clean fixture library.
//
// Everything here is scannable audio: a seeded sandbox built from this
// root must produce a stable track count, so deliberately broken files
// live in a separate root (see brokenFiles).
//
//nolint:gochecknoglobals // the fixture spec is the point of this cmd.
var libraryFixtures = []fixture{
	// 1. A plain album whose four tracks carry the same embedded
	//    cover: the dedup path should store one blob, not four.
	{
		Rel:  "Aurora Fields/Glass Harbour/01 Salt Air.mp3",
		Case: caseCoverDedup, Format: tagwriter.FormatMP3,
		Duration: durNormal, FreqHz: 220, Cover: "glass-harbour",
		Tags: tags{
			Title: "Salt Air", Artist: "Aurora Fields",
			Album: "Glass Harbour", AlbumArtist: "Aurora Fields",
			Genre: "Dream Pop", Year: 2019, TrackNumber: 1,
		},
	},
	{
		Rel:  "Aurora Fields/Glass Harbour/02 Tideline.mp3",
		Case: caseCoverDedup, Format: tagwriter.FormatMP3,
		Duration: durMedium, FreqHz: 247, Cover: "glass-harbour",
		Tags: duplicateTags,
	},
	{
		Rel:  "Aurora Fields/Glass Harbour/03 Harbour Lights.mp3",
		Case: caseCoverDedup, Format: tagwriter.FormatMP3,
		Duration: durNormal, FreqHz: 262, Cover: "glass-harbour",
		Tags: tags{
			Title: "Harbour Lights", Artist: "Aurora Fields",
			Album: "Glass Harbour", AlbumArtist: "Aurora Fields",
			Genre: "Dream Pop", Year: 2019, TrackNumber: 3,
		},
	},
	{
		Rel:  "Aurora Fields/Glass Harbour/04 Low Water.mp3",
		Case: caseCoverDedup, Format: tagwriter.FormatMP3,
		Duration: durShort, FreqHz: 294, Cover: "glass-harbour",
		Tags: tags{
			Title: "Low Water", Artist: "Aurora Fields",
			Album: "Glass Harbour", AlbumArtist: "Aurora Fields",
			Genre: "Dream Pop", Year: 2019, TrackNumber: 4,
		},
	},

	// 2. Multi-disc, with the disc split reflected both in the
	//    directory layout and in the disc number tag.  The
	//    semicolon-separated genre also covers metadata.ParseGenres.
	{
		Rel:  "Aurora Fields/Long Way Round/Disc 1/01 Departure.mp3",
		Case: caseMultiDisc, Format: tagwriter.FormatMP3,
		Duration: durNormal, FreqHz: 330, Cover: "long-way-round",
		Tags: tags{
			Title: "Departure", Artist: "Aurora Fields",
			Album: "Long Way Round", AlbumArtist: "Aurora Fields",
			Genre: "Dream Pop; Ambient", Year: 2021,
			TrackNumber: 1, DiscNumber: 1,
		},
	},
	{
		Rel:  "Aurora Fields/Long Way Round/Disc 1/02 Waystation.mp3",
		Case: caseMultiDisc, Format: tagwriter.FormatMP3,
		Duration: durShort, FreqHz: 349, Cover: "long-way-round",
		Tags: tags{
			Title: "Waystation", Artist: "Aurora Fields",
			Album: "Long Way Round", AlbumArtist: "Aurora Fields",
			Genre: "Dream Pop; Ambient", Year: 2021,
			TrackNumber: 2, DiscNumber: 1,
		},
	},
	{
		Rel:  "Aurora Fields/Long Way Round/Disc 2/01 Return.mp3",
		Case: caseMultiDisc, Format: tagwriter.FormatMP3,
		Duration: durNormal, FreqHz: 392, Cover: "long-way-round",
		Tags: tags{
			Title: "Return", Artist: "Aurora Fields",
			Album: "Long Way Round", AlbumArtist: "Aurora Fields",
			Genre: "Ambient", Year: 2021,
			TrackNumber: 1, DiscNumber: 2,
		},
	},
	{
		Rel:  "Aurora Fields/Long Way Round/Disc 2/02 Homing.mp3",
		Case: caseMultiDisc, Format: tagwriter.FormatMP3,
		Duration: durShort, FreqHz: 440, Cover: "long-way-round",
		Tags: tags{
			Title: "Homing", Artist: "Aurora Fields",
			Album: "Long Way Round", AlbumArtist: "Aurora Fields",
			Genre: "Ambient", Year: 2021,
			TrackNumber: 2, DiscNumber: 2,
		},
	},

	// 3. Compilation: per-track artists under a Various Artists
	//    album artist, which groups differently from everything else.
	{
		Rel:  "Various Artists/Night Shift Vol. 1/01 Blue Hour.mp3",
		Case: caseVariousArtist, Format: tagwriter.FormatMP3,
		Duration: durShort, FreqHz: 466, Cover: "night-shift",
		Tags: tags{
			Title: "Blue Hour", Artist: "Kilowatt",
			Album: "Night Shift Vol. 1", AlbumArtist: "Various Artists",
			Genre: "Electronic", Year: 2003, TrackNumber: 1,
		},
	},
	{
		Rel:  "Various Artists/Night Shift Vol. 1/02 Concrete Sun.mp3",
		Case: caseVariousArtist, Format: tagwriter.FormatMP3,
		Duration: durNormal, FreqHz: 494, Cover: "night-shift",
		Tags: tags{
			Title: "Concrete Sun", Artist: "Marisol Vega",
			Album: "Night Shift Vol. 1", AlbumArtist: "Various Artists",
			Genre: "Electronic", Year: 2003, TrackNumber: 2,
		},
	},
	{
		Rel:  "Various Artists/Night Shift Vol. 1/03 Dry Season.mp3",
		Case: caseVariousArtist, Format: tagwriter.FormatMP3,
		Duration: durShort, FreqHz: 523, Cover: "night-shift",
		Tags: tags{
			Title: "Dry Season", Artist: "The Hollow Coast",
			Album: "Night Shift Vol. 1", AlbumArtist: "Various Artists",
			Genre: "Jazz", Year: 2003, TrackNumber: 3,
		},
	},

	// 4. FLAC, whose cover art rides in a METADATA_BLOCK_PICTURE and
	//    whose reader/writer share no code with the ID3 path.
	{
		Rel:  "Pale Circuit/Static Bloom/01 Static Bloom.flac",
		Case: caseFLACAlbum, Format: tagwriter.FormatFLAC,
		Duration: durShort, FreqHz: 262, Cover: "static-bloom",
		Tags: tags{
			Title: "Static Bloom", Artist: "Pale Circuit",
			Album: "Static Bloom", AlbumArtist: "Pale Circuit",
			Genre: "Electronic", Year: 1998, TrackNumber: 1,
			Composer: "P. Circuit",
		},
	},
	{
		Rel:  "Pale Circuit/Static Bloom/02 Cold Cathode.flac",
		Case: caseFLACAlbum, Format: tagwriter.FormatFLAC,
		Duration: durNormal, FreqHz: 277, Cover: "static-bloom",
		Tags: tags{
			Title: "Cold Cathode", Artist: "Pale Circuit",
			Album: "Static Bloom", AlbumArtist: "Pale Circuit",
			Genre: "Electronic", Year: 1998, TrackNumber: 2,
		},
	},
	{
		Rel:  "Pale Circuit/Static Bloom/03 Dust Loop.flac",
		Case: caseFLACAlbum, Format: tagwriter.FormatFLAC,
		Duration: durShort, FreqHz: 311, Cover: "static-bloom",
		Tags: tags{
			Title: "Dust Loop", Artist: "Pale Circuit",
			Album: "Static Bloom", AlbumArtist: "Pale Circuit",
			Genre: "Electronic", Year: 1998, TrackNumber: 3,
		},
	},

	// 5. Ogg Vorbis, whose writer rebuilds the page structure by hand
	//    and is the most fragile of the four.
	{
		Rel:  "Pale Circuit/Ribbon Road/01 Ribbon Road.ogg",
		Case: caseOGGAlbum, Format: tagwriter.FormatOGG,
		Duration: durShort, FreqHz: 349, Cover: "ribbon-road",
		Tags: tags{
			Title: "Ribbon Road", Artist: "Pale Circuit",
			Album: "Ribbon Road", AlbumArtist: "Pale Circuit",
			Genre: "Ambient", Year: 2015, TrackNumber: 1,
		},
	},
	{
		Rel:  "Pale Circuit/Ribbon Road/02 Verge.ogg",
		Case: caseOGGAlbum, Format: tagwriter.FormatOGG,
		Duration: durNormal, FreqHz: 370, Cover: "ribbon-road",
		Tags: tags{
			Title: "Verge", Artist: "Pale Circuit",
			Album: "Ribbon Road", AlbumArtist: "Pale Circuit",
			Genre: "Ambient", Year: 2015, TrackNumber: 2,
		},
	},

	// 6. WAV, where tags live in a RIFF ID3 chunk.  One with cover
	//    art, one without, since the chunk layouts differ.
	{
		Rel:  "Field Recordings/Test Tones/01 Tone A.wav",
		Case: caseWAVTracks, Format: tagwriter.FormatWAV,
		Duration: durShort, FreqHz: 400, Cover: "test-tones",
		Tags: tags{
			Title: "Tone A", Artist: "Field Recordings",
			Album: "Test Tones", AlbumArtist: "Field Recordings",
			Genre: "Field Recording", Year: 2024, TrackNumber: 1,
		},
	},
	{
		Rel:  "Field Recordings/Test Tones/02 Tone B.wav",
		Case: caseWAVTracks, Format: tagwriter.FormatWAV,
		Duration: durShort, FreqHz: 800,
		Tags: tags{
			Title: "Tone B", Artist: "Field Recordings",
			Album: "Test Tones", AlbumArtist: "Field Recordings",
			Genre: "Field Recording", Year: 2024, TrackNumber: 2,
		},
	},

	// 7. Degrees of missing metadata, which is what the "Unknown
	//    Artist" fallbacks and the autotag candidate list are for.
	{
		Rel:  "unsorted/no-tags-at-all.mp3",
		Case: casePartialTags, Format: tagwriter.FormatMP3,
		Duration: durShort, FreqHz: 200,
	},
	{
		Rel:  "unsorted/title-only.mp3",
		Case: casePartialTags, Format: tagwriter.FormatMP3,
		Duration: durShort, FreqHz: 210,
		Tags: tags{Title: "Title Only"},
	},
	{
		Rel:  "unsorted/no-track-number.mp3",
		Case: casePartialTags, Format: tagwriter.FormatMP3,
		Duration: durShort, FreqHz: 230,
		Tags: tags{
			Title: "No Track Number", Artist: "Loose Ends",
			Album: "Odds And Sods",
		},
	},

	// 8. Scripts the layout engine handles differently, plus
	//    filenames with characters that break naive URL building.
	{
		Rel:  "Unicode Tests/多言語アルバム/01 さくら.mp3",
		Case: caseUnicode, Format: tagwriter.FormatMP3,
		Duration: durShort, FreqHz: 261, Cover: "unicode",
		Tags: tags{
			Title: "さくら", Artist: "サンプル・アーティスト",
			Album: "多言語アルバム", AlbumArtist: "サンプル・アーティスト",
			Genre: "J-Pop", Year: 2020, TrackNumber: 1,
		},
	},
	{
		Rel:  "Unicode Tests/多言語アルバム/02 Привет мир.mp3",
		Case: caseUnicode, Format: tagwriter.FormatMP3,
		Duration: durShort, FreqHz: 293, Cover: "unicode",
		Tags: tags{
			Title: "Привет мир", Artist: "Тестовый исполнитель",
			Album: "多言語アルバム", AlbumArtist: "サンプル・アーティスト",
			Genre: "J-Pop", Year: 2020, TrackNumber: 2,
		},
	},
	{
		Rel:  "Unicode Tests/多言語アルバム/03 مرحبا بالعالم.mp3",
		Case: caseUnicode, Format: tagwriter.FormatMP3,
		Duration: durShort, FreqHz: 329, Cover: "unicode",
		Tags: tags{
			Title: "مرحبا بالعالم", Artist: "فنان تجريبي",
			Album: "多言語アルバム", AlbumArtist: "サンプル・アーティスト",
			Genre: "J-Pop", Year: 2020, TrackNumber: 3,
		},
	},
	{
		// Precomposed é in the filename, decomposed e+U+0301 in the
		// title: a genuine source of "the same track twice" bugs.
		Rel:  "Unicode Tests/多言語アルバム/04 Café ☕ Über #1's.mp3",
		Case: caseUnicode, Format: tagwriter.FormatMP3,
		Duration: durShort, FreqHz: 415, Cover: "unicode",
		Tags: tags{
			Title: "Cafe\u0301 ☕ Über #1's", Artist: "サンプル・アーティスト",
			Album: "多言語アルバム", AlbumArtist: "サンプル・アーティスト",
			Genre: "J-Pop", Year: 2020, TrackNumber: 4,
		},
	},

	// 9. The deliberate duplicate pair: identical tags and length as
	//    "02 Tideline.mp3" above, in another directory and another
	//    format, for the duplicate-tracks dialog to match on.
	{
		Rel:  "unsorted/dupes/Tideline (copy).mp3",
		Case: caseDuplicates, Format: tagwriter.FormatMP3,
		Duration: durMedium, FreqHz: 247, Cover: "glass-harbour",
		Tags: duplicateTags,
	},
	{
		Rel:  "unsorted/dupes/Tideline.flac",
		Case: caseDuplicates, Format: tagwriter.FormatFLAC,
		Duration: durMedium, FreqHz: 247, Cover: "glass-harbour",
		Tags: duplicateTags,
	},

	// 10. Extremes of text length and track length, plus a track with
	//     no year at all for smart-playlist range rules to exclude.
	{
		Rel:  "Edge Cases/Extremes/01 Long Title.mp3",
		Case: caseEdgeLengths, Format: tagwriter.FormatMP3,
		Duration: durShort, FreqHz: 180,
		Tags: tags{
			Title: longTitle, Artist: longArtist,
			Album: "Extremes", AlbumArtist: longArtist,
			Genre: "Jazz", Year: 1975, TrackNumber: 1,
		},
	},
	{
		Rel:  "Edge Cases/Extremes/02 Brief.mp3",
		Case: caseEdgeLengths, Format: tagwriter.FormatMP3,
		Duration: durShort, FreqHz: 190,
		Tags: tags{
			Title: "Brief", Artist: longArtist,
			Album: "Extremes", AlbumArtist: longArtist,
			Genre: "Jazz", Year: 1975, TrackNumber: 2,
		},
	},
	{
		Rel:  "Edge Cases/Extremes/03 Long Player.mp3",
		Case: caseEdgeLengths, Format: tagwriter.FormatMP3,
		Duration: durLong, FreqHz: 165,
		Tags: tags{
			Title: "Long Player", Artist: longArtist,
			Album: "Extremes", AlbumArtist: longArtist,
			Genre: "Jazz", Year: 1975, TrackNumber: 3,
		},
	},
	{
		Rel:  "Edge Cases/Extremes/04 Undated.mp3",
		Case: caseEdgeLengths, Format: tagwriter.FormatMP3,
		Duration: durShort, FreqHz: 175,
		Tags: tags{
			Title: "Undated", Artist: longArtist,
			Album: "Extremes", AlbumArtist: longArtist,
			Genre: "Jazz", TrackNumber: 4,
		},
	},
}

// auxFile is a non-audio or malformed file: either debris that
// legitimately sits inside a music library, or a deliberately broken
// file used to exercise scanner error handling.
type auxFile struct {
	Rel string
	// Source, when set, names a library fixture whose encoded bytes
	// get truncated to Bytes; otherwise Literal is written verbatim.
	Source  string
	Bytes   int
	Literal string
}

// brokenFiles live in their own root so the clean library's track count
// stays deterministic.  A test that wants the error paths adds this
// root as a second library on purpose.
//
//nolint:gochecknoglobals // the fixture spec is the point of this cmd.
var brokenFiles = []auxFile{
	{Rel: "notes.txt", Literal: "not audio\n"},
	{Rel: "empty.flac"},
	{
		Rel:    "truncated.mp3",
		Source: "Aurora Fields/Glass Harbour/01 Salt Air.mp3",
		Bytes:  512,
	},
}

// libraryExtras are the non-audio files a real library is full of.
// They belong inside the clean root because ignoring them is itself
// behaviour worth testing — folder art in particular, which is a
// separate cover source from embedded art.
//
//nolint:gochecknoglobals // the fixture spec is the point of this cmd.
var libraryExtras = []auxFile{
	{Rel: "Pale Circuit/Ribbon Road/cover.jpg"},
	{Rel: "Pale Circuit/Ribbon Road/ripping notes.txt", Literal: "EAC log\n"},
}
