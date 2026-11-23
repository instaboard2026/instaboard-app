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

// CameraRoot holds the persisted Manage camera folder path.
type CameraRoot struct {
	Root      string
	Label     string
	OpenedAt  string
	HasRoot   bool
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

// GetCameraRoot returns the last chosen camera folder, if any.
func (r *SettingsRepo) GetCameraRoot(ctx context.Context, userID int64) (CameraRoot, error) {
	var root, label, opened sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT camera_root, camera_label, camera_opened_at FROM tbl_user_settings WHERE user_id = ?`,
		userID,
	).Scan(&root, &label, &opened)
	if errors.Is(err, sql.ErrNoRows) {
		return CameraRoot{}, nil
	}
	if err != nil {
		return CameraRoot{}, err
	}
	return CameraRoot{
		Root:     root.String,
		Label:    label.String,
		OpenedAt: opened.String,
		HasRoot:  root.Valid && root.String != "",
	}, nil
}

// SetCameraRoot persists the chosen camera folder path and label.
func (r *SettingsRepo) SetCameraRoot(ctx context.Context, userID int64, root, label string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tbl_user_settings (user_id, camera_root, camera_label, camera_opened_at)
		 VALUES (?, ?, ?, strftime('%Y-%m-%d %H:%M:%f', 'now'))
		 ON CONFLICT (user_id) DO UPDATE SET
		   camera_root = excluded.camera_root,
		   camera_label = excluded.camera_label,
		   camera_opened_at = excluded.camera_opened_at,
		   updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')`,
		userID, root, label,
	)
	return err
}

// ClearCameraRoot forgets the saved camera folder (Local media untouched).
func (r *SettingsRepo) ClearCameraRoot(ctx context.Context, userID int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tbl_user_settings SET
		   camera_root = NULL,
		   camera_label = NULL,
		   camera_opened_at = NULL,
		   updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')
		 WHERE user_id = ?`,
		userID,
	)
	return err
}
