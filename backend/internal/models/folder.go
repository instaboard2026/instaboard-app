package models

// Folder represents a row from tbl_folders plus the number of saves in it.
type Folder struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	Count    int    `json:"count"`
	// SectionID is the sidebar section the folder sits in (nil = ungrouped, shown after the sections).
	SectionID *int64 `json:"section_id"`
}

// FolderSection groups folders in the sidebar.
type FolderSection struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}
