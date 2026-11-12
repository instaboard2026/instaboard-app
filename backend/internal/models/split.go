package models

import "time"

// SplitProject is one video brought into the Split analysis workspace.
type SplitProject struct {
	ID              uint64     `json:"id"`
	SourceType      string     `json:"source_type"` // "save" | "upload"
	SourceSaveID    *uint64    `json:"source_save_id,omitempty"`
	SourceFile      *string    `json:"source_file,omitempty"`
	Title           *string    `json:"title,omitempty"`
	Status          string     `json:"status"`
	ErrorMessage    *string    `json:"error_message,omitempty"`
	MasterFile      string     `json:"master_file"`
	DurationMs      *int64     `json:"duration_ms,omitempty"`
	DetectThreshold float64    `json:"detect_threshold"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// SplitClip is one detected segment of a split project (master is on the project).
type SplitClip struct {
	ID        uint64    `json:"id"`
	ProjectID uint64    `json:"project_id"`
	Position  int       `json:"position"`
	StartMs   int64     `json:"start_ms"`
	EndMs     int64     `json:"end_ms"`
	FileName  string    `json:"file_name"`
	CreatedAt time.Time `json:"created_at"`
}
