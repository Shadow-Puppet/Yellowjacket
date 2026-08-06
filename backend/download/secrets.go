package download

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"yellowjacket/backend/system"
)

// Provider credentials — slskd API keys, Lidarr tokens, qBittorrent
// passwords — must not sit in the TOML config, which is world-readable
// by default and gets pasted into bug reports.
//
// They live in a separate 0600 JSON file in the user data directory.
// That is deliberately not encryption: a key stored beside the data it
// unlocks protects nothing, and pretending otherwise is worse than
// being clear about it.  What the file mode does buy is protection from
// other local users and from the config file being shared casually.
//
// An OS keyring backend (libsecret / Keychain / DPAPI) is the right
// long-term answer and the Store interface exists so it can be added
// without touching any provider.

// ErrSecretNotFound is returned when a named secret has never been set.
var ErrSecretNotFound = errors.New("secret not found")

// secretsFileName is the store's file inside the user data directory.
const secretsFileName = "download-secrets.json"

// secretsFileMode is owner read/write only.
const secretsFileMode = 0o600

// SecretStore holds provider credentials.
type SecretStore interface {
	// Get returns the secret for a provider's named field.
	Get(providerID int64, name string) (string, error)

	// Set stores a secret.  An empty value deletes it.
	Set(providerID int64, name, value string) error

	// DeleteProvider removes every secret belonging to a provider.
	DeleteProvider(providerID int64) error
}

// fileSecretStore is the default SecretStore: a 0600 JSON file.
type fileSecretStore struct {
	path string

	mu     sync.RWMutex
	loaded bool
	data   map[string]string
}

// NewFileSecretStore returns a SecretStore backed by a 0600 file in the
// user data directory.
func NewFileSecretStore() (SecretStore, error) {
	dir, err := system.GetUserDataDirPath()
	if err != nil {
		return nil, fmt.Errorf("resolve user data dir: %w", err)
	}

	return &fileSecretStore{
		path: filepath.Join(dir, secretsFileName),
		data: map[string]string{},
	}, nil
}

// NewFileSecretStoreAt returns a store backed by an explicit path.
// Tests use this; production goes through NewFileSecretStore.
func NewFileSecretStoreAt(path string) SecretStore {
	return &fileSecretStore{path: path, data: map[string]string{}}
}

// secretKey namespaces a secret by provider so two slskd instances do
// not share credentials.
func secretKey(providerID int64, name string) string {
	return strconv.FormatInt(providerID, 10) + ":" + name
}

// load reads the file once.  A missing file is an empty store, not an
// error — nothing has been configured yet.
func (s *fileSecretStore) load() error {
	if s.loaded {
		return nil
	}

	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.data = map[string]string{}
			s.loaded = true

			return nil
		}

		return fmt.Errorf("read secrets file: %w", err)
	}

	data := map[string]string{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("parse secrets file: %w", err)
	}

	s.data = data
	s.loaded = true

	return nil
}

// save writes the file atomically with restrictive permissions.
func (s *fileSecretStore) save() error {
	raw, err := json.Marshal(s.data)
	if err != nil {
		return fmt.Errorf("encode secrets: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create secrets dir: %w", err)
	}

	// Write to a temp file in the same directory, chmod before the
	// rename, so the secret is never briefly world-readable.
	tmp := s.path + ".tmp"

	if err := os.WriteFile(tmp, raw, secretsFileMode); err != nil {
		return fmt.Errorf("write secrets file: %w", err)
	}

	if err := os.Chmod(tmp, secretsFileMode); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("chmod secrets file: %w", err)
	}

	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("replace secrets file: %w", err)
	}

	return nil
}

// Get returns a stored secret.
func (s *fileSecretStore) Get(providerID int64, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.load(); err != nil {
		return "", err
	}

	v, ok := s.data[secretKey(providerID, name)]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrSecretNotFound, name)
	}

	return v, nil
}

// Set stores or clears a secret.
func (s *fileSecretStore) Set(providerID int64, name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.load(); err != nil {
		return err
	}

	key := secretKey(providerID, name)

	if value == "" {
		delete(s.data, key)
	} else {
		s.data[key] = value
	}

	return s.save()
}

// DeleteProvider drops every secret for a provider.
func (s *fileSecretStore) DeleteProvider(providerID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.load(); err != nil {
		return err
	}

	prefix := strconv.FormatInt(providerID, 10) + ":"

	for k := range s.data {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			delete(s.data, k)
		}
	}

	return s.save()
}

// lookupFor binds a store to one provider, producing the SecretLookup
// handed to constructors.
func lookupFor(store SecretStore, providerID int64) SecretLookup {
	return func(name string) (string, error) {
		if store == nil {
			return "", fmt.Errorf("%w: %s", ErrSecretNotFound, name)
		}

		return store.Get(providerID, name)
	}
}

// memSecretStore is an in-memory SecretStore for tests.
type memSecretStore struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewMemSecretStore returns an in-memory SecretStore.
func NewMemSecretStore() SecretStore {
	return &memSecretStore{data: map[string]string{}}
}

func (s *memSecretStore) Get(providerID int64, name string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.data[secretKey(providerID, name)]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrSecretNotFound, name)
	}

	return v, nil
}

func (s *memSecretStore) Set(providerID int64, name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if value == "" {
		delete(s.data, secretKey(providerID, name))

		return nil
	}

	s.data[secretKey(providerID, name)] = value

	return nil
}

func (s *memSecretStore) DeleteProvider(providerID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prefix := strconv.FormatInt(providerID, 10) + ":"

	for k := range s.data {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			delete(s.data, k)
		}
	}

	return nil
}
