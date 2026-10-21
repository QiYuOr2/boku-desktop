package domain

import "time"

// EpisodeRelease is one publicly scheduled main episode. It deliberately
// contains no provider fields: features map their provider response into this
// small, reusable shape before presentation reads it.
type EpisodeRelease struct {
	SubjectID   int64
	SubjectName string
	CoverURL    string
	EpisodeID   int64
	EpisodeName string
	EpisodeSort float64
	AirDate     time.Time
}
