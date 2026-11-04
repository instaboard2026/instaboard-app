package repository

import (
	"context"
	"database/sql"
	"errors"
)

// SettingsRepo handles database access for tbl_user_settings.
type SettingsRepo struct {
	db *sql.DB
}

// NewSettingsRepo creates a new SettingsRepo.
func NewSettingsRepo(db *sql.DB) *SettingsRepo {
	return &SettingsRepo{db: db}
}

// DownloadDir returns the user's chosen download folder, or "" if they use the default.
func (r *SettingsRepo) DownloadDir(ctx context.Context, userID int64) (string, error) {
	var dir sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT download_dir FROM tbl_user_settings WHERE user_id = ?`, userID,
	).Scan(&dir)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return dir.String, nil
}

// SetDownloadDir saves the user's download folder; "" resets it to the default.
func (r *SettingsRepo) SetDownloadDir(ctx context.Context, userID int64, dir string) error {
	var value any
	if dir != "" {
		value = dir
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tbl_user_settings (user_id, download_dir) VALUES (?, ?)
		 ON CONFLICT (user_id) DO UPDATE SET
		   download_dir = excluded.download_dir,
		   updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')`,
		userID, value,
	)
	return err
}
