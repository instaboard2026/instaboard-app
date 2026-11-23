-- Allow junk kind (XML / AppleDouble / tiny stubs) on camera index.

CREATE TABLE tbl_camera_index_new (
  user_id   INTEGER NOT NULL REFERENCES tbl_users (id) ON DELETE CASCADE,
  rel_path  TEXT NOT NULL,
  name      TEXT NOT NULL,
  kind      TEXT NOT NULL CHECK (kind IN ('image', 'video', 'junk')),
  byte_size INTEGER NOT NULL CHECK (byte_size >= 0),
  mtime_ns  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (user_id, rel_path)
);

INSERT INTO tbl_camera_index_new (user_id, rel_path, name, kind, byte_size, mtime_ns)
SELECT user_id, rel_path, name, kind, byte_size, mtime_ns FROM tbl_camera_index;

DROP TABLE tbl_camera_index;
ALTER TABLE tbl_camera_index_new RENAME TO tbl_camera_index;

CREATE INDEX idx_camera_index_user_rel ON tbl_camera_index (user_id, rel_path);
CREATE INDEX idx_camera_index_user_kind_rel ON tbl_camera_index (user_id, kind, rel_path);
