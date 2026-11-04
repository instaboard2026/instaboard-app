-- Instaboard schema (SQLite). Applied automatically on startup; see internal/database/migrate.go.
-- Timestamps are stored as UTC text 'YYYY-MM-DD HH:MM:SS.SSS'.

-- The local app has a single user (is_local = 1), named after the computer account.
CREATE TABLE tbl_users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  primary_email TEXT UNIQUE,
  name          TEXT,
  is_local      INTEGER NOT NULL DEFAULT 0,
  created_at    DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  updated_at    DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now'))
);

-- Folders in the sidebar. Deleting a folder deletes its saves (downloaded files on disk are left alone).
CREATE TABLE tbl_folders (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES tbl_users (id) ON DELETE CASCADE,
  name       TEXT NOT NULL COLLATE NOCASE,
  position   INTEGER NOT NULL,
  created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  UNIQUE (user_id, name)
);
CREATE INDEX idx_folders_user_position ON tbl_folders (user_id, position);

-- Saved Instagram / Pinterest / YouTube links. folder_id NULL = on Home.
-- url is normalized by the backend, so the same post can only be saved once per user.
CREATE TABLE tbl_saves (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES tbl_users (id) ON DELETE CASCADE,
  url        TEXT NOT NULL,
  source     TEXT NOT NULL,
  added_from TEXT NOT NULL DEFAULT 'web',
  created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  folder_id  INTEGER REFERENCES tbl_folders (id) ON DELETE CASCADE,
  UNIQUE (user_id, url)
);
CREATE INDEX idx_saves_user_created ON tbl_saves (user_id, created_at);
CREATE INDEX idx_saves_user_folder_created ON tbl_saves (user_id, folder_id, created_at);

-- Per-user settings. download_dir NULL = the backend's DOWNLOAD_DIR default.
CREATE TABLE tbl_user_settings (
  user_id      INTEGER PRIMARY KEY REFERENCES tbl_users (id) ON DELETE CASCADE,
  download_dir TEXT,
  updated_at   DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now'))
);
