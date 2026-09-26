package download

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Multi-disc rips, single-track results and coverage counted in tracks
// rather than files (#270).

func TestParsePathReadsTheDiscFromItsFolder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path   string
		disc   int
		track  int
		folder string
	}{
		{`\share\Pink Floyd - The Wall (1979)\CD2\03 Hey You.flac`, 2, 3, "The Wall"},
		{`\share\The Wall\Disc 1\01 In The Flesh.flac`, 1, 1, "The Wall"},
		{`\share\The Wall\[Disk-2]\01 Hey You.flac`, 2, 1, "The Wall"},
		{`\share\The Wall\CD1 - Live\04 Mother.flac`, 1, 4, "The Wall"},
		// The filename's own disc number is more specific than the folder.
		{`\share\The Wall\CD1\2-05 Comfortably Numb.flac`, 2, 5, "The Wall"},
		// Not a disc folder: a number is required.
		{`\share\CDs\The Wall\01 In The Flesh.flac`, 0, 1, "The Wall"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			got := ParsePath(tc.path)
			if got.Disc != tc.disc || got.Track != tc.track || got.Folder != tc.folder {
				t.Errorf(
					"ParsePath = disc %d track %d folder %q, want %d %d %q",
					got.Disc, got.Track, got.Folder, tc.disc, tc.track, tc.folder,
				)
			}
		})
	}
}

func TestAlbumDir(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		`\share\Album\CD1\01 A.flac`:  "/share/Album",
		`\share\Album\01 A.flac`:      "/share/Album",
		`CD1\01 A.flac`:               "CD1",
		`\share\CD Collection\01.mp3`: "/share/CD Collection",
	}

	for in, want := range cases {
		if got := AlbumDir(in); got != want {
			t.Errorf("AlbumDir(%q) = %q, want %q", in, got, want)
		}
	}
}

// One album shared as CD1/CD2 is one candidate, named after the album.
func TestSlskdGroupsDiscFoldersIntoOneCandidate(t *testing.T) {
	t.Parallel()

	stub := newSlskdStub(t)
	stub.responses = []slskdResponse{{
		Username: "peer",
		Files: []slskdFile{
			{Filename: `\m\The Wall\CD1\01 In The Flesh.flac`, Size: 1},
			{Filename: `\m\The Wall\CD1\02 The Thin Ice.flac`, Size: 1},
			{Filename: `\m\The Wall\CD2\01 Hey You.flac`, Size: 1},
			{Filename: `\m\The Wall\CD2\02 Is There Anybody Out There.flac`, Size: 1},
		},
	}}

	s, _ := newStubSlskd(t, stub)

	got, err := s.Search(context.Background(), Download{Query: "the wall"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d candidates, want the two discs as one", len(got))
	}

	if got[0].Title != "The Wall" || len(got[0].Files) != 4 {
		t.Errorf(
			"candidate = %q with %d files, want \"The Wall\" with 4",
			got[0].Title, len(got[0].Files),
		)
	}
}

// A track search matches one file per folder, so a single-track request
// must accept a one-file folder that an album request rightly drops.
func TestSlskdKeepsASingleFileForATrackRequest(t *testing.T) {
	t.Parallel()

	stub := newSlskdStub(t)
	stub.responses = []slskdResponse{{
		Username: "peer",
		Files: []slskdFile{
			{Filename: `\m\OK Computer\02 Paranoid Android.flac`, Size: 1},
		},
	}}

	s, _ := newStubSlskd(t, stub)

	track, err := s.Search(context.Background(), Download{
		RecordingMBID: "rec-1", Artist: "Radiohead", Album: "Paranoid Android",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(track) != 1 {
		t.Errorf("track request: got %d candidates, want 1", len(track))
	}

	album, err := s.Search(context.Background(), Download{
		ReleaseMBID: "rel-1", Artist: "Radiohead", Album: "OK Computer",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(album) != 0 {
		t.Errorf("album request: got %d candidates, want the one-file folder dropped", len(album))
	}
}

// Two discs with a file of the same name both reach staging, each under
// its disc folder, where the importer reads the disc number from.
func TestSlskdCollectKeepsDiscFolders(t *testing.T) {
	t.Parallel()

	stub := newSlskdStub(t)
	s, downloads := newStubSlskd(t, stub)

	for _, disc := range []string{"CD1", "CD2"} {
		dir := filepath.Join(downloads, disc)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		if err := os.WriteFile(
			filepath.Join(dir, "01 Intro.flac"), []byte(disc), 0o600,
		); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	dst := t.TempDir()

	got, err := s.collect(Candidate{Files: []CandidateFile{
		{Path: `\m\Album\CD1\01 Intro.flac`, IsAudio: true},
		{Path: `\m\Album\CD2\01 Intro.flac`, IsAudio: true},
	}}, dst)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	if len(got.Files) != 2 {
		t.Fatalf("collected %d files, want 2", len(got.Files))
	}

	for _, disc := range []string{"CD1", "CD2"} {
		data, err := os.ReadFile(filepath.Join(dst, disc, "01 Intro.flac"))
		if err != nil || string(data) != disc {
			t.Errorf("%s's file missing or overwritten: %q, %v", disc, data, err)
		}

		if hint := ParsePath(filepath.Join(dst, disc, "01 Intro.flac")); hint.Disc == 0 {
			t.Errorf("staged %s file lost its disc number", disc)
		}
	}
}

// A two-disc release whose discs both number from 01 aligns completely
// once the disc comes from the folder; before, disc 2's 01 collided with
// disc 1's.
func TestMultiDiscCandidateAlignsEveryTrack(t *testing.T) {
	t.Parallel()

	dl := Download{
		ReleaseMBID: "the-wall",
		Artist:      "Pink Floyd",
		Album:       "The Wall",
		Expected: []ExpectedTrack{
			{DiscNumber: 1, Position: 1, Title: "In the Flesh?"},
			{DiscNumber: 1, Position: 2, Title: "The Thin Ice"},
			{DiscNumber: 2, Position: 1, Title: "Hey You"},
			{DiscNumber: 2, Position: 2, Title: "Is There Anybody Out There?"},
		},
	}

	c := Candidate{
		Title: "The Wall",
		Files: []CandidateFile{
			{Path: `\m\Pink Floyd - The Wall\CD1\01 In the Flesh.flac`, Size: 1},
			{Path: `\m\Pink Floyd - The Wall\CD1\02 The Thin Ice.flac`, Size: 1},
			{Path: `\m\Pink Floyd - The Wall\CD2\01 Hey You.flac`, Size: 1},
			{Path: `\m\Pink Floyd - The Wall\CD2\02 Is There Anybody Out There.flac`, Size: 1},
		},
	}

	got := Score(dl, c, 50, AutoDownloadPrefs{})

	if got.Match.Completeness != 1 {
		t.Errorf("completeness = %f, want 1", got.Match.Completeness)
	}

	if got.Match.AlbumFit < 0.99 {
		t.Errorf("album fit = %f, want the album's own name to match", got.Match.AlbumFit)
	}

	if got.Match.Overall < minMatch {
		t.Errorf("match = %f, want it to clear the auto-pick bar %f", got.Match.Overall, minMatch)
	}
}

// Ten files against a ten-track album is not a complete album when only
// three of them are its tracks.
func TestCompletenessCountsTracksNotFiles(t *testing.T) {
	t.Parallel()

	dl := okComputer()

	c := Candidate{Title: "OK Computer", Files: []CandidateFile{
		{Path: `\m\Radiohead - OK Computer\Airbag.flac`, Size: 1},
		{Path: `\m\Radiohead - OK Computer\Paranoid Android.flac`, Size: 1},
		{Path: `\m\Radiohead - OK Computer\Exit Music (For a Film).flac`, Size: 1},
		{Path: `\m\Radiohead - OK Computer\Creep.flac`, Size: 1},
	}}

	got := Score(dl, c, 50, AutoDownloadPrefs{})

	if got.Match.Completeness > 0.76 {
		t.Errorf(
			"completeness = %f with 3 of 4 tracks present, want at most 0.75",
			got.Match.Completeness,
		)
	}
}
