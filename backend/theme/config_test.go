package theme

import (
	"testing"
)

func TestThemeConfig_Validate_ValidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		color string
		shade BackgroundShade
	}{
		{"short hex dark", "#fff", BackgroundDark},
		{"six-digit hex darker", "#ffd43b", BackgroundDarker},
		{"black hex light", "#000000", BackgroundLight},
		{"uppercase hex", "#AABBCC", BackgroundDark},
		{"mixed case hex", "#aAbBcC", BackgroundDark},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &Config{AccentColor: tt.color, BackgroundShade: tt.shade}
			if err := c.Validate(); err != nil {
				t.Errorf("Validate() returned unexpected error: %v", err)
			}
		})
	}
}

func TestThemeConfig_Validate_InvalidHexColor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		color string
	}{
		{"missing hash", "fff"},
		{"invalid chars", "#gg0000"},
		{"wrong length 5", "#12345"},
		{"word color", "red"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &Config{AccentColor: tt.color, BackgroundShade: BackgroundDark}
			err := c.Validate()
			if err == nil {
				t.Error("Validate() expected error for invalid hex color, got nil")
			}
		})
	}
}

func TestThemeConfig_Validate_InvalidBackgroundShade(t *testing.T) {
	t.Parallel()

	c := &Config{AccentColor: "#ffd43b", BackgroundShade: "neon"}
	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for unknown shade, got nil")
	}
}

func TestThemeConfig_ApplyDefaults(t *testing.T) {
	t.Parallel()

	c := &Config{}
	c.ApplyDefaults()

	if c.AccentColor != DefaultAccentColor {
		t.Errorf("AccentColor = %q, want %q", c.AccentColor, DefaultAccentColor)
	}

	if c.BackgroundShade != DefaultBackgroundShade {
		t.Errorf("BackgroundShade = %q, want %q", c.BackgroundShade, DefaultBackgroundShade)
	}
}
