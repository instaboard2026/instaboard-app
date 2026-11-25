-- Camera card files moved to the Mac's Trash from Instaboard, so the Bin can list them,
-- put them back onto the card, or delete them for good. trash_path is where macOS put the file.
CREATE TABLE tbl_camera_trash (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES tbl_users (id) ON DELETE CASCADE,
  rel_path   TEXT NOT NULL,
  name       TEXT NOT NULL,
  kind       TEXT NOT NULL,
  byte_size  INTEGER NOT NULL DEFAULT 0,
  mtime_ns   INTEGER NOT NULL DEFAULT 0,
  card_root  TEXT NOT NULL,
  trash_path TEXT NOT NULL DEFAULT '',
  trashed_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now'))
);
CREATE INDEX idx_camera_trash_user ON tbl_camera_trash (user_id, trashed_at DESC);
