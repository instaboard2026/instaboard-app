-- Engagement snapshot captured at Instagram download time (nullable = not yet known).
ALTER TABLE tbl_saves ADD COLUMN like_count INTEGER;
ALTER TABLE tbl_saves ADD COLUMN comment_count INTEGER;
ALTER TABLE tbl_saves ADD COLUMN repost_count INTEGER;
ALTER TABLE tbl_saves ADD COLUMN share_count INTEGER;
ALTER TABLE tbl_saves ADD COLUMN play_count INTEGER;
