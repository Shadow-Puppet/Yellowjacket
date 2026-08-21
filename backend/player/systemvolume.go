package player

// Who owns the volume, and what follows when it is not us.
//
// On Android the hardware keys *are* the volume control and the
// framework mixes our stream against the device level, so a second
// control inside the app is a slider that moves something the user
// already moved (#64).  Where that is true the player's own level sits
// at maximum, nothing changes it, and nothing persists it.
//
// **The predicate is named after the capability, not the platform.**
// The frontend asks "is there a volume for me to control", which is a
// question about this build; asking "is this a phone" instead would
// key the answer to a viewport, and an Android tablet at 600px or more
// would then draw the bottom bar's slider over a level pinned at
// maximum -- a control that cannot act, which is the thing
// `library-status-indicator` already settled is worse than none.
//
// **Only `platformOwnsVolume` is behind a build tag**, in two files
// that declare nothing else.  A tagged file is compiled by nothing
// `make lint` or `make test` runs and is untestable off a phone, which
// is the reasoning `mediacontrols/androidpayload.go` states for
// keeping its contract out of one -- so everything decidable here is
// decided against `Player.systemVolume`, a field a test sets either
// way, and the tag decides only what that field starts as.
//
// The one thing this must not disturb is ducking.  `SetDuck` applies
// its attenuation by re-applying the *user's* level through
// `setVolumeLocked`, so pinning that level to maximum leaves the
// offset arithmetic exactly as it was: an OS asking us to get out of
// the way of a navigation prompt is not the user setting a volume, and
// it is the only thing that may move the output on such a platform.

// SystemOwnsVolume reports whether the platform's own control is the
// only volume control there is, so this app neither offers one nor
// remembers a level.
//
// It is bound: the frontend renders no `<volume-control>` when it is
// true, at any width.
func (p *Player) SystemOwnsVolume() bool {
	return p.systemVolume
}
