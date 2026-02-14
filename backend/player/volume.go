package player

// UserVolume represents volume on a user-facing scale (0-100).
type UserVolume int

// PlayerVolume represents volume on an internal scale (-10 to 10).
type PlayerVolume float64

// User volume range bounds.
const (
	MinUserVol UserVolume = 0
	MaxUserVol UserVolume = 100
)

// Player volume range bounds.
const (
	MinPlayerVol PlayerVolume = -4
	MaxPlayerVol PlayerVolume = 0
)

// ToPlayerVolume converts user volume to internal player volume.
func (oldVol UserVolume) ToPlayerVolume() PlayerVolume {
	var newVol PlayerVolume

	if oldVol >= MinUserVol && oldVol <= MaxUserVol {
		ratio := PlayerVolume(oldVol-MinUserVol) / PlayerVolume(MaxUserVol-MinUserVol)
		newVol = ratio*(MaxPlayerVol-MinPlayerVol) + MinPlayerVol
	}

	return newVol
}

// ToUserVolume converts internal player volume to user volume.
func (oldVolFloat PlayerVolume) ToUserVolume() UserVolume {
	var newVol UserVolume

	if oldVolFloat >= MinPlayerVol && oldVolFloat <= MaxPlayerVol {
		ratio := (oldVolFloat - MinPlayerVol) / (MaxPlayerVol - MinPlayerVol)
		newVol = UserVolume(ratio*PlayerVolume(MaxUserVol-MinUserVol)) + MinUserVol
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
