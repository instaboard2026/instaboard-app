package repository

import (
	"context"
	"database/sql"
	"errors"
)

// ErrCardTrashNotFound means no such trashed card file for the user.
var ErrCardTrashNotFound = errors.New("file not found in the Bin")

// CardTrashItem is a camera file moved to the Mac's Trash from Instaboard.
type CardTrashItem struct {
	ID        int64
	RelPath   string
	Name      string
	Kind      string
	ByteSize  int64
	MtimeNs   int64
	CardRoot  string
	TrashPath string
	TrashedAt string
}

// CameraTrashRepo stores trashed card files (tbl_camera_trash).
type CameraTrashRepo struct {
	db *sql.DB
}

// NewCameraTrashRepo creates a CameraTrashRepo.
func NewCameraTrashRepo(db *sql.DB) *CameraTrashRepo {
	return &CameraTrashRepo{db: db}
}

// Add records a trashed card file.
func (r *CameraTrashRepo) Add(ctx context.Context, userID int64, it CardTrashItem) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tbl_camera_trash (user_id, rel_path, name, kind, byte_size, mtime_ns, card_root, trash_path)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, it.RelPath, it.Name, it.Kind, it.ByteSize, it.MtimeNs, it.CardRoot, it.TrashPath)
	return err
}

// List returns trashed card files, newest first.
func (r *CameraTrashRepo) List(ctx context.Context, userID int64) ([]CardTrashItem, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, rel_path, name, kind, byte_size, mtime_ns, card_root, trash_path, trashed_at
		 FROM tbl_camera_trash WHERE user_id = ? ORDER BY trashed_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CardTrashItem{}
	for rows.Next() {
		var it CardTrashItem
		if err := rows.Scan(&it.ID, &it.RelPath, &it.Name, &it.Kind, &it.ByteSize, &it.MtimeNs, &it.CardRoot, &it.TrashPath, &it.TrashedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Get returns one trashed card file.
func (r *CameraTrashRepo) Get(ctx context.Context, userID, id int64) (*CardTrashItem, error) {
	var it CardTrashItem
	err := r.db.QueryRowContext(ctx,
		`SELECT id, rel_path, name, kind, byte_size, mtime_ns, card_root, trash_path, trashed_at
		 FROM tbl_camera_trash WHERE user_id = ? AND id = ?`, userID, id,
	).Scan(&it.ID, &it.RelPath, &it.Name, &it.Kind, &it.ByteSize, &it.MtimeNs, &it.CardRoot, &it.TrashPath, &it.TrashedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCardTrashNotFound
	}
	return &it, err
}

// Delete forgets a trashed card file record.
func (r *CameraTrashRepo) Delete(ctx context.Context, userID, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM tbl_camera_trash WHERE user_id = ? AND id = ?`, userID, id)
	return err
}

// SetTrashPath records where a trashed file was found.
func (r *CameraTrashRepo) SetTrashPath(ctx context.Context, userID, id int64, path string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE tbl_camera_trash SET trash_path = ? WHERE user_id = ? AND id = ?`, path, userID, id)
	return err
}
