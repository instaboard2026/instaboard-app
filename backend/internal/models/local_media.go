package models

// LocalMediaFolder is a row from tbl_local_media_folders plus item count.
type LocalMediaFolder struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	Count    int    `json:"count"`
}

// LocalMedia is a copied image/video in a Local media folder.
type LocalMedia struct {
	ID             uint64  `json:"id"`
	UserID         int64   `json:"-"`
	FolderID       int64   `json:"folder_id"`
	OriginalName   string  `json:"original_name"`
	StoredName     string  `json:"stored_name"`
	Kind           string  `json:"kind"` // image | video
	ByteSize       int64   `json:"byte_size"`
	SourceRelPath  *string `json:"source_rel_path,omitempty"`
	SourceRoot     *string `json:"source_root,omitempty"`
	CreatedAt      string  `json:"created_at"`
}
