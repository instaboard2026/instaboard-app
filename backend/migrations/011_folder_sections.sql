-- Sections group sidebar folders. Folders with section_id NULL are ungrouped (shown after the sections).
CREATE TABLE tbl_folder_sections (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES tbl_users (id) ON DELETE CASCADE,
  name       TEXT NOT NULL COLLATE NOCASE,
  position   INTEGER NOT NULL,
  created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  UNIQUE (user_id, name)
);
CREATE INDEX idx_folder_sections_user_position ON tbl_folder_sections (user_id, position);

-- Deleting a section leaves its folders, ungrouped.
ALTER TABLE tbl_folders ADD COLUMN section_id INTEGER REFERENCES tbl_folder_sections (id) ON DELETE SET NULL;

-- Camera files already copied into a folder, so the camera page can mark them and skip repeats.
-- The row goes away when its save is permanently deleted.
CREATE TABLE tbl_camera_copies (
  user_id       INTEGER NOT NULL REFERENCES tbl_users (id) ON DELETE CASCADE,
  original_name TEXT NOT NULL,
  save_id       INTEGER NOT NULL REFERENCES tbl_saves (id) ON DELETE CASCADE,
  created_at    DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  PRIMARY KEY (user_id, original_name)
);
