package library

import (
	"testing"
)

func TestLibraryConfig_Validate_ValidDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	modes := []ScanConcurrency{
		ScanConcurrencyAuto,
		ScanConcurrencySSD,
		ScanConcurrencyHDD,
	}

	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()

			c := &Config{
				DirectoryPath:   Directory(dir),
				ScanConcurrency: mode,
			}
			if err := c.Validate(); err != nil {
				t.Errorf("Validate() returned unexpected error: %v", err)
			}
		})
	}
}

func TestLibraryConfig_Validate_NonexistentDirectory(t *testing.T) {
	t.Parallel()

	c := &Config{
		DirectoryPath:   "/nonexistent/path/xyz",
		ScanConcurrency: ScanConcurrencyAuto,
	}

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for nonexistent directory, got nil")
	}
}

func TestLibraryConfig_Validate_InvalidScanConcurrency(t *testing.T) {
	t.Parallel()

	c := &Config{
		DirectoryPath:   Directory(t.TempDir()),
		ScanConcurrency: "turbo",
	}

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for unknown scan concurrency, got nil")
	}
}

func TestLibraryConfig_Validate_EmptyDirectory(t *testing.T) {
	t.Parallel()

	c := &Config{
		DirectoryPath:   "",
		ScanConcurrency: ScanConcurrencyAuto,
	}

	if err := c.Validate(); err != nil {
		t.Errorf("Validate() returned unexpected error for empty directory: %v", err)
	}
}

func TestLibraryConfig_ApplyDefaults(t *testing.T) {
	t.Parallel()

	c := &Config{}
	c.ApplyDefaults()

	if c.ScanConcurrency != DefaultScanConcurrency {
		t.Errorf("ScanConcurrency = %q, want %q", c.ScanConcurrency, DefaultScanConcurrency)
	}
}
