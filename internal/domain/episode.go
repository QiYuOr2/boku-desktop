package domain

import "time"

// EpisodeRelease 是一条已公开定档的正片剧集。它有意不含任何 provider 字段：
// 各功能先把 provider 响应映射成这个可复用的小形状，再由展示层读取。
type EpisodeRelease struct {
	SubjectID   int64
	SubjectName string
	CoverURL    string
	EpisodeID   int64
	EpisodeName string
	EpisodeSort float64
	AirDate     time.Time
}
