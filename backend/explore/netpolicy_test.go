package explore

import (
	"errors"
	"testing"
)

// The catalog is ~0.6 GB and the decision not to fetch it is the only
// part of plan 016 B4 that can be tested anywhere but on a phone: the
// platform call is a one-line closure injected from app.go, and
// everything that decides anything is here.

func TestParseNetworkJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		want    Network
	}{{
		name:    "cellular is metered",
		payload: `{"connected":true,"type":"cellular"}`,
		want:    Network{Known: true, Connected: true, Metered: true},
	}, {
		name:    "wifi is not",
		payload: `{"connected":true,"type":"wifi"}`,
		want:    Network{Known: true, Connected: true},
	}, {
		name:    "ethernet is not",
		payload: `{"connected":true,"type":"ethernet"}`,
		want:    Network{Known: true, Connected: true},
	}, {
		name:    "the case is the platform's business, not ours",
		payload: `{"connected":true,"type":"Cellular"}`,
		want:    Network{Known: true, Connected: true, Metered: true},
	}, {
		name:    "offline is known and unmetered",
		payload: `{"connected":false,"type":"none"}`,
		want:    Network{Known: true},
	}, {
		// The desktop stub. This is the case that must not read as
		// "metered": every desktop in the world answers this way.
		name:    "an empty payload is unknown",
		payload: "",
		want:    Network{},
	}, {
		name:    "so is a malformed one",
		payload: `{"connected":`,
		want:    Network{},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ParseNetworkJSON(tt.payload); got != tt.want {
				t.Errorf("ParseNetworkJSON(%q) = %+v, want %+v", tt.payload, got, tt.want)
			}
		})
	}
}

func TestNetworkPolicyRefuses(t *testing.T) {
	t.Parallel()

	cellular := func() Network {
		return Network{Known: true, Connected: true, Metered: true}
	}
	wifi := func() Network { return Network{Known: true, Connected: true} }
	unknown := func() Network { return Network{} }
	yes := func() bool { return true }
	no := func() bool { return false }

	tests := []struct {
		name         string
		probe        NetworkProbe
		allowMetered func() bool
		want         bool
	}{{
		name:  "no probe wired refuses nothing",
		probe: nil,
		want:  false,
	}, {
		name:  "an unknown connection refuses nothing",
		probe: unknown,
		want:  false,
	}, {
		name:  "wifi refuses nothing",
		probe: wifi,
		want:  false,
	}, {
		name:  "cellular refuses by default",
		probe: cellular,
		want:  true,
	}, {
		name:         "cellular with no permission refuses",
		probe:        cellular,
		allowMetered: no,
		want:         true,
	}, {
		name:         "cellular the user opted into does not",
		probe:        cellular,
		allowMetered: yes,
		want:         false,
	}, {
		// The permission is read at decision time rather than captured,
		// so turning it on takes effect on the next attempt instead of
		// the next launch.
		name:         "permission is asked, not remembered",
		probe:        cellular,
		allowMetered: yes,
		want:         false,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var p networkPolicy

			p.set(tt.probe, tt.allowMetered)

			if got := p.refuses(); got != tt.want {
				t.Errorf("refuses() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The gate has to come before anything is staged: a declined download is
// a no-op, not a job in the indicator or a status the user must dismiss.
func TestTryCoreArtifactDeclinesMeteredWithoutStaging(t *testing.T) {
	t.Parallel()

	si := &SearchIndex{}

	si.SetNetworkPolicy(
		func() Network { return Network{Known: true, Connected: true, Metered: true} },
		nil,
	)

	err := si.tryCoreArtifact(t.Context())

	if !errors.Is(err, ErrMeteredNetwork) {
		t.Fatalf("tryCoreArtifact() error = %v, want ErrMeteredNetwork", err)
	}

	// Nothing announced itself: no build status, no tiers, no job. A
	// SearchIndex with no database would panic on any of the work below
	// the gate, which is itself part of the assertion.
	if si.buildStatus.Building {
		t.Error("declining a metered download still reported a build in progress")
	}

	if len(si.buildStatus.Tiers) != 0 {
		t.Errorf("declining staged %d tiers, want none", len(si.buildStatus.Tiers))
	}
}
