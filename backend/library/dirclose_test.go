package library

import "testing"

func TestIsWithinDir(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		dir  string
		want bool
	}{
		{"root contains anything", "Artist/Album/01.mp3", ".", true},
		{"root contains itself", ".", ".", true},
		{"same directory", "Artist/Album", "Artist/Album", true},
		{"direct child file", "Artist/Album/01.mp3", "Artist/Album", true},
		{"nested subdirectory", "Artist/Album/CD1/01.mp3", "Artist/Album", true},
		{"sibling not contained", "Artist/OtherAlbum", "Artist/Album", false},
		{
			"prefix-colliding sibling not contained",
			"Artist/Album2/01.mp3", "Artist/Album",
			false,
		},
		{"parent not contained in child", "Artist", "Artist/Album", false},
		{"unrelated tree", "Other/Thing", "Artist/Album", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := isWithinDir(tc.path, tc.dir); got != tc.want {
				t.Errorf("isWithinDir(%q, %q) = %v, want %v", tc.path, tc.dir, got, tc.want)
			}
		})
	}
}
