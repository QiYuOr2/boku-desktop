package domain

import "time"

// FollowedUpdate is the newest public activity of one explicitly followed
// creator. The feature maps provider response names to this neutral shape.
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
