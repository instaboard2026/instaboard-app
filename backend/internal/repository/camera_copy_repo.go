package repository

import (
	"context"
	"database/sql"
)

// CameraCopyRepo remembers which camera files were copied into folders (tbl_camera_copies).
type CameraCopyRepo struct {
	db *sql.DB
}

// NewCameraCopyRepo creates a CameraCopyRepo.
func NewCameraCopyRepo(db *sql.DB) *CameraCopyRepo {
	return &CameraCopyRepo{db: db}
}

// Has reports whether a camera file with this name was already copied.
func (r *CameraCopyRepo) Has(ctx context.Context, userID int64, name string) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tbl_camera_copies WHERE user_id = ? AND original_name = ?`, userID, name,
	).Scan(&n)
	return n > 0, err
}

// Record notes that a camera file now lives in save saveID.
func (r *CameraCopyRepo) Record(ctx context.Context, userID int64, name string, saveID uint64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO tbl_camera_copies (user_id, original_name, save_id) VALUES (?, ?, ?)`,
		userID, name, saveID)
	return err
}

// Names returns every copied camera file name for the user.
func (r *CameraCopyRepo) Names(ctx context.Context, userID int64) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT original_name FROM tbl_camera_copies WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}
