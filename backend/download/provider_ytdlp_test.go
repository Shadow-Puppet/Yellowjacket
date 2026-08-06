package download

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// yt-dlp tests drive a stub shell script rather than the real binary:
// the adapter's contract is "what argv do we build and what do we do
// with the output", and a stub tests exactly that without a network,
// a YouTube account, or a 40MB dependency.

// stubYtDlp writes an executable script that echoes the given stdout
// and returns it as a provider config binary path.
func stubYtDlp(t *testing.T, script string) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("stub binary test uses a shell script")
	}

	path := filepath.Join(t.TempDir(), "yt-dlp")

	if err := os.WriteFile(
		path, []byte("#!/bin/sh\n"+script), 0o700,
	); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	return path
}

// newStubYtDlp builds the provider over a stub binary.
func newStubYtDlp(t *testing.T, script string) *ytDlp {
	t.Helper()

	p, err := newYtDlp(
		Config{
			ID:       1,
			Kind:     KindYtDlp,
			Name:     "yt-dlp",
			Enabled:  true,
			Priority: 50,
			Settings: map[string]string{
				"binary":      stubYtDlp(t, script),
				"audioFormat": "flac",
			},
		},
		nil,
		slogDiscard(),
	)
	if err != nil {
		t.Fatalf("newYtDlp: %v", err)
	}

	y, ok := p.(*ytDlp)
	if !ok {
		t.Fatalf("provider is %T, want *ytDlp", p)
	}

	return y
}

func TestYtDlpCheckVersion(t *testing.T) {
	t.Parallel()

	t.Run("recent version passes", func(t *testing.T) {
		t.Parallel()

		y := newStubYtDlp(t, `echo "2024.08.06"`)

		if err := y.Check(context.Background()); err != nil {
			t.Errorf("Check: %v", err)
		}
	})

	t.Run("old version is rejected", func(t *testing.T) {
		t.Parallel()

		y := newStubYtDlp(t, `echo "2021.01.01"`)

		if err := y.Check(context.Background()); !errors.Is(
			err, ErrYtDlpTooOld,
		) {
			t.Errorf("error = %v, want ErrYtDlpTooOld", err)
		}
	})

	t.Run("non-zero exit is reported", func(t *testing.T) {
		t.Parallel()

		y := newStubYtDlp(t, `exit 1`)

		if err := y.Check(context.Background()); !errors.Is(
			err, ErrYtDlpFailed,
		) {
			t.Errorf("error = %v, want ErrYtDlpFailed", err)
		}
	})
}

func TestYtDlpMissingBinary(t *testing.T) {
	t.Parallel()

	_, err := newYtDlp(
		Config{Settings: map[string]string{
			"binary": "definitely-not-a-real-binary-xyzzy",
		}},
		nil,
		slogDiscard(),
	)

	if !errors.Is(err, ErrYtDlpMissing) {
		t.Errorf("error = %v, want ErrYtDlpMissing", err)
	}
}

func TestYtDlpSearchParsesJSONLines(t *testing.T) {
	t.Parallel()

	y := newStubYtDlp(t, `
cat <<'EOF'
{"id":"aaa","title":"Airbag","webpage_url":"https://example.com/aaa","uploader":"Radiohead","duration":284,"filesize_approx":5000000}
{"id":"bbb","title":"Paranoid Android","webpage_url":"https://example.com/bbb","uploader":"Radiohead","duration":383}
EOF
`)

	got, err := y.Search(context.Background(), Request{
		Artist: "Radiohead",
		Album:  "OK Computer",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2", len(got))
	}

	if got[0].Title != "Airbag" {
		t.Errorf("title = %q, want Airbag", got[0].Title)
	}

	if got[0].Protocol != ProtocolDirect {
		t.Errorf("protocol = %q, want direct", got[0].Protocol)
	}

	if len(got[0].Files) != 1 {
		t.Fatalf("got %d files, want 1", len(got[0].Files))
	}

	link := got[0].Payload[got[0].Files[0].Path]
	if link != "https://example.com/aaa" {
		t.Errorf("payload url = %q, want the webpage_url", link)
	}
}

// Warning text mixed into stdout must not discard valid results.
func TestYtDlpSearchSkipsUnparseableLines(t *testing.T) {
	t.Parallel()

	y := newStubYtDlp(t, `
cat <<'EOF'
WARNING: something happened
{"id":"aaa","title":"Airbag","webpage_url":"https://example.com/aaa"}
not json at all
{"id":"bbb","title":"Lucky","webpage_url":"https://example.com/bbb"}
EOF
`)

	got, err := y.Search(context.Background(), Request{Query: "radiohead"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2 valid ones", len(got))
	}
}

// With a tracklist the adapter assembles an album from per-track
// searches, because a single "full album" video cannot be imported as
// separate tracks.
func TestYtDlpAssemblesAlbumFromTracklist(t *testing.T) {
	t.Parallel()

	y := newStubYtDlp(t, `
echo '{"id":"x","title":"whatever the uploader called it","webpage_url":"https://example.com/x","filesize_approx":4000000}'
`)

	req := Request{
		ID:          "req-1",
		ReleaseMBID: "mbid-1",
		Artist:      "Radiohead",
		Album:       "OK Computer",
		Expected: []ExpectedTrack{
			{Position: 1, Title: "Airbag"},
			{Position: 2, Title: "Paranoid Android"},
			{Position: 3, Title: "Subterranean Homesick Alien"},
		},
	}

	got, err := y.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1 assembled album", len(got))
	}

	album := got[0]

	if len(album.Files) != 3 {
		t.Fatalf("got %d files, want 3", len(album.Files))
	}

	// Files are named after the expected tracks, not the video titles,
	// because the import step matches on filename.
	names := make([]string, 0, len(album.Files))
	for _, f := range album.Files {
		names = append(names, f.Path)
	}

	for _, want := range []string{
		"01 - Airbag.flac",
		"02 - Paranoid Android.flac",
		"03 - Subterranean Homesick Alien.flac",
	} {
		if !containsString(names, want) {
			t.Errorf("missing %q in %v", want, names)
		}
	}

	if album.TotalSize != 12_000_000 {
		t.Errorf("total size = %d, want 12000000", album.TotalSize)
	}
}

// A track with no search result is left out, so completeness scoring
// can speak for the gap instead of the search failing outright.
func TestYtDlpAssembleToleratesMissingTracks(t *testing.T) {
	t.Parallel()

	y := newStubYtDlp(t, `
case "$*" in
  *Airbag*) echo '{"id":"a","title":"Airbag","webpage_url":"https://example.com/a"}' ;;
  *) exit 1 ;;
esac
`)

	got, err := y.Search(context.Background(), Request{
		ID:          "req-1",
		ReleaseMBID: "mbid-1",
		Artist:      "Radiohead",
		Album:       "OK Computer",
		Expected: []ExpectedTrack{
			{Position: 1, Title: "Airbag"},
			{Position: 2, Title: "Paranoid Android"},
		},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1", len(got))
	}

	if len(got[0].Files) != 1 {
		t.Errorf("got %d files, want just the one that was found", len(got[0].Files))
	}
}

func TestYtDlpGrabWritesIntoStaging(t *testing.T) {
	t.Parallel()

	// The stub writes a file at whatever --output stem it is given,
	// mimicking yt-dlp's post-extraction naming.
	y := newStubYtDlp(t, `
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    --output) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
target=$(printf '%s' "$out" | sed 's/%(ext)s/flac/')
printf 'audio' > "$target"
`)

	dst := t.TempDir()

	c := Candidate{
		ID:       "ytdlp:album:req-1",
		Protocol: ProtocolDirect,
		Files: []CandidateFile{
			{Path: "01 - Airbag.flac", IsAudio: true},
		},
		Payload: map[string]string{
			"01 - Airbag.flac": "https://example.com/a",
		},
	}

	got, err := y.Grab(context.Background(), c, dst, nil)
	if err != nil {
		t.Fatalf("Grab: %v", err)
	}

	if len(got.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(got.Files))
	}

	if _, err := os.Stat(got.Files[0]); err != nil {
		t.Errorf("downloaded file missing: %v", err)
	}

	if !strings.HasSuffix(got.Files[0], ".flac") {
		t.Errorf("file = %s, want a .flac", got.Files[0])
	}
}

// A search result is untrusted input, and yt-dlp accepts schemes that
// would read the local filesystem.
func TestYtDlpGrabRejectsNonHTTPURL(t *testing.T) {
	t.Parallel()

	y := newStubYtDlp(t, `exit 0`)

	c := Candidate{
		Files:   []CandidateFile{{Path: "x.flac", IsAudio: true}},
		Payload: map[string]string{"x.flac": "file:///etc/passwd"},
	}

	_, err := y.Grab(context.Background(), c, t.TempDir(), nil)
	if !errors.Is(err, ErrUnsafeURL) {
		t.Errorf("error = %v, want ErrUnsafeURL", err)
	}
}

func TestValidateHTTPURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url     string
		wantErr bool
	}{
		{"https://example.com/a", false},
		{"http://example.com/a", false},
		{"file:///etc/passwd", true},
		{"ftp://example.com/a", true},
		{"javascript:alert(1)", true},
		{"://nonsense", true},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			t.Parallel()

			err := validateHTTPURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateHTTPURL(%q) error = %v, wantErr %v",
					tt.url, err, tt.wantErr)
			}
		})
	}
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}

	return false
}
