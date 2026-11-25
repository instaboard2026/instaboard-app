package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"instaboard/internal/models"
)

// MaxSections is how many sidebar sections one user can have.
const MaxSections = 50

var (
	ErrSectionNotFound  = errors.New("section not found")
	ErrSectionNameTaken = errors.New("you already have a section with that name")
	ErrTooManySections  = fmt.Errorf("you can have up to %d sections", MaxSections)
	ErrFolderLayout     = errors.New("layout must list each of your folders and sections exactly once")
)

// FolderLayoutItem places one folder: its section (nil = ungrouped) in list order.
type FolderLayoutItem struct {
	ID        int64  `json:"id"`
	SectionID *int64 `json:"section_id"`
}

// ListSections returns the user's sidebar sections in order.
func (r *FolderRepo) ListSections(ctx context.Context, userID int64) ([]models.FolderSection, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, position FROM tbl_folder_sections WHERE user_id = ? ORDER BY position, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.FolderSection{}
	for rows.Next() {
		var s models.FolderSection
		if err := rows.Scan(&s.ID, &s.Name, &s.Position); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CreateSection adds a section at the bottom. An empty name becomes the lowest free "Section N".
func (r *FolderRepo) CreateSection(ctx context.Context, userID int64, name string) (*models.FolderSection, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var count, maxPos int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(MAX(position), 0) FROM tbl_folder_sections WHERE user_id = ?`, userID,
	).Scan(&count, &maxPos); err != nil {
		return nil, err
	}
	if count >= MaxSections {
		return nil, ErrTooManySections
	}
	if strings.TrimSpace(name) == "" {
		taken := map[string]bool{}
		rows, err := tx.QueryContext(ctx, `SELECT name FROM tbl_folder_sections WHERE user_id = ?`, userID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return nil, err
			}
			taken[strings.ToLower(n)] = true
		}
		rows.Close()
		for i := 1; ; i++ {
			name = "Section " + strconv.Itoa(i)
			if !taken[strings.ToLower(name)] {
				break
			}
		}
	} else if name, err = CleanFolderName(name); err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO tbl_folder_sections (user_id, name, position) VALUES (?, ?, ?)`, userID, name, maxPos+1)
	if isUniqueViolation(err) {
		return nil, ErrSectionNameTaken
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
	return &models.FolderSection{ID: id, Name: name, Position: maxPos + 1}, nil
}

// RenameSection changes a section's name.
func (r *FolderRepo) RenameSection(ctx context.Context, userID, id int64, name string) (*models.FolderSection, error) {
	name, err := CleanFolderName(name)
	if err != nil {
		return nil, err
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE tbl_folder_sections SET name = ?, updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')
		 WHERE id = ? AND user_id = ?`, name, id, userID)
	if isUniqueViolation(err) {
		return nil, ErrSectionNameTaken
	}
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrSectionNotFound
	}
	var s models.FolderSection
	err = r.db.QueryRowContext(ctx,
		`SELECT id, name, position FROM tbl_folder_sections WHERE id = ? AND user_id = ?`, id, userID,
	).Scan(&s.ID, &s.Name, &s.Position)
	return &s, err
}

// DeleteSection removes a section; its folders stay and become ungrouped.
func (r *FolderRepo) DeleteSection(ctx context.Context, userID, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE tbl_folders SET section_id = NULL WHERE section_id = ? AND user_id = ?`, id, userID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM tbl_folder_sections WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSectionNotFound
	}
	return tx.Commit()
}

// SetLayout saves the whole sidebar in one go: section order, and every folder's section and order.
func (r *FolderRepo) SetLayout(ctx context.Context, userID int64, sectionIDs []int64, folders []FolderLayoutItem) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	ownedSections, err := ownedIDs(ctx, tx, `SELECT id FROM tbl_folder_sections WHERE user_id = ?`, userID)
	if err != nil {
		return err
	}
	ownedFolders, err := ownedIDs(ctx, tx, `SELECT id FROM tbl_folders WHERE user_id = ?`, userID)
	if err != nil {
		return err
	}
	if len(sectionIDs) != len(ownedSections) || len(folders) != len(ownedFolders) {
		return ErrFolderLayout
	}
	seen := map[int64]bool{}
	for _, id := range sectionIDs {
		if !ownedSections[id] || seen[id] {
			return ErrFolderLayout
		}
		seen[id] = true
	}
	seen = map[int64]bool{}
	for _, f := range folders {
		if !ownedFolders[f.ID] || seen[f.ID] || (f.SectionID != nil && !ownedSections[*f.SectionID]) {
			return ErrFolderLayout
		}
		seen[f.ID] = true
	}

	for i, id := range sectionIDs {
		if _, err := tx.ExecContext(ctx,
			`UPDATE tbl_folder_sections SET position = ? WHERE id = ? AND user_id = ?`, i+1, id, userID); err != nil {
			return err
		}
	}
	for i, f := range folders {
		if _, err := tx.ExecContext(ctx,
			`UPDATE tbl_folders SET position = ?, section_id = ? WHERE id = ? AND user_id = ?`,
			i+1, f.SectionID, f.ID, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func ownedIDs(ctx context.Context, tx *sql.Tx, query string, userID int64) (map[int64]bool, error) {
	rows, err := tx.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
