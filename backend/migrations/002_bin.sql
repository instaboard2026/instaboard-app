-- Soft-delete bin: saves with deleted_at set are hidden from Home/folders and listed in the Bin.
ALTER TABLE tbl_saves ADD COLUMN deleted_at DATETIME;
CREATE INDEX idx_saves_user_deleted ON tbl_saves (user_id, deleted_at);
