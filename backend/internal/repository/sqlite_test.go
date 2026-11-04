package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"instaboard/internal/database"
	"instaboard/internal/models"
	"instaboard/migrations"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.NewSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := database.Migrate(context.Background(), db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	return db
}

func createUser(t *testing.T, db *sql.DB, email string) *models.User {
	t.Helper()
	u := &models.User{
		PrimaryEmail: sql.NullString{String: email, Valid: true},
		Name:         sql.NullString{String: "Test", Valid: true},
	}
	if err := NewUserRepo(db).Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	applied, err := database.Migrate(context.Background(), db, migrations.FS)
	if err != nil || len(applied) != 0 {
		t.Fatalf("second Migrate = %v, %v; want nothing applied", applied, err)
	}
}

func TestUserRepo(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	users := NewUserRepo(db)
	u := createUser(t, db, "a@example.com")

	createUser(t, db, "b@example.com")

	got, err := users.First(ctx)
	if err != nil || got == nil || got.ID != u.ID {
		t.Fatalf("First = %+v, %v; want user %d", got, err, u.ID)
	}
	if !got.CreatedAt.Valid || time.Since(got.CreatedAt.Time) > time.Minute || time.Since(got.CreatedAt.Time) < -time.Minute {
		t.Fatalf("created_at not parsed as recent UTC time: %+v", got.CreatedAt)
	}
	got.Name = sql.NullString{String: "Renamed", Valid: true}
	if err := users.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	if byID, _ := users.FindByID(ctx, u.ID); byID == nil || byID.Name.String != "Renamed" || byID.PrimaryEmail.String != "a@example.com" {
		t.Fatalf("FindByID after update = %+v", byID)
	}
}

func TestUserRepoFirstEmpty(t *testing.T) {
	got, err := NewUserRepo(openTestDB(t)).First(context.Background())
	if err != nil || got != nil {
		t.Fatalf("First on empty db = %+v, %v; want nil, nil", got, err)
	}
}

func TestUserRepoLocalFlag(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	users := NewUserRepo(db)
	a := createUser(t, db, "a@example.com")
	b := createUser(t, db, "b@example.com")

	if got, err := users.LocalUser(ctx); err != nil || got != nil {
		t.Fatalf("LocalUser before flagging = %+v, %v; want nil", got, err)
	}
	for _, want := range []int64{b.ID, a.ID} {
		if err := users.SetLocal(ctx, want); err != nil {
			t.Fatal(err)
		}
		if got, err := users.LocalUser(ctx); err != nil || got == nil || got.ID != want {
			t.Fatalf("LocalUser after SetLocal(%d) = %+v, %v", want, got, err)
		}
	}
}

func TestSaveRepo(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	saves := NewSaveRepo(db)
	u := createUser(t, db, "s@example.com")
	other := createUser(t, db, "o@example.com")

	s, err := saves.Insert(ctx, u.ID, "https://instagram.com/reel/abc", "instagram", "web", nil)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(s.CreatedAt) > time.Minute || time.Since(s.CreatedAt) < -time.Minute {
		t.Fatalf("created_at = %v, want now (UTC)", s.CreatedAt)
	}
	if _, err := saves.Insert(ctx, u.ID, "https://instagram.com/reel/abc", "instagram", "web", nil); !errors.Is(err, ErrDuplicateSave) {
		t.Fatalf("duplicate insert err = %v, want ErrDuplicateSave", err)
	}
	if _, err := saves.Insert(ctx, other.ID, "https://instagram.com/reel/abc", "instagram", "web", nil); err != nil {
		t.Fatalf("another user saving the same link should work: %v", err)
	}
	if _, err := saves.Get(ctx, other.ID, s.ID); !errors.Is(err, ErrSaveNotFound) {
		t.Fatalf("Get of someone else's save err = %v, want ErrSaveNotFound", err)
	}
	list, err := saves.ListDump(ctx, u.ID)
	if err != nil || len(list) != 1 || list[0].ID != s.ID {
		t.Fatalf("ListDump = %+v, %v", list, err)
	}
}

func TestSettingsRepo(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	settings := NewSettingsRepo(db)
	u := createUser(t, db, "set@example.com")

	if dir, err := settings.DownloadDir(ctx, u.ID); err != nil || dir != "" {
		t.Fatalf("default DownloadDir = %q, %v", dir, err)
	}
	for _, want := range []string{"/tmp/one", "/tmp/two", ""} {
		if err := settings.SetDownloadDir(ctx, u.ID, want); err != nil {
			t.Fatal(err)
		}
		if dir, _ := settings.DownloadDir(ctx, u.ID); dir != want {
			t.Fatalf("DownloadDir = %q, want %q", dir, want)
		}
	}
}
