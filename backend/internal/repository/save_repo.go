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
)

const saveColumns = `id, url, source, added_from, folder_id, created_at`

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
	if err := row.Scan(&s.ID, &s.URL, &s.Source, &s.AddedFrom, &folderID, &s.CreatedAt); err != nil {
		return s, err
	}
	if folderID.Valid {
		s.FolderID = &folderID.Int64
	}
	return s, nil
}

func (r *SaveRepo) list(ctx context.Context, where string, args ...any) ([]models.Save, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+saveColumns+` FROM tbl_saves WHERE `+where+` ORDER BY created_at DESC, id DESC`,
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

// ListDump returns the user's saves that aren't in a folder, newest first.
func (r *SaveRepo) ListDump(ctx context.Context, userID int64) ([]models.Save, error) {
	return r.list(ctx, `user_id = ? AND folder_id IS NULL`, userID)
}

// ListAll returns every save of the user (Home and all folders), newest first.
func (r *SaveRepo) ListAll(ctx context.Context, userID int64) ([]models.Save, error) {
	return r.list(ctx, `user_id = ?`, userID)
}

// ListByFolder returns the saves in one of the user's folders, newest first.
func (r *SaveRepo) ListByFolder(ctx context.Context, userID, folderID int64) ([]models.Save, error) {
	return r.list(ctx, `user_id = ? AND folder_id = ?`, userID, folderID)
}

// Get returns one of the user's saves. Returns ErrSaveNotFound if it doesn't exist or isn't theirs.
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
// Returns ErrDuplicateSave if already saved, or ErrFolderNotFound if the folder isn't the user's.
func (r *SaveRepo) Insert(ctx context.Context, userID int64, url, source, addedFrom string, folderID *int64) (*models.Save, error) {
	if folderID != nil {
		if err := r.checkFolder(ctx, userID, *folderID); err != nil {
			return nil, err
		}
	}
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO tbl_saves (user_id, url, source, added_from, folder_id) VALUES (?, ?, ?, ?, ?)`,
		userID, url, source, addedFrom, nullableID(folderID),
	)
	if err != nil {
		var se *sqlite.Error
		if errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return nil, ErrDuplicateSave
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, userID, uint64(id))
}

// Move puts one of the user's saves into folderID, or back into the Dump when folderID is nil.
func (r *SaveRepo) Move(ctx context.Context, userID int64, saveID uint64, folderID *int64) (*models.Save, error) {
	if folderID != nil {
		if err := r.checkFolder(ctx, userID, *folderID); err != nil {
			return nil, err
		}
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE tbl_saves SET folder_id = ? WHERE id = ? AND user_id = ?`,
		nullableID(folderID), saveID, userID,
	)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrSaveNotFound
	}
	return r.Get(ctx, userID, saveID)
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
