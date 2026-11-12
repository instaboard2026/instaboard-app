-- Split video analysis workspace. Projects copy a master off dump/upload media;
-- clips are detected segments. Dump tbl_saves / download files are never rewritten.

CREATE TABLE tbl_split_projects (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id          INTEGER NOT NULL REFERENCES tbl_users (id) ON DELETE CASCADE,
  source_type      TEXT NOT NULL CHECK (source_type IN ('save', 'upload')),
  source_save_id   INTEGER REFERENCES tbl_saves (id) ON DELETE SET NULL,
  source_file      TEXT,
  title            TEXT,
  status           TEXT NOT NULL DEFAULT 'ready'
                     CHECK (status IN ('ready', 'detecting', 'splitting', 'done', 'error')),
  error_message    TEXT,
  master_file      TEXT NOT NULL DEFAULT 'master.mp4',
  duration_ms      INTEGER,
  detect_threshold REAL NOT NULL DEFAULT 10,
  created_at       DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  updated_at       DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now'))
);
CREATE INDEX idx_split_projects_user_created ON tbl_split_projects (user_id, created_at DESC);
CREATE INDEX idx_split_projects_source_save ON tbl_split_projects (source_save_id);

CREATE TABLE tbl_split_clips (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id  INTEGER NOT NULL REFERENCES tbl_split_projects (id) ON DELETE CASCADE,
  position    INTEGER NOT NULL,
  start_ms    INTEGER NOT NULL,
  end_ms      INTEGER NOT NULL,
  file_name   TEXT NOT NULL,
  created_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  UNIQUE (project_id, position),
  CHECK (end_ms > start_ms)
);
CREATE INDEX idx_split_clips_project_position ON tbl_split_clips (project_id, position);
