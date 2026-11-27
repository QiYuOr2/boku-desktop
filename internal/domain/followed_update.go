package domain

import "time"

// FollowedUpdate 是某个被显式关注的创作者最近的一条公开动态。
// 该功能把 provider 响应中的名称映射成这个中性形状。
type FollowedUpdate struct {
	CreatorID    int64
	CreatorName  string
	AvatarURL    string
	Kind         string
	Summary      string
	URL          string
	PublishedAt  time.Time
	Live         bool
	LiveRoomID   int64
	LiveTitle    string
	LiveAudience string
}
