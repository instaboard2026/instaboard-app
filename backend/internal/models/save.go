package models

import "time"

// Save represents a row from tbl_saves (a saved Instagram, Pinterest or YouTube link).
// FolderID is nil when the save is in the Dump. DeletedAt is set when the save is in the Bin.
// Position is the manual board order within Home or a folder (lower = earlier).
type Save struct {
	ID        uint64     `json:"id"`
	URL       string     `json:"url"`
	Source    string     `json:"source"`
	AddedFrom string     `json:"added_from"`
	FolderID  *int64     `json:"folder_id"`
	Position  int        `json:"position"`
	CreatedAt time.Time  `json:"created_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}
