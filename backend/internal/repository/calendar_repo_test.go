package repository

import (
	"context"
	"errors"
	"testing"
)

func TestCalendarRepo(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	saves := NewSaveRepo(db)
	cal := NewCalendarRepo(db)
	u := createUser(t, db, "cal@example.com")
	other := createUser(t, db, "other-cal@example.com")

	s, err := saves.Insert(ctx, u.ID, "https://instagram.com/reel/cal", "instagram", "web", nil)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := saves.Insert(ctx, other.ID, "https://instagram.com/reel/theirs", "instagram", "web", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := cal.Schedule(ctx, u.ID, s.ID, "2026-11-04"); err != nil {
		t.Fatal(err)
	}
	if err := cal.Schedule(ctx, u.ID, s.ID, "2026-11-05"); err != nil {
		t.Fatal(err)
	}
	if err := cal.Schedule(ctx, u.ID, theirs.ID, "2026-11-04"); !errors.Is(err, ErrSaveNotFound) {
		t.Fatalf("schedule someone else's save err = %v, want ErrSaveNotFound", err)
	}
	if err := cal.Schedule(ctx, u.ID, s.ID, "2026-02-31"); !errors.Is(err, ErrInvalidDate) {
		t.Fatalf("bad date err = %v, want ErrInvalidDate", err)
	}

	schedule, notes, err := cal.List(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(schedule) != 1 || schedule[0].SaveID != s.ID || schedule[0].Date != "2026-11-05" {
		t.Fatalf("schedule after replace = %+v", schedule)
	}
	if len(notes) != 0 {
		t.Fatalf("notes = %+v, want none", notes)
	}

	if err := cal.SetNote(ctx, u.ID, "2026-11-04", "  shoot day  "); err != nil {
		t.Fatal(err)
	}
	if err := cal.SetNote(ctx, u.ID, "not-a-date", "x"); !errors.Is(err, ErrInvalidDate) {
		t.Fatalf("bad note date err = %v, want ErrInvalidDate", err)
	}
	_, notes, err = cal.List(ctx, u.ID)
	if err != nil || len(notes) != 1 || notes[0].Date != "2026-11-04" || notes[0].Body != "shoot day" {
		t.Fatalf("notes after set = %+v, %v", notes, err)
	}

	if err := cal.SetNote(ctx, u.ID, "2026-11-04", "   "); err != nil {
		t.Fatal(err)
	}
	_, notes, err = cal.List(ctx, u.ID)
	if err != nil || len(notes) != 0 {
		t.Fatalf("notes after clear = %+v, %v", notes, err)
	}

	if err := cal.Unschedule(ctx, u.ID, s.ID); err != nil {
		t.Fatal(err)
	}
	if err := cal.Unschedule(ctx, u.ID, s.ID); err != nil {
		t.Fatal(err)
	}
	schedule, _, err = cal.List(ctx, u.ID)
	if err != nil || len(schedule) != 0 {
		t.Fatalf("schedule after unschedule = %+v, %v", schedule, err)
	}

	if err := cal.Schedule(ctx, u.ID, s.ID, "2026-11-04"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM tbl_saves WHERE id = ?`, s.ID); err != nil {
		t.Fatal(err)
	}
	schedule, _, err = cal.List(ctx, u.ID)
	if err != nil || len(schedule) != 0 {
		t.Fatalf("schedule after save delete = %+v, %v", schedule, err)
	}
}
