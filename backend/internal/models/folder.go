package models

// Folder represents a row from tbl_folders plus the number of saves in it.
type Folder struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	Count    int    `json:"count"`
}
