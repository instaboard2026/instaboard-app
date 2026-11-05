-- Calendar day assignments and day notes. One date per save; notes are per day.
-- Shared by every browser through the API (this file lives on local disk).

CREATE TABLE tbl_calendar_schedule (
  user_id      INTEGER NOT NULL REFERENCES tbl_users (id) ON DELETE CASCADE,
  save_id      INTEGER NOT NULL REFERENCES tbl_saves (id) ON DELETE CASCADE,
  scheduled_on TEXT NOT NULL,
  created_at   DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  updated_at   DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  PRIMARY KEY (user_id, save_id)
);
CREATE INDEX idx_calendar_schedule_user_date ON tbl_calendar_schedule (user_id, scheduled_on);

CREATE TABLE tbl_calendar_notes (
  user_id    INTEGER NOT NULL REFERENCES tbl_users (id) ON DELETE CASCADE,
  note_date  TEXT NOT NULL,
  body       TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
  PRIMARY KEY (user_id, note_date)
);
