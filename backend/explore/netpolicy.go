package explore

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

// Whether the catalog artifact may be downloaded on this connection
// (plan 016 B4).
//
// The artifact is ~0.6 GB. On a desktop that is a minute of someone
// else's bandwidth; on a phone it can be a month's allowance, and the
// app had no awareness of the difference at all.
//
// Three decisions shape this file.
//
// **The policy lives here and the platform call does not.** `explore` is
// imported by `cmd/indexbuild`, which is built with `CGO_ENABLED=0` in a
// plain Go container, so naming `application` here would break the one
// job that must not fail (see `TestIndexToolsDoNotImportWails`). What is
// injected is a closure; what is *tested* is the parsing and the
// decision, on every platform.
//
// **An unknown answer is not a metered one.** Only mobile answers this
// question — the desktop stub returns an empty string — so a policy that
// treated silence as "metered" would refuse the download on every
// desktop in the world. Silence means "no reason to refuse".
//
// **Cellular is the signal, and it is the only one available.** Wails
// reports `{"connected":bool,"type":"wifi|cellular|ethernet|none"}` and
// no metered flag, so a metered *wifi* — a phone hotspot, a hotel — is
// invisible to us and will not be refused. That is a known gap rather
// than an oversight: Android knows (`NET_CAPABILITY_NOT_METERED`) and
// the runtime does not pass it on.

// ErrMeteredNetwork is returned instead of downloading the catalog when
// the connection looks metered and the user has not opted in. Every
// failure path in `tryCoreArtifact` is already non-fatal, so this
// behaves like any other reason the artifact is not available yet.
var ErrMeteredNetwork = errors.New(
	"explore: catalog download declined on a metered connection",
)

// Network is what the platform can say about the connection.
type Network struct {
	// Known is false when nothing answered — every desktop, and any
	// mobile build whose bridge is not up yet.
	Known bool
	// Connected reports a usable connection of any kind.
	Connected bool
	// Metered reports a connection the user is plausibly paying for by
	// the byte. See the note above on what this cannot see.
	Metered bool
}

// NetworkProbe answers "what kind of connection is this", or an unknown
// Network when the platform does not say.
type NetworkProbe func() Network

// ParseNetworkJSON reads the runtime's network payload.
//
// Anything unparseable is `Known: false` rather than an error: this
// decides whether to *skip* an optional download, and a malformed
// payload is not a reason to refuse one.
func ParseNetworkJSON(payload string) Network {
	var raw struct {
		Connected bool   `json:"connected"`
		Type      string `json:"type"`
	}

	if strings.TrimSpace(payload) == "" {
		return Network{}
	}

	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return Network{}
	}

	return Network{
		Known:     true,
		Connected: raw.Connected,
		Metered:   strings.EqualFold(raw.Type, "cellular"),
	}
}

// networkPolicy is the injected half: how to ask, and whether the user
// has said yes anyway.
type networkPolicy struct {
	mu           sync.RWMutex
	probe        NetworkProbe
	allowMetered func() bool
}

func (p *networkPolicy) set(probe NetworkProbe, allowMetered func() bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.probe = probe
	p.allowMetered = allowMetered
}

// refuses reports whether a large optional download should be skipped.
func (p *networkPolicy) refuses() bool {
	p.mu.RLock()
	probe, allow := p.probe, p.allowMetered
	p.mu.RUnlock()

	if probe == nil {
		return false
	}

	if allow != nil && allow() {
		return false
	}

	state := probe()

	return state.Known && state.Metered
}

// SetNetworkPolicy wires how the catalog download decides whether this
// connection is one to spend 0.6 GB on. Both arguments may be nil, which
// is the desktop's answer: never refuse.
//
//wails:ignore // internal wiring, not part of the app's IPC surface.
func (si *SearchIndex) SetNetworkPolicy(probe NetworkProbe, allowMetered func() bool) {
	si.netPolicy.set(probe, allowMetered)
}

// SetNetworkPolicy wires the metered-connection policy into the index.
//
//wails:ignore // internal wiring, not part of the app's IPC surface.
func (e *Service) SetNetworkPolicy(probe NetworkProbe, allowMetered func() bool) {
	e.index.SetNetworkPolicy(probe, allowMetered)
}
