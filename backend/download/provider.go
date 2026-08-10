package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"sync"
)

// Provider errors.
var (
	// ErrUnknownKind is returned when a stored provider row names a kind
	// no constructor is registered for — an old row after a provider
	// was removed, or a config file from a newer build.
	ErrUnknownKind = errors.New("unknown provider kind")

	// ErrNotConfigured means the provider exists but is missing
	// required settings (host, API key) and cannot be used yet.
	ErrNotConfigured = errors.New("provider is not configured")

	// ErrUnsupported is returned when a caller asks a provider for a
	// role it does not fill.
	ErrUnsupported = errors.New("provider does not support this operation")

	// ErrNoTransport means a search-only provider produced a candidate
	// whose protocol no enabled transport can fetch.
	ErrNoTransport = errors.New("no enabled transport handles this protocol")
)

// Provider is the common surface every adapter implements.  The three
// role interfaces below are optional and discovered by type assertion,
// gated on what Caps declares.
type Provider interface {
	// Info returns the provider's identity and declared capabilities.
	Info() ProviderInfo

	// Check verifies the provider is reachable and configured
	// correctly.  It backs the "test connection" button, and the
	// pipeline calls it before first use in a session.
	Check(ctx context.Context) error

	// Close releases any long-lived resources (sessions, cookies).
	Close() error
}

// Searcher turns a request into candidates.  Implementations must
// respect ctx deadlines: the pipeline searches providers concurrently
// with a per-provider timeout and takes whatever came back in time.
type Searcher interface {
	Search(ctx context.Context, dl Download) ([]Candidate, error)
}

// Transporter moves a candidate's bytes into dst, which the pipeline
// has already created and which the transport owns for the duration.
//
// Implementations report progress through onProgress (best-effort, may
// be nil) and must return promptly when ctx is cancelled, leaving
// partial files in place — the pipeline sweeps them.
type Transporter interface {
	Grab(
		ctx context.Context,
		c Candidate,
		dst string,
		onProgress ProgressFunc,
	) (Result, error)
}

// Delegator hands the whole request to an external manager.  Unlike a
// Transporter we do not own the transfer, so the pipeline polls until
// the manager reports terminal state.
type Delegator interface {
	// Delegate submits the request and returns the manager's own ID.
	Delegate(ctx context.Context, dl Download) (string, error)

	// Poll reports on a previously delegated request.
	Poll(ctx context.Context, externalID string) (DelegateStatus, error)

	// Withdraw asks the manager to drop the request.  Best-effort.
	Withdraw(ctx context.Context, externalID string) error
}

// Lister is a provider that keeps a persistent wanted list of its own —
// Lidarr monitoring an artist, say.  It is the fourth role, and it
// exists because for those systems "I want this" is a durable statement
// they already model, and mirroring it there means the user's intent
// survives in the place they will look for it.
//
// Sync through this interface is one-directional in the loop: this app
// pushes, the external system receives.  Pulling happens only when the
// user explicitly imports.
type Lister interface {
	// PushRequest records a request in the provider's own list and
	// returns the provider's identifier for it.  Implementations must
	// be idempotent: pushing a request the provider already has returns
	// the existing identifier rather than duplicating it.
	PushRequest(ctx context.Context, r Request) (string, error)

	// RemoveRequest drops a previously pushed request.  Best-effort.
	RemoveRequest(ctx context.Context, externalID string) error

	// ListRequests reads the provider's list back, for the deliberate
	// import path.  LibraryID is filled in by the caller.
	ListRequests(ctx context.Context) ([]Request, error)
}

// ProviderInfo is a provider's identity as the frontend sees it.
type ProviderInfo struct {
	ID      int64  `json:"id"`
	Kind    Kind   `json:"kind"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`

	// Priority breaks ties between providers that found equally good
	// candidates.  Higher wins; default 50.
	Priority int `json:"priority"`

	Caps Caps `json:"caps"`
}

// Config is a provider's stored settings.  Secret values are not held
// here — they live in the secrets store keyed by provider ID, so a
// config blob can be logged or shown in the UI without redaction.
type Config struct {
	ID       int64             `json:"id"`
	Kind     Kind              `json:"kind"`
	Name     string            `json:"name"`
	Enabled  bool              `json:"enabled"`
	Priority int               `json:"priority"`
	Settings map[string]string `json:"settings"`

	// SetSecrets names which of the descriptor's secret fields already
	// have a stored value, without exposing it.  Populated only when a
	// Config is built for the frontend (see Service.withSecretFlags);
	// empty when read from or written to the store.
	SetSecrets map[string]bool `json:"setSecrets,omitempty"`
}

// Setting returns a config value, or fallback when unset.
func (c Config) Setting(key, fallback string) string {
	if v, ok := c.Settings[key]; ok && v != "" {
		return v
	}

	return fallback
}

// Constructor builds a provider from its stored config.  The secret
// lookup is passed in rather than the secret itself so a provider can
// fetch several (username and password, say) and so nothing forces the
// secret into a struct field that might get logged.
type Constructor func(
	cfg Config,
	secrets SecretLookup,
	logger *slog.Logger,
) (Provider, error)

// SecretLookup retrieves a named secret for a provider.
type SecretLookup func(name string) (string, error)

// registry maps provider kinds to their constructors.  Adapters
// register themselves in an init function, so adding a provider does
// not require editing this file.
var (
	registryMu   sync.RWMutex
	constructors = map[Kind]Constructor{}
	descriptors  = map[Kind]Descriptor{}
)

// Descriptor is the static, instance-independent description of a
// provider kind: what it is called, what it can do, and which settings
// it needs.  The settings page renders its form from this, so a new
// provider gets a config UI without any frontend work.
type Descriptor struct {
	Kind Kind   `json:"kind"`
	Name string `json:"name"`

	// Summary is one line explaining what connecting this gets you.
	Summary string `json:"summary"`

	// Caps are the kind's inherent capabilities, before configuration.
	Caps Caps `json:"caps"`

	// Fields are the settings the user must supply.
	Fields []Field `json:"fields"`

	// RequiresExternal names the software the user must run themselves
	// (a slskd daemon, a Lidarr instance), or is empty for providers
	// that need nothing but a binary on PATH.
	RequiresExternal string `json:"requiresExternal,omitempty"`
}

// Field describes one provider setting for the settings form.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`

	// Secret marks a value stored in the secrets store rather than the
	// provider config row, and rendered as a password input.
	Secret bool `json:"secret"`

	// Path marks a value that names a local filesystem directory, so
	// the settings form can offer a native folder picker beside the
	// text input rather than making the user type or paste it.
	Path bool `json:"path"`

	Required bool   `json:"required"`
	Default  string `json:"default,omitempty"`
}

// Register makes a provider kind available.  Called from adapter init
// functions; panics on a duplicate kind because that is a build-time
// programming error, not a runtime condition.
func Register(d Descriptor, c Constructor) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := constructors[d.Kind]; exists {
		panic("download: duplicate provider kind " + string(d.Kind))
	}

	// Every provider that moves bytes gets a transfer limit it can be
	// tuned with, appended here rather than repeated in each adapter's
	// descriptor: the setting means the same thing everywhere, only the
	// sensible default differs.
	if d.Caps.CanTransport {
		d.Fields = append(d.Fields, concurrencyField(d.Kind))
	}

	constructors[d.Kind] = c
	descriptors[d.Kind] = d
}

// concurrencyField describes the per-provider transfer limit, with help
// text explaining why the default is what it is — a user who raises
// slskd from 1 to 8 and gets themselves queued behind every other
// Soulseek user deserves to have been warned.
func concurrencyField(k Kind) Field {
	help := "Maximum simultaneous transfers from this client."

	if k == KindSlskd {
		help = "Maximum simultaneous transfers. Soulseek peers serve " +
			"one file at a time and queue or ban clients that ask for " +
			"more, so 1 is both the polite setting and usually the " +
			"fastest."
	}

	return Field{
		Key:     concurrencyKey,
		Label:   "Simultaneous transfers",
		Help:    help,
		Default: strconv.Itoa(kindConcurrency[k]),
	}
}

// New builds a provider instance from stored config.
func New(
	cfg Config,
	secrets SecretLookup,
	logger *slog.Logger,
) (Provider, error) {
	registryMu.RLock()

	ctor, ok := constructors[cfg.Kind]

	registryMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKind, cfg.Kind)
	}

	p, err := ctor(cfg, secrets, logger)
	if err != nil {
		return nil, fmt.Errorf("build %s provider: %w", cfg.Kind, err)
	}

	return p, nil
}

// Descriptors returns every registered provider kind, name-ordered, for
// the "add a download client" picker.
func Descriptors() []Descriptor {
	registryMu.RLock()
	defer registryMu.RUnlock()

	out := make([]Descriptor, 0, len(descriptors))

	for _, d := range descriptors {
		if d.Kind == KindFake {
			continue
		}

		out = append(out, d)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})

	return out
}

// DescriptorFor returns the descriptor for a kind.
func DescriptorFor(k Kind) (Descriptor, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	d, ok := descriptors[k]

	return d, ok
}

// asSearcher returns the provider's Searcher role, gated on Caps so a
// type that implements the method but declares it unsupported (because
// it is misconfigured) is not used.
func asSearcher(p Provider) (Searcher, bool) {
	if !p.Info().Caps.CanSearch {
		return nil, false
	}

	s, ok := p.(Searcher)

	return s, ok
}

// asTransporter returns the provider's Transporter role.
func asTransporter(p Provider) (Transporter, bool) {
	if !p.Info().Caps.CanTransport {
		return nil, false
	}

	t, ok := p.(Transporter)

	return t, ok
}

// asDelegator returns the provider's Delegator role.
func asDelegator(p Provider) (Delegator, bool) {
	if !p.Info().Caps.CanDelegate {
		return nil, false
	}

	d, ok := p.(Delegator)

	return d, ok
}

// asLister returns the provider's Lister role.
func asLister(p Provider) (Lister, bool) {
	if !p.Info().Caps.CanList {
		return nil, false
	}

	l, ok := p.(Lister)

	return l, ok
}
