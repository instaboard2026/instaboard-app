-- Camera SD root (persists across restart) + Local media folders/items under download dir.

ALTER TABLE tbl_user_settings ADD COLUMN camera_root TEXT;
ALTER TABLE tbl_user_settings ADD COLUMN camera_label TEXT;
ALTER TABLE tbl_user_settings ADD COLUMN camera_opened_at DATETIME;

CREATE TABLE tbl_local_media_folders (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES tbl_users (id) ON DELETE CASCADE,
  name       TEXT NOT NULL COLLATE NOCASE,
  position   INTEGER NOT NULL,
  created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  UNIQUE (user_id, name)
);
CREATE INDEX idx_local_media_folders_user_position
  ON tbl_local_media_folders (user_id, position);

CREATE TABLE tbl_local_media (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id          INTEGER NOT NULL REFERENCES tbl_users (id) ON DELETE CASCADE,
  folder_id        INTEGER NOT NULL REFERENCES tbl_local_media_folders (id) ON DELETE CASCADE,
  original_name    TEXT NOT NULL,
  stored_name      TEXT NOT NULL,
  kind             TEXT NOT NULL CHECK (kind IN ('image', 'video')),
  byte_size        INTEGER NOT NULL CHECK (byte_size >= 0),
  source_rel_path  TEXT,
  source_root      TEXT,
  created_at       DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  UNIQUE (user_id, original_name)
);
CREATE INDEX idx_local_media_user_folder_created
  ON tbl_local_media (user_id, folder_id, created_at DESC);
CREATE INDEX idx_local_media_user_kind ON tbl_local_media (user_id, kind);
