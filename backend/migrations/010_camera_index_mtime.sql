-- Speed Latest/Oldest paging on the camera index.
CREATE INDEX IF NOT EXISTS idx_camera_index_user_kind_mtime
  ON tbl_camera_index (user_id, kind, mtime_ns, rel_path);
