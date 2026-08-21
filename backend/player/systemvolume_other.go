//go:build !android

package player

// platformOwnsVolume is false everywhere but Android: a desktop mixer
// is per-application, so our level is the one the user reaches for.
//
// See systemvolume.go for why this constant is the whole of what a
// build tag decides here.
const platformOwnsVolume = false
