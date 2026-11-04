package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFolderCreateAutoNamesAndUniqueness(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	folders := NewFolderRepo(db)
	u := createUser(t, db, "f@example.com")

	f1, err := folders.Create(ctx, u.ID, "")
	if err != nil || f1.Name != "Folder 1" || f1.Position != 1 {
		t.Fatalf("first auto folder = %+v, %v", f1, err)
	}
	f2, _ := folders.Create(ctx, u.ID, "  ")
	if f2.Name != "Folder 2" || f2.Position != 2 {
		t.Fatalf("second auto folder = %+v", f2)
	}
	if _, err := folders.Rename(ctx, u.ID, f1.ID, "Shirts"); err != nil {
		t.Fatal(err)
	}
	f3, _ := folders.Create(ctx, u.ID, "")
	if f3.Name != "Folder 1" {
		t.Fatalf("auto name should reuse the free 'Folder 1', got %q", f3.Name)
	}

	if _, err := folders.Create(ctx, u.ID, "shirts"); !errors.Is(err, ErrFolderNameTaken) {
		t.Fatalf("case-insensitive duplicate create err = %v", err)
	}
	if _, err := folders.Rename(ctx, u.ID, f2.ID, "SHIRTS"); !errors.Is(err, ErrFolderNameTaken) {
		t.Fatalf("case-insensitive duplicate rename err = %v", err)
	}
	if _, err := folders.Rename(ctx, u.ID, f2.ID, strings.Repeat("x", MaxFolderNameLength+1)); !errors.Is(err, ErrFolderNameInvalid) {
		t.Fatalf("too long name err = %v", err)
	}
	if got, _ := folders.Rename(ctx, u.ID, f2.ID, "  Summer   looks "); got == nil || got.Name != "Summer looks" {
		t.Fatalf("rename should collapse spaces, got %+v", got)
	}

	other := createUser(t, db, "other@example.com")
	if _, err := folders.Create(ctx, other.ID, "Shirts"); err != nil {
		t.Fatalf("another user can use the same name: %v", err)
	}
	if _, err := folders.Rename(ctx, other.ID, f1.ID, "Mine"); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("renaming someone else's folder err = %v", err)
	}
}

func TestFolderLimit(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	folders := NewFolderRepo(db)
	u := createUser(t, db, "limit@example.com")
	for i := 0; i < MaxFolders; i++ {
		if _, err := folders.Create(ctx, u.ID, ""); err != nil {
			t.Fatalf("create #%d: %v", i+1, err)
		}
	}
	if _, err := folders.Create(ctx, u.ID, ""); !errors.Is(err, ErrTooManyFolders) {
		t.Fatalf("over limit err = %v", err)
	}
}

func TestFolderReorder(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	folders := NewFolderRepo(db)
	u := createUser(t, db, "r@example.com")
	a, _ := folders.Create(ctx, u.ID, "A")
	b, _ := folders.Create(ctx, u.ID, "B")
	c, _ := folders.Create(ctx, u.ID, "C")

	if err := folders.Reorder(ctx, u.ID, []int64{c.ID, a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	list, _ := folders.List(ctx, u.ID)
	if len(list) != 3 || list[0].Name != "C" || list[1].Name != "A" || list[2].Name != "B" {
		t.Fatalf("order after reorder = %+v", list)
	}

	other := createUser(t, db, "r2@example.com")
	x, _ := folders.Create(ctx, other.ID, "X")
	for _, bad := range [][]int64{
		{a.ID, b.ID},           // missing one
		{a.ID, b.ID, b.ID},     // duplicate
		{a.ID, b.ID, x.ID},     // someone else's folder
		{a.ID, b.ID, c.ID, 99}, // unknown id
	} {
		if err := folders.Reorder(ctx, u.ID, bad); !errors.Is(err, ErrFolderOrder) {
			t.Errorf("Reorder(%v) err = %v, want ErrFolderOrder", bad, err)
		}
	}
}

func TestFolderSavesMoveAndDelete(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	folders := NewFolderRepo(db)
	saves := NewSaveRepo(db)
	u := createUser(t, db, "m@example.com")
	other := createUser(t, db, "m2@example.com")

	shirts, _ := folders.Create(ctx, u.ID, "Shirts")
	othersFolder, _ := folders.Create(ctx, other.ID, "Theirs")

	s1, _ := saves.Insert(ctx, u.ID, "https://instagram.com/reel/1", "instagram", "web", nil)
	s2, _ := saves.Insert(ctx, u.ID, "https://instagram.com/reel/2", "instagram", "web", &shirts.ID)
	if s1.FolderID != nil || s2.FolderID == nil || *s2.FolderID != shirts.ID {
		t.Fatalf("inserted folder ids: s1=%v s2=%v", s1.FolderID, s2.FolderID)
	}
	if _, err := saves.Insert(ctx, u.ID, "https://instagram.com/reel/3", "instagram", "web", &othersFolder.ID); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("insert into someone else's folder err = %v", err)
	}

	dump, _ := saves.ListDump(ctx, u.ID)
	if len(dump) != 1 || dump[0].ID != s1.ID {
		t.Fatalf("Dump should only hold unfiled saves: %+v", dump)
	}

	moved, err := saves.Move(ctx, u.ID, s1.ID, &shirts.ID)
	if err != nil || moved.FolderID == nil || *moved.FolderID != shirts.ID {
		t.Fatalf("Move into folder = %+v, %v", moved, err)
	}
	if dump, _ := saves.ListDump(ctx, u.ID); len(dump) != 0 {
		t.Fatalf("Dump after move = %+v", dump)
	}
	if list, _ := folders.List(ctx, u.ID); list[0].Count != 2 {
		t.Fatalf("folder count = %+v", list)
	}
	if _, err := saves.Move(ctx, u.ID, s1.ID, &othersFolder.ID); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("move into someone else's folder err = %v", err)
	}
	if _, err := saves.Move(ctx, other.ID, s1.ID, nil); !errors.Is(err, ErrSaveNotFound) {
		t.Fatalf("moving someone else's save err = %v", err)
	}
	back, err := saves.Move(ctx, u.ID, s1.ID, nil)
	if err != nil || back.FolderID != nil {
		t.Fatalf("Move back to Dump = %+v, %v", back, err)
	}

	n, err := folders.Delete(ctx, u.ID, shirts.ID)
	if err != nil || n != 1 {
		t.Fatalf("Delete = %d, %v; want 1 save removed", n, err)
	}
	if _, err := saves.Get(ctx, u.ID, s2.ID); !errors.Is(err, ErrSaveNotFound) {
		t.Fatalf("save in deleted folder should be gone, err = %v", err)
	}
	if _, err := saves.Get(ctx, u.ID, s1.ID); err != nil {
		t.Fatalf("Dump save should survive folder delete: %v", err)
	}
	if _, err := folders.Delete(ctx, other.ID, othersFolder.ID+1000); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("delete unknown folder err = %v", err)
	}
}
