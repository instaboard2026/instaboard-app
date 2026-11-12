package repository

import (
	"context"
	"database/sql"
	"errors"

	"instaboard/internal/models"
)

// ErrSplitProjectNotFound is returned when the project is missing or not the user's.
var ErrSplitProjectNotFound = errors.New("split project not found")

const splitProjectColumns = `id, source_type, source_save_id, source_file, title, status,
	error_message, master_file, duration_ms, detect_threshold, created_at, updated_at`

// SplitRepo handles database access for split analysis projects and clips.
type SplitRepo struct {
	db *sql.DB
}

// NewSplitRepo creates a new SplitRepo.
func NewSplitRepo(db *sql.DB) *SplitRepo {
	return &SplitRepo{db: db}
}

// CreateProject inserts a new project row and returns it.
func (r *SplitRepo) CreateProject(ctx context.Context, userID int64, sourceType string, sourceSaveID *uint64, sourceFile, title *string) (*models.SplitProject, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO tbl_split_projects (user_id, source_type, source_save_id, source_file, title)
		 VALUES (?, ?, ?, ?, ?)`,
		userID, sourceType, nullableUint64(sourceSaveID), nullableString(sourceFile), nullableString(title),
	)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return r.GetProject(ctx, userID, uint64(id))
}

// ListProjects returns the user's projects newest first.
func (r *SplitRepo) ListProjects(ctx context.Context, userID int64) ([]models.SplitProject, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+splitProjectColumns+` FROM tbl_split_projects
		 WHERE user_id = ? ORDER BY created_at DESC, id DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.SplitProject{}
	for rows.Next() {
		p, err := scanSplitProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProject returns one project owned by the user.
func (r *SplitRepo) GetProject(ctx context.Context, userID int64, id uint64) (*models.SplitProject, error) {
	p, err := scanSplitProject(r.db.QueryRowContext(ctx,
		`SELECT `+splitProjectColumns+` FROM tbl_split_projects WHERE id = ? AND user_id = ?`, id, userID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSplitProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListClips returns clips for a project in position order.
func (r *SplitRepo) ListClips(ctx context.Context, projectID uint64) ([]models.SplitClip, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, position, start_ms, end_ms, file_name, created_at
		 FROM tbl_split_clips WHERE project_id = ? ORDER BY position ASC`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.SplitClip{}
	for rows.Next() {
		var c models.SplitClip
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Position, &c.StartMs, &c.EndMs, &c.FileName, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ClipCount returns how many clips a project has.
func (r *SplitRepo) ClipCount(ctx context.Context, projectID uint64) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tbl_split_clips WHERE project_id = ?`, projectID,
	).Scan(&n)
	return n, err
}

// UpdateStatus sets status and optional error message; clears error when msg is nil and status is not error.
func (r *SplitRepo) UpdateStatus(ctx context.Context, userID int64, id uint64, status string, errMsg *string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tbl_split_projects SET status = ?, error_message = ?,
		 updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')
		 WHERE id = ? AND user_id = ?`,
		status, nullableString(errMsg), id, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrSplitProjectNotFound
	}
	return nil
}

// UpdateAfterIngest sets duration after the master file is copied.
func (r *SplitRepo) UpdateAfterIngest(ctx context.Context, userID int64, id uint64, durationMs int64, title *string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tbl_split_projects SET duration_ms = ?, title = COALESCE(?, title),
		 updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')
		 WHERE id = ? AND user_id = ?`,
		durationMs, nullableString(title), id, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrSplitProjectNotFound
	}
	return nil
}

// ReplaceClips deletes existing clips and inserts the new set in one transaction.
func (r *SplitRepo) ReplaceClips(ctx context.Context, userID int64, projectID uint64, threshold float64, clips []models.SplitClip) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var n int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tbl_split_projects WHERE id = ? AND user_id = ?`, projectID, userID,
	).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrSplitProjectNotFound
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM tbl_split_clips WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	for _, c := range clips {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO tbl_split_clips (project_id, position, start_ms, end_ms, file_name)
			 VALUES (?, ?, ?, ?, ?)`,
			projectID, c.Position, c.StartMs, c.EndMs, c.FileName,
		); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE tbl_split_projects SET status = 'done', error_message = NULL,
		 detect_threshold = ?, updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')
		 WHERE id = ? AND user_id = ?`,
		threshold, projectID, userID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteProject removes the project row (clips cascade).
func (r *SplitRepo) DeleteProject(ctx context.Context, userID int64, id uint64) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM tbl_split_projects WHERE id = ? AND user_id = ?`, id, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrSplitProjectNotFound
	}
	return nil
}

func scanSplitProject(row rowScanner) (models.SplitProject, error) {
	var p models.SplitProject
	var sourceSaveID sql.NullInt64
	var sourceFile, title, errMsg sql.NullString
	var durationMs sql.NullInt64
	if err := row.Scan(
		&p.ID, &p.SourceType, &sourceSaveID, &sourceFile, &title, &p.Status,
		&errMsg, &p.MasterFile, &durationMs, &p.DetectThreshold, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return p, err
	}
	if sourceSaveID.Valid {
		v := uint64(sourceSaveID.Int64)
		p.SourceSaveID = &v
	}
	if sourceFile.Valid {
		s := sourceFile.String
		p.SourceFile = &s
	}
	if title.Valid {
		s := title.String
		p.Title = &s
	}
	if errMsg.Valid {
		s := errMsg.String
		p.ErrorMessage = &s
	}
	if durationMs.Valid {
		v := durationMs.Int64
		p.DurationMs = &v
	}
	return p, nil
}

func nullableUint64(v *uint64) any {
	if v == nil {
		return nil
	}
	return int64(*v)
}

func nullableString(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}
