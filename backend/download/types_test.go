package download

import "testing"

func TestDownload_SearchText(t *testing.T) {
	tests := []struct {
		name string
		dl   Download
		want string
	}{
		{
			name: "query overrides everything",
			dl:   Download{Artist: "Blank Banshee", Album: "0", Query: "raw text"},
			want: "raw text",
		},
		{
			name: "ordinary album keeps artist and album",
			dl:   Download{Artist: "Pink Floyd", Album: "The Wall"},
			want: "Pink Floyd The Wall",
		},
		{
			name: "album title leads with artist name",
			dl:   Download{Artist: "Blank Banshee", Album: "Blank Banshee 0"},
			want: "Blank Banshee 0",
		},
		{
			name: "self-titled album",
			dl:   Download{Artist: "Boston", Album: "Boston"},
			want: "Boston",
		},
		{
			name: "artist name as a substring, not a word prefix",
			dl:   Download{Artist: "Air", Album: "Repair"},
			want: "Air Repair",
		},
		{
			name: "case-insensitive match",
			dl:   Download{Artist: "blank banshee", Album: "BLANK BANSHEE 0"},
			want: "BLANK BANSHEE 0",
		},
		{
			name: "no artist",
			dl:   Download{Album: "Compilation"},
			want: "Compilation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.dl.SearchText(); got != tt.want {
				t.Errorf("SearchText() = %q, want %q", got, tt.want)
			}
		})
	}
}
