package repository

import (
	"context"
	"database/sql"

	"instaboard/internal/models"
)

// UserRepo handles database access for tbl_users.
type UserRepo struct {
	db *sql.DB
}

// NewUserRepo creates a new UserRepo.
func NewUserRepo(db *sql.DB) *UserRepo {
	return &UserRepo{db: db}
}

const userColumns = `id, primary_email, name, created_at, updated_at`

// FindByID returns the user with the given id, or nil if not found.
func (r *UserRepo) FindByID(ctx context.Context, id int64) (*models.User, error) {
	return r.findOne(ctx, `WHERE id = ?`, id)
}

// LocalUser returns the user flagged as the local app's user, or nil if none is flagged yet.
func (r *UserRepo) LocalUser(ctx context.Context) (*models.User, error) {
	return r.findOne(ctx, `WHERE is_local = 1 ORDER BY id`)
}

// First returns the earliest user (lowest id), or nil if there are none.
func (r *UserRepo) First(ctx context.Context) (*models.User, error) {
	return r.findOne(ctx, `ORDER BY id`)
}

// SetLocal flags the given user as the local app's user (and no one else).
func (r *UserRepo) SetLocal(ctx context.Context, userID int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE tbl_users SET is_local = (id = ?)`, userID)
	return err
}

func (r *UserRepo) findOne(ctx context.Context, clause string, args ...any) (*models.User, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM tbl_users `+clause+` LIMIT 1`, args...)
	var u models.User
	err := row.Scan(&u.ID, &u.PrimaryEmail, &u.Name, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// Create inserts a new user and sets u.ID.
func (r *UserRepo) Create(ctx context.Context, u *models.User) error {
	res, err := r.db.ExecContext(ctx, `INSERT INTO tbl_users (primary_email, name) VALUES (?, ?)`,
		nullStr(u.PrimaryEmail), nullStr(u.Name))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

// Update saves the user's email and name.
func (r *UserRepo) Update(ctx context.Context, u *models.User) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE tbl_users SET primary_email = ?, name = ?, updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')
		WHERE id = ?`,
		nullStr(u.PrimaryEmail), nullStr(u.Name), u.ID)
	return err
}

func nullStr(n sql.NullString) any {
	if n.Valid {
		return n.String
	}
	return nil
}
