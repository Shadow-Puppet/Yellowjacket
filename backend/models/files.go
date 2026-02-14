package models

import "time"

// AudioFileType identifies the format of an audio file.
type AudioFileType int

// AudioFile represents a music file with its metadata.
type AudioFile struct {
	Path   string
	Type   AudioFileType
	Length time.Duration
}
