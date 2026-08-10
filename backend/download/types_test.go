package download

import "testing"

func TestRequest_SearchText(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		want string
	}{
		{
			name: "query overrides everything",
			req:  Request{Artist: "Blank Banshee", Album: "0", Query: "raw text"},
			want: "raw text",
		},
		{
			name: "ordinary album keeps artist and album",
			req:  Request{Artist: "Pink Floyd", Album: "The Wall"},
			want: "Pink Floyd The Wall",
		},
		{
			name: "album title leads with artist name",
			req:  Request{Artist: "Blank Banshee", Album: "Blank Banshee 0"},
			want: "Blank Banshee 0",
		},
		{
			name: "self-titled album",
			req:  Request{Artist: "Boston", Album: "Boston"},
			want: "Boston",
		},
		{
			name: "artist name as a substring, not a word prefix",
			req:  Request{Artist: "Air", Album: "Repair"},
			want: "Air Repair",
		},
		{
			name: "case-insensitive match",
			req:  Request{Artist: "blank banshee", Album: "BLANK BANSHEE 0"},
			want: "BLANK BANSHEE 0",
		},
		{
			name: "no artist",
			req:  Request{Album: "Compilation"},
			want: "Compilation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.req.SearchText(); got != tt.want {
				t.Errorf("SearchText() = %q, want %q", got, tt.want)
			}
		})
	}
}
