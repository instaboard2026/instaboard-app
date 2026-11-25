-- AirDrop tray: camera files queued to AirDrop together (references to files on the card, not copies).
CREATE TABLE tbl_airdrop_tray (
  user_id   INTEGER NOT NULL REFERENCES tbl_users (id) ON DELETE CASCADE,
  rel_path  TEXT NOT NULL,
  name      TEXT NOT NULL,
  kind      TEXT NOT NULL,
  byte_size INTEGER NOT NULL DEFAULT 0,
  mtime_ns  INTEGER NOT NULL DEFAULT 0,
  added_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  PRIMARY KEY (user_id, rel_path)
);

-- Camera files already sent by AirDrop (by file name, like tbl_camera_copies), for the "Sent" tag.
CREATE TABLE tbl_camera_sent (
  user_id       INTEGER NOT NULL REFERENCES tbl_users (id) ON DELETE CASCADE,
  original_name TEXT NOT NULL,
  sent_at       DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  PRIMARY KEY (user_id, original_name)
);
