package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"instaboard/internal/models"
)

var (
	ErrLocalMediaFolderNotFound  = errors.New("local media folder not found")
	ErrLocalMediaFolderNameTaken = errors.New("you already have a Local media folder with that name")
	ErrLocalMediaFolderOrder     = errors.New("folder order must list each of your Local media folders exactly once")
	ErrLocalMediaNotFound        = errors.New("local media item not found")
	ErrLocalMediaNameTaken      = errors.New("that file is already in Local media")
	ErrTooManyLocalMediaFolders  = fmt.Errorf("you can have up to %d Local media folders", MaxFolders)
)

// LocalMediaRepo handles tbl_local_media_folders and tbl_local_media.
type LocalMediaRepo struct {
	db *sql.DB
}

// NewLocalMediaRepo creates a LocalMediaRepo.
func NewLocalMediaRepo(db *sql.DB) *LocalMediaRepo {
	return &LocalMediaRepo{db: db}
}

// ListFolders returns Local media folders in sidebar order with item counts.
func (r *LocalMediaRepo) ListFolders(ctx context.Context, userID int64) ([]models.LocalMediaFolder, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT f.id, f.name, f.position, COUNT(m.id)
		 FROM tbl_local_media_folders f
		 LEFT JOIN tbl_local_media m ON m.folder_id = f.id
		 WHERE f.user_id = ?
		 GROUP BY f.id
		 ORDER BY f.position, f.id`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.LocalMediaFolder{}
	for rows.Next() {
		var f models.LocalMediaFolder
		if err := rows.Scan(&f.ID, &f.Name, &f.Position, &f.Count); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetFolder returns one Local media folder with its item count.
func (r *LocalMediaRepo) GetFolder(ctx context.Context, userID, id int64) (*models.LocalMediaFolder, error) {
	var f models.LocalMediaFolder
	err := r.db.QueryRowContext(ctx,
		`SELECT f.id, f.name, f.position,
		        (SELECT COUNT(*) FROM tbl_local_media m WHERE m.folder_id = f.id)
		 FROM tbl_local_media_folders f WHERE f.id = ? AND f.user_id = ?`,
		id, userID,
	).Scan(&f.ID, &f.Name, &f.Position, &f.Count)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLocalMediaFolderNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// CreateFolder adds a folder at the end. Empty name → "Folder N".
func (r *LocalMediaRepo) CreateFolder(ctx context.Context, userID int64, name string) (*models.LocalMediaFolder, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var count, maxPos int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(MAX(position), 0) FROM tbl_local_media_folders WHERE user_id = ?`, userID,
	).Scan(&count, &maxPos); err != nil {
		return nil, err
	}
	if count >= MaxFolders {
		return nil, ErrTooManyLocalMediaFolders
	}

	if strings.TrimSpace(name) == "" {
		name, err = nextLocalMediaDefaultName(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
	} else if name, err = CleanFolderName(name); err != nil {
		return nil, err
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO tbl_local_media_folders (user_id, name, position) VALUES (?, ?, ?)`,
		userID, name, maxPos+1,
	)
	if isUniqueViolation(err) {
		return nil, ErrLocalMediaFolderNameTaken
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
	return &models.LocalMediaFolder{ID: id, Name: name, Position: maxPos + 1}, nil
}

func nextLocalMediaDefaultName(ctx context.Context, tx *sql.Tx, userID int64) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM tbl_local_media_folders WHERE user_id = ?`, userID)
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

// RenameFolder changes a Local media folder name.
func (r *LocalMediaRepo) RenameFolder(ctx context.Context, userID, id int64, name string) (*models.LocalMediaFolder, error) {
	name, err := CleanFolderName(name)
	if err != nil {
		return nil, err
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE tbl_local_media_folders SET name = ?, updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')
		 WHERE id = ? AND user_id = ?`,
		name, id, userID,
	)
	if isUniqueViolation(err) {
		return nil, ErrLocalMediaFolderNameTaken
	}
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrLocalMediaFolderNotFound
	}
	return r.GetFolder(ctx, userID, id)
}

// DeleteFolder removes a folder; returns the media rows that were cascade-deleted (for disk cleanup).
func (r *LocalMediaRepo) DeleteFolder(ctx context.Context, userID, id int64) (items []models.LocalMedia, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx,
		`SELECT id, user_id, folder_id, original_name, stored_name, kind, byte_size,
		        source_rel_path, source_root, created_at
		 FROM tbl_local_media WHERE folder_id = ? AND user_id = ?`,
		id, userID,
	)
	if err != nil {
		return nil, err
	}
	items, err = scanLocalMediaRows(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	res, err := tx.ExecContext(ctx,
		`DELETE FROM tbl_local_media_folders WHERE id = ? AND user_id = ?`, id, userID,
	)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrLocalMediaFolderNotFound
	}
	return items, tx.Commit()
}

// ReorderFolders sets sidebar order; ids must list each folder once.
func (r *LocalMediaRepo) ReorderFolders(ctx context.Context, userID int64, ids []int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id FROM tbl_local_media_folders WHERE user_id = ?`, userID)
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
		return ErrLocalMediaFolderOrder
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if !owned[id] || seen[id] {
			return ErrLocalMediaFolderOrder
		}
		seen[id] = true
	}

	for i, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE tbl_local_media_folders SET position = ? WHERE id = ? AND user_id = ?`,
			i+1, id, userID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListItems returns media in a folder (newest first).
func (r *LocalMediaRepo) ListItems(ctx context.Context, userID, folderID int64) ([]models.LocalMedia, error) {
	if _, err := r.GetFolder(ctx, userID, folderID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, folder_id, original_name, stored_name, kind, byte_size,
		        source_rel_path, source_root, created_at
		 FROM tbl_local_media
		 WHERE user_id = ? AND folder_id = ?
		 ORDER BY created_at DESC, id DESC`,
		userID, folderID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLocalMediaRows(rows)
}

// CopiedBasenames returns the set of original_name values already in Local media.
func (r *LocalMediaRepo) CopiedBasenames(ctx context.Context, userID int64) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT original_name FROM tbl_local_media WHERE user_id = ?`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

// InsertItemParams are fields for a new Local media row (stored_name set after id known).
type InsertItemParams struct {
	UserID        int64
	FolderID      int64
	OriginalName  string
	Kind          string
	ByteSize      int64
	SourceRelPath string
	SourceRoot    string
}

// InsertItemPlaceholder inserts a row with a temporary stored_name, then updates to "{id}{ext}".
func (r *LocalMediaRepo) InsertItemPlaceholder(ctx context.Context, p InsertItemParams, ext string) (*models.LocalMedia, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var folderOwner int64
	err = tx.QueryRowContext(ctx,
		`SELECT user_id FROM tbl_local_media_folders WHERE id = ?`, p.FolderID,
	).Scan(&folderOwner)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLocalMediaFolderNotFound
	}
	if err != nil {
		return nil, err
	}
	if folderOwner != p.UserID {
		return nil, ErrLocalMediaFolderNotFound
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO tbl_local_media
		 (user_id, folder_id, original_name, stored_name, kind, byte_size, source_rel_path, source_root)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.UserID, p.FolderID, p.OriginalName, "pending"+ext, p.Kind, p.ByteSize, nullIfEmpty(p.SourceRelPath), nullIfEmpty(p.SourceRoot),
	)
	if isUniqueViolation(err) {
		return nil, ErrLocalMediaNameTaken
	}
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	stored := fmt.Sprintf("%d%s", id, ext)
	if _, err := tx.ExecContext(ctx,
		`UPDATE tbl_local_media SET stored_name = ? WHERE id = ?`, stored, id,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	item, err := r.GetItem(ctx, p.UserID, uint64(id))
	if err != nil {
		return nil, err
	}
	return item, nil
}

// MoveItems moves Local media rows into toFolderID. Files stay on disk (flat library).
// Returns how many rows changed folder.
func (r *LocalMediaRepo) MoveItems(ctx context.Context, userID, toFolderID int64, ids []uint64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if _, err := r.GetFolder(ctx, userID, toFolderID); err != nil {
		return 0, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	moved := 0
	for _, id := range ids {
		res, err := tx.ExecContext(ctx,
			`UPDATE tbl_local_media SET folder_id = ?
			 WHERE id = ? AND user_id = ? AND folder_id != ?`,
			toFolderID, id, userID, toFolderID,
		)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		moved += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return moved, nil
}

// DeleteItem removes one media row and returns it for disk cleanup.
func (r *LocalMediaRepo) DeleteItem(ctx context.Context, userID int64, id uint64) (*models.LocalMedia, error) {
	item, err := r.GetItem(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM tbl_local_media WHERE id = ? AND user_id = ?`, id, userID,
	)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrLocalMediaNotFound
	}
	return item, nil
}

// GetItem returns one Local media row owned by the user.
func (r *LocalMediaRepo) GetItem(ctx context.Context, userID int64, id uint64) (*models.LocalMedia, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, folder_id, original_name, stored_name, kind, byte_size,
		        source_rel_path, source_root, created_at
		 FROM tbl_local_media WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	item, err := scanLocalMedia(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLocalMediaNotFound
	}
	return item, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

type scanner interface {
	Scan(dest ...any) error
}

func scanLocalMedia(row scanner) (*models.LocalMedia, error) {
	var m models.LocalMedia
	var rel, root sql.NullString
	if err := row.Scan(
		&m.ID, &m.UserID, &m.FolderID, &m.OriginalName, &m.StoredName, &m.Kind, &m.ByteSize,
		&rel, &root, &m.CreatedAt,
	); err != nil {
		return nil, err
	}
	if rel.Valid {
		m.SourceRelPath = &rel.String
	}
	if root.Valid {
		m.SourceRoot = &root.String
	}
	return &m, nil
}

func scanLocalMediaRows(rows *sql.Rows) ([]models.LocalMedia, error) {
	out := []models.LocalMedia{}
	for rows.Next() {
		item, err := scanLocalMedia(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// UserIDs returns every user that still has Local media folders (for the one-time move to Folders).
func (r *LocalMediaRepo) UserIDs(ctx context.Context) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT user_id FROM tbl_local_media_folders`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
