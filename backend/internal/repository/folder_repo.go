package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"instaboard/internal/models"
)

const (
	// MaxFolders is how many folders one user can have.
	MaxFolders = 100
	// MaxFolderNameLength is the longest folder name allowed (in characters).
	MaxFolderNameLength = 50
)

var (
	ErrFolderNotFound    = errors.New("folder not found")
	ErrFolderNameTaken   = errors.New("you already have a folder with that name")
	ErrFolderNameInvalid = fmt.Errorf("folder names must be 1 to %d characters", MaxFolderNameLength)
	ErrTooManyFolders    = fmt.Errorf("you can have up to %d folders", MaxFolders)
	ErrFolderOrder       = errors.New("folder order must list each of your folders exactly once")
)

// FolderRepo handles database access for tbl_folders.
type FolderRepo struct {
	db *sql.DB
}

// NewFolderRepo creates a new FolderRepo.
func NewFolderRepo(db *sql.DB) *FolderRepo {
	return &FolderRepo{db: db}
}

// CleanFolderName trims a folder name and checks its length.
func CleanFolderName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || utf8.RuneCountInString(name) > MaxFolderNameLength {
		return "", ErrFolderNameInvalid
	}
	return name, nil
}

func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}

// List returns the user's folders in sidebar order, each with its number of saves.
func (r *FolderRepo) List(ctx context.Context, userID int64) ([]models.Folder, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT f.id, f.name, f.position, COUNT(s.id)
		 FROM tbl_folders f
		 LEFT JOIN tbl_saves s ON s.folder_id = f.id
		 WHERE f.user_id = ?
		 GROUP BY f.id
		 ORDER BY f.position, f.id`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	folders := []models.Folder{}
	for rows.Next() {
		var f models.Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.Position, &f.Count); err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

// Get returns one of the user's folders with its save count.
func (r *FolderRepo) Get(ctx context.Context, userID, id int64) (*models.Folder, error) {
	var f models.Folder
	err := r.db.QueryRowContext(ctx,
		`SELECT f.id, f.name, f.position, (SELECT COUNT(*) FROM tbl_saves s WHERE s.folder_id = f.id)
		 FROM tbl_folders f WHERE f.id = ? AND f.user_id = ?`,
		id, userID,
	).Scan(&f.ID, &f.Name, &f.Position, &f.Count)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFolderNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// Create adds a folder at the end of the user's list. An empty name becomes the lowest free "Folder N".
func (r *FolderRepo) Create(ctx context.Context, userID int64, name string) (*models.Folder, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var count, maxPos int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(MAX(position), 0) FROM tbl_folders WHERE user_id = ?`, userID,
	).Scan(&count, &maxPos); err != nil {
		return nil, err
	}
	if count >= MaxFolders {
		return nil, ErrTooManyFolders
	}

	if strings.TrimSpace(name) == "" {
		name, err = nextDefaultName(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
	} else if name, err = CleanFolderName(name); err != nil {
		return nil, err
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO tbl_folders (user_id, name, position) VALUES (?, ?, ?)`, userID, name, maxPos+1,
	)
	if isUniqueViolation(err) {
		return nil, ErrFolderNameTaken
	}
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &models.Folder{ID: id, Name: name, Position: maxPos + 1}, nil
}

// nextDefaultName returns the lowest "Folder N" (N >= 1) the user doesn't have yet.
func nextDefaultName(ctx context.Context, tx *sql.Tx, userID int64) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM tbl_folders WHERE user_id = ?`, userID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	taken := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return "", err
		}
		taken[strings.ToLower(n)] = true
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	for i := 1; ; i++ {
		name := fmt.Sprintf("Folder %d", i)
		if !taken[strings.ToLower(name)] {
			return name, nil
		}
	}
}

// Rename changes the name of one of the user's folders.
func (r *FolderRepo) Rename(ctx context.Context, userID, id int64, name string) (*models.Folder, error) {
	name, err := CleanFolderName(name)
	if err != nil {
		return nil, err
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE tbl_folders SET name = ?, updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')
		 WHERE id = ? AND user_id = ?`,
		name, id, userID,
	)
	if isUniqueViolation(err) {
		return nil, ErrFolderNameTaken
	}
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrFolderNotFound
	}
	return r.Get(ctx, userID, id)
}

// Delete removes one of the user's folders and the saves in it; returns how many saves were removed.
func (r *FolderRepo) Delete(ctx context.Context, userID, id int64) (deletedSaves int, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tbl_saves WHERE folder_id = ? AND user_id = ?`, id, userID,
	).Scan(&deletedSaves); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM tbl_folders WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrFolderNotFound
	}
	return deletedSaves, tx.Commit()
}

// Reorder sets the sidebar order. ids must contain each of the user's folders exactly once.
func (r *FolderRepo) Reorder(ctx context.Context, userID int64, ids []int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id FROM tbl_folders WHERE user_id = ?`, userID)
	if err != nil {
		return err
	}
	owned := map[int64]bool{}
	for rows.Next() {
		var id int64
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
		return ErrFolderOrder
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if !owned[id] || seen[id] {
			return ErrFolderOrder
		}
		seen[id] = true
	}

	for i, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE tbl_folders SET position = ? WHERE id = ? AND user_id = ?`, i+1, id, userID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}
