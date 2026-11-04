package models

import "database/sql"

// User represents a row from tbl_users. The local app has a single user.
type User struct {
	ID           int64
	PrimaryEmail sql.NullString
	Name         sql.NullString
	CreatedAt    sql.NullTime
	UpdatedAt    sql.NullTime
}
