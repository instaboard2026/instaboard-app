package repository

import (
	"context"
	"database/sql"
)

// AirDropTrayRepo stores where AIRDROP/ files came from (tbl_airdrop_origin) and AirDropped names (tbl_camera_sent).
type AirDropTrayRepo struct {
	db *sql.DB
}

// NewAirDropTrayRepo creates an AirDropTrayRepo.
func NewAirDropTrayRepo(db *sql.DB) *AirDropTrayRepo {
	return &AirDropTrayRepo{db: db}
}

// RecordOrigin remembers where a file moved into AIRDROP/ came from.
func (r *AirDropTrayRepo) RecordOrigin(ctx context.Context, userID int64, relPath, original string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO tbl_airdrop_origin (user_id, rel_path, original_rel_path) VALUES (?, ?, ?)`,
		userID, relPath, original)
	return err
}

// Origin returns where a file in AIRDROP/ came from ("" if unknown).
func (r *AirDropTrayRepo) Origin(ctx context.Context, userID int64, relPath string) (string, error) {
	var o string
	err := r.db.QueryRowContext(ctx,
		`SELECT original_rel_path FROM tbl_airdrop_origin WHERE user_id = ? AND rel_path = ?`, userID, relPath).Scan(&o)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return o, err
}

// ForgetOrigin drops the origin record for a file that left AIRDROP/.
func (r *AirDropTrayRepo) ForgetOrigin(ctx context.Context, userID int64, relPath string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM tbl_airdrop_origin WHERE user_id = ? AND rel_path = ?`, userID, relPath)
	return err
}

// MarkSent records file names as AirDropped.
func (r *AirDropTrayRepo) MarkSent(ctx context.Context, userID int64, names []string) error {
	for _, n := range names {
		if _, err := r.db.ExecContext(ctx,
			`INSERT OR REPLACE INTO tbl_camera_sent (user_id, original_name) VALUES (?, ?)`, userID, n); err != nil {
			return err
		}
	}
	return nil
}

// SentNames returns every AirDropped camera file name.
func (r *AirDropTrayRepo) SentNames(ctx context.Context, userID int64) (map[string]bool, error) {
	return stringSet(ctx, r.db, `SELECT original_name FROM tbl_camera_sent WHERE user_id = ?`, userID)
}

func stringSet(ctx context.Context, db *sql.DB, query string, userID int64) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out[s] = true
	}
	return out, rows.Err()
}
