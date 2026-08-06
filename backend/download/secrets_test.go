package download

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileSecretStoreRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "secrets.json")
	store := NewFileSecretStoreAt(path)

	if err := store.Set(1, "apiKey", "hunter2"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := store.Get(1, "apiKey")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got != "hunter2" {
		t.Errorf("Get = %q, want hunter2", got)
	}

	// A fresh store over the same file must see the value.
	if got, err := NewFileSecretStoreAt(path).Get(1, "apiKey"); err != nil ||
		got != "hunter2" {
		t.Errorf("reload got %q, err %v; want hunter2", got, err)
	}
}

// Credentials must not be readable by other local users.
func TestFileSecretStorePermissions(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "secrets.json")
	store := NewFileSecretStoreAt(path)

	if err := store.Set(1, "apiKey", "hunter2"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if perm := info.Mode().Perm(); perm != secretsFileMode {
		t.Errorf("mode = %o, want %o", perm, secretsFileMode)
	}
}

// Two configured instances of the same provider kind must not share
// credentials.
func TestFileSecretStoreNamespacesByProvider(t *testing.T) {
	t.Parallel()

	store := NewFileSecretStoreAt(filepath.Join(t.TempDir(), "secrets.json"))

	if err := store.Set(1, "apiKey", "first"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := store.Set(2, "apiKey", "second"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	for id, want := range map[int64]string{1: "first", 2: "second"} {
		got, err := store.Get(id, "apiKey")
		if err != nil {
			t.Fatalf("Get(%d): %v", id, err)
		}

		if got != want {
			t.Errorf("Get(%d) = %q, want %q", id, got, want)
		}
	}
}

func TestFileSecretStoreDeleteProvider(t *testing.T) {
	t.Parallel()

	store := NewFileSecretStoreAt(filepath.Join(t.TempDir(), "secrets.json"))

	if err := store.Set(1, "apiKey", "a"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := store.Set(1, "password", "b"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := store.Set(2, "apiKey", "keep"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := store.DeleteProvider(1); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}

	for _, name := range []string{"apiKey", "password"} {
		if _, err := store.Get(1, name); !errors.Is(err, ErrSecretNotFound) {
			t.Errorf("Get(1, %q) error = %v, want ErrSecretNotFound", name, err)
		}
	}

	if got, err := store.Get(2, "apiKey"); err != nil || got != "keep" {
		t.Errorf("other provider's secret was removed: %q, %v", got, err)
	}
}

func TestFileSecretStoreMissingFileIsEmpty(t *testing.T) {
	t.Parallel()

	store := NewFileSecretStoreAt(filepath.Join(t.TempDir(), "nope.json"))

	if _, err := store.Get(1, "apiKey"); !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("error = %v, want ErrSecretNotFound", err)
	}
}

func TestSetEmptyValueDeletes(t *testing.T) {
	t.Parallel()

	store := NewFileSecretStoreAt(filepath.Join(t.TempDir(), "secrets.json"))

	if err := store.Set(1, "apiKey", "x"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := store.Set(1, "apiKey", ""); err != nil {
		t.Fatalf("Set empty: %v", err)
	}

	if _, err := store.Get(1, "apiKey"); !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("error = %v, want ErrSecretNotFound", err)
	}
}
