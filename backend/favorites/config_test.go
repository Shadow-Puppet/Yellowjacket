package favorites

import (
	"testing"
)

func TestFavoritesConfig_Validate_ValidIconStyles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		style IconStyle
	}{
		{"heart", IconHeart},
		{"star", IconStar},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &Config{IconStyle: tt.style}
			if err := c.Validate(); err != nil {
				t.Errorf("Validate() returned unexpected error: %v", err)
			}
		})
	}
}

func TestFavoritesConfig_Validate_InvalidIconStyle(t *testing.T) {
	t.Parallel()

	c := &Config{IconStyle: "diamond"}
	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for unknown icon style, got nil")
	}
}

func TestFavoritesConfig_ApplyDefaults(t *testing.T) {
	t.Parallel()

	c := &Config{}
	c.ApplyDefaults()

	if c.IconStyle != DefaultIconStyle {
		t.Errorf("IconStyle = %q, want %q", c.IconStyle, DefaultIconStyle)
	}
}
