package player

type UserVolume int
type PlayerVolume float64

const MinUserVol UserVolume = 0
const MaxUserVol UserVolume = 100

const MinPlayerVol PlayerVolume = -10
const MaxPlayerVol PlayerVolume = 10

func (oldVol UserVolume) ToPlayerVolume() PlayerVolume {
	var newVol PlayerVolume

	if oldVol >= MinUserVol && oldVol <= MaxUserVol {
		ratio := PlayerVolume(oldVol-MinUserVol) / PlayerVolume(MaxUserVol-MinUserVol)
		newVol = ratio*(MaxPlayerVol-MinPlayerVol) + MinPlayerVol
	}
	return newVol
}

func (oldVolFloat PlayerVolume) ToUserVolume() UserVolume {
	var newVol UserVolume

	if oldVolFloat >= MinPlayerVol && oldVolFloat <= MaxPlayerVol {
		ratio := (oldVolFloat - MinPlayerVol) / (MaxPlayerVol - MinPlayerVol)
		newVol = UserVolume(ratio*PlayerVolume(MaxUserVol-MinUserVol)) + MinUserVol
	}
	return newVol
}
