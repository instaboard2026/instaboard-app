-- Persisted Manage camera file index (metadata only) for fast reopen + paging.

CREATE TABLE tbl_camera_index (
  user_id   INTEGER NOT NULL REFERENCES tbl_users (id) ON DELETE CASCADE,
  rel_path  TEXT NOT NULL,
  name      TEXT NOT NULL,
  kind      TEXT NOT NULL CHECK (kind IN ('image', 'video')),
  byte_size INTEGER NOT NULL CHECK (byte_size >= 0),
  mtime_ns  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (user_id, rel_path)
);
CREATE INDEX idx_camera_index_user_rel ON tbl_camera_index (user_id, rel_path);
CREATE INDEX idx_camera_index_user_kind_rel ON tbl_camera_index (user_id, kind, rel_path);

CREATE TABLE tbl_camera_scan_meta (
  user_id        INTEGER PRIMARY KEY REFERENCES tbl_users (id) ON DELETE CASCADE,
  status         TEXT NOT NULL DEFAULT 'idle'
                   CHECK (status IN ('idle', 'running', 'done', 'error')),
  indexed_count  INTEGER NOT NULL DEFAULT 0,
  truncated      INTEGER NOT NULL DEFAULT 0,
  timed_out      INTEGER NOT NULL DEFAULT 0,
  error_message  TEXT,
  updated_at     DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now'))
);
