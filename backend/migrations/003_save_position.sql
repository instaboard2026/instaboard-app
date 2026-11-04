-- Manual order for Home and folder boards (drag to shuffle). Backfill keeps today's newest-first look.
ALTER TABLE tbl_saves ADD COLUMN position INTEGER NOT NULL DEFAULT 0;

UPDATE tbl_saves
SET position = (
  SELECT COUNT(*) + 1
  FROM tbl_saves AS s2
  WHERE s2.user_id = tbl_saves.user_id
    AND s2.deleted_at IS NULL
    AND (
      (s2.folder_id IS NULL AND tbl_saves.folder_id IS NULL)
      OR s2.folder_id = tbl_saves.folder_id
    )
    AND (
      s2.created_at > tbl_saves.created_at
      OR (s2.created_at = tbl_saves.created_at AND s2.id > tbl_saves.id)
    )
)
WHERE deleted_at IS NULL;

CREATE INDEX idx_saves_user_folder_position ON tbl_saves (user_id, folder_id, position);
