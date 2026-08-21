package player

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopxl/beep/v2/effects"

	"yellowjacket/backend/database"
)

// pinnedPlayer is a player on a platform whose volume belongs to the
// device.  The field is set rather than the build constant read,
// because the constant is true on exactly one platform and no tier
// here runs on it -- see systemvolume.go.
func pinnedPlayer(t *testing.T, db *database.DB) *Player {
	t.Helper()

	p := NewPlayer(slog.Default(), db)
	p.systemVolume = true
	p.volume = &effects.Volume{Base: 2}
	p.setVolumeLocked(MaxUserVol)

	return p
}

// TestSystemVolumeRefusesEveryWayToChangeTheLevel is the first half of
// #64: where the device owns the volume, ours sits at maximum and none
// of the three routes to a level moves it.  Mute is in that list
// because it is a level of zero by another name, and because with no
// control rendered it is the one state on such a platform there would
// be nothing to get out of.
func TestSystemVolumeRefusesEveryWayToChangeTheLevel(t *testing.T) {
	t.Parallel()

	p := pinnedPlayer(t, nil)

	if !p.SystemOwnsVolume() {
		t.Fatal("SystemOwnsVolume() = false on a pinned player")
	}

	if got := p.getUserVolume(); got != MaxUserVol {
		t.Errorf("starting volume = %d, want %d", got, MaxUserVol)
	}

	p.SetVolume(20)

	if got := p.getUserVolume(); got != MaxUserVol {
		t.Errorf("volume after SetVolume(20) = %d, want %d", got, MaxUserVol)
	}

	if err := p.ChangeVolume(-30); err != nil {
		t.Fatalf("ChangeVolume: %v", err)
	}

	if got := p.getUserVolume(); got != MaxUserVol {
		t.Errorf("volume after ChangeVolume(-30) = %d, want %d", got, MaxUserVol)
	}

	if err := p.MuteToggle(); err != nil {
		t.Fatalf("MuteToggle: %v", err)
	}

	if p.volume.Silent {
		t.Error("MuteToggle silenced a player whose volume the system owns")
	}
}

// TestAnUnpinnedPlayerStillChangesItsVolume is the other side of the
// same switch.  Without it the test above passes on a player that
// refuses everything, which is what a mis-wired field would produce.
func TestAnUnpinnedPlayerStillChangesItsVolume(t *testing.T) {
	t.Parallel()

	p := NewPlayer(slog.Default(), nil)
	p.volume = &effects.Volume{Base: 2}
	p.setVolumeLocked(MaxUserVol)

	if p.SystemOwnsVolume() {
		t.Fatal("SystemOwnsVolume() = true off Android")
	}

	p.SetVolume(20)

	if got := p.getUserVolume(); got != 20 {
		t.Errorf("volume after SetVolume(20) = %d, want 20", got)
	}

	if err := p.MuteToggle(); err != nil {
		t.Fatalf("MuteToggle: %v", err)
	}

	if !p.volume.Silent {
		t.Error("MuteToggle did not silence an ordinary player")
	}
}

// TestSystemVolumeStillDucks is the issue's second Finding, made a
// test: pinning the user's level must leave the OS's attenuation
// working, because a duck is not a volume the user chose and is the
// only thing that may move the output on such a platform.
func TestSystemVolumeStillDucks(t *testing.T) {
	t.Parallel()

	p := pinnedPlayer(t, nil)
	open := p.volume.Volume

	p.SetDuck(true)

	if p.volume.Volume >= open {
		t.Errorf(
			"ducked output = %v, want less than %v", p.volume.Volume, open,
		)
	}

	if got := p.getUserVolume(); got != MaxUserVol {
		t.Errorf("user volume while ducked = %d, want %d", got, MaxUserVol)
	}

	// A refused SetVolume must not disturb the offset either: it
	// returns before setVolumeLocked, which is what re-applies it.
	ducked := p.volume.Volume

	p.SetVolume(10)

	if p.volume.Volume != ducked {
		t.Errorf(
			"output after a refused SetVolume = %v, want %v",
			p.volume.Volume, ducked,
		)
	}

	p.SetDuck(false)

	if p.volume.Volume != open {
		t.Errorf("output after unduck = %v, want %v", p.volume.Volume, open)
	}
}

// TestSystemVolumeWritesBackTheLevelItFound is the rest of the
// Direction: "make sure nothing writes a persisted volume from that
// platform".  The maximum the player runs at is synthetic, so saving
// must not record it over whatever the row already said.
func TestSystemVolumeWritesBackTheLevelItFound(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	// A level set by some earlier, unpinned session.
	writer := NewPlayer(slog.Default(), db)
	writer.volume = &effects.Volume{Base: 2}
	writer.setVolumeLocked(30)
	writer.SaveState()

	p := pinnedPlayer(t, db)
	p.RestoreState()

	if got := p.getUserVolume(); got != MaxUserVol {
		t.Errorf("restored volume = %d, want %d (the level is pinned)", got, MaxUserVol)
	}

	if p.volume.Silent {
		t.Error("restore muted a player whose volume the system owns")
	}

	p.SaveState()

	state, err := db.Queries.GetPlayerState(db.Ctx)
	if err != nil {
		t.Fatalf("GetPlayerState: %v", err)
	}

	if state.Volume != 30 {
		t.Errorf("persisted volume = %d, want 30 (untouched)", state.Volume)
	}
}

// TestPlatformVolumeOwnershipIsDeclaredOncePerPlatform sweeps the
// source, because the pair of tagged files is the one thing here no
// tier compiles both halves of: `make lint` and `make test` build the
// `!android` side only, so a deleted or edited android file fails
// nothing until somebody has a phone in their hand.
func TestPlatformVolumeOwnershipIsDeclaredOncePerPlatform(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"systemvolume_other.go":   "const platformOwnsVolume = false",
		"systemvolume_android.go": "const platformOwnsVolume = true",
	}

	tags := map[string]string{
		"systemvolume_other.go":   "//go:build !android",
		"systemvolume_android.go": "//go:build android",
	}

	for name, decl := range want {
		src, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Errorf("%s: %v", name, err)

			continue
		}

		if !strings.Contains(string(src), decl) {
			t.Errorf("%s does not declare %q", name, decl)
		}

		if !strings.Contains(string(src), tags[name]) {
			t.Errorf("%s does not carry %q", name, tags[name])
		}
	}
}
