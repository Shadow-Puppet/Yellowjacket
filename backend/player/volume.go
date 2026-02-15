package player

// UserVolume represents volume on a user-facing scale (0-100).
type UserVolume int

// Volume represents volume on an internal scale (-4 to 0).
type Volume float64

// User volume range bounds.
const (
	MinUserVol UserVolume = 0
	MaxUserVol UserVolume = 100
)

// Internal volume range bounds.
const (
	MinVol Volume = -6
	MaxVol Volume = 0
)

// ToVolume converts user volume to internal player volume.
func (oldVol UserVolume) ToVolume() Volume {
	var newVol Volume

	if oldVol >= MinUserVol && oldVol <= MaxUserVol {
		ratio := Volume(oldVol-MinUserVol) / Volume(MaxUserVol-MinUserVol)
		newVol = ratio*(MaxVol-MinVol) + MinVol
	}

	return newVol
}

// ToUserVolume converts internal player volume to user volume.
func (oldVolFloat Volume) ToUserVolume() UserVolume {
	var newVol UserVolume

	if oldVolFloat >= MinVol && oldVolFloat <= MaxVol {
		ratio := (oldVolFloat - MinVol) / (MaxVol - MinVol)
		newVol = UserVolume(ratio*Volume(MaxUserVol-MinUserVol)) + MinUserVol
	}

	return newVol
}

func clampVolume(v UserVolume) UserVolume {
	if v > MaxUserVol {
		return MaxUserVol
	}

	if v < MinUserVol {
		return MinUserVol
	}

	return v
}
