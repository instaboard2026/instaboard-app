package repository

import (
	"context"
	"database/sql"
	"errors"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"instaboard/internal/models"
)

var (
	// ErrDuplicateSave is returned when the user already saved the same link.
	ErrDuplicateSave = errors.New("link already saved")
	// ErrSaveNotFound is returned when the save doesn't exist or belongs to another user.
	ErrSaveNotFound = errors.New("save not found")
	// ErrSaveOrder is returned when a reorder payload doesn't match the scope's active saves.
	ErrSaveOrder = errors.New("save order must list each item in this board exactly once")
)

const saveColumns = `id, url, source, added_from, folder_id, position, created_at, deleted_at`

// SaveRepo handles database access for tbl_saves.
type SaveRepo struct {
	db *sql.DB
}

// NewSaveRepo creates a new SaveRepo.
func NewSaveRepo(db *sql.DB) *SaveRepo {
	return &SaveRepo{db: db}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSave(row rowScanner) (models.Save, error) {
	var s models.Save
	var folderID sql.NullInt64
	var deletedAt sql.NullTime
	if err := row.Scan(&s.ID, &s.URL, &s.Source, &s.AddedFrom, &folderID, &s.Position, &s.CreatedAt, &deletedAt); err != nil {
		return s, err
	}
	if folderID.Valid {
		s.FolderID = &folderID.Int64
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		s.DeletedAt = &t
	}
	return s, nil
}

func (r *SaveRepo) list(ctx context.Context, where string, args ...any) ([]models.Save, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+saveColumns+` FROM tbl_saves WHERE `+where+` ORDER BY position ASC, id DESC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	saves := []models.Save{}
	for rows.Next() {
		s, err := scanSave(rows)
		if err != nil {
			return nil, err
		}
		saves = append(saves, s)
	}
	return saves, rows.Err()
}

// ListDump returns the user's active saves that aren't in a folder, in board order.
func (r *SaveRepo) ListDump(ctx context.Context, userID int64) ([]models.Save, error) {
	return r.list(ctx, `user_id = ? AND folder_id IS NULL AND deleted_at IS NULL`, userID)
}

// ListAll returns every active save of the user (Home and all folders), in board order.
func (r *SaveRepo) ListAll(ctx context.Context, userID int64) ([]models.Save, error) {
	return r.list(ctx, `user_id = ? AND deleted_at IS NULL`, userID)
}

// ListByFolder returns the active saves in one of the user's folders, in board order.
func (r *SaveRepo) ListByFolder(ctx context.Context, userID, folderID int64) ([]models.Save, error) {
	return r.list(ctx, `user_id = ? AND folder_id = ? AND deleted_at IS NULL`, userID, folderID)
}

// ListBin returns the user's soft-deleted saves (the Bin), newest deleted first.
func (r *SaveRepo) ListBin(ctx context.Context, userID int64) ([]models.Save, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+saveColumns+` FROM tbl_saves WHERE user_id = ? AND deleted_at IS NOT NULL
		 ORDER BY deleted_at DESC, id DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	saves := []models.Save{}
	for rows.Next() {
		s, err := scanSave(rows)
		if err != nil {
			return nil, err
		}
		saves = append(saves, s)
	}
	return saves, rows.Err()
}

// Get returns one of the user's saves (including binned). Returns ErrSaveNotFound if missing.
func (r *SaveRepo) Get(ctx context.Context, userID int64, id uint64) (*models.Save, error) {
	s, err := scanSave(r.db.QueryRowContext(ctx,
		`SELECT `+saveColumns+` FROM tbl_saves WHERE id = ? AND user_id = ?`, id, userID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSaveNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// Insert saves a link for the user (into folderID, or the Dump when nil) and returns the stored row.
// New items go to the front of that board. If the same URL is already in the Bin, it is restored into folderID instead.
// Returns ErrDuplicateSave if already saved actively, or ErrFolderNotFound if the folder isn't the user's.
func (r *SaveRepo) Insert(ctx context.Context, userID int64, url, source, addedFrom string, folderID *int64) (*models.Save, error) {
	if folderID != nil {
		if err := r.checkFolder(ctx, userID, *folderID); err != nil {
			return nil, err
		}
	}
	pos, err := r.frontPosition(ctx, userID, folderID)
	if err != nil {
		return nil, err
	}
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO tbl_saves (user_id, url, source, added_from, folder_id, position) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, url, source, addedFrom, nullableID(folderID), pos,
	)
	if err != nil {
		var se *sqlite.Error
		if errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			existing, getErr := r.getByURL(ctx, userID, url)
			if getErr != nil {
				return nil, ErrDuplicateSave
			}
			if existing.DeletedAt == nil {
				return nil, ErrDuplicateSave
			}
			return r.Restore(ctx, userID, existing.ID, folderID)
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, userID, uint64(id))
}

func (r *SaveRepo) getByURL(ctx context.Context, userID int64, url string) (*models.Save, error) {
	s, err := scanSave(r.db.QueryRowContext(ctx,
		`SELECT `+saveColumns+` FROM tbl_saves WHERE user_id = ? AND url = ?`, userID, url,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSaveNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// Move puts one of the user's active saves into folderID, or back into the Dump when folderID is nil.
// The save is placed at the front of the destination board.
func (r *SaveRepo) Move(ctx context.Context, userID int64, saveID uint64, folderID *int64) (*models.Save, error) {
	if folderID != nil {
		if err := r.checkFolder(ctx, userID, *folderID); err != nil {
			return nil, err
		}
	}
	pos, err := r.frontPosition(ctx, userID, folderID)
	if err != nil {
		return nil, err
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE tbl_saves SET folder_id = ?, position = ? WHERE id = ? AND user_id = ? AND deleted_at IS NULL`,
		nullableID(folderID), pos, saveID, userID,
	)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrSaveNotFound
	}
	return r.Get(ctx, userID, saveID)
}

// SoftDelete moves a save into the Bin (sets deleted_at). Downloaded files stay on disk until ClearBin.
func (r *SaveRepo) SoftDelete(ctx context.Context, userID int64, saveID uint64) (*models.Save, error) {
	// Clear folder_id so deleting a folder later doesn't CASCADE-delete binned saves.
	res, err := r.db.ExecContext(ctx,
		`UPDATE tbl_saves SET deleted_at = strftime('%Y-%m-%d %H:%M:%f', 'now'), folder_id = NULL
		 WHERE id = ? AND user_id = ? AND deleted_at IS NULL`,
		saveID, userID,
	)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrSaveNotFound
	}
	return r.Get(ctx, userID, saveID)
}

// Restore takes a save out of the Bin into folderID (nil = Dump), at the front of that board.
func (r *SaveRepo) Restore(ctx context.Context, userID int64, saveID uint64, folderID *int64) (*models.Save, error) {
	if folderID != nil {
		if err := r.checkFolder(ctx, userID, *folderID); err != nil {
			return nil, err
		}
	}
	pos, err := r.frontPosition(ctx, userID, folderID)
	if err != nil {
		return nil, err
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE tbl_saves SET deleted_at = NULL, folder_id = ?, position = ?
		 WHERE id = ? AND user_id = ? AND deleted_at IS NOT NULL`,
		nullableID(folderID), pos, saveID, userID,
	)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrSaveNotFound
	}
	return r.Get(ctx, userID, saveID)
}

// ReorderDump sets the Home board order. ids must list each of the user's Dump saves exactly once.
func (r *SaveRepo) ReorderDump(ctx context.Context, userID int64, ids []uint64) error {
	return r.reorder(ctx, userID, nil, ids)
}

// ReorderFolder sets a folder board's order. ids must list each active save in that folder exactly once.
func (r *SaveRepo) ReorderFolder(ctx context.Context, userID, folderID int64, ids []uint64) error {
	if err := r.checkFolder(ctx, userID, folderID); err != nil {
		return err
	}
	return r.reorder(ctx, userID, &folderID, ids)
}

func (r *SaveRepo) reorder(ctx context.Context, userID int64, folderID *int64, ids []uint64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var rows *sql.Rows
	if folderID == nil {
		rows, err = tx.QueryContext(ctx,
			`SELECT id FROM tbl_saves WHERE user_id = ? AND folder_id IS NULL AND deleted_at IS NULL`, userID)
	} else {
		rows, err = tx.QueryContext(ctx,
			`SELECT id FROM tbl_saves WHERE user_id = ? AND folder_id = ? AND deleted_at IS NULL`, userID, *folderID)
	}
	if err != nil {
		return err
	}
	owned := map[uint64]bool{}
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		owned[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if len(ids) != len(owned) {
		return ErrSaveOrder
	}
	seen := map[uint64]bool{}
	for _, id := range ids {
		if !owned[id] || seen[id] {
			return ErrSaveOrder
		}
		seen[id] = true
	}

	for i, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE tbl_saves SET position = ? WHERE id = ? AND user_id = ?`, i+1, id, userID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// frontPosition returns a position that sorts before every current item in the scope (new cards appear first).
func (r *SaveRepo) frontPosition(ctx context.Context, userID int64, folderID *int64) (int, error) {
	var min sql.NullInt64
	var err error
	if folderID == nil {
		err = r.db.QueryRowContext(ctx,
			`SELECT MIN(position) FROM tbl_saves WHERE user_id = ? AND folder_id IS NULL AND deleted_at IS NULL`,
			userID,
		).Scan(&min)
	} else {
		err = r.db.QueryRowContext(ctx,
			`SELECT MIN(position) FROM tbl_saves WHERE user_id = ? AND folder_id = ? AND deleted_at IS NULL`,
			userID, *folderID,
		).Scan(&min)
	}
	if err != nil {
		return 0, err
	}
	if !min.Valid {
		return 1, nil
	}
	return int(min.Int64) - 1, nil
}

// Delete permanently removes one of the user's saves (row only; caller removes files).
func (r *SaveRepo) Delete(ctx context.Context, userID int64, saveID uint64) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM tbl_saves WHERE id = ? AND user_id = ?`,
		saveID, userID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSaveNotFound
	}
	return nil
}

// ClearBin permanently deletes every binned save for the user. Returns the deleted rows (for file cleanup).
func (r *SaveRepo) ClearBin(ctx context.Context, userID int64) ([]models.Save, error) {
	saves, err := r.ListBin(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(saves) == 0 {
		return saves, nil
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM tbl_saves WHERE user_id = ? AND deleted_at IS NOT NULL`, userID); err != nil {
		return nil, err
	}
	return saves, nil
}

func (r *SaveRepo) checkFolder(ctx context.Context, userID, folderID int64) error {
	var exists int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tbl_folders WHERE id = ? AND user_id = ?`, folderID, userID,
	).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrFolderNotFound
	}
	return nil
}

func nullableID(id *int64) any {
	if id == nil {
		return nil
	}
	return *id
}
