package models

import "time"

// Save represents a row from tbl_saves (a saved Instagram, Pinterest or YouTube link).
// FolderID is nil when the save is in the Dump. DeletedAt is set when the save is in the Bin.
// Position is the manual board order within Home or a folder (lower = earlier).
// Engagement counts are a snapshot from Instagram media-info at download time (nil = unknown).
type Save struct {
	ID           uint64     `json:"id"`
	URL          string     `json:"url"`
	Source       string     `json:"source"`
	AddedFrom    string     `json:"added_from"`
	FolderID     *int64     `json:"folder_id"`
	Position     int        `json:"position"`
	CreatedAt    time.Time  `json:"created_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
	LikeCount    *int64     `json:"like_count,omitempty"`
	CommentCount *int64     `json:"comment_count,omitempty"`
	RepostCount  *int64     `json:"repost_count,omitempty"`
	ShareCount   *int64     `json:"share_count,omitempty"`
	PlayCount    *int64     `json:"play_count,omitempty"`
}
