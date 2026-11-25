package repository

import (
	"context"
	"database/sql"
	"strings"
)

// CameraIndexRow is one indexed camera file.
type CameraIndexRow struct {
	RelPath  string
	Name     string
	Kind     string
	ByteSize int64
	MtimeNs  int64
}

// CameraScanMeta is the latest scan state for a user.
type CameraScanMeta struct {
	Status       string
	IndexedCount int
	Truncated    bool
	TimedOut     bool
	ErrorMessage string
}

// CameraIndexRepo persists Manage camera file listings.
type CameraIndexRepo struct {
	db *sql.DB
}

// NewCameraIndexRepo creates a CameraIndexRepo.
func NewCameraIndexRepo(db *sql.DB) *CameraIndexRepo {
	return &CameraIndexRepo{db: db}
}

// ReplaceAll clears and inserts a full index for the user (used at end of scan batching via Clear+InsertBatch).
func (r *CameraIndexRepo) Clear(ctx context.Context, userID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM tbl_camera_index WHERE user_id = ?`, userID)
	return err
}

// InsertBatch upserts index rows.
func (r *CameraIndexRepo) InsertBatch(ctx context.Context, userID int64, rows []CameraIndexRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO tbl_camera_index (user_id, rel_path, name, kind, byte_size, mtime_ns)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (user_id, rel_path) DO UPDATE SET
		   name = excluded.name,
		   kind = excluded.kind,
		   byte_size = excluded.byte_size,
		   mtime_ns = excluded.mtime_ns`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if _, err := stmt.ExecContext(ctx, userID, row.RelPath, row.Name, row.Kind, row.ByteSize, row.MtimeNs); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CountAll returns every indexed row (including junk).
func (r *CameraIndexRepo) CountAll(ctx context.Context, userID int64) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_camera_index WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// Count returns indexed media file count (image + video, excludes junk).
func (r *CameraIndexRepo) Count(ctx context.Context, userID int64, scope string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tbl_camera_index WHERE user_id = ? AND kind IN ('image', 'video')`+ScopeSQL(scope), userID,
	).Scan(&n)
	return n, err
}

// CountByKind returns total for a filter (all = image+video / image / video / junk).
func (r *CameraIndexRepo) CountByKind(ctx context.Context, userID int64, kind, scope string) (int, error) {
	if kind == "image" || kind == "video" || kind == "junk" {
		var n int
		err := r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM tbl_camera_index WHERE user_id = ? AND kind = ?`+ScopeSQL(scope), userID, kind,
		).Scan(&n)
		return n, err
	}
	return r.Count(ctx, userID, scope)
}

// CameraSortLatest / CameraSortOldest control ListOffset ordering by mtime_ns.
const (
	CameraSortLatest = "latest"
	CameraSortOldest = "oldest"
)

func normalizeCameraSort(sort string) string {
	if sort == CameraSortOldest {
		return CameraSortOldest
	}
	return CameraSortLatest
}

func cameraMtimeOrderSQL(sort string) string {
	if normalizeCameraSort(sort) == CameraSortOldest {
		return "mtime_ns ASC, rel_path ASC"
	}
	return "mtime_ns DESC, rel_path ASC"
}

// ListOffset returns a page by offset/limit, ordered by mtime (latest or oldest).
func (r *CameraIndexRepo) ListOffset(ctx context.Context, userID int64, kind string, offset, limit int, sort, scope string) ([]CameraIndexRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 24
	}
	if offset < 0 {
		offset = 0
	}
	order := cameraMtimeOrderSQL(sort)
	var rows *sql.Rows
	var err error
	if kind == "image" || kind == "video" || kind == "junk" {
		rows, err = r.db.QueryContext(ctx,
			`SELECT rel_path, name, kind, byte_size, mtime_ns FROM tbl_camera_index
			 WHERE user_id = ? AND kind = ?`+ScopeSQL(scope)+` ORDER BY `+order+` LIMIT ? OFFSET ?`,
			userID, kind, limit, offset,
		)
	} else {
		rows, err = r.db.QueryContext(ctx,
			`SELECT rel_path, name, kind, byte_size, mtime_ns FROM tbl_camera_index
			 WHERE user_id = ? AND kind IN ('image', 'video')`+ScopeSQL(scope)+` ORDER BY `+order+` LIMIT ? OFFSET ?`,
			userID, limit, offset,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIndexRows(rows)
}

// ListPage returns items after cursor (exclusive), ordered by rel_path.
func (r *CameraIndexRepo) ListPage(ctx context.Context, userID int64, kind, cursor string, limit int, scope string) ([]CameraIndexRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var rows *sql.Rows
	var err error
	if kind == "image" || kind == "video" || kind == "junk" {
		if cursor == "" {
			rows, err = r.db.QueryContext(ctx,
				`SELECT rel_path, name, kind, byte_size, mtime_ns FROM tbl_camera_index
				 WHERE user_id = ? AND kind = ?`+ScopeSQL(scope)+` ORDER BY rel_path LIMIT ?`,
				userID, kind, limit,
			)
		} else {
			rows, err = r.db.QueryContext(ctx,
				`SELECT rel_path, name, kind, byte_size, mtime_ns FROM tbl_camera_index
				 WHERE user_id = ? AND kind = ? AND rel_path > ?`+ScopeSQL(scope)+` ORDER BY rel_path LIMIT ?`,
				userID, kind, cursor, limit,
			)
		}
	} else {
		if cursor == "" {
			rows, err = r.db.QueryContext(ctx,
				`SELECT rel_path, name, kind, byte_size, mtime_ns FROM tbl_camera_index
				 WHERE user_id = ? AND kind IN ('image', 'video')`+ScopeSQL(scope)+` ORDER BY rel_path LIMIT ?`,
				userID, limit,
			)
		} else {
			rows, err = r.db.QueryContext(ctx,
				`SELECT rel_path, name, kind, byte_size, mtime_ns FROM tbl_camera_index
				 WHERE user_id = ? AND kind IN ('image', 'video') AND rel_path > ?`+ScopeSQL(scope)+` ORDER BY rel_path LIMIT ?`,
				userID, cursor, limit,
			)
		}
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIndexRows(rows)
}

func scanIndexRows(rows *sql.Rows) ([]CameraIndexRow, error) {
	out := []CameraIndexRow{}
	for rows.Next() {
		var row CameraIndexRow
		if err := rows.Scan(&row.RelPath, &row.Name, &row.Kind, &row.ByteSize, &row.MtimeNs); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// KindCounts returns totals for filter badges (all = image+video).
func (r *CameraIndexRepo) KindCounts(ctx context.Context, userID int64, scope string) (all, video, image, junk int, err error) {
	err = r.db.QueryRowContext(ctx,
		`SELECT
		   COALESCE(SUM(CASE WHEN kind IN ('image', 'video') THEN 1 ELSE 0 END), 0),
		   COALESCE(SUM(CASE WHEN kind = 'video' THEN 1 ELSE 0 END), 0),
		   COALESCE(SUM(CASE WHEN kind = 'image' THEN 1 ELSE 0 END), 0),
		   COALESCE(SUM(CASE WHEN kind = 'junk' THEN 1 ELSE 0 END), 0)
		 FROM tbl_camera_index WHERE user_id = ?`+ScopeSQL(scope), userID,
	).Scan(&all, &video, &image, &junk)
	return
}

// DeleteByRelPaths removes indexed rows for the given relative paths.
func (r *CameraIndexRepo) DeleteByRelPaths(ctx context.Context, userID int64, relPaths []string) (int, error) {
	if len(relPaths) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx,
		`DELETE FROM tbl_camera_index WHERE user_id = ? AND rel_path = ?`,
	)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	deleted := 0
	for _, rel := range relPaths {
		res, err := stmt.ExecContext(ctx, userID, rel)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		deleted += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

// GetScanMeta returns scan status; missing row → idle.
func (r *CameraIndexRepo) GetScanMeta(ctx context.Context, userID int64) (CameraScanMeta, error) {
	var m CameraScanMeta
	var trunc, timed int
	var errMsg sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT status, indexed_count, truncated, timed_out, error_message
		 FROM tbl_camera_scan_meta WHERE user_id = ?`, userID,
	).Scan(&m.Status, &m.IndexedCount, &trunc, &timed, &errMsg)
	if err == sql.ErrNoRows {
		return CameraScanMeta{Status: "idle"}, nil
	}
	if err != nil {
		return CameraScanMeta{}, err
	}
	m.Truncated = trunc != 0
	m.TimedOut = timed != 0
	if errMsg.Valid {
		m.ErrorMessage = errMsg.String
	}
	return m, nil
}

// SetScanMeta upserts scan status.
func (r *CameraIndexRepo) SetScanMeta(ctx context.Context, userID int64, m CameraScanMeta) error {
	trunc, timed := 0, 0
	if m.Truncated {
		trunc = 1
	}
	if m.TimedOut {
		timed = 1
	}
	var errMsg any
	if strings.TrimSpace(m.ErrorMessage) != "" {
		errMsg = m.ErrorMessage
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tbl_camera_scan_meta (user_id, status, indexed_count, truncated, timed_out, error_message, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, strftime('%Y-%m-%d %H:%M:%f', 'now'))
		 ON CONFLICT (user_id) DO UPDATE SET
		   status = excluded.status,
		   indexed_count = excluded.indexed_count,
		   truncated = excluded.truncated,
		   timed_out = excluded.timed_out,
		   error_message = excluded.error_message,
		   updated_at = excluded.updated_at`,
		userID, m.Status, m.IndexedCount, trunc, timed, errMsg,
	)
	return err
}

// GetByRelPaths returns indexed rows for the given rel paths (missing ones are skipped).
func (r *CameraIndexRepo) GetByRelPaths(ctx context.Context, userID int64, relPaths []string) ([]CameraIndexRow, error) {
	out := []CameraIndexRow{}
	for _, p := range relPaths {
		var c CameraIndexRow
		err := r.db.QueryRowContext(ctx,
			`SELECT rel_path, name, kind, byte_size, mtime_ns FROM tbl_camera_index WHERE user_id = ? AND rel_path = ?`,
			userID, p).Scan(&c.RelPath, &c.Name, &c.Kind, &c.ByteSize, &c.MtimeNs)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// AirDropFolder is the folder at the top of the card that holds files waiting to be AirDropped.
const AirDropFolder = "AIRDROP"

// Index scopes: the main camera grid (everything but AIRDROP) or the AIRDROP folder only.
const (
	ScopeMain    = "main"
	ScopeAirDrop = "airdrop"
)

// ScopeSQL is the extra WHERE condition for a scope.
func ScopeSQL(scope string) string {
	if scope == ScopeAirDrop {
		return ` AND rel_path LIKE '` + AirDropFolder + `/%'`
	}
	return ` AND rel_path NOT LIKE '` + AirDropFolder + `/%'`
}
