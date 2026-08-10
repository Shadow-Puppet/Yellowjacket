package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"yellowjacket/backend/events"
)

// ErrNoLibrary means the request did not name a library to attach the
// download to.  Letting it through would hit the download_requests /
// download_wants foreign key on library_id and surface as a raw SQLite
// error, so it is rejected here with a message the UI can show.
var ErrNoLibrary = errors.New("no library selected")

// Service is the frontend-facing surface of the download subsystem.
// Its methods are bound into Wails and called from TypeScript, so
// signatures use plain types and return errors the UI can render.
type Service struct {
	logger  *slog.Logger
	manager *Manager
	store   *Store
	secrets SecretStore

	// reconciler works the wanted list.  Optional; nil means wants are
	// stored but never acted on.
	reconciler *Reconciler

	ctx context.Context
}

// NewService builds the bound service.
func NewService(
	logger *slog.Logger,
	manager *Manager,
	store *Store,
	secrets SecretStore,
) *Service {
	return &Service{
		logger:  logger,
		manager: manager,
		store:   store,
		secrets: secrets,
	}
}

// SetContext injects the Wails runtime context for event emission.
func (s *Service) SetContext(ctx context.Context) {
	s.ctx = ctx
}

// emit publishes an event, tolerating a service that has no runtime
// context yet.  Emitting on a non-runtime context is fatal in Wails, so
// the nil check is load-bearing rather than defensive.
func (s *Service) emit(name string, data ...any) {
	if s.ctx == nil {
		return
	}

	runtime.EventsEmit(s.ctx, name, data...)
}

// ---------------------------------------------------------------------------
// Provider configuration
// ---------------------------------------------------------------------------

// ProviderKinds returns every provider type that can be added, with the
// settings each one needs.  The settings page renders its forms from
// this, so a new adapter needs no frontend change.
func (s *Service) ProviderKinds() []Descriptor {
	return Descriptors()
}

// ListProviders returns the user's configured download clients.
func (s *Service) ListProviders() ([]Config, error) {
	cfgs, err := s.store.ListProviders(context.Background())
	if err != nil {
		return nil, err
	}

	for i := range cfgs {
		cfgs[i].SetSecrets = s.setSecretFlags(cfgs[i])
	}

	return cfgs, nil
}

// setSecretFlags reports, for each secret field the provider's kind
// declares, whether a value is already stored — so the settings form
// can distinguish an unset secret from one it simply isn't shown.
func (s *Service) setSecretFlags(cfg Config) map[string]bool {
	desc, ok := DescriptorFor(cfg.Kind)
	if !ok {
		return nil
	}

	flags := map[string]bool{}

	for _, field := range desc.Fields {
		if !field.Secret {
			continue
		}

		v, err := s.secrets.Get(cfg.ID, field.Key)
		flags[field.Key] = err == nil && v != ""
	}

	return flags
}

// AddProvider creates a provider and stores any secret settings
// separately.  Secrets arrive in the same map as ordinary settings
// because that is what the form submits; they are split out here and
// never written to the provider row.
func (s *Service) AddProvider(
	kind string,
	name string,
	settings map[string]string,
) (int64, error) {
	desc, ok := DescriptorFor(Kind(kind))
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}

	plain, secret := splitSecrets(desc, settings)

	id, err := s.store.CreateProvider(context.Background(), Config{
		Kind:     Kind(kind),
		Name:     name,
		Enabled:  true,
		Priority: 50,
		Settings: plain,
	})
	if err != nil {
		return 0, err
	}

	for k, v := range secret {
		if err := s.secrets.Set(id, k, v); err != nil {
			return 0, err
		}
	}

	if err := s.manager.Reload(context.Background()); err != nil {
		return 0, err
	}

	s.emit(events.DownloadProvidersChanged)

	return id, nil
}

// UpdateProvider saves changes to a provider.  A secret field left
// blank keeps its stored value rather than clearing it — the form does
// not echo secrets back, so an empty box means "unchanged", not
// "delete".
func (s *Service) UpdateProvider(
	id int64,
	name string,
	enabled bool,
	priority int,
	settings map[string]string,
) error {
	ctx := context.Background()

	existing, err := s.store.GetProvider(ctx, id)
	if err != nil {
		return err
	}

	desc, ok := DescriptorFor(existing.Kind)
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKind, existing.Kind)
	}

	plain, secret := splitSecrets(desc, settings)

	if err := s.store.UpdateProvider(ctx, Config{
		ID:       id,
		Kind:     existing.Kind,
		Name:     name,
		Enabled:  enabled,
		Priority: priority,
		Settings: plain,
	}); err != nil {
		return err
	}

	for k, v := range secret {
		if v == "" {
			continue
		}

		if err := s.secrets.Set(id, k, v); err != nil {
			return err
		}
	}

	if err := s.manager.Reload(ctx); err != nil {
		return err
	}

	s.emit(events.DownloadProvidersChanged)

	return nil
}

// DeleteProvider removes a provider and its credentials.
func (s *Service) DeleteProvider(id int64) error {
	ctx := context.Background()

	if err := s.store.DeleteProvider(ctx, id); err != nil {
		return err
	}

	if err := s.secrets.DeleteProvider(id); err != nil {
		s.logger.Warn(
			"could not delete provider secrets", "provider", id, "error", err,
		)
	}

	if err := s.manager.Reload(ctx); err != nil {
		return err
	}

	s.emit(events.DownloadProvidersChanged)

	return nil
}

// TestProvider backs the "test connection" button.  It builds the
// provider from its stored config and asks it to check itself, so the
// result reflects exactly what a real search would use.
func (s *Service) TestProvider(id int64) error {
	ctx := context.Background()

	cfg, err := s.store.GetProvider(ctx, id)
	if err != nil {
		return err
	}

	p, err := New(cfg, lookupFor(s.secrets, id), s.logger)
	if err != nil {
		return err
	}

	defer func() { _ = p.Close() }()

	if err := p.Check(ctx); err != nil {
		return fmt.Errorf("%s: %w", cfg.Name, err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Requests
// ---------------------------------------------------------------------------

// SearchRequest is what the frontend submits to start a download.
type SearchRequest struct {
	LibraryID        int64           `json:"libraryId"`
	ReleaseMBID      string          `json:"releaseMbid"`
	ReleaseGroupMBID string          `json:"releaseGroupMbid"`
	Artist           string          `json:"artist"`
	Album            string          `json:"album"`
	Query            string          `json:"query"`
	Expected         []ExpectedTrack `json:"expected"`
}

// StartResult is what the picker needs after a search.
type StartResult struct {
	RequestID  string      `json:"requestId"`
	Candidates []Candidate `json:"candidates"`

	// AutoPicked reports that the pipeline already chose and is
	// downloading, so the picker should show progress rather than a
	// list of choices.
	AutoPicked bool `json:"autoPicked"`
}

// Start searches for a release and either auto-picks a clear winner or
// returns ranked candidates for the user to choose from.
func (s *Service) Start(req SearchRequest) (StartResult, error) {
	if req.LibraryID <= 0 {
		return StartResult{}, ErrNoLibrary
	}

	r := Request{
		ID:               newID(),
		LibraryID:        req.LibraryID,
		ReleaseMBID:      req.ReleaseMBID,
		ReleaseGroupMBID: req.ReleaseGroupMBID,
		Artist:           req.Artist,
		Album:            req.Album,
		Query:            req.Query,
		Expected:         req.Expected,
	}

	candidates, err := s.manager.Start(context.Background(), r)
	if err != nil {
		return StartResult{}, err
	}

	result := StartResult{
		RequestID:  r.ID,
		Candidates: candidates,
		AutoPicked: AutoPickable(r, candidates),
	}

	s.emit(events.DownloadsChanged)

	return result, nil
}

// Pick starts the transfer for the candidate the user chose.
func (s *Service) Pick(requestID, candidateID string) error {
	if err := s.manager.Pick(
		context.Background(), requestID, candidateID,
	); err != nil {
		return err
	}

	s.emit(events.DownloadsChanged)

	return nil
}

// Cancel aborts a live request.
func (s *Service) Cancel(requestID string) error {
	if err := s.manager.Cancel(context.Background(), requestID); err != nil {
		return err
	}

	s.emit(events.DownloadsChanged)

	return nil
}

// Candidates returns the ranked candidates of a live request, so the
// picker can be reopened without searching again.
func (s *Service) Candidates(requestID string) []Candidate {
	return s.manager.Candidates(requestID)
}

// RequestView is one row of the downloads list.
type RequestView struct {
	Request

	State State  `json:"state"`
	Error string `json:"error,omitempty"`
	Items []Item `json:"items"`
}

// ListRequests returns recent download requests, newest first.
func (s *Service) ListRequests(limit int) ([]RequestView, error) {
	const defaultLimit = 50

	if limit <= 0 {
		limit = defaultLimit
	}

	ctx := context.Background()

	requests, err := s.store.ListRequests(ctx, limit)
	if err != nil {
		return nil, err
	}

	out := make([]RequestView, 0, len(requests))

	for _, r := range requests {
		state, errText, err := s.store.GetRequestState(ctx, r.ID)
		if err != nil {
			return nil, err
		}

		items, err := s.store.ListItemsForRequest(ctx, r.ID)
		if err != nil {
			return nil, err
		}

		out = append(out, RequestView{
			Request: r,
			State:   state,
			Error:   errText,
			Items:   items,
		})
	}

	return out, nil
}

// ClearFinished removes terminal requests from the list.
func (s *Service) ClearFinished() error {
	if err := s.store.ClearFinished(context.Background()); err != nil {
		return err
	}

	s.emit(events.DownloadsChanged)

	return nil
}

// ---------------------------------------------------------------------------
// Wanted list
// ---------------------------------------------------------------------------

// SetReconciler wires the wanted-list loop.  Optional: without it the
// wanted list still stores and lists wants, it just never acts on them.
func (s *Service) SetReconciler(r *Reconciler) {
	s.reconciler = r
}

// WantRequest is what the frontend submits to want something.  It is
// one MBID and the type of thing it names, because that is genuinely
// all a want is.
type WantRequest struct {
	MBID      string `json:"mbid"`
	Entity    string `json:"entity"`
	LibraryID int64  `json:"libraryId"`

	// Artist and Title are display text only, and optional: the
	// reconciler fills them in from the catalog when the caller has
	// nothing but an MBID.
	Artist string `json:"artist"`
	Title  string `json:"title"`

	// Scope and Secondary apply to artist wants.
	Scope     string `json:"scope"`
	Secondary bool   `json:"secondary"`
}

// AddWant puts something on the wanted list and asks for a reconcile
// pass, so the user sees something happen rather than waiting six hours
// for the next scheduled one.
func (s *Service) AddWant(req WantRequest) (int64, error) {
	if req.LibraryID <= 0 {
		return 0, ErrNoLibrary
	}

	entity := Entity(req.Entity)
	if !entity.Valid() {
		return 0, fmt.Errorf("%w: entity %q", ErrUnsupported, req.Entity)
	}

	scope := WantScope(req.Scope)
	if scope != ScopeAll {
		scope = ScopeFuture
	}

	id, err := s.store.AddWant(context.Background(), Want{
		MBID:      req.MBID,
		Entity:    entity,
		LibraryID: req.LibraryID,
		Artist:    req.Artist,
		Title:     req.Title,
		Scope:     scope,
		Secondary: req.Secondary,
	})
	if err != nil {
		return 0, err
	}

	s.emit(events.WantedListChanged)

	if s.reconciler != nil {
		s.reconciler.Trigger()
	}

	return id, nil
}

// ListWants returns the whole wanted list.
func (s *Service) ListWants() ([]Want, error) {
	return s.store.ListWants(context.Background())
}

// IsWanted answers the Explore pages' question — should this album show
// "want" or "wanted?" — without making them load the whole list.
func (s *Service) IsWanted(mbid string, libraryID int64) (bool, error) {
	_, found, err := s.store.FindWant(context.Background(), mbid, libraryID)

	return found, err
}

// RemoveWant takes something off the list.  Removing an artist takes
// its derived albums with it, by cascade; an album the user pinned
// themselves has no parent and survives.
func (s *Service) RemoveWant(id int64) error {
	ctx := context.Background()

	// Tell any external list first, while the row is still readable.
	s.withdrawExternal(ctx, id)

	if err := s.store.DeleteWant(ctx, id); err != nil {
		return err
	}

	s.emit(events.WantedListChanged)

	return nil
}

// PauseWant stops attempts without forgetting the want.
func (s *Service) PauseWant(id int64, paused bool) error {
	state := WantStateWanted
	if paused {
		state = WantStatePaused
	}

	if err := s.store.SetWantState(
		context.Background(), id, state, "",
	); err != nil {
		return err
	}

	s.emit(events.WantedListChanged)

	return nil
}

// ClearSatisfiedWants drops everything already owned.
func (s *Service) ClearSatisfiedWants() error {
	if err := s.store.ClearSatisfiedWants(context.Background()); err != nil {
		return err
	}

	s.emit(events.WantedListChanged)

	return nil
}

// ReconcileWanted runs a pass now and reports what it did.  This backs
// the "check now" button, so it runs synchronously: the user pressed it
// and is waiting for an answer.
func (s *Service) ReconcileWanted() (Summary, error) {
	if s.reconciler == nil {
		return Summary{}, fmt.Errorf(
			"%w: the wanted list is not running", ErrUnsupported,
		)
	}

	summary, err := s.reconciler.RunOnce(context.Background())
	if err != nil {
		return summary, err
	}

	s.emit(events.WantedListChanged)

	return summary, nil
}

// ImportExternalWants adopts a provider's own list — "import the
// artists Lidarr is already monitoring".
func (s *Service) ImportExternalWants(
	providerID int64,
	libraryID int64,
) (int, error) {
	if s.reconciler == nil {
		return 0, fmt.Errorf(
			"%w: the wanted list is not running", ErrUnsupported,
		)
	}

	n, err := s.reconciler.ImportExternal(
		context.Background(), providerID, libraryID,
	)
	if err != nil {
		return 0, err
	}

	s.emit(events.WantedListChanged)

	return n, nil
}

// withdrawExternal best-effort unmonitors a want in the external lists
// it was pushed to.  Failures are logged and ignored: the user asked to
// remove it from *this* list, and an unreachable Lidarr is not a reason
// to refuse.
func (s *Service) withdrawExternal(ctx context.Context, id int64) {
	w, err := s.store.GetWant(ctx, id)
	if err != nil || len(w.ExternalIDs) == 0 {
		return
	}

	for key, externalID := range w.ExternalIDs {
		providerID, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			continue
		}

		l, ok := s.manager.listers()[providerID]
		if !ok {
			continue
		}

		if err := l.RemoveWant(ctx, externalID); err != nil {
			s.logger.Debug(
				"could not withdraw want from external list",
				"want", id,
				"provider", providerID,
				"error", err,
			)
		}
	}
}

// splitSecrets partitions a submitted settings map into plain values,
// which go in the provider row, and secret values, which go in the
// secret store.  The split is driven by the descriptor so a provider
// declaring a field secret is enough to keep it out of the database.
func splitSecrets(
	desc Descriptor,
	settings map[string]string,
) (plain, secret map[string]string) {
	plain = make(map[string]string, len(settings))
	secret = make(map[string]string)

	secretKeys := make(map[string]bool, len(desc.Fields))

	for _, f := range desc.Fields {
		if f.Secret {
			secretKeys[f.Key] = true
		}
	}

	for k, v := range settings {
		if secretKeys[k] {
			secret[k] = v

			continue
		}

		plain[k] = v
	}

	return plain, secret
}
