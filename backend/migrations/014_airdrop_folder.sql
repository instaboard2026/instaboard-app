-- AirDrop is now a real folder on the card (AIRDROP/). Remember where each moved file came from,
-- so "Move back" can return it. The old queue-only tray is gone.
CREATE TABLE tbl_airdrop_origin (
  user_id           INTEGER NOT NULL REFERENCES tbl_users (id) ON DELETE CASCADE,
  rel_path          TEXT NOT NULL,
  original_rel_path TEXT NOT NULL,
  moved_at          DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  PRIMARY KEY (user_id, rel_path)
);

DROP TABLE tbl_airdrop_tray;
