//go:build android

package player

// platformOwnsVolume is true on Android: volume is the device's, set
// with the hardware keys, and `mediacontrols`' Android handler
// implements no volume callback for the same reason.
//
// See systemvolume.go for why this constant is the whole of what a
// build tag decides here.
const platformOwnsVolume = true
